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

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RescheduleInput struct {
	SlotID  string `json:"slotId"`
	Version int    `json:"version"`
}
type BookingExtensionsRepository interface {
	RescheduleAppointment(context.Context, string, string, string, RescheduleInput, time.Time) (model.Appointment, error)
	AppointmentTimeline(context.Context, string, string, time.Time) ([]model.AppointmentEvent, error)
}

func (s *MySQL) AppointmentTimeline(ctx context.Context, id, patientID string, now time.Time) ([]model.AppointmentEvent, error) {
	if _, err := s.GetAppointment(ctx, id, patientID, now); err != nil {
		return nil, err
	}
	events := []model.AppointmentEvent{}
	err := s.db.WithContext(ctx).Where("appointment_id = ?", id).Order("created_at, sequence").Find(&events).Error
	return events, err
}

// A waiting patient explicitly claims a vacancy. Moving a reservation and its
// audit event commit together; failed moves leave the original slot untouched.
func (s *MySQL) RescheduleAppointment(ctx context.Context, id, patientID, key string, in RescheduleInput, now time.Time) (model.Appointment, error) {
	started := time.Now()
	requestNow := now
	if _, err := s.ExpirePending(ctx, now); err != nil {
		return model.Appointment{}, err
	}
	initial, err := loadAppointment(s.db.WithContext(ctx), id, patientID)
	if err != nil {
		return model.Appointment{}, err
	}
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPatient(tx, patientID); err != nil {
			return err
		}
		var event model.AppointmentEvent
		err := tx.Where("appointment_id = ? AND request_key = ?", id, key).First(&event).Error
		if err == nil {
			if event.RequestHash != hash {
				return ErrIdempotency
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		ids := []string{initial.SlotID}
		if in.SlotID != initial.SlotID {
			ids = append(ids, in.SlotID)
		}
		sort.Strings(ids)
		slots := map[string]model.Slot{}
		for _, slotID := range ids {
			slot, err := lockSlot(tx, slotID)
			if err != nil {
				return err
			}
			slots[slotID] = slot
		}
		var row model.Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ? AND patient_id = ?", id, patientID).Error; err != nil {
			return err
		}
		if row.Version != in.Version || row.SlotID != initial.SlotID {
			return ErrConflict
		}
		target, original := slots[in.SlotID], slots[row.SlotID]
		if row.Status != "pending" && row.Status != "confirmed" && row.Status != "waiting" {
			return ErrTransition
		}
		if row.ArrivedAt != nil {
			return ErrTransition
		}
		var treatments int64
		if err := tx.Model(&model.Treatment{}).Where("appointment_id = ?", id).Count(&treatments).Error; err != nil {
			return err
		}
		if treatments > 0 {
			return ErrTransition
		}
		if row.Status != "waiting" && target.ID == original.ID {
			return ErrTransition
		}
		// Keep the doctor and clinic associated with existing clinical records.
		if target.DoctorID != original.DoctorID || target.ClinicID != original.ClinicID {
			return ErrTransition
		}
		var doctor model.Doctor
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&doctor, "id = ?", target.DoctorID).Error; err != nil {
			return err
		}
		var clinic model.Clinic
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&clinic, "id = ?", target.ClinicID).Error; err != nil {
			return err
		}
		now = requestNow.Add(time.Since(started)).In(database.Shanghai).Truncate(time.Millisecond)
		if now.Before(row.UpdatedAt) {
			now = row.UpdatedAt
		}
		if !original.StartsAt.After(now) || !target.StartsAt.After(now) || !row.ConfirmationDeadline.After(now) && row.Status == "pending" {
			return ErrTransition
		}
		if target.Status != "open" || !doctor.Active || doctor.PublishedJSON == nil || !clinic.Active || clinic.PublishedJSON == nil {
			return ErrSlotClosed
		}
		if target.Reserved >= target.Capacity {
			return ErrSlotFull
		}
		var duplicate int64
		if err := tx.Model(&model.Appointment{}).Where("patient_id = ? AND active_slot_id = ? AND id <> ?", patientID, target.ID, id).Count(&duplicate).Error; err != nil {
			return err
		}
		if duplicate > 0 {
			return ErrActiveAppointment
		}
		if row.Status != "waiting" {
			if err := releaseReservation(tx, original.ID); err != nil {
				return err
			}
		}
		result := tx.Model(&model.Slot{}).Where("id = ? AND reserved < capacity", target.ID).Updates(map[string]any{"reserved": gorm.Expr("reserved + 1"), "version": gorm.Expr("version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrSlotFull
		}
		deadline := now.Add(s.pendingConfirmation)
		if target.StartsAt.Before(deadline) {
			deadline = target.StartsAt
		}
		result = tx.Model(&model.Appointment{}).Where("id = ? AND version = ?", id, in.Version).Updates(map[string]any{"slot_id": target.ID, "status": "pending", "confirmation_deadline": deadline, "rejection_reason": "", "version": row.Version + 1, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		action := "reschedule"
		detail := fmt.Sprintf("%s → %s（北京时间）", original.StartsAt.In(database.Shanghai).Format("2006-01-02 15:04"), target.StartsAt.In(database.Shanghai).Format("2006-01-02 15:04"))
		if row.Status == "waiting" && original.ID == target.ID {
			action = "claim"
			detail = "候补转为待确认，名额已暂留"
		}
		actorType, actorID := "patient", patientID
		if employeeScoped(ctx) {
			actorType, actorID = "operator", employeeActor(ctx)
		}
		return tx.Create(&model.AppointmentEvent{ID: randomID("event-"), AppointmentID: id, ActorType: actorType, ActorID: actorID, Action: action, FromStatus: row.Status, ToStatus: "pending", CreatedAt: now, RequestKey: &key, RequestHash: hash, Detail: detail}).Error
	})
	if err != nil {
		return model.Appointment{}, err
	}
	return s.GetAppointment(ctx, id, patientID, requestNow.Add(time.Since(started)))
}
