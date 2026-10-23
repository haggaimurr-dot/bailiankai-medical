package store

import (
	"context"
	"encoding/json"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type FollowupToolsRepository interface {
	FollowupTemplates(context.Context) ([]model.FollowupTemplate, error)
	SaveFollowupTemplate(context.Context, model.FollowupTemplate, time.Time) (model.FollowupTemplate, error)
	NursingFeedbacks(context.Context, string, DateRange) ([]model.NursingFeedback, error)
	RecordNursingFeedback(context.Context, model.NursingFeedback, time.Time) (model.NursingFeedback, error)
}

func (s *MySQL) FollowupTemplates(ctx context.Context) ([]model.FollowupTemplate, error) {
	rows := make([]model.FollowupTemplate, 0)
	err := clinicScope(ctx, s.db.WithContext(ctx), "clinic_id").Order("updated_at DESC, id").Find(&rows).Error
	return rows, err
}
func (s *MySQL) SaveFollowupTemplate(ctx context.Context, in model.FollowupTemplate, now time.Time) (model.FollowupTemplate, error) {
	scope, ok := scopeFrom(ctx)
	if !ok || scope.Role != "manager" {
		return in, ErrForbidden
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var clinic model.Clinic
		if err := clinicScope(ctx, tx, "id").First(&clinic, "id = ?", in.ClinicID).Error; err != nil {
			return followupDBError(err)
		}
		if in.ID == "" {
			in.ID = randomID("followup-template-")
			in.Version = 1
		} else {
			var old model.FollowupTemplate
			if err := clinicScope(ctx, tx, "clinic_id").Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "id = ?", in.ID).Error; err != nil {
				return followupDBError(err)
			}
			if old.Version != in.Version || old.ClinicID != in.ClinicID {
				return ErrConflict
			}
			in.Version++
		}
		in.UpdatedAt = now
		if err := tx.Save(&in).Error; err != nil {
			return err
		}
		snapshot, err := json.Marshal(in)
		if err != nil {
			return err
		}
		return tx.Create(&model.FollowupTemplateEvent{ID: randomID("template-event-"), TemplateID: in.ID, ActorID: employeeActor(ctx), ContentJSON: string(snapshot), CreatedAt: now}).Error
	})
	return in, err
}
func (s *MySQL) NursingFeedbacks(ctx context.Context, appointmentID string, dates DateRange) ([]model.NursingFeedback, error) {
	allowed, err := s.CanAccessAppointment(ctx, appointmentID, false)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrNotFound
	}
	rows := make([]model.NursingFeedback, 0)
	q := s.db.WithContext(ctx).Where("appointment_id = ?", appointmentID)
	if dates.Active() {
		q = q.Where("observed_at >= ? AND observed_at < ?", dates.From, dates.Until)
	}
	err = q.Order("observed_at DESC, id").Find(&rows).Error
	return rows, err
}
func (s *MySQL) RecordNursingFeedback(ctx context.Context, in model.NursingFeedback, now time.Time) (model.NursingFeedback, error) {
	scope, ok := scopeFrom(ctx)
	if !ok || scope.Role != "manager" && !ClinicalRole(scope.Role) {
		return in, ErrForbidden
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		bound := *s
		bound.db = tx
		var appointment model.Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&appointment, "id = ?", in.AppointmentID).Error; err != nil {
			return followupDBError(err)
		}
		allowed, err := bound.CanAccessAppointment(ctx, in.AppointmentID, false)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrNotFound
		}
		in.ActorID = employeeActor(ctx)
		var old model.NursingFeedback
		err = tx.Where("actor_id = ? AND request_key = ?", in.ActorID, in.RequestKey).First(&old).Error
		if err == nil {
			if old.AppointmentID != in.AppointmentID || !old.ObservedAt.Equal(in.ObservedAt) || old.Content != in.Content || old.Action != in.Action {
				return ErrConflict
			}
			in = old
			return nil
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		in.ID = randomID("nursing-feedback-")
		in.CreatedAt = now
		return tx.Create(&in).Error
	})
	return in, err
}
