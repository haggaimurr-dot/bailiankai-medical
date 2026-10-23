package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrHandoffRecipientMissing = errors.New("handoff recipient has no active employee account")
var ErrHandoffNotOverdue = errors.New("handoff receipt was not overdue")

type HandoffReceiptFilter struct {
	ReceiptID string
	Status    string
	ToStaffID string
	Page      int
	PageSize  int
	Now       time.Time
}
type HandoffReceiptPage struct {
	Data         []model.FollowupHandoffReceipt
	Total        int64
	OverdueTotal int64
	TodoTotal    int64
	OverdueHours int
	Page         int
	PageSize     int
}

func (s *MySQL) HandoffSetting(ctx context.Context) (model.FollowupHandoffSetting, error) {
	var setting model.FollowupHandoffSetting
	err := s.db.WithContext(ctx).Where("id = 1").First(&setting).Error
	return setting, followupDBError(err)
}

func (s *MySQL) UpdateHandoffSetting(ctx context.Context, hours, version int, actorID string, now time.Time) (model.FollowupHandoffSetting, error) {
	var setting model.FollowupHandoffSetting
	if employeeScoped(ctx) {
		var total, allowed int64
		if err := s.db.WithContext(ctx).Model(&model.Clinic{}).Count(&total).Error; err != nil {
			return setting, err
		}
		if err := clinicScope(ctx, s.db.WithContext(ctx).Model(&model.Clinic{}), "id").Count(&allowed).Error; err != nil {
			return setting, err
		}
		if total == 0 || allowed != total {
			return setting, ErrForbidden
		}
	}
	if hours < 1 || hours > 720 || version < 1 {
		return setting, ErrCareState
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = 1").First(&setting).Error; err != nil {
			return followupDBError(err)
		}
		if setting.Version != version {
			return ErrConflict
		}
		if setting.OverdueHours == hours {
			return nil
		}
		setting.OverdueHours, setting.Version, setting.UpdatedBy, setting.UpdatedAt = hours, version+1, &actorID, now
		update := tx.Model(&model.FollowupHandoffSetting{}).Where("id = 1 AND version = ?", version).Updates(map[string]any{"overdue_hours": hours, "version": setting.Version, "updated_by": actorID, "updated_at": now})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
	return setting, err
}

func requireHandoffRecipient(tx *gorm.DB, staffID string) error {
	var count int64
	if err := tx.Model(&model.EmployeeAccount{}).Where("staff_id = ? AND active = ?", staffID, true).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrHandoffRecipientMissing
	}
	return nil
}
func supersedeHandoffReceipt(tx *gorm.DB, taskID string, now time.Time) error {
	return tx.Model(&model.FollowupHandoffReceipt{}).Where("task_id = ? AND status = ?", taskID, "pending").Updates(map[string]any{"status": "superseded", "superseded_at": now, "version": gorm.Expr("version + 1")}).Error
}
func createHandoffReceipt(tx *gorm.DB, taskID, fromStaffID, toStaffID, reason, actorID string, now time.Time) error {
	receiptID := randomID("handoff-")
	if err := tx.Create(&model.FollowupHandoffReceipt{ID: receiptID, TaskID: taskID, FromStaffID: fromStaffID, ToStaffID: toStaffID, Reason: reason, AssignedBy: actorID, CreatedAt: now, Status: "pending", Version: 1, FollowupStatus: "untracked", FollowupVersion: 1}).Error; err != nil {
		return err
	}
	return createHandoffNotification(tx, receiptID, toStaffID, now)
}

func receiptQuery(tx *gorm.DB) *gorm.DB {
	return tx.Model(&model.FollowupHandoffReceipt{}).
		Select("followup_handoff_receipts.*, appointments.contact_name AS patient_name, followup_tasks.title AS task_title, followup_tasks.due_at AS due_at, source.display_name AS from_name, target.display_name AS to_name, COALESCE(assigner.display_name, followup_handoff_receipts.assigned_by) AS assigned_by_name, COALESCE(confirmer.display_name, '') AS confirmed_by_name, (SELECT COUNT(*) FROM handoff_followup_events hfe WHERE hfe.receipt_id = followup_handoff_receipts.id) AS followup_count").
		Joins("JOIN followup_tasks ON followup_tasks.id = followup_handoff_receipts.task_id").
		Joins("JOIN appointments ON appointments.id = followup_tasks.appointment_id").
		Joins("JOIN followup_staff source ON source.id = followup_handoff_receipts.from_staff_id").
		Joins("JOIN followup_staff target ON target.id = followup_handoff_receipts.to_staff_id").
		Joins("LEFT JOIN employee_accounts assigner ON assigner.id = followup_handoff_receipts.assigned_by").
		Joins("LEFT JOIN employee_accounts confirmer ON confirmer.id = followup_handoff_receipts.confirmed_by")
}

