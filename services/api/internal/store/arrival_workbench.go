package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrCheckInCode = errors.New("invalid or expired check-in code")

type CheckInCode struct {
	TokenHash          string `gorm:"primaryKey;size:64"`
	AppointmentID      string
	AppointmentVersion int
	ExpiresAt          time.Time
	UsedAt             *time.Time
}

type AdminRescheduleInput struct {
	SlotID     string `json:"slotId"`
	Version    int    `json:"version"`
	Reason     string `json:"reason"`
	RequestKey string `json:"requestKey"`
}

type ArrivalWorkbenchRepository interface {
	NewCheckInCode(context.Context, string, string, time.Time) (string, time.Time, error)
	ResolveCheckInCode(context.Context, string, time.Time) (model.Appointment, error)
	AdminRescheduleAppointment(context.Context, string, AdminRescheduleInput, time.Time) (model.Appointment, error)
}

func (s *MySQL) NewCheckInCode(ctx context.Context, id, patientID string, now time.Time) (string, time.Time, error) {
	row, err := s.GetAppointment(ctx, id, patientID, now)
	if err != nil {
		return "", time.Time{}, err
	}
	if row.Status != "confirmed" || row.ArrivedAt != nil || row.TreatmentRecorded || row.SlotStatus != "open" || !sameShanghaiDay(row.StartsAt, now) {
		return "", time.Time{}, ErrTransition
	}
	token := "BLK-CHECKIN:" + randomID("") + randomID("")
	expires := now.Add(5 * time.Minute)
	err = s.db.WithContext(ctx).Create(&CheckInCode{TokenHash: tokenHash(token), AppointmentID: id, AppointmentVersion: row.Version, ExpiresAt: expires}).Error
	return token, expires, err
}

func sameShanghaiDay(a, b time.Time) bool {
	return a.In(database.Shanghai).Format("2006-01-02") == b.In(database.Shanghai).Format("2006-01-02")
}

func (s *MySQL) ResolveCheckInCode(ctx context.Context, token string, now time.Time) (model.Appointment, error) {
	var code CheckInCode
	if err := s.db.WithContext(ctx).First(&code, "token_hash = ?", tokenHash(token)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Appointment{}, ErrCheckInCode
		}
		return model.Appointment{}, err
	}
	if !code.ExpiresAt.After(now) {
		return model.Appointment{}, ErrCheckInCode
	}
	row, err := s.GetAppointment(ctx, code.AppointmentID, "", now)
	if err != nil {
		return model.Appointment{}, err
	}
	if code.UsedAt != nil && row.ArrivedAt != nil {
		return row, nil
	}
	if row.Version != code.AppointmentVersion || row.Status != "confirmed" || row.ArrivedAt != nil || row.SlotStatus != "open" || !sameShanghaiDay(row.StartsAt, now) {
		return model.Appointment{}, ErrCheckInCode
	}
	return row, nil
}

