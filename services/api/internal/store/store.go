package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrConflict            = errors.New("version conflict")
	ErrCapacity            = errors.New("capacity below reserved")
	ErrPast                = errors.New("slot already started")
	ErrPhotoConsent        = errors.New("photo consent is not active")
	ErrCareState           = errors.New("care action is not allowed for this appointment state")
	ErrTreatmentTimeLocked = errors.New("treatment time cannot change after followup activity")
	ErrStaffOffDuty        = errors.New("followup staff is off duty")
)

type SlotFilter struct {
	ClinicID string
	DoctorID string
	Date     time.Time
	Now      time.Time
	// After, when set, only returns slots starting strictly after it (used by
	// reschedule recommendations, where the original slot time is the floor).
	After time.Time
	// AvailableOnly restricts to open slots with remaining capacity.
	AvailableOnly bool
	// Limit caps the result size; 0 keeps the default cap.
	Limit int
}

type SlotUpdate struct {
	Capacity int
	Status   string
	Version  int
}

type Repository interface {
	Ready(context.Context) error
	Clinics(context.Context) ([]model.Clinic, error)
	Doctors(context.Context, string) ([]model.Doctor, error)
	Slots(context.Context, SlotFilter) ([]model.Slot, error)
	UpdateSlot(context.Context, string, SlotUpdate, time.Time) (model.Slot, error)
}

type MySQL struct {
	patientProvider     PatientProvider
	db                  *gorm.DB
	pendingConfirmation time.Duration
}

func New(db *gorm.DB) *MySQL { return &MySQL{db: db, pendingConfirmation: 120 * time.Minute} }

