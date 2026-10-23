package store

import (
	"context"
	"log/slog"
	"time"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
)

type HandoffNotificationPage struct {
	Data        []model.HandoffNotification `json:"data"`
	Total       int64                       `json:"total"`
	UnreadTotal int64                       `json:"unreadTotal"`
}

type HandoffNotificationRepository interface {
	HandoffNotifications(context.Context, int, int) (HandoffNotificationPage, error)
	ReadHandoffNotification(context.Context, string, time.Time) error
}

func createHandoffNotification(tx *gorm.DB, receiptID, staffID string, now time.Time) error {
	return tx.Exec(`INSERT INTO handoff_notifications (id, receipt_id, employee_id, kind, created_at)
 SELECT SHA2(CONCAT(?, ':', e.id, ':assigned'), 256), ?, e.id, 'assigned', ?
 FROM employee_accounts e WHERE e.staff_id = ? AND e.active = true`, receiptID, receiptID, now, staffID).Error
}

// Unique receipt/recipient/kind keys make repeated runs and multiple workers idempotent.
func (s *MySQL) EscalateHandoffs(ctx context.Context, now time.Time) error {
	return s.db.WithContext(ctx).Exec(`INSERT INTO handoff_notifications (id, receipt_id, employee_id, kind, created_at)
 SELECT SHA2(CONCAT(r.id, ':', e.id, ':escalated'), 256), r.id, e.id, 'escalated', ?
 FROM followup_handoff_receipts r
 JOIN followup_handoff_settings settings ON settings.id = 1
 JOIN followup_tasks task ON task.id = r.task_id
 JOIN appointments a ON a.id = task.appointment_id
 JOIN slots slot ON slot.id = a.slot_id
 JOIN employee_accounts e ON e.role = 'manager' AND e.active = true
 AND JSON_CONTAINS(e.clinic_ids, JSON_QUOTE(slot.clinic_id))
 WHERE r.status = 'pending' AND r.followup_status <> 'resolved'
 AND r.created_at < TIMESTAMPADD(HOUR, -settings.overdue_hours, ?)
 ON DUPLICATE KEY UPDATE id = handoff_notifications.id`, now, now).Error
}

func (s *MySQL) RunHandoffNotificationWorker(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if err := s.EscalateHandoffs(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
			slog.Error("handoff escalation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *MySQL) notificationScope(ctx context.Context) *gorm.DB {
	return s.taskClinicScope(ctx, s.db.WithContext(ctx).Model(&model.HandoffNotification{}).
		Joins("JOIN followup_handoff_receipts r ON r.id = handoff_notifications.receipt_id").
		Where("handoff_notifications.employee_id = ?", employeeActor(ctx)), "r.task_id")
}

func (s *MySQL) HandoffNotifications(ctx context.Context, page, size int) (HandoffNotificationPage, error) {
	result := HandoffNotificationPage{Data: make([]model.HandoffNotification, 0)}
	if page < 1 || size < 1 || size > 100 {
		return result, ErrCareState
	}
	if err := s.notificationScope(ctx).Count(&result.Total).Error; err != nil {
		return result, err
	}
	if err := s.notificationScope(ctx).Where("handoff_notifications.read_at IS NULL").Count(&result.UnreadTotal).Error; err != nil {
		return result, err
	}
	err := s.notificationScope(ctx).
		Select("handoff_notifications.*, task.title AS task_title, source.display_name AS from_name, target.display_name AS to_name, r.status AS receipt_status").
		Joins("JOIN followup_tasks task ON task.id = r.task_id").
		Joins("JOIN followup_staff source ON source.id = r.from_staff_id").
		Joins("JOIN followup_staff target ON target.id = r.to_staff_id").
		Order("handoff_notifications.created_at DESC, handoff_notifications.id DESC").Offset((page - 1) * size).Limit(size).Find(&result.Data).Error
	return result, err
}

func (s *MySQL) ReadHandoffNotification(ctx context.Context, id string, now time.Time) error {
	var row model.HandoffNotification
	if err := s.notificationScope(ctx).Select("handoff_notifications.*").Where("handoff_notifications.id = ?", id).First(&row).Error; err != nil {
		return followupDBError(err)
	}
	return s.db.WithContext(ctx).Model(&model.HandoffNotification{}).Where("id = ? AND employee_id = ? AND read_at IS NULL", id, employeeActor(ctx)).Update("read_at", now).Error
}