func (s *MySQL) HandoffFollowupEvents(ctx context.Context, receiptID string) ([]model.HandoffFollowupEvent, error) {
	if _, err := s.HandoffReceipt(ctx, receiptID); err != nil {
		return nil, err
	}
	rows := make([]model.HandoffFollowupEvent, 0)
	err := s.db.WithContext(ctx).Model(&model.HandoffFollowupEvent{}).
		Select("handoff_followup_events.*, COALESCE(actor.display_name, handoff_followup_events.actor_id) AS actor_name").
		Joins("LEFT JOIN employee_accounts actor ON actor.id = handoff_followup_events.actor_id").
		Where("handoff_followup_events.receipt_id = ?", receiptID).
		Order("handoff_followup_events.created_at, handoff_followup_events.id").Find(&rows).Error
	return rows, err
}

func (s *MySQL) handoffFollowupEvent(ctx context.Context, id string) (model.HandoffFollowupEvent, error) {
	var row model.HandoffFollowupEvent
	err := s.db.WithContext(ctx).Model(&model.HandoffFollowupEvent{}).
		Select("handoff_followup_events.*, COALESCE(actor.display_name, handoff_followup_events.actor_id) AS actor_name").
		Joins("LEFT JOIN employee_accounts actor ON actor.id = handoff_followup_events.actor_id").
		Where("handoff_followup_events.id = ?", id).First(&row).Error
	return row, followupDBError(err)
}

func (s *MySQL) RecordHandoffFollowup(ctx context.Context, receiptID, requestKey, actionText, resultText, statusAfter, actorID string, followupVersion int, now time.Time) (model.HandoffFollowupEvent, error) {
	var event model.HandoffFollowupEvent
	if _, err := s.HandoffReceipt(ctx, receiptID); err != nil {
		return event, err
	}
	actionText, resultText = strings.TrimSpace(actionText), strings.TrimSpace(resultText)
	if receiptID == "" || requestKey == "" || actorID == "" || followupVersion < 1 || (statusAfter != "awaiting_reply" && statusAfter != "needs_action" && statusAfter != "resolved") || utf8.RuneCountInString(actionText) < 1 || utf8.RuneCountInString(actionText) > 500 || utf8.RuneCountInString(resultText) < 1 || utf8.RuneCountInString(resultText) > 1000 {
		return event, ErrCareState
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var receipt model.FollowupHandoffReceipt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", receiptID).First(&receipt).Error; err != nil {
			return followupDBError(err)
		}
		var existing model.HandoffFollowupEvent
		lookup := tx.Where("request_key = ?", requestKey).First(&existing).Error
		if lookup == nil {
			if existing.ReceiptID != receiptID || existing.ActionText != actionText || existing.ResultText != resultText || existing.StatusAfter != statusAfter || existing.ActorID != actorID {
				return ErrConflict
			}
			event = existing
			return nil
		}
		if !errors.Is(lookup, gorm.ErrRecordNotFound) {
			return lookup
		}
		if receipt.FollowupVersion != followupVersion {
			return ErrConflict
		}
		var setting model.FollowupHandoffSetting
		if err := tx.Where("id = 1").First(&setting).Error; err != nil {
			return followupDBError(err)
		}
		endedAt := now
		switch receipt.Status {
		case "confirmed":
			if receipt.ConfirmedAt == nil {
				return ErrCareState
			}
			endedAt = *receipt.ConfirmedAt
		case "superseded":
			if receipt.SupersededAt == nil {
				return ErrCareState
			}
			endedAt = *receipt.SupersededAt
		case "pending":
		default:
			return ErrCareState
		}
		var previous int64
		if err := tx.Model(&model.HandoffFollowupEvent{}).Where("receipt_id = ?", receiptID).Count(&previous).Error; err != nil {
			return err
		}
		if previous == 0 && !receipt.CreatedAt.Before(endedAt.Add(-time.Duration(setting.OverdueHours)*time.Hour)) {
			return ErrHandoffNotOverdue
		}
		event = model.HandoffFollowupEvent{ID: randomID("handoff-followup-"), ReceiptID: receiptID, RequestKey: requestKey, ActionText: actionText, ResultText: resultText, StatusAfter: statusAfter, ActorID: actorID, CreatedAt: now}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		update := tx.Model(&model.FollowupHandoffReceipt{}).Where("id = ? AND followup_version = ?", receiptID, followupVersion).Updates(map[string]any{"followup_status": statusAfter, "followup_version": followupVersion + 1})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		return event, err
	}
	return s.handoffFollowupEvent(ctx, event.ID)
}