func (s *MySQL) Ready(ctx context.Context) error {
	for _, row := range []any{&[]Assessment{}, &[]AssessmentPhoto{}, &[]AssessmentAttempt{}, &[]AssessmentResult{}, &[]AssessmentReview{}, &[]AssessmentOperation{}, &[]AssessmentInputSnapshot{}, &[]AssessmentConsentEvent{}, &[]AssessmentEvent{}} {
		if err := s.db.WithContext(ctx).Limit(1).Find(row).Error; err != nil {
			return err
		}
	}
	for _, probe := range []struct {
		row     any
		columns string
	}{
		{&[]model.RecordExportJob{}, recordExportPublicColumns + ", snapshot_json, payload, lease_until, lease_token"},
		{&[]model.RecordExportEvent{}, "id, job_id, actor_id, action, detail_code, created_at"},
	} {
		if err := s.db.WithContext(ctx).Select(probe.columns).Where("1 = 0").Limit(1).Find(probe.row).Error; err != nil {
			return err
		}
	}
	for _, row := range []any{&[]model.FollowupTemplate{}, &[]model.FollowupTemplateEvent{}, &[]model.NursingFeedback{}, &[]model.CaseRecord{}, &[]model.CaseImage{}, &[]model.CaseEvent{}} {
		if err := s.db.WithContext(ctx).Limit(1).Find(row).Error; err != nil {
			return err
		}
	}
	for _, row := range []any{&[]model.MessageTemplate{}, &[]model.MessageConsent{}, &[]model.Notification{}, &[]model.NotificationAttempt{}, &[]model.MessageConsentEvent{}, &[]model.PatientMessage{}, &[]model.KnowledgeCategory{}, &[]model.KnowledgeArticle{}, &[]model.KnowledgeEvent{}} {
		if err := s.db.WithContext(ctx).Limit(1).Find(row).Error; err != nil {
			return err
		}
	}
	// Reading the expected columns checks connectivity and completed migrations.
	var directory []model.Doctor
	if err := s.db.WithContext(ctx).Select("active", "version", "published_json", "published_at", "avatar_url", "service_introduction", "presentation").Limit(1).Find(&directory).Error; err != nil {
		return err
	}
	var directoryClinics []model.Clinic
	if err := s.db.WithContext(ctx).Select("active", "version", "published_json", "published_at", "presentation").Limit(1).Find(&directoryClinics).Error; err != nil {
		return err
	}
	var history []model.DirectoryRevision
	if err := s.db.WithContext(ctx).Limit(1).Find(&history).Error; err != nil {
		return err
	}
	var rows []model.Slot
	var scheduleEvents []ScheduleEvent
	if err := s.db.WithContext(ctx).Limit(1).Find(&scheduleEvents).Error; err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Select("id", "clinic_id", "doctor_id", "starts_at", "ends_at", "capacity", "reserved", "status", "version").Limit(1).Find(&rows).Error; err != nil {
		return err
	}
	var appointments []model.Appointment
	if err := s.db.WithContext(ctx).Select("id", "patient_id", "slot_id", "confirmation_deadline", "request_key", "request_hash", "status", "version").Limit(1).Find(&appointments).Error; err != nil {
		return err
	}
	var sessions []model.PatientSession
	if err := s.db.WithContext(ctx).Select("token_hash", "patient_id", "expires_at").Limit(1).Find(&sessions).Error; err != nil {
		return err
	}
	var patients []model.Patient
	if err := s.db.WithContext(ctx).Select("id", "display_name", "created_at", "wechat_app_id", "wechat_open_id", "phone", "phone_authorization", "phone_authorized_at", "version").Limit(1).Find(&patients).Error; err != nil {
		return err
	}
	var events []model.AppointmentEvent
	if err := s.db.WithContext(ctx).Select("id", "appointment_id", "actor_type", "actor_id", "action", "from_status", "to_status", "created_at").Limit(1).Find(&events).Error; err != nil {
		return err
	}
	var photos []model.CarePhoto
	if err := s.db.WithContext(ctx).Select("id", "appointment_id", "patient_id", "angle", "media_type", "size_bytes", "sha256", "uploaded_at").Limit(1).Find(&photos).Error; err != nil {
		return err
	}
	var consents []model.PhotoConsentEvent
	if err := s.db.WithContext(ctx).Select("id", "appointment_id", "patient_id", "action", "version", "recorded_at").Limit(1).Find(&consents).Error; err != nil {
		return err
	}
	var plans []model.CarePlan
	if err := s.db.WithContext(ctx).Select("id", "appointment_id", "draft_version", "draft_json", "updated_at").Limit(1).Find(&plans).Error; err != nil {
		return err
	}
	var versions []model.CarePlanVersion
	if err := s.db.WithContext(ctx).Select("id", "care_plan_id", "version", "content_json", "change_reason", "published_by", "published_at").Limit(1).Find(&versions).Error; err != nil {
		return err
	}
	var followups []model.FollowupTask
	if err := s.db.WithContext(ctx).Limit(1).Find(&followups).Error; err != nil {
		return err
	}
	var staff []model.FollowupStaff
	if err := s.db.WithContext(ctx).Limit(1).Find(&staff).Error; err != nil {
		return err
	}
	var assignments []model.FollowupAssignmentEvent
	if err := s.db.WithContext(ctx).Limit(1).Find(&assignments).Error; err != nil {
		return err
	}
	var staffEvents []model.FollowupStaffEvent
	if err := s.db.WithContext(ctx).Limit(1).Find(&staffEvents).Error; err != nil {
		return err
	}
	var handoffReceipts []model.FollowupHandoffReceipt
	if err := s.db.WithContext(ctx).Limit(1).Find(&handoffReceipts).Error; err != nil {
		return err
	}
	var handoffSettings []model.FollowupHandoffSetting
	if err := s.db.WithContext(ctx).Limit(1).Find(&handoffSettings).Error; err != nil {
		return err
	}
	var handoffFollowups []model.HandoffFollowupEvent
	if err := s.db.WithContext(ctx).Limit(1).Find(&handoffFollowups).Error; err != nil {
		return err
	}
	var employees []model.EmployeeAccount
	if err := s.db.WithContext(ctx).Limit(1).Find(&employees).Error; err != nil {
		return err
	}
	var employeeSessions []model.EmployeeSession
	if err := s.db.WithContext(ctx).Limit(1).Find(&employeeSessions).Error; err != nil {
		return err
	}
	var employeeEvents []model.EmployeeAccountEvent
	if err := s.db.WithContext(ctx).Limit(1).Find(&employeeEvents).Error; err != nil {
		return err
	}
	var treatments []model.Treatment
	if err := s.db.WithContext(ctx).Limit(1).Find(&treatments).Error; err != nil {
		return err
	}
	var revisions []model.TreatmentRevision
	if err := s.db.WithContext(ctx).Limit(1).Find(&revisions).Error; err != nil {
		return err
	}
	var followupPhotos []model.FollowupPhoto
	if err := s.db.WithContext(ctx).Limit(1).Find(&followupPhotos).Error; err != nil {
		return err
	}
	var followupEvents []model.FollowupEvent
	if err := s.db.WithContext(ctx).Limit(1).Find(&followupEvents).Error; err != nil {
		return err
	}
	var access []model.CareAccessEvent
	return s.db.WithContext(ctx).Select("id", "appointment_id", "patient_id", "resource_type", "resource_id", "action", "actor_type", "actor_id", "created_at").Limit(1).Find(&access).Error
}

