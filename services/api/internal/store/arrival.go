package store

import (
	"context"
	"errors"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrArrivalTime = errors.New("invalid actual arrival time")

type ArrivalInput struct {
	CheckInToken string
	Version      int
	ArrivedAt    time.Time
}

type ArrivalRepository interface {
	RegisterArrival(context.Context, string, ArrivalInput, time.Time) (model.Appointment, error)
}

// RegisterArrival records a verified visit independently from booking status.
// The same recorded time is an idempotent retry, including after treatment.
func (s *MySQL) RegisterArrival(ctx context.Context, id string, input ArrivalInput, now time.Time) (model.Appointment, error) {
	if scope, ok := scopeFrom(ctx); ok {
		if scope.Role != "manager" && scope.Role != "customer_service" && scope.Role != "doctor" && scope.Role != "clinician" {
			return model.Appointment{}, ErrForbidden
		}
		allowed, err := s.CanAccessAppointment(ctx, id, false)
		if err != nil {
			return model.Appointment{}, err
		}
		if !allowed {
			return model.Appointment{}, ErrNotFound
		}
	}
	now = now.In(database.Shanghai).Truncate(time.Millisecond)
	arrivedAt := input.ArrivedAt.In(database.Shanghai).Truncate(time.Millisecond)
	if input.Version < 1 || input.ArrivedAt.IsZero() || arrivedAt.After(now) {
		return model.Appointment{}, ErrArrivalTime
	}
	initial, err := loadAppointment(s.db.WithContext(ctx), id, "")
	if err != nil {
		return model.Appointment{}, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Use the booking/treatment lock order to serialize arrival with cancellation.
		if err := lockPatient(tx, initial.PatientID); err != nil {
			return err
		}
		if _, err := lockSlot(tx, initial.SlotID); err != nil {
			return err
		}
		var row model.Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if row.SlotID != initial.SlotID {
			return ErrConflict
		}
		if input.CheckInToken != "" {
			var code CheckInCode
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&code, "token_hash = ?", tokenHash(input.CheckInToken)).Error; err != nil {
				return ErrCheckInCode
			}
			if code.AppointmentID != id || !code.ExpiresAt.After(now) {
				return ErrCheckInCode
			}
			if code.UsedAt != nil && row.ArrivedAt != nil {
				return nil
			}
			var slot model.Slot
			if err := tx.First(&slot, "id = ?", row.SlotID).Error; err != nil {
				return err
			}
			if row.Version != code.AppointmentVersion || row.Status != "confirmed" || slot.Status != "open" || !sameShanghaiDay(slot.StartsAt, now) {
				return ErrCheckInCode
			}
			if err := tx.Model(&code).Update("used_at", arrivedAt).Error; err != nil {
				return err
			}
		}
		if row.ArrivedAt != nil {
			if row.ArrivedAt.Equal(arrivedAt) {
				return nil
			}
			return ErrConflict
		}
		if row.Status != "confirmed" && row.Status != "completed" {
			return ErrTransition
		}
		if row.Version != input.Version {
			return ErrConflict
		}
		if arrivedAt.Before(row.CreatedAt) {
			return ErrArrivalTime
		}
		var treatment model.Treatment
		err := tx.Where("appointment_id = ?", id).First(&treatment).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && arrivedAt.After(treatment.TreatedAt) {
			return ErrArrivalTime
		}
		actor := employeeActor(ctx)
		result := tx.Model(&model.Appointment{}).Where("id = ? AND version = ?", id, row.Version).
			Updates(map[string]any{"arrived_at": arrivedAt, "arrived_by": actor, "version": row.Version + 1, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return writeEvent(tx, row, "operator", actor, "arrive", row.Status, now)
	})
	if err != nil {
		return model.Appointment{}, err
	}
	return loadAppointment(s.db.WithContext(ctx), id, "")
}
