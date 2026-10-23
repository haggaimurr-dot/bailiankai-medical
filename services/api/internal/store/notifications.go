package store

import (
	"context"
	"errors"
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"log/slog"
	"time"
)

type MessageSender interface {
	SendReminder(context.Context, string, model.MessageTemplate, time.Time) (string, string)
}
type NotificationRepository interface {
	MessageTemplates(context.Context, bool) ([]model.MessageTemplate, error)
	SaveMessageTemplate(context.Context, model.MessageTemplate) (model.MessageTemplate, error)
	MessageConsents(context.Context, string) ([]model.MessageConsent, error)
	SetMessageConsent(context.Context, string, string, string, string, time.Time) error
	Notifications(context.Context, string, int, int) ([]model.Notification, int64, error)
	NotificationAttempts(context.Context, string) ([]model.NotificationAttempt, error)
	RetryNotification(context.Context, string, time.Time) error
}

func (s *MySQL) MessageTemplates(ctx context.Context, public bool) ([]model.MessageTemplate, error) {
	rows := make([]model.MessageTemplate, 0)
	q := s.db.WithContext(ctx)
	if public {
		q = q.Where("enabled = ?", true)
	} else {
		q = clinicScope(ctx, q, "clinic_id")
	}
	err := q.Order("clinic_id, kind").Find(&rows).Error
	return rows, err
}
func (s *MySQL) SaveMessageTemplate(ctx context.Context, in model.MessageTemplate) (model.MessageTemplate, error) {
	scope, ok := scopeFrom(ctx)
	if !ok || scope.Role != "manager" {
		return in, ErrForbidden
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var clinic model.Clinic
		if err := clinicScope(ctx, tx, "id").First(&clinic, "id = ?", in.ClinicID).Error; err != nil {
			return followupDBError(err)
		}
		var old model.MessageTemplate
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "clinic_id = ? AND kind = ?", in.ClinicID, in.Kind).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if in.Version != 0 {
				return ErrConflict
			}
			in.ID = randomID("template-")
			in.Version = 1
			return tx.Create(&in).Error
		}
		if err != nil {
			return err
		}
		if old.Version != in.Version {
			return ErrConflict
		}
		in.ID = old.ID
		in.Version++
		return tx.Save(&in).Error
	})
	return in, err
}
func (s *MySQL) MessageConsents(ctx context.Context, patient string) ([]model.MessageConsent, error) {
	rows := make([]model.MessageConsent, 0)
	err := s.db.WithContext(ctx).Where("patient_id = ?", patient).Find(&rows).Error
	return rows, err
}
func (s *MySQL) SetMessageConsent(ctx context.Context, patient, template, status, key string, now time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Patient lock serializes first-time consent and idempotency checks.
		var p model.Patient
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, "id = ?", patient).Error; err != nil {
			return err
		}
		var event model.MessageConsentEvent
		err := tx.First(&event, "patient_id = ? AND request_key = ?", patient, key).Error
		if err == nil {
			if event.TemplateID != template || event.Status != status {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var count int64
		if err := tx.Model(&model.MessageTemplate{}).Where("template_id = ? AND enabled = ?", template, true).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 && status != "revoked" {
			return ErrNotFound
		}
		row := model.MessageConsent{PatientID: patient, TemplateID: template, Status: status, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "patient_id"}, {Name: "template_id"}}, DoUpdates: clause.AssignmentColumns([]string{"status", "updated_at"})}).Create(&row).Error; err != nil {
			return err
		}
		return tx.Create(&model.MessageConsentEvent{ID: randomID("consent-event-"), PatientID: patient, TemplateID: template, RequestKey: key, Status: status, CreatedAt: now}).Error
	})
}
func (s *MySQL) Notifications(ctx context.Context, status string, page, size int) ([]model.Notification, int64, error) {
	rows := make([]model.Notification, 0)
	q := clinicScope(ctx, s.db.WithContext(ctx).Model(&model.Notification{}), "clinic_id")
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return rows, 0, err
	}
	err := q.Order("due_at DESC, id").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}
