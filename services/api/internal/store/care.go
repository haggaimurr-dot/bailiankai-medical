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

type CarePlanStage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type CarePlanContent struct {
	Summary      string          `json:"summary"`
	Stages       []CarePlanStage `json:"stages"`
	Instructions string          `json:"instructions"`
}

type CareRepository interface {
	SetPhotoConsent(context.Context, string, string, bool, time.Time) error
	PhotoConsent(context.Context, string, string) (bool, error)
	ListCarePhotos(context.Context, string, string, bool) ([]model.CarePhoto, error)
	CarePhotoByID(context.Context, string, string, bool) (model.CarePhoto, error)
	SaveCarePhoto(context.Context, model.CarePhoto, time.Time) (string, error)
	RecordCareAccess(context.Context, string, string, string, string, string, string, time.Time) error
	PatientCarePlan(context.Context, string, string) ([]model.CarePlanVersion, error)
	AdminCarePlan(context.Context, string) (model.CarePlan, []model.CarePlanVersion, error)
	SaveCarePlanDraft(context.Context, string, int, CarePlanContent, time.Time) (model.CarePlan, error)
	PublishCarePlan(context.Context, string, int, string, time.Time) (model.CarePlanVersion, error)
}

func recordCareAccess(tx *gorm.DB, appointmentID, patientID, resourceType, resourceID, action, actorType, actorID string, now time.Time) error {
	return tx.Create(&model.CareAccessEvent{ID: randomID("care-event-"), AppointmentID: appointmentID, PatientID: patientID, ResourceType: resourceType, ResourceID: resourceID, Action: action, ActorType: actorType, ActorID: actorID, CreatedAt: now}).Error
}

func (s *MySQL) RecordCareAccess(ctx context.Context, appointmentID, resourceType, resourceID, action, actorType, actorID string, now time.Time) error {
	var appointment model.Appointment
	if err := s.db.WithContext(ctx).Select("id", "patient_id").Where("id = ?", appointmentID).First(&appointment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if actorType == "patient" && actorID != appointment.PatientID {
		return ErrNotFound
	}
	return s.db.WithContext(ctx).Create(&model.CareAccessEvent{ID: randomID("care-event-"), AppointmentID: appointment.ID, PatientID: appointment.PatientID, ResourceType: resourceType, ResourceID: resourceID, Action: action, ActorType: actorType, ActorID: actorID, CreatedAt: now}).Error
}

func latestPhotoConsent(tx *gorm.DB, appointmentID, patientID string) (model.PhotoConsentEvent, error) {
	var event model.PhotoConsentEvent
	err := tx.Where("appointment_id = ? AND patient_id = ?", appointmentID, patientID).Order("recorded_at DESC, id DESC").First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return event, ErrPhotoConsent
	}
	return event, err
}

func (s *MySQL) SetPhotoConsent(ctx context.Context, appointmentID, patientID string, granted bool, now time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var appointment model.Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND patient_id = ?", appointmentID, patientID).First(&appointment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if appointment.Status != "pending" && appointment.Status != "confirmed" && appointment.Status != "completed" {
			return ErrCareState
		}
		action := "revoked"
		if granted {
			action = "granted"
		}
		latest, err := latestPhotoConsent(tx, appointmentID, patientID)
		if err != nil && !errors.Is(err, ErrPhotoConsent) {
			return err
		}
		if err == nil && latest.Action == action && latest.Version == "photo-care-v2" {
			return nil
		}
		if err == nil && !now.After(latest.RecordedAt) {
			now = latest.RecordedAt.Add(time.Millisecond)
		}
		return tx.Create(&model.PhotoConsentEvent{ID: randomID("consent-"), AppointmentID: appointmentID, PatientID: patientID, Action: action, Version: "photo-care-v2", RecordedAt: now}).Error
	})
}

func (s *MySQL) PhotoConsent(ctx context.Context, appointmentID, patientID string) (bool, error) {
	var appointment model.Appointment
	if err := s.db.WithContext(ctx).Where("id = ? AND patient_id = ?", appointmentID, patientID).First(&appointment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, ErrNotFound
		}
		return false, err
	}
	event, err := latestPhotoConsent(s.db.WithContext(ctx), appointmentID, patientID)
	if errors.Is(err, ErrPhotoConsent) {
		return false, nil
	}
	return err == nil && event.Action == "granted" && event.Version == "photo-care-v2", err
}