func (s *MySQL) Clinics(ctx context.Context) ([]model.Clinic, error) {
	rows := make([]model.Clinic, 0)
	err := s.db.WithContext(ctx).Where("active = ? AND published_json IS NOT NULL", true).Order("id").Find(&rows).Error
	if err == nil {
		for i := range rows {
			var published model.Clinic
			if e := json.Unmarshal([]byte(*rows[i].PublishedJSON), &published); e != nil {
				return nil, e
			}
			rows[i] = published
		}
	}
	return rows, err
}

func (s *MySQL) Doctors(ctx context.Context, clinicID string) ([]model.Doctor, error) {
	rows := make([]model.Doctor, 0)
	q := s.db.WithContext(ctx).Where("doctors.active = ? AND doctors.published_json IS NOT NULL AND EXISTS (SELECT 1 FROM clinics WHERE clinics.id = doctors.clinic_id AND clinics.active = TRUE AND clinics.published_json IS NOT NULL)", true)
	if clinicID != "" {
		q = q.Where("clinic_id = ?", clinicID)
	}
	err := q.Order("id").Find(&rows).Error
	if err == nil {
		for i := range rows {
			var published model.Doctor
			if e := json.Unmarshal([]byte(*rows[i].PublishedJSON), &published); e != nil {
				return nil, e
			}
			rows[i] = published
		}
	}
	return rows, err
}

func (s *MySQL) Slots(ctx context.Context, f SlotFilter) ([]model.Slot, error) {
	if _, err := s.ExpirePending(ctx, f.Now); err != nil {
		return nil, err
	}
	rows := make([]model.Slot, 0)
	lower := f.Now
	if !f.After.IsZero() && f.After.After(lower) {
		lower = f.After
	}
	q := s.db.WithContext(ctx).Where("starts_at > ? AND EXISTS (SELECT 1 FROM doctors d JOIN clinics c ON c.id = d.clinic_id WHERE d.id = slots.doctor_id AND d.active = TRUE AND d.published_json IS NOT NULL AND c.active = TRUE AND c.published_json IS NOT NULL)", lower)
	if f.ClinicID != "" {
		q = q.Where("clinic_id = ?", f.ClinicID)
	}
	if f.DoctorID != "" {
		q = q.Where("doctor_id = ?", f.DoctorID)
	}
	if !f.Date.IsZero() {
		q = q.Where("starts_at >= ? AND starts_at < ?", f.Date, f.Date.AddDate(0, 0, 1))
	}
	if f.AvailableOnly {
		q = q.Where("status = 'open' AND capacity > reserved")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 500
	}
	err := q.Order("starts_at, doctor_id").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *MySQL) UpdateSlot(ctx context.Context, id string, u SlotUpdate, now time.Time) (model.Slot, error) {
	if _, err := s.ExpirePending(ctx, now); err != nil {
		return model.Slot{}, err
	}
	var row model.Slot
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := clinicScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"}), "clinic_id").Where("id = ?", id).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if row.Version != u.Version {
			return ErrConflict
		}
		if !row.StartsAt.After(now) {
			return ErrPast
		}
		if u.Capacity < row.Reserved {
			return ErrCapacity
		}
		if row.Status == "open" && u.Status == "closed" {
			if err := tx.Create(&ScheduleEvent{ID: randomID("schedule-"), SlotID: row.ID, ActorID: employeeActor(ctx), Action: "suspended", Reason: "排班关闭", CreatedAt: now}).Error; err != nil {
				return err
			}
			if err := createSchedulePatientTasks(tx, ctx, row.ID, "排班关闭", now); err != nil {
				return err
			}
		}
		result := tx.Model(&model.Slot{}).Where("id = ? AND version = ?", id, u.Version).Updates(map[string]any{"capacity": u.Capacity, "status": u.Status, "version": row.Version + 1})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		row.Capacity, row.Status, row.Version = u.Capacity, u.Status, row.Version+1
		return nil
	})
	return row, err
}