func (s *MySQL) HandoffReceipts(ctx context.Context, filter HandoffReceiptFilter) (HandoffReceiptPage, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	result := HandoffReceiptPage{Data: make([]model.FollowupHandoffReceipt, 0), Page: filter.Page, PageSize: filter.PageSize}
	setting, err := s.HandoffSetting(ctx)
	if err != nil {
		return result, err
	}
	result.OverdueHours = setting.OverdueHours
	cutoff := filter.Now.Add(-time.Duration(setting.OverdueHours) * time.Hour)
	overdueQuery := s.taskClinicScope(ctx, s.db.WithContext(ctx).Model(&model.FollowupHandoffReceipt{}).Where("status = ? AND created_at < ?", "pending", cutoff), "followup_handoff_receipts.task_id")
	if filter.ToStaffID != "" {
		overdueQuery = overdueQuery.Where("to_staff_id = ?", filter.ToStaffID)
	}
	if staffID := clinicianStaff(ctx); staffID != "" {
		overdueQuery = overdueQuery.Where("to_staff_id = ?", staffID)
	}
	if err := overdueQuery.Count(&result.OverdueTotal).Error; err != nil {
		return result, err
	}
	todoQuery := s.taskClinicScope(ctx, s.db.WithContext(ctx).Model(&model.FollowupHandoffReceipt{}), "followup_handoff_receipts.task_id").Where("(followup_status IN ? OR (status = ? AND created_at < ? AND followup_status = ?))", []string{"awaiting_reply", "needs_action"}, "pending", cutoff, "untracked")
	if filter.ToStaffID != "" {
		todoQuery = todoQuery.Where("to_staff_id = ?", filter.ToStaffID)
	}
	if staffID := clinicianStaff(ctx); staffID != "" {
		todoQuery = todoQuery.Where("to_staff_id = ?", staffID)
	}
	if err := todoQuery.Count(&result.TodoTotal).Error; err != nil {
		return result, err
	}
	q := s.taskClinicScope(ctx, receiptQuery(s.db.WithContext(ctx)), "followup_handoff_receipts.task_id")
	if filter.ReceiptID != "" {
		q = q.Where("followup_handoff_receipts.id = ?", filter.ReceiptID)
	}
	if filter.Status == "todo" {
		q = q.Where("(followup_handoff_receipts.followup_status IN ? OR (followup_handoff_receipts.status = ? AND followup_handoff_receipts.created_at < ? AND followup_handoff_receipts.followup_status = ?))", []string{"awaiting_reply", "needs_action"}, "pending", cutoff, "untracked")
	} else if filter.Status == "overdue" {
		q = q.Where("followup_handoff_receipts.status = ? AND followup_handoff_receipts.created_at < ?", "pending", cutoff)
	} else if filter.Status != "" {
		q = q.Where("followup_handoff_receipts.status = ?", filter.Status)
	}
	if filter.ToStaffID != "" {
		q = q.Where("followup_handoff_receipts.to_staff_id = ?", filter.ToStaffID)
	}
	if staffID := clinicianStaff(ctx); staffID != "" {
		q = q.Where("followup_handoff_receipts.to_staff_id = ?", staffID)
	}
	if err := q.Count(&result.Total).Error; err != nil {
		return result, err
	}
	err = q.Order("CASE followup_handoff_receipts.status WHEN 'pending' THEN 0 ELSE 1 END, followup_handoff_receipts.created_at ASC, followup_handoff_receipts.id ASC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&result.Data).Error
	return result, err
}

func (s *MySQL) HandoffReceipt(ctx context.Context, id string) (model.FollowupHandoffReceipt, error) {
	var row model.FollowupHandoffReceipt
	q := s.taskClinicScope(ctx, receiptQuery(s.db.WithContext(ctx)), "followup_handoff_receipts.task_id").Where("followup_handoff_receipts.id = ?", id)
	if staffID := clinicianStaff(ctx); staffID != "" {
		q = q.Where("followup_handoff_receipts.to_staff_id = ?", staffID)
	}
	return row, followupDBError(q.First(&row).Error)
}

func (s *MySQL) ConfirmHandoffReceipt(ctx context.Context, id string, version int, now time.Time) (model.FollowupHandoffReceipt, error) {
	var initial model.FollowupHandoffReceipt
	if version < 1 {
		return initial, ErrCareState
	}
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&initial).Error; err != nil {
		return initial, followupDBError(err)
	}
	allowed, err := s.CanAccessTask(ctx, initial.TaskID)
	if err != nil {
		return initial, err
	}
	if !allowed {
		return initial, ErrForbidden
	}
	scope, ok := scopeFrom(ctx)
	if !ok || scope.StaffID == "" || scope.StaffID != initial.ToStaffID {
		return initial, ErrForbidden
	}
	actorID := employeeActor(ctx)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := lockFollowup(tx, initial.TaskID, "")
		if err != nil {
			return err
		}
		var row model.FollowupHandoffReceipt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&row).Error; err != nil {
			return followupDBError(err)
		}
		if row.ToStaffID != scope.StaffID {
			return ErrForbidden
		}
		if row.Status == "confirmed" && row.Version == version+1 && row.ConfirmedBy != nil && *row.ConfirmedBy == actorID {
			return nil
		}
		if row.Version != version {
			return ErrConflict
		}
		if row.Status != "pending" || task.AssigneeID != row.ToStaffID {
			return ErrCareState
		}
		update := tx.Model(&model.FollowupHandoffReceipt{}).Where("id = ? AND version = ? AND status = ?", id, version, "pending").Updates(map[string]any{"status": "confirmed", "confirmed_by": actorID, "confirmed_at": now, "version": version + 1})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrConflict
		}
		return followupEvent(tx, task.AppointmentID, task.ID, "clinician", actorID, "handoff-confirmed", map[string]string{"handoffId": id}, now)
	})
	if err != nil {
		return initial, err
	}
	return s.HandoffReceipt(ctx, id)
}
