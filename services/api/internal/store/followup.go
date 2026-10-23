package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TreatmentInput struct {
	model.TreatmentFields
	Version       int
	PlanVersionID string
	Summary       string
	Notes         string
	TreatedAt     time.Time
	Offsets       []int
	AssigneeID    string `json:"AssigneeID,omitempty"`
	ActorID       string `json:"-"`
}
type TreatmentCorrectionInput struct {
	Fields           TreatmentFieldPatch
	Version          int
	PlanVersionID    string
	Summary          string
	Notes            string
	TreatedAt        time.Time
	CorrectionReason string
	ActorID          string
}
type FollowupInput struct {
	Version  int
	Symptoms string
	Severity int
	Concern  bool
}
type FollowupReviewInput struct {
	Version     int
	Action      string
	Response    string
	Disposition string
}
type FollowupFilter struct {
	Dates      DateRange
	PatientID  string
	AssigneeID string
	Status     string
	Page       int
	PageSize   int
	Now        time.Time
}
type FollowupRepository interface {
	HandoffFollowupEvents(context.Context, string) ([]model.HandoffFollowupEvent, error)
	RecordHandoffFollowup(context.Context, string, string, string, string, string, string, int, time.Time) (model.HandoffFollowupEvent, error)
	HandoffSetting(context.Context) (model.FollowupHandoffSetting, error)
	UpdateHandoffSetting(context.Context, int, int, string, time.Time) (model.FollowupHandoffSetting, error)
	HandoffReceipts(context.Context, HandoffReceiptFilter) (HandoffReceiptPage, error)
	HandoffReceipt(context.Context, string) (model.FollowupHandoffReceipt, error)
	ConfirmHandoffReceipt(context.Context, string, int, time.Time) (model.FollowupHandoffReceipt, error)
	FollowupStaff(context.Context) ([]model.FollowupStaff, error)
	AssignFollowup(context.Context, string, FollowupAssignmentInput, time.Time) (model.FollowupTask, error)
	FollowupAssignmentHistory(context.Context, string) ([]model.FollowupAssignmentEvent, error)
	FollowupStaffEvents(context.Context, string) ([]model.FollowupStaffEvent, error)
	LeaveFollowupStaff(context.Context, string, FollowupLeaveInput, time.Time) (model.FollowupStaff, int, error)
	ReturnFollowupStaff(context.Context, string, int, time.Time, ...string) (model.FollowupStaff, error)
	RecordTreatment(context.Context, string, TreatmentInput, time.Time) (model.Treatment, error)
	CorrectTreatment(context.Context, string, TreatmentCorrectionInput, time.Time) (model.Treatment, error)
	TreatmentRevisions(context.Context, string, string) ([]model.TreatmentRevision, error)
	FollowupWorkspaceWithHistory(context.Context, string, string) (*model.Treatment, []model.TreatmentRevision, []model.FollowupTask, error)
	FollowupWorkspace(context.Context, string, string) (*model.Treatment, []model.FollowupTask, error)
	FollowupQueue(context.Context, FollowupFilter) ([]model.FollowupTask, int64, error)
	FollowupTask(context.Context, string, string) (model.FollowupTask, error)
	SubmitFollowup(context.Context, string, string, FollowupInput, time.Time) (model.FollowupTask, error)
	ReviewFollowup(context.Context, string, FollowupReviewInput, time.Time) (model.FollowupTask, error)
	SaveFollowupPhoto(context.Context, string, string, model.FollowupPhoto, time.Time) (model.FollowupPhoto, string, error)
	FollowupPhotos(context.Context, string, string, bool) ([]model.FollowupPhoto, bool, error)
	FollowupPhoto(context.Context, string, string, bool) (model.FollowupPhoto, error)
	RecordFollowupAccess(context.Context, string, string, string, string, string, time.Time) error
}