func (s *MySQL) ListCarePhotos(ctx context.Context, appointmentID, patientID string, requireConsent bool) ([]model.CarePhoto, error) {
	if employeeScoped(ctx) {
		allowed, err := s.CanAccessAppointment(ctx, appointmentID, false)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, ErrForbidden
		}
	}
	var appointment model.Appointment
	q := s.db.WithContext(ctx).Where("id = ?", appointmentID)
	if patientID != "" {
		q = q.Where("patient_id = ?", patientID)
	}
	if err := q.First(&appointment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if requireConsent {
		granted, err := s.PhotoConsent(ctx, appointmentID, appointment.PatientID)
		if err != nil {
			return nil, err
		}
		if !granted {
			return []model.CarePhoto{}, nil
		}
	}
	rows := make([]model.CarePhoto, 0, 3)
	err := s.db.WithContext(ctx).Where("appointment_id = ?", appointmentID).Order("FIELD(angle, 'front', 'left', 'right')").Find(&rows).Error
	return rows, err
}

func (s *MySQL) CarePhotoByID(ctx context.Context, id, patientID string, requireConsent bool) (model.CarePhoto, error) {
	var row model.CarePhoto
	if employeeScoped(ctx) {
		allowed, err := s.CanAccessCarePhoto(ctx, id)
		if err != nil {
			return row, err
		}
		if !allowed {
			return row, ErrForbidden
		}
	}
	q := s.db.WithContext(ctx).Where("id = ?", id)
	if patientID != "" {
		q = q.Where("patient_id = ?", patientID)
	}
	err := q.First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, ErrNotFound
	}
	if err != nil {
		return row, err
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

func (s *MySQL) SaveCarePhoto(ctx context.Context, row model.CarePhoto, now time.Time) (string, error) {
	var oldID string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var appointment model.Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND patient_id = ?", row.AppointmentID, row.PatientID).First(&appointment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if appointment.Status != "pending" && appointment.Status != "confirmed" {
			return ErrCareState
		}
		consent, err := latestPhotoConsent(tx, row.AppointmentID, row.PatientID)
		if errors.Is(err, ErrPhotoConsent) || (err == nil && (consent.Action != "granted" || consent.Version != "photo-care-v2")) {
			return ErrPhotoConsent
		}
		if err != nil {
			return err
		}
		var existing model.CarePhoto
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("appointment_id = ? AND angle = ?", row.AppointmentID, row.Angle).First(&existing).Error
		if err == nil {
			oldID = existing.ID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "appointment_id"}, {Name: "angle"}}, DoUpdates: clause.AssignmentColumns([]string{"id", "patient_id", "media_type", "size_bytes", "sha256", "uploaded_at"})}).Create(&row).Error; err != nil {
			return err
		}
		return recordCareAccess(tx, row.AppointmentID, row.PatientID, "photo", row.ID, "uploaded", "patient", row.PatientID, now)
	})
	return oldID, err
}

func (s *MySQL) PatientCarePlan(ctx context.Context, appointmentID, patientID string) ([]model.CarePlanVersion, error) {
	var appointment model.Appointment
	if err := s.db.WithContext(ctx).Where("id = ? AND patient_id = ?", appointmentID, patientID).First(&appointment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var plan model.CarePlan
	if err := s.db.WithContext(ctx).Where("appointment_id = ?", appointmentID).First(&plan).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return []model.CarePlanVersion{}, nil
		}
		return nil, err
	}
	rows := make([]model.CarePlanVersion, 0)
	err := s.db.WithContext(ctx).Where("care_plan_id = ?", plan.ID).Order("version DESC").Find(&rows).Error
	return rows, err
}

