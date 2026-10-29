package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func validBatchID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// ============ 行 103：方案模板库（共享模板） ============

// CarePlanTemplate 扩展字段见 care_templates.go；此处复用并增加共享字段。
// GORM 按列名映射新增列 is_shared / clinic_id。
type CarePlanTemplateShared struct {
	CarePlanTemplate
	IsShared bool    `json:"isShared" gorm:"column:is_shared"`
	ClinicID *string `json:"clinicId,omitempty" gorm:"column:clinic_id"`
	OwnerName string `json:"ownerName,omitempty" gorm:"->;-:migration"`
}

func (CarePlanTemplateShared) TableName() string { return "care_plan_templates" }

// ListCareTemplateLibrary 返回个人模板 + 本院区共享模板。
func (s *MySQL) ListCareTemplateLibrary(ctx context.Context, ownerID string, clinicIDs []string) ([]CarePlanTemplateShared, error) {
	rows := []CarePlanTemplateShared{}
	q := s.db.WithContext(ctx).Table("care_plan_templates t").
		Select("t.*, COALESCE(e.display_name, t.owner_id) AS owner_name").
		Joins("LEFT JOIN employee_accounts e ON e.id = t.owner_id").
		Where("t.owner_id = ?", ownerID)
	if len(clinicIDs) > 0 {
		q = q.Or("t.is_shared = 1 AND t.clinic_id IN ?", clinicIDs)
	}
	err := q.Order("t.is_shared DESC, t.created_at DESC, t.id DESC").Find(&rows).Error
	return rows, err
}

// ShareCareTemplate 将个人模板发布为院区共享模板（仅模板所有者可操作）。
func (s *MySQL) ShareCareTemplate(ctx context.Context, templateID, ownerID, clinicID string, now time.Time) (CarePlanTemplateShared, error) {
	var row CarePlanTemplateShared
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND owner_id = ?", templateID, ownerID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		// 校验院区在员工可见范围内
		if employeeScoped(ctx) {
			allowed := false
			var emp model.EmployeeAccount
			if err := tx.Where("id = ?", ownerID).First(&emp).Error; err != nil {
				return err
			}
			for _, id := range emp.ClinicIDs {
				if id == clinicID {
					allowed = true
					break
				}
			}
			if !allowed {
				return ErrForbidden
			}
		}
		if err := tx.Model(&CarePlanTemplate{}).Where("id = ?", templateID).
			Updates(map[string]any{"is_shared": 1, "clinic_id": clinicID}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", templateID).First(&row).Error
	})
	return row, err
}

// ApplyCareTemplate 用模板内容为指定预约新建/覆盖方案草稿。
func (s *MySQL) ApplyCareTemplate(ctx context.Context, templateID, appointmentID string, expectedVersion int, actorEmployeeID string, clinicIDs []string, now time.Time) (model.CarePlan, error) {
	var tpl CarePlanTemplateShared
	visible := false
	rows, err := s.ListCareTemplateLibrary(ctx, actorEmployeeID, clinicIDs)
	if err != nil {
		return model.CarePlan{}, err
	}
	for _, r := range rows {
		if r.ID == templateID {
			tpl = r
			visible = true
			break
		}
	}
	if !visible {
		return model.CarePlan{}, ErrNotFound
	}
	var content CarePlanContent
	if err := json.Unmarshal([]byte(tpl.ContentJSON), &content); err != nil {
		return model.CarePlan{}, err
	}
	withActor := WithEmployeeActor(ctx, actorEmployeeID)
	return s.SaveCarePlanDraft(withActor, appointmentID, expectedVersion, content, now)
}

// ============ 行 104：用药/护理提醒打卡 ============

type CareReminder struct {
	ID           string     `json:"id" gorm:"primaryKey;size:64"`
	PatientID    string     `json:"patientId"`
	ClinicID     string     `json:"clinicId"`
	Kind         string     `json:"kind"`
	Title        string     `json:"title"`
	Detail       string     `json:"detail"`
	ScheduleJSON string     `json:"scheduleJson"`
	StartDate    time.Time  `json:"startDate" gorm:"type:date"`
	EndDate      *time.Time `json:"endDate,omitempty" gorm:"type:date"`
	CreatedBy    string     `json:"-"`
	CreatedAt    time.Time  `json:"createdAt"`
	Active       bool       `json:"active"`
}

func (CareReminder) TableName() string { return "care_reminders" }