// Patient -> sorted slots -> appointment matches the patient rescheduling lock
// order. Counter changes, appointment update and audit commit as one unit.
func (s *MySQL) AdminRescheduleAppointment(ctx context.Context, id string, in AdminRescheduleInput, now time.Time) (model.Appointment, error) {
	if scope, ok := scopeFrom(ctx); ok && scope.Role != "manager" && scope.Role != "customer_service" {
		return model.Appointment{}, ErrForbidden
	}
	initial, err := s.GetAppointment(ctx, id, "", now)
	if err != nil {
		return model.Appointment{}, err
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Version < 1 || utf8.RuneCountInString(in.Reason) < 1 || utf8.RuneCountInString(in.Reason) > 200 || in.RequestKey == "" {
		return model.Appointment{}, ErrScheduleInput
	}
	started := time.Now()
	requestNow := now
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPatient(tx, initial.PatientID); err != nil {
			return err
		}
		hash := requestHash(AppointmentInput{SlotID: in.SlotID, Note: in.Reason, ContactName: "admin-reschedule"})
		var event model.AppointmentEvent
		e := tx.Where("appointment_id = ? AND request_key = ?", id, in.RequestKey).First(&event).Error
		if e == nil {
			if event.RequestHash != hash {
				return ErrIdempotency
			}
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		ids := []string{initial.SlotID, in.SlotID}
		sort.Strings(ids)
		if ids[0] == ids[1] {
			return ErrTransition
		}
		slots := map[string]model.Slot{}
		for _, slotID := range ids {
			slot, e := lockSlot(tx, slotID)
			if e != nil {
				return e
			}
			slots[slotID] = slot
		}
		var row model.Appointment
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; e != nil {
			return e
		}
		if row.Version != in.Version || row.SlotID != initial.SlotID {
			return ErrConflict
		}
		original, target := slots[row.SlotID], slots[in.SlotID]
		now = now.Add(time.Since(started)).In(database.Shanghai).Truncate(time.Millisecond)
		if original.Status != "closed" || (row.Status != "pending" && row.Status != "confirmed") || row.ArrivedAt != nil {
			return ErrTransition
		}
		var count int64
		if e := tx.Model(&model.Treatment{}).Where("appointment_id = ?", id).Count(&count).Error; e != nil {
			return e
		}
		if count > 0 {
			return ErrTransition
		}
		if target.DoctorID != original.DoctorID || target.ClinicID != original.ClinicID {
			return ErrTransition
		}
		if scope, ok := scopeFrom(ctx); ok && scope.EnforceClinics {
			found := false
			for _, clinic := range scope.ClinicIDs {
				if clinic == target.ClinicID {
					found = true
				}
			}
			if !found {
				return ErrForbidden
			}
		}
		var doctor model.Doctor
		var clinic model.Clinic
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&doctor, "id = ?", target.DoctorID).Error; e != nil {
			return e
		}
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&clinic, "id = ?", target.ClinicID).Error; e != nil {
			return e
		}
		now = requestNow.Add(time.Since(started)).In(database.Shanghai).Truncate(time.Millisecond)
		if !target.StartsAt.After(now) {
			return ErrPast
		}
		if target.Status != "open" || !doctor.Active || doctor.PublishedJSON == nil || !clinic.Active || clinic.PublishedJSON == nil {
			return ErrSlotClosed
		}
		if target.Reserved >= target.Capacity {
			return ErrSlotFull
		}
		if e := tx.Model(&model.Appointment{}).Where("patient_id = ? AND active_slot_id = ? AND id <> ?", row.PatientID, target.ID, id).Count(&count).Error; e != nil {
			return e
		}
		if count > 0 {
			return ErrActiveAppointment
		}
		if e := releaseReservation(tx, original.ID); e != nil {
			return e
		}
		if e := tx.Model(&model.Slot{}).Where("id = ?", target.ID).Updates(map[string]any{"reserved": gorm.Expr("reserved + 1"), "version": gorm.Expr("version + 1")}).Error; e != nil {
			return e
		}
		deadline := now.Add(s.pendingConfirmation)
		if target.StartsAt.Before(deadline) {
			deadline = target.StartsAt
		}
		if e := tx.Model(&row).Updates(map[string]any{"slot_id": target.ID, "confirmation_deadline": deadline, "version": row.Version + 1, "updated_at": now}).Error; e != nil {
			return e
		}
		detail := original.StartsAt.In(database.Shanghai).Format("2006-01-02 15:04") + " → " + target.StartsAt.In(database.Shanghai).Format("2006-01-02 15:04") + "；" + in.Reason
		return tx.Create(&model.AppointmentEvent{ID: randomID("event-"), AppointmentID: id, ActorType: "operator", ActorID: employeeActor(ctx), Action: "suspend-move", FromStatus: initial.Status, ToStatus: initial.Status, CreatedAt: now, Detail: detail, RequestKey: &in.RequestKey, RequestHash: hash}).Error
	})
	if err != nil {
		return model.Appointment{}, err
	}
	return s.GetAppointment(ctx, id, "", now)
}
