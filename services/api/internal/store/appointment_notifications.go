package store

import (
	"context"
	"log/slog"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
)

// 新增-P1：超时未确认预约的自动升级提醒，镜像交接通知 worker 的做法。
//
// pending_soon：确认截止在未来 30 分钟内的待确认预约，给授权院区的可审核
// 员工（主管、客服）各发一条；pending_escalated：已因超时未确认而过期的
// 预约（过期事件 from_status=pending），只给授权主管发一条升级提醒。
// 通知 id 由预约 + 员工 + 类型哈希确定，重复扫表幂等。

const pendingSoonWindow = 30 * time.Minute

type AppointmentNotificationPage struct {
	Data        []model.AppointmentNotification `json:"data"`
	Total       int64                           `json:"total"`
	UnreadTotal int64                           `json:"unreadTotal"`
}

type AppointmentNotificationRepository interface {
	AppointmentNotifications(context.Context, int, int) (AppointmentNotificationPage, error)
	ReadAppointmentNotification(context.Context, string, time.Time) error
}

// SweepPendingAppointmentReminders creates pending_soon and pending_escalated
// notices for the current time. It returns how many new notices each kind
// created (re-runs are no-ops thanks to the deterministic ids).
func (s *MySQL) SweepPendingAppointmentReminders(ctx context.Context, now time.Time) (soon, escalated int64, err error) {
	db := s.db.WithContext(ctx)
	until := now.Add(pendingSoonWindow)
	soonResult := db.Exec(`INSERT INTO appointment_notifications (id, appointment_id, employee_id, kind, created_at)
	 SELECT SHA2(CONCAT(a.id, ':', e.id, ':pending_soon'), 256), a.id, e.id, 'pending_soon', ?
	 FROM appointments a
	 JOIN slots slot ON slot.id = a.slot_id
	 JOIN employee_accounts e ON e.active = true AND e.role IN ('manager', 'customer_service')
	   AND JSON_CONTAINS(e.clinic_ids, JSON_QUOTE(slot.clinic_id))
	 WHERE a.status = 'pending' AND a.confirmation_deadline > ? AND a.confirmation_deadline <= ?
	 ON DUPLICATE KEY UPDATE id = appointment_notifications.id`, now, now, until)
	if soonResult.Error != nil {
		return 0, 0, soonResult.Error
	}
	escalatedResult := db.Exec(`INSERT INTO appointment_notifications (id, appointment_id, employee_id, kind, created_at)
	 SELECT SHA2(CONCAT(a.id, ':', e.id, ':pending_escalated'), 256), a.id, e.id, 'pending_escalated', ?
	 FROM appointments a
	 JOIN slots slot ON slot.id = a.slot_id
	 JOIN appointment_events ev ON ev.appointment_id = a.id AND ev.action = 'expire' AND ev.from_status = 'pending'
	 JOIN employee_accounts e ON e.active = true AND e.role = 'manager'
	   AND JSON_CONTAINS(e.clinic_ids, JSON_QUOTE(slot.clinic_id))
	 WHERE a.status = 'expired'
	 ON DUPLICATE KEY UPDATE id = appointment_notifications.id`, now)
	if escalatedResult.Error != nil {
		return 0, 0, escalatedResult.Error
	}
	return soonResult.RowsAffected, escalatedResult.RowsAffected, nil
}

func (s *MySQL) RunPendingAppointmentReminderWorker(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if soon, escalated, err := s.SweepPendingAppointmentReminders(ctx, time.Now().In(database.Shanghai)); err != nil && ctx.Err() == nil {
			slog.Error("pending appointment reminders failed", "error", err)
		} else if soon+escalated > 0 {
			slog.Info("pending appointment reminders created", "soon", soon, "escalated", escalated)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *MySQL) appointmentNotificationScope(ctx context.Context) *gorm.DB {
	return clinicScope(ctx, s.db.WithContext(ctx).Model(&model.AppointmentNotification{}).
		Joins("JOIN appointments a ON a.id = appointment_notifications.appointment_id").
		Joins("JOIN slots slot ON slot.id = a.slot_id").
		Where("appointment_notifications.employee_id = ?", employeeActor(ctx)), "slot.clinic_id")
}

func (s *MySQL) AppointmentNotifications(ctx context.Context, page, size int) (AppointmentNotificationPage, error) {
	result := AppointmentNotificationPage{Data: make([]model.AppointmentNotification, 0)}
	if page < 1 || size < 1 || size > 100 {
		return result, ErrCareState
	}
	if err := s.appointmentNotificationScope(ctx).Count(&result.Total).Error; err != nil {
		return result, err
	}
	if err := s.appointmentNotificationScope(ctx).Where("appointment_notifications.read_at IS NULL").Count(&result.UnreadTotal).Error; err != nil {
		return result, err
	}
	err := s.appointmentNotificationScope(ctx).
		Select("appointment_notifications.*, COALESCE(a.doctor_name_snapshot, doctors.name) AS doctor_name, COALESCE(a.clinic_name_snapshot, clinics.name) AS clinic_name, slot.starts_at AS starts_at, a.contact_name AS contact_name, a.status AS status").
		Joins("JOIN doctors ON doctors.id = slot.doctor_id").
		Joins("JOIN clinics ON clinics.id = slot.clinic_id").
		Order("appointment_notifications.created_at DESC, appointment_notifications.id DESC").Offset((page - 1) * size).Limit(size).Find(&result.Data).Error
	return result, err
}

func (s *MySQL) ReadAppointmentNotification(ctx context.Context, id string, now time.Time) error {
	var row model.AppointmentNotification
	if err := s.appointmentNotificationScope(ctx).Select("appointment_notifications.*").Where("appointment_notifications.id = ?", id).First(&row).Error; err != nil {
		return followupDBError(err)
	}
	return s.db.WithContext(ctx).Model(&model.AppointmentNotification{}).Where("id = ? AND employee_id = ? AND read_at IS NULL", id, employeeActor(ctx)).Update("read_at", now).Error
}