func followupDBError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
func followupEvent(tx *gorm.DB, appointmentID, taskID, actorType, actorID, action string, detail any, now time.Time) error {
	data, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	return tx.Create(&model.FollowupEvent{ID: randomID("fu-event-"), AppointmentID: appointmentID, TaskID: taskID, ActorType: actorType, ActorID: actorID, Action: action, DetailJSON: string(data), CreatedAt: now}).Error
}
func (s *MySQL) RecordTreatment(ctx context.Context, id string, input TreatmentInput, now time.Time) (model.Treatment, error) {
	var result model.Treatment
	if employeeScoped(ctx) {
		allowed, err := s.CanAccessAppointment(ctx, id, true)
		if err != nil {
			return result, err
		}
		if !allowed {
			return result, ErrForbidden
		}
	}
	if !ValidTreatmentFields(input.TreatmentFields) || input.Version < 1 || input.PlanVersionID == "" || input.Summary == "" || input.TreatedAt.IsZero() || input.TreatedAt.After(now) || len(input.Offsets) < 1 || len(input.Offsets) > 8 {
		return result, ErrCareState
	}
	input.Offsets = append([]int(nil), input.Offsets...)
	sort.Ints(input.Offsets)
	for i, n := range input.Offsets {
		if n < 1 || n > 90 || (i > 0 && n == input.Offsets[i-1]) {
			return result, ErrCareState
		}
	}
	canonical := input
	canonical.Version = 0
	canonical.ActorID = ""
	encoded, _ := json.Marshal(canonical)
	sum := sha256.Sum256(encoded)
	hash := hex.EncodeToString(sum[:])
	initial, err := loadAppointment(s.db.WithContext(ctx), id, "")
	if err != nil {
		return result, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		actorID := input.ActorID
		if actorID == "" {
			actorID = "demo-clinician"
		}
		assigneeID := input.AssigneeID
		if assigneeID == "" {
			assigneeID = initial.DoctorID
		}
		var assignee model.FollowupStaff
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", assigneeID).First(&assignee).Error; err != nil {
			return followupDBError(err)
		}
		if err := lockPatient(tx, initial.PatientID); err != nil {
			return err
		}
		if _, err := lockSlot(tx, initial.SlotID); err != nil {
			return err
		}
		var appointment model.Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&appointment).Error; err != nil {
			return followupDBError(err)
		}
		err := tx.Where("appointment_id = ?", id).First(&result).Error
		if err == nil {
			if result.RequestHash == hash {
				return nil
			}
			return ErrConflict
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if appointment.Status != "confirmed" {
			return ErrCareState
		}
		if appointment.ArrivedAt != nil && input.TreatedAt.Before(*appointment.ArrivedAt) {
			return ErrCareState
		}
		if appointment.Version != input.Version {
			return ErrConflict
		}
		if assignee.Status != "on_duty" {
			return ErrStaffOffDuty
		}
		var plan model.CarePlanVersion
		if err := tx.Joins("JOIN care_plans ON care_plans.id = care_plan_versions.care_plan_id").Where("care_plan_versions.id = ? AND care_plans.appointment_id = ?", input.PlanVersionID, id).First(&plan).Error; err != nil {
			return followupDBError(err)
		}
		if !validPlanStage(plan.ContentJSON, input.StageIndex) {
			return ErrCareState
		}
		result = model.Treatment{TreatmentFields: input.TreatmentFields, ID: randomID("treatment-"), AppointmentID: id, PatientID: appointment.PatientID, PlanVersionID: plan.ID, Summary: input.Summary, Notes: input.Notes, TreatedAt: input.TreatedAt, RecordedAt: now, Version: 1, RequestHash: hash}
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.TreatmentRevision{TreatmentFields: result.TreatmentFields, ID: randomID("treatment-revision-"), TreatmentID: result.ID, Version: 1, PlanVersionID: result.PlanVersionID, Summary: result.Summary, Notes: result.Notes, TreatedAt: result.TreatedAt, ChangedAt: now, ActorID: actorID, CorrectionReason: ""}).Error; err != nil {
			return err
		}
		for _, offset := range input.Offsets {
			task := model.FollowupTask{ID: randomID("followup-"), AppointmentID: id, TreatmentID: result.ID, PatientID: appointment.PatientID, AssigneeID: assigneeID, AssignedAt: now, Title: fmt.Sprintf("治疗后第 %d 天随访", offset), DueAt: input.TreatedAt.AddDate(0, 0, offset), Status: "pending", Version: 1}
			if err := tx.Create(&task).Error; err != nil {
				return err
			}
			if err := tx.Create(&model.FollowupAssignmentEvent{ID: randomID("assignment-"), TaskID: task.ID, TaskVersion: task.Version, ToStaffID: assigneeID, EventType: "initial", Reason: "记录实际治疗时指定", ActorID: actorID, CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		if err := persistTransition(tx, &appointment, "completed", "", "operator", actorID, "treat", true, now); err != nil {
			return err
		}
		return followupEvent(tx, id, "", "clinician", actorID, "treatment-recorded", input, now)
	})
	return result, err
}
func (s *MySQL) CorrectTreatment(ctx context.Context, appointmentID string, input TreatmentCorrectionInput, now time.Time) (model.Treatment, error) {
	var result model.Treatment
	if employeeScoped(ctx) {
		allowed, err := s.CanAccessAppointment(ctx, appointmentID, true)
		if err != nil {
			return result, err
		}
		if !allowed {
			return result, ErrForbidden
		}
	}
	actorID := input.ActorID
	if actorID == "" {
		actorID = "demo-clinician"
	}
	if input.Version < 1 || input.PlanVersionID == "" || input.Summary == "" || input.TreatedAt.IsZero() || input.TreatedAt.After(now) || input.CorrectionReason == "" {
		return result, ErrCareState
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var appointment model.Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", appointmentID).First(&appointment).Error; err != nil {
			return followupDBError(err)
		}
		if appointment.Status != "completed" {
			return ErrCareState
		}
		if appointment.ArrivedAt != nil && input.TreatedAt.Before(*appointment.ArrivedAt) {
			return ErrCareState
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("appointment_id = ?", appointmentID).First(&result).Error; err != nil {
			return followupDBError(err)
		}
		fields := input.Fields.Apply(result.TreatmentFields)
		if !ValidTreatmentFields(fields) {
			return ErrCareState
		}
		matches := result.TreatmentFields == fields && result.PlanVersionID == input.PlanVersionID && result.Summary == input.Summary && result.Notes == input.Notes && result.TreatedAt.Equal(input.TreatedAt)
		if result.Version == input.Version+1 && matches {
			var latest model.TreatmentRevision
			if err := tx.Where("treatment_id = ? AND version = ?", result.ID, result.Version).First(&latest).Error; err != nil {
				return err
			}
			if latest.CorrectionReason == input.CorrectionReason {
				return nil
			}
		}
		if result.Version != input.Version {
			return ErrConflict
		}
		if matches {
			return ErrCareState
		}
		var plan model.CarePlanVersion
		if err := tx.Joins("JOIN care_plans ON care_plans.id = care_plan_versions.care_plan_id").Where("care_plan_versions.id = ? AND care_plans.appointment_id = ?", input.PlanVersionID, appointmentID).First(&plan).Error; err != nil {
			return followupDBError(err)
		}
		if !validPlanStage(plan.ContentJSON, fields.StageIndex) {
			return ErrCareState
		}
		if !result.TreatedAt.Equal(input.TreatedAt) {
			var tasks []model.FollowupTask
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("appointment_id = ?", appointmentID).Order("id").Find(&tasks).Error; err != nil {
				return err
			}
			var photoCount int64
			if err := tx.Model(&model.FollowupPhoto{}).Where("appointment_id = ?", appointmentID).Count(&photoCount).Error; err != nil {
				return err
			}
			if photoCount > 0 {
				return ErrTreatmentTimeLocked
			}
			for _, task := range tasks {
				if task.Status != "pending" {
					return ErrTreatmentTimeLocked
				}
				newDue := input.TreatedAt.Add(task.DueAt.Sub(result.TreatedAt))
				if err := tx.Model(&model.FollowupTask{}).Where("id = ?", task.ID).Updates(map[string]any{"due_at": newDue, "version": task.Version + 1}).Error; err != nil {
					return err
				}
			}
		}
		result.TreatmentFields = fields
		result.Version++
		result.PlanVersionID, result.Summary, result.Notes, result.TreatedAt = input.PlanVersionID, input.Summary, input.Notes, input.TreatedAt
		result.CorrectedAt = &now
		update := tx.Model(&model.Treatment{}).Where("id = ? AND version = ?", result.ID, input.Version).Updates(map[string]any{"stage_index": fields.StageIndex, "procedure": fields.Procedure, "body_area": fields.BodyArea, "device": fields.Device, "parameters": fields.Parameters, "version": result.Version, "plan_version_id": result.PlanVersionID, "summary": result.Summary, "notes": result.Notes, "treated_at": result.TreatedAt, "corrected_at": now})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrConflict
		}
		revision := model.TreatmentRevision{TreatmentFields: result.TreatmentFields, ID: randomID("treatment-revision-"), TreatmentID: result.ID, Version: result.Version, PlanVersionID: result.PlanVersionID, Summary: result.Summary, Notes: result.Notes, TreatedAt: result.TreatedAt, ChangedAt: now, ActorID: actorID, CorrectionReason: input.CorrectionReason}
		if err := tx.Create(&revision).Error; err != nil {
			return err
		}
		return followupEvent(tx, appointmentID, "", "clinician", actorID, "treatment-corrected", map[string]any{"version": result.Version, "reason": input.CorrectionReason}, now)
	})
	return result, err
}
func (s *MySQL) TreatmentRevisions(ctx context.Context, appointmentID, patientID string) ([]model.TreatmentRevision, error) {
	var treatment model.Treatment
	q := s.db.WithContext(ctx).Where("appointment_id = ?", appointmentID)
	if patientID != "" {
		q = q.Where("patient_id = ?", patientID)
	}
	if err := q.First(&treatment).Error; err != nil {
		return nil, followupDBError(err)
	}
	revisions := make([]model.TreatmentRevision, 0)
	err := s.db.WithContext(ctx).Where("treatment_id = ?", treatment.ID).Order("version DESC").Find(&revisions).Error
	return revisions, err
}
func taskQuery(tx *gorm.DB) *gorm.DB {
	return tx.Model(&model.FollowupTask{}).Select("followup_tasks.*, appointments.contact_name AS patient_name, followup_staff.display_name AS assignee_name").Joins("JOIN appointments ON appointments.id = followup_tasks.appointment_id").Joins("JOIN followup_staff ON followup_staff.id = followup_tasks.assignee_id")
}
func (s *MySQL) FollowupWorkspace(ctx context.Context, id, patientID string) (*model.Treatment, []model.FollowupTask, error) {
	treatment, _, rows, err := s.FollowupWorkspaceWithHistory(ctx, id, patientID)
	return treatment, rows, err
}
func (s *MySQL) FollowupWorkspaceWithHistory(ctx context.Context, id, patientID string) (*model.Treatment, []model.TreatmentRevision, []model.FollowupTask, error) {
	if employeeScoped(ctx) {
		allowed, err := s.CanAccessAppointment(ctx, id, false)
		if err != nil {
			return nil, nil, nil, err
		}
		if !allowed {
			return nil, nil, nil, ErrForbidden
		}
	}
	var treatment *model.Treatment
	revisions := make([]model.TreatmentRevision, 0)
	rows := make([]model.FollowupTask, 0)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var appointment model.Appointment
		q := tx.Where("id = ?", id)
		if patientID != "" {
			q = q.Where("patient_id = ?", patientID)
		}
		if err := q.First(&appointment).Error; err != nil {
			return followupDBError(err)
		}
		var record model.Treatment
		if err := tx.Where("appointment_id = ?", id).First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		treatment = &record
		if err := tx.Where("treatment_id = ?", record.ID).Order("version DESC").Find(&revisions).Error; err != nil {
			return err
		}
		q = taskQuery(tx).Where("followup_tasks.appointment_id = ?", id)
		if staffID := clinicianStaff(ctx); staffID != "" {
			q = q.Where("followup_tasks.assignee_id = ?", staffID)
		}
		return q.Order("due_at, followup_tasks.id").Find(&rows).Error
	})
	return treatment, revisions, rows, err
}
func (s *MySQL) FollowupQueue(ctx context.Context, f FollowupFilter) ([]model.FollowupTask, int64, error) {
	q := s.taskClinicScope(ctx, taskQuery(s.db.WithContext(ctx)), "followup_tasks.id")
	if f.PatientID != "" {
		q = q.Where("followup_tasks.patient_id = ?", f.PatientID)
	}
	if f.AssigneeID != "" {
		q = q.Where("followup_tasks.assignee_id = ?", f.AssigneeID)
	}
	if staffID := clinicianStaff(ctx); staffID != "" {
		q = q.Where("followup_tasks.assignee_id = ?", staffID)
	}
	if f.Status == "overdue" {
		q = q.Where("followup_tasks.status = 'pending' AND due_at < ?", f.Now)
	} else if f.Status != "" {
		q = q.Where("followup_tasks.status = ?", f.Status)
	}
	if f.Dates.Active() {
		q = q.Where("(followup_tasks.due_at >= ? AND followup_tasks.due_at < ?) OR (followup_tasks.submitted_at >= ? AND followup_tasks.submitted_at < ?) OR (followup_tasks.reviewed_at >= ? AND followup_tasks.reviewed_at < ?)", f.Dates.From, f.Dates.Until, f.Dates.From, f.Dates.Until, f.Dates.From, f.Dates.Until)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]model.FollowupTask, 0)
	err := q.Order("CASE followup_tasks.status WHEN 'alert' THEN 0 WHEN 'submitted' THEN 1 WHEN 'pending' THEN 2 ELSE 3 END, due_at, followup_tasks.id").Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).Find(&rows).Error
	return rows, total, err
}
func (s *MySQL) FollowupTask(ctx context.Context, id, patientID string) (model.FollowupTask, error) {
	var row model.FollowupTask
	q := s.taskClinicScope(ctx, taskQuery(s.db.WithContext(ctx)), "followup_tasks.id").Where("followup_tasks.id = ?", id)
	if patientID != "" {
		q = q.Where("followup_tasks.patient_id = ?", patientID)
	}
	if staffID := clinicianStaff(ctx); staffID != "" {
		q = q.Where("followup_tasks.assignee_id = ?", staffID)
	}
	err := q.First(&row).Error
	return row, followupDBError(err)
}
func lockFollowup(tx *gorm.DB, id, patientID string) (model.FollowupTask, error) {
	var task model.FollowupTask
	// Always lock appointment before task, matching consent and upload lock order.
	q := tx.Select("appointment_id").Where("id = ?", id)
	if patientID != "" {
		q = q.Where("patient_id = ?", patientID)
	}
	if err := q.First(&task).Error; err != nil {
		return task, followupDBError(err)
	}
	var appointment model.Appointment
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", task.AppointmentID).First(&appointment).Error; err != nil {
		return task, followupDBError(err)
	}
	q = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id)
	if patientID != "" {
		q = q.Where("patient_id = ?", patientID)
	}
	err := q.First(&task).Error
	task.PatientName = appointment.ContactName
	var assignee model.FollowupStaff
	if err == nil {
		err = tx.Select("display_name").Where("id = ?", task.AssigneeID).First(&assignee).Error
		task.AssigneeName = assignee.DisplayName
	}
	return task, followupDBError(err)
}
func (s *MySQL) SubmitFollowup(ctx context.Context, id, patientID string, input FollowupInput, now time.Time) (model.FollowupTask, error) {
	var row model.FollowupTask
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		row, err = lockFollowup(tx, id, patientID)
		if err != nil {
			return err
		}
		if row.Status != "pending" {
			if row.SubmittedAt != nil && row.Symptoms == input.Symptoms && row.Severity == input.Severity && row.Concern == input.Concern && input.Version == row.Version-1 {
				return nil
			}
			return ErrConflict
		}
		if row.Version != input.Version {
			return ErrConflict
		}
		if input.Symptoms == "" || input.Severity < 0 || input.Severity > 3 {
			return ErrCareState
		}
		row.Status = "submitted"
		if input.Severity >= 2 || input.Concern {
			row.Status = "alert"
		}
		row.Symptoms = input.Symptoms
		row.Severity = input.Severity
		row.Concern = input.Concern
		row.SubmittedAt = &now
		row.Version++
		if err := tx.Model(&model.FollowupTask{}).Where("id = ?", row.ID).Updates(map[string]any{"status": row.Status, "symptoms": row.Symptoms, "severity": row.Severity, "concern": row.Concern, "submitted_at": now, "version": row.Version}).Error; err != nil {
			return err
		}
		return followupEvent(tx, row.AppointmentID, row.ID, "patient", patientID, "submitted", input, now)
	})
	return row, err
}
func (s *MySQL) ReviewFollowup(ctx context.Context, id string, input FollowupReviewInput, now time.Time) (model.FollowupTask, error) {
	var row model.FollowupTask
	if employeeScoped(ctx) {
		allowed, err := s.CanAccessTask(ctx, id)
		if err != nil {
			return row, err
		}
		if !allowed {
			return row, ErrForbidden
		}
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		row, err = lockFollowup(tx, id, "")
		if err != nil {
			return err
		}
		if staffID := clinicianStaff(ctx); staffID != "" && row.AssigneeID != staffID {
			return ErrForbidden
		}
		target := "reviewed"
		if input.Action == "handle" {
			target = "handled"
		}
		if row.Status == target && row.Version == input.Version+1 && row.Response == input.Response && row.Disposition == input.Disposition {
			return nil
		}
		if row.Version != input.Version {
			return ErrConflict
		}
		if input.Response == "" || !((row.Status == "submitted" && input.Action == "review") || (row.Status == "alert" && input.Action == "handle" && input.Disposition != "")) {
			return ErrCareState
		}
		row.Status = target
		row.Response = input.Response
		row.Disposition = input.Disposition
		row.ReviewedAt = &now
		row.Version++
		if err := tx.Model(&model.FollowupTask{}).Where("id = ?", row.ID).Updates(map[string]any{"status": row.Status, "response": row.Response, "disposition": row.Disposition, "reviewed_at": now, "version": row.Version}).Error; err != nil {
			return err
		}
		return followupEvent(tx, row.AppointmentID, row.ID, "clinician", employeeActor(ctx), input.Action, input, now)
	})
	return row, err
}
func (s *MySQL) SaveFollowupPhoto(ctx context.Context, id, patientID string, photo model.FollowupPhoto, now time.Time) (model.FollowupPhoto, string, error) {
	var oldID string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := lockFollowup(tx, id, patientID)
		if err != nil {
			return err
		}
		if task.Status != "pending" {
			return ErrCareState
		}
		consent, err := latestPhotoConsent(tx, task.AppointmentID, patientID)
		if errors.Is(err, ErrPhotoConsent) || (err == nil && (consent.Action != "granted" || consent.Version != "photo-care-v2")) {
			return ErrPhotoConsent
		}
		if err != nil {
			return err
		}
		photo.TaskID = id
		photo.AppointmentID = task.AppointmentID
		photo.PatientID = patientID
		var previous model.FollowupPhoto
		err = tx.Where("task_id = ? AND angle = ?", id, photo.Angle).First(&previous).Error
		if err == nil {
			oldID = previous.ID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "task_id"}, {Name: "angle"}}, DoUpdates: clause.AssignmentColumns([]string{"id", "size_bytes", "sha256", "uploaded_at"})}).Create(&photo).Error; err != nil {
			return err
		}
		return followupEvent(tx, task.AppointmentID, id, "patient", patientID, "photo-uploaded", map[string]string{"photoId": photo.ID, "angle": photo.Angle}, now)
	})
	return photo, oldID, err
}
func (s *MySQL) FollowupPhotos(ctx context.Context, id, patientID string, requireConsent bool) ([]model.FollowupPhoto, bool, error) {
	task, err := s.FollowupTask(ctx, id, patientID)
	if err != nil {
		return nil, false, err
	}
	granted, err := s.PhotoConsent(ctx, task.AppointmentID, task.PatientID)
	if err != nil {
		return nil, false, err
	}
	rows := make([]model.FollowupPhoto, 0, 3)
	if requireConsent && !granted {
		return rows, granted, nil
	}
	err = s.db.WithContext(ctx).Where("task_id = ?", id).Order("FIELD(angle,'front','left','right')").Find(&rows).Error
	return rows, granted, err
}
func (s *MySQL) FollowupPhoto(ctx context.Context, id, patientID string, requireConsent bool) (model.FollowupPhoto, error) {
	var row model.FollowupPhoto
	if employeeScoped(ctx) {
		allowed, err := s.CanAccessFollowupPhoto(ctx, id)
		if err != nil {
			return row, err
		}
		if !allowed {
			return row, ErrNotFound
		}
	}
	q := s.db.WithContext(ctx).Where("id = ?", id)
	if staffID := clinicianStaff(ctx); staffID != "" {
		q = q.Where("task_id IN (SELECT id FROM followup_tasks WHERE assignee_id = ?)", staffID)
	}
	if patientID != "" {
		q = q.Where("patient_id = ?", patientID)
	}
	if err := q.First(&row).Error; err != nil {
		return row, followupDBError(err)
	}
	if requireConsent {
		granted, err := s.PhotoConsent(ctx, row.AppointmentID, row.PatientID)
		if err != nil {
			return row, err
		}
		if !granted {
			return row, ErrPhotoConsent
		}
	}
	return row, nil
}
func (s *MySQL) RecordFollowupAccess(ctx context.Context, appointmentID, taskID, actorType, actorID, action string, now time.Time) error {
	return followupEvent(s.db.WithContext(ctx), appointmentID, taskID, actorType, actorID, action, map[string]string{}, now)
}