func (s *MySQL) AdminCarePlan(ctx context.Context, appointmentID string) (model.CarePlan, []model.CarePlanVersion, error) {
	if employeeScoped(ctx) {
		allowed, err := s.CanAccessAppointment(ctx, appointmentID, false)
		if err != nil {
			return model.CarePlan{}, nil, err
		}
		if !allowed {
			return model.CarePlan{}, nil, ErrForbidden
		}
	}
	var appointment model.Appointment
	if err := s.db.WithContext(ctx).Where("id = ?", appointmentID).First(&appointment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.CarePlan{}, nil, ErrNotFound
		}
		return model.CarePlan{}, nil, err
	}
	var plan model.CarePlan
	if err := s.db.WithContext(ctx).Where("appointment_id = ?", appointmentID).First(&plan).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.CarePlan{AppointmentID: appointmentID}, []model.CarePlanVersion{}, nil
		}
		return plan, nil, err
	}
	rows := make([]model.CarePlanVersion, 0)
	err := s.db.WithContext(ctx).Where("care_plan_id = ?", plan.ID).Order("version DESC").Find(&rows).Error
	return plan, rows, err
}

func (s *MySQL) SaveCarePlanDraft(ctx context.Context, appointmentID string, expectedVersion int, content CarePlanContent, now time.Time) (model.CarePlan, error) {
	if employeeScoped(ctx) {
		allowed, err := s.CanAccessAppointment(ctx, appointmentID, true)
		if err != nil {
			return model.CarePlan{}, err
		}
		if !allowed {
			return model.CarePlan{}, ErrForbidden
		}
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return model.CarePlan{}, err
	}
	value := string(encoded)
	var result model.CarePlan
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var appointment model.Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", appointmentID).First(&appointment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if appointment.Status != "confirmed" {
			return ErrCareState
		}
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("appointment_id = ?", appointmentID).First(&result).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expectedVersion != 0 {
				return ErrConflict
			}
			result = model.CarePlan{ID: randomID("plan-"), AppointmentID: appointmentID, DraftVersion: 1, DraftJSON: &value, UpdatedAt: now}
			if err := tx.Create(&result).Error; err != nil {
				return err
			}
			return recordCareAccess(tx, appointmentID, appointment.PatientID, "care-plan", result.ID, "draft-saved", "clinician", employeeActor(ctx), now)
		}
		if err != nil {
			return err
		}
		if result.DraftVersion != expectedVersion {
			return ErrConflict
		}
		result.DraftVersion++
		result.DraftJSON = &value
		result.UpdatedAt = now
		if err := tx.Model(&result).Updates(map[string]any{"draft_version": result.DraftVersion, "draft_json": value, "updated_at": now}).Error; err != nil {
			return err
		}
		return recordCareAccess(tx, appointmentID, appointment.PatientID, "care-plan", result.ID, "draft-saved", "clinician", employeeActor(ctx), now)
	})
	return result, err
}

func (s *MySQL) PublishCarePlan(ctx context.Context, appointmentID string, expectedVersion int, reason string, now time.Time) (model.CarePlanVersion, error) {
	if employeeScoped(ctx) {
		allowed, err := s.CanAccessAppointment(ctx, appointmentID, true)
		if err != nil {
			return model.CarePlanVersion{}, err
		}
		if !allowed {
			return model.CarePlanVersion{}, ErrForbidden
		}
	}
	var published model.CarePlanVersion
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var appointment model.Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", appointmentID).First(&appointment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if appointment.Status != "confirmed" {
			return ErrCareState
		}
		var plan model.CarePlan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("appointment_id = ?", appointmentID).First(&plan).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCareState
			}
			return err
		}
		if plan.DraftJSON == nil || plan.DraftVersion != expectedVersion {
			return ErrConflict
		}
		var latest model.CarePlanVersion
		err := tx.Where("care_plan_id = ?", plan.ID).Order("version DESC").First(&latest).Error
		version := 1
		if err == nil {
			version = latest.Version + 1
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		published = model.CarePlanVersion{ID: randomID("plan-version-"), CarePlanID: plan.ID, Version: version, ContentJSON: *plan.DraftJSON, ChangeReason: reason, PublishedBy: employeeActor(ctx), PublishedAt: now}
		if err := tx.Create(&published).Error; err != nil {
			return err
		}
		if err := tx.Model(&plan).Updates(map[string]any{"draft_json": nil, "updated_at": now}).Error; err != nil {
			return err
		}
		return recordCareAccess(tx, appointmentID, appointment.PatientID, "care-plan", published.ID, "published", "clinician", employeeActor(ctx), now)
	})
	return published, err
}