func (s *MySQL) NotificationAttempts(ctx context.Context, id string) ([]model.NotificationAttempt, error) {
	var row model.Notification
	if err := clinicScope(ctx, s.db.WithContext(ctx), "clinic_id").First(&row, "id = ?", id).Error; err != nil {
		return nil, followupDBError(err)
	}
	rows := make([]model.NotificationAttempt, 0)
	err := s.db.WithContext(ctx).Where("notification_id = ?", id).Order("created_at, id").Find(&rows).Error
	return rows, err
}
func (s *MySQL) RetryNotification(ctx context.Context, id string, now time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.Notification
		if err := clinicScope(ctx, tx, "clinic_id").Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
			return followupDBError(err)
		}
		if row.Status != "blocked" || !row.DueAt.After(now) || row.Attempts >= 5 {
			return ErrCareState
		}
		row.Status = "queued"
		row.SendAt = now
		row.UpdatedAt = now
		return tx.Save(&row).Error
	})
}

// Schedule scans persisted business records; unique source/date keys make restart and concurrent scheduling safe.
func (s *MySQL) ScheduleReminders(ctx context.Context, now time.Time) error {
	var templates []model.MessageTemplate
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&templates).Error; err != nil {
		return err
	}
	for _, t := range templates {
		var sources []struct {
			ID, PatientID string
			DueAt         time.Time
		}
		q := s.db.WithContext(ctx)
		if t.Kind == "revisit" {
			q = q.Table("consultations").Select("id, patient_id, next_visit_at AS due_at").Where("clinic_id = ? AND next_visit_at > ?", t.ClinicID, now)
		} else {
			q = q.Table("followup_tasks f").Select("f.id, f.patient_id, f.due_at").Joins("JOIN appointments a ON a.id = f.appointment_id JOIN slots sl ON sl.id = a.slot_id").Where("sl.clinic_id = ? AND f.status = ? AND f.due_at > ?", t.ClinicID, "pending", now)
		}
		// Bounded horizon avoids scanning all future visits on every tick.
		if t.Kind == "revisit" {
			q = q.Where("next_visit_at <= ?", now.Add(time.Duration(t.LeadMinutes)*time.Minute+time.Minute))
		} else {
			q = q.Where("f.due_at <= ?", now.Add(time.Duration(t.LeadMinutes)*time.Minute+time.Minute))
		}
		if err := q.Find(&sources).Error; err != nil {
			return err
		}
		for _, src := range sources {
			row := model.Notification{ID: randomID("notification-"), ClinicID: t.ClinicID, PatientID: src.PatientID, TemplateID: t.ID, Kind: t.Kind, SourceID: src.ID, DueAt: src.DueAt, SendAt: src.DueAt.Add(-time.Duration(t.LeadMinutes) * time.Minute), Status: "queued", UpdatedAt: now}
			if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
func reminderOutcome(code string, attempt int) (string, time.Duration) {
	if code == "0" {
		return "sent", 0
	}
	if code == "unknown" {
		return "unknown", 0
	}
	if (code == "-1" || code == "45009" || code == "token_unavailable") && attempt < 5 {
		return "retry", time.Minute * time.Duration(1<<uint(attempt-1))
	}
	return "failed", 0
}
func (s *MySQL) DispatchReminder(ctx context.Context, id string, sender MessageSender, now time.Time) error {
	if sender == nil {
		return nil
	}
	var row model.Notification
	var t model.MessageTemplate
	var p model.Patient
	var ready bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if (row.Status != "queued" && row.Status != "retry") || row.SendAt.After(now) {
			return nil
		}
		if err := tx.First(&t, "id = ?", row.TemplateID).Error; err != nil {
			return err
		}
		// Lock order matches patient consent writes; withdrawal waits for an already started send.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, "id = ?", row.PatientID).Error; err != nil {
			return err
		}
		if !t.Enabled || !row.DueAt.After(now) {
			row.Status = "cancelled"
			row.LastCode = "expired_or_disabled"
		} else {
			var active int64
			if row.Kind == "followup" {
				if err := tx.Model(&model.FollowupTask{}).Where("id = ? AND due_at = ? AND status = ?", row.SourceID, row.DueAt, "pending").Count(&active).Error; err != nil {
					return err
				}
			} else {
				if err := tx.Model(&model.Consultation{}).Where("id = ? AND next_visit_at = ?", row.SourceID, row.DueAt).Count(&active).Error; err != nil {
					return err
				}
			}
			var consent model.MessageConsent
			e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&consent, "patient_id = ? AND template_id = ?", row.PatientID, t.TemplateID).Error
			if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			if active == 0 {
				row.Status = "cancelled"
				row.LastCode = "source_changed"
			} else if p.WechatOpenID == nil || p.WechatAppID == nil || e != nil || (row.Status != "retry" && consent.Status != "accepted") || (row.Status == "retry" && consent.Status != "consumed" && consent.Status != "accepted") {
				row.Status = "blocked"
				row.LastCode = "authorization_required"
			} else {
				consent.Status = "consumed"
				consent.UpdatedAt = now
				if err := tx.Save(&consent).Error; err != nil {
					return err
				}
				row.Status = "sending"
				row.Attempts++
				ready = true
			}
		}
		row.UpdatedAt = now
		return tx.Save(&row).Error
	})
	if err != nil || !ready {
		return err
	}
	// Persist claim before the external call. A crash leaves 'sending', recovered as unknown without resending.
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if row.Status != "sending" {
			return nil
		}
		var patient model.Patient
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&patient, "id = ?", row.PatientID).Error; err != nil {
			return err
		}
		var consent model.MessageConsent
		if err := tx.First(&consent, "patient_id = ? AND template_id = ?", row.PatientID, t.TemplateID).Error; err != nil {
			return err
		}
		code := "withdrawn"
		status := "cancelled"
		if consent.Status != "revoked" && consent.Status != "rejected" {
			code, _ = sender.SendReminder(ctx, *p.WechatOpenID, t, row.DueAt)
			var delay time.Duration
			status, delay = reminderOutcome(code, row.Attempts)
			row.SendAt = now.Add(delay)
		}
		row.Status = status
		row.LastCode = code
		row.UpdatedAt = now
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return tx.Create(&model.NotificationAttempt{ID: randomID("send-"), NotificationID: row.ID, Status: status, Code: code, CreatedAt: now}).Error
	})
}
func (s *MySQL) RunNotificationWorker(ctx context.Context, sender MessageSender) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	run := func() {
		work, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		now := time.Now().In(database.Shanghai)
		err := s.db.WithContext(work).Transaction(func(tx *gorm.DB) error {
			var stale []model.Notification
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status = ? AND updated_at < ?", "sending", now.Add(-2*time.Minute)).Find(&stale).Error; err != nil {
				return err
			}
			for _, n := range stale {
				n.Status = "unknown"
				n.LastCode = "worker_interrupted"
				n.UpdatedAt = now
				if err := tx.Save(&n).Error; err != nil {
					return err
				}
				if err := tx.Create(&model.NotificationAttempt{ID: randomID("send-"), NotificationID: n.ID, Status: n.Status, Code: n.LastCode, CreatedAt: now}).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err == nil {
			err = s.ScheduleReminders(work, now)
		}
		if err == nil && sender != nil {
			var rows []model.Notification
			err = s.db.WithContext(work).Where("status IN ? AND send_at <= ?", []string{"queued", "retry"}, now).Order("send_at, id").Limit(20).Find(&rows).Error
			for _, n := range rows {
				if err != nil {
					break
				}
				err = s.DispatchReminder(work, n.ID, sender, now)
			}
		}
		if err != nil && ctx.Err() == nil {
			slog.Error("notification worker failed", "error", err)
		}
	}
	run()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			run()
		}
	}
}