type ReminderCheckin struct {
	ID          string    `json:"id" gorm:"primaryKey;size:64"`
	ReminderID  string    `json:"reminderId"`
	PatientID   string    `json:"patientId"`
	CheckinDate time.Time `json:"checkinDate" gorm:"type:date"`
	Done        bool      `json:"done"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (ReminderCheckin) TableName() string { return "reminder_checkins" }

func validReminderKind(kind string) bool { return kind == "medication" || kind == "care" }

func (s *MySQL) CreateCareReminder(ctx context.Context, patientID, clinicID, kind, title, detail, scheduleJSON string, startDate time.Time, endDate *time.Time, createdBy string, now time.Time) (CareReminder, error) {
	if !validReminderKind(kind) {
		return CareReminder{}, ErrCareState
	}
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 120 {
		return CareReminder{}, ErrCareState
	}
	if scheduleJSON == "" {
		scheduleJSON = "{}"
	}
	var sched map[string]any
	if err := json.Unmarshal([]byte(scheduleJSON), &sched); err != nil {
		return CareReminder{}, ErrCareState
	}
	row := CareReminder{
		ID: randomID("remind-"), PatientID: patientID, ClinicID: clinicID, Kind: kind,
		Title: title, Detail: detail, ScheduleJSON: scheduleJSON,
		StartDate: startDate, EndDate: endDate, CreatedBy: createdBy, CreatedAt: now, Active: true,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return CareReminder{}, err
	}
	return row, nil
}

func (s *MySQL) ListCareReminders(ctx context.Context, patientID string) ([]CareReminder, error) {
	rows := []CareReminder{}
	err := s.db.WithContext(ctx).Where("patient_id = ? AND active = 1", patientID).
		Order("start_date DESC, id DESC").Find(&rows).Error
	return rows, err
}

// CheckinReminder 打卡（同一天重复打卡幂等覆盖）。
func (s *MySQL) CheckinReminder(ctx context.Context, reminderID, patientID string, date time.Time, now time.Time) error {
	day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rem CareReminder
		if err := tx.Where("id = ? AND patient_id = ? AND active = 1", reminderID, patientID).First(&rem).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		var existing ReminderCheckin
		err := tx.Where("reminder_id = ? AND checkin_date = ?", reminderID, day).First(&existing).Error
		if err == nil {
			return tx.Model(&existing).Updates(map[string]any{"done": 1}).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(&ReminderCheckin{ID: randomID("checkin-"), ReminderID: reminderID, PatientID: patientID, CheckinDate: day, Done: true, CreatedAt: now}).Error
	})
}

func (s *MySQL) ReminderCheckins(ctx context.Context, reminderID, patientID string, limit int) ([]ReminderCheckin, error) {
	rows := []ReminderCheckin{}
	err := s.db.WithContext(ctx).Where("reminder_id = ? AND patient_id = ?", reminderID, patientID).
		Order("checkin_date DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// ============ 行 105：交接进度看板 ============

type HandoverBatch struct {
	BatchID   string    `json:"batchId"`
	Reason    string    `json:"reason"`
	FromStaff string    `json:"fromStaffId"`
	ToStaff   string    `json:"toStaffId"`
	FromName  string    `json:"fromName"`
	ToName    string    `json:"toName"`
	Total     int       `json:"total"`
	Confirmed int       `json:"confirmed"`
	Pending   int       `json:"pending"`
	CreatedAt time.Time `json:"createdAt"`
	AssignedBy string   `json:"assignedBy"`
}

// CreateHandoverBatch 发起一次含多条任务的交接（同一批次号）。
func (s *MySQL) CreateHandoverBatch(ctx context.Context, taskIDs []string, toStaffID, reason string, actorID string, now time.Time) (string, error) {
	if len(taskIDs) == 0 || len(taskIDs) > 50 {
		return "", ErrCareState
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 200 {
		return "", ErrCareState
	}
	batchID := randomID("hbatch-")
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var target model.FollowupStaff
		if err := tx.Where("id = ?", toStaffID).First(&target).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if target.Status != "on_duty" {
			return ErrStaffOffDuty
		}
		if err := requireHandoffRecipient(tx, target.ID); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, taskID := range taskIDs {
			if !validBatchID(taskID) || seen[taskID] {
				return ErrCareState
			}
			seen[taskID] = true
			var task model.FollowupTask
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", taskID).First(&task).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrNotFound
				}
				return err
			}
			if task.Status != "pending" && task.Status != "submitted" && task.Status != "alert" {
				return ErrCareState
			}
			if employeeScoped(ctx) {
				allowed, err := s.CanAccessAppointment(ctx, task.AppointmentID, false)
				if err != nil {
					return err
				}
				if !allowed {
					return ErrForbidden
				}
				if err := requireClinicRecipient(tx, task.AppointmentID, target.ID); err != nil {
					return err
				}
			}
			if err := supersedeHandoffReceipt(tx, task.ID, now); err != nil {
				return err
			}
			receiptID := randomID("handoff-")
			if err := tx.Create(&model.FollowupHandoffReceipt{ID: receiptID, TaskID: task.ID, FromStaffID: task.AssigneeID, ToStaffID: target.ID, Reason: reason, AssignedBy: actorID, CreatedAt: now, Status: "pending", Version: 1, FollowupStatus: "untracked", FollowupVersion: 1, BatchID: &batchID}).Error; err != nil {
				return err
			}
			if err := createHandoffNotification(tx, receiptID, target.ID, now); err != nil {
				return err
			}
			previous := task.AssigneeID
			update := tx.Model(&model.FollowupTask{}).Where("id = ? AND version = ?", task.ID, task.Version).
				Updates(map[string]any{"assignee_id": target.ID, "assigned_at": now, "version": task.Version + 1})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return ErrConflict
			}
			if err := tx.Create(&model.FollowupAssignmentEvent{ID: randomID("assignment-"), TaskID: task.ID, TaskVersion: task.Version + 1, FromStaffID: &previous, ToStaffID: target.ID, EventType: "assign", Reason: reason, ActorID: actorID, CreatedAt: now}).Error; err != nil {
				return err
			}
			if err := followupEvent(tx, task.AppointmentID, task.ID, "clinician", actorID, "assignment-changed", map[string]string{"from": previous, "to": target.ID, "reason": reason, "batch": batchID}, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return batchID, nil
}

// HandoverBatches 交接看板：按批次聚合（总数/已确认/待确认）。
func (s *MySQL) HandoverBatches(ctx context.Context, page, size int) ([]HandoverBatch, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&model.FollowupHandoffReceipt{}).Where("batch_id IS NOT NULL").Distinct("batch_id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []HandoverBatch{}
	err := s.db.WithContext(ctx).Table("followup_handoff_receipts r").
		Select(`r.batch_id AS batch_id, MAX(r.reason) AS reason, MAX(r.from_staff_id) AS from_staff,
			MAX(r.to_staff_id) AS to_staff, MAX(sf.display_name) AS from_name, MAX(st.display_name) AS to_name,
			COUNT(*) AS total,
			SUM(CASE WHEN r.status = 'confirmed' THEN 1 ELSE 0 END) AS confirmed,
			SUM(CASE WHEN r.status = 'pending' THEN 1 ELSE 0 END) AS pending,
			MIN(r.created_at) AS created_at, MAX(r.assigned_by) AS assigned_by`).
		Joins("JOIN followup_staff sf ON sf.id = r.from_staff_id").
		Joins("JOIN followup_staff st ON st.id = r.to_staff_id").
		Where("r.batch_id IS NOT NULL").
		Group("r.batch_id").Order("MIN(r.created_at) DESC").
		Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

// NudgeHandoverBatch 催办：给批次中未确认任务的接手人发催办通知。
func (s *MySQL) NudgeHandoverBatch(ctx context.Context, batchID, actorID string, now time.Time) (int, error) {
	if !validBatchID(batchID) {
		return 0, ErrCareState
	}
	sent := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var receipts []model.FollowupHandoffReceipt
		if err := tx.Where("batch_id = ? AND status = 'pending'", batchID).Find(&receipts).Error; err != nil {
			return err
		}
		if len(receipts) == 0 {
			return ErrNotFound
		}
		for _, r := range receipts {
			// 重新催办时先清掉旧的 nudged 通知，保证收件箱里只有最新一条
			if err := tx.Where("receipt_id = ? AND kind = 'nudged'", r.ID).Delete(&model.HandoffNotification{}).Error; err != nil {
				return err
			}
			id := "nudged-" + r.ID
			if len(id) > 64 {
				id = id[:64]
			}
			res := tx.Exec(`INSERT INTO handoff_notifications (id, receipt_id, employee_id, kind, created_at)
				SELECT ?, ?, e.id, 'nudged', ? FROM employee_accounts e WHERE e.staff_id = ? AND e.active = true`,
				id, r.ID, now, r.ToStaffID)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected > 0 {
				sent++
			}
			// 同时记一条催办跟进事件，审计可查
			_ = tx.Create(&model.HandoffFollowupEvent{ID: randomID("hevent-"), ReceiptID: r.ID, ActorID: actorID, ActionText: "催办", ResultText: "已向接手人发送催办通知", StatusAfter: r.Status, CreatedAt: now}).Error
		}
		return nil
	})
	return sent, err
}
