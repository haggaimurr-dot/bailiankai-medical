package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrSlotOverlap = errors.New("schedule overlaps an existing slot")
var ErrScheduleInput = errors.New("invalid schedule input")

type ScheduleRepository interface {
	CreateSlots(context.Context, []model.Slot, time.Time) ([]model.Slot, error)
	SuspendSlots(context.Context, []SlotReference, string, time.Time) ([]model.Slot, error)
}
type SlotReference struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}
type ScheduleEvent struct {
	ID        string `gorm:"primaryKey"`
	SlotID    string
	ActorID   string
	Action    string
	Reason    string
	CreatedAt time.Time
}

func (s *MySQL) CreateSlots(ctx context.Context, inputs []model.Slot, now time.Time) ([]model.Slot, error) {
	if len(inputs) < 1 || len(inputs) > 500 {
		return nil, ErrScheduleInput
	}
	rows := append([]model.Slot(nil), inputs...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].DoctorID != rows[j].DoctorID {
			return rows[i].DoctorID < rows[j].DoctorID
		}
		return rows[i].StartsAt.Before(rows[j].StartsAt)
	})
	for i, row := range rows {
		if row.ClinicID == "" || row.DoctorID == "" || !row.EndsAt.After(row.StartsAt) || row.EndsAt.Sub(row.StartsAt) > 24*time.Hour || row.Capacity < 0 || row.Capacity > 100 || (row.Status != "open" && row.Status != "closed") {
			return nil, ErrScheduleInput
		}
		if !row.StartsAt.After(now) {
			return nil, ErrPast
		}
		if i > 0 && rows[i-1].DoctorID == row.DoctorID && rows[i-1].EndsAt.After(row.StartsAt) {
			return nil, ErrSlotOverlap
		}
	}
	started := time.Now()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize creation for each doctor, including when no slots yet exist.
		locked := map[string]string{}
		for _, row := range rows {
			if clinic, ok := locked[row.DoctorID]; ok {
				if clinic != row.ClinicID {
					return ErrNotFound
				}
				continue
			}
			var doctor model.Doctor
			if err := clinicScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"}), "clinic_id").Where("id = ? AND clinic_id = ? AND active = TRUE AND published_json IS NOT NULL", row.DoctorID, row.ClinicID).First(&doctor).Error; err != nil {
				return followupDBError(err)
			}
			var clinic model.Clinic
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND active = TRUE AND published_json IS NOT NULL", row.ClinicID).First(&clinic).Error; err != nil {
				return followupDBError(err)
			}
			locked[row.DoctorID] = row.ClinicID
		}
		for i := range rows {
			row := &rows[i]
			if err := scheduleHolidayCheck(tx, *row); err != nil {
				return err
			}
			if !row.StartsAt.After(now.Add(time.Since(started))) {
				return ErrPast
			}
			// A locking read observes any creation committed while waiting for the doctor lock.
			var overlaps []model.Slot
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("doctor_id = ? AND starts_at < ? AND ends_at > ?", row.DoctorID, row.EndsAt, row.StartsAt).Find(&overlaps).Error; err != nil {
				return err
			}
			if len(overlaps) > 0 {
				return ErrSlotOverlap
			}
			row.ID, row.Reserved, row.Version = randomID("slot-"), 0, 1
			if err := tx.Create(row).Error; err != nil {
				return err
			}
			if err := tx.Create(&ScheduleEvent{ID: randomID("schedule-"), SlotID: row.ID, ActorID: employeeActor(ctx), Action: "created", CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *MySQL) SuspendSlots(ctx context.Context, refs []SlotReference, reason string, now time.Time) ([]model.Slot, error) {
	reason = strings.TrimSpace(reason)
	if len(refs) < 1 || len(refs) > 500 || utf8.RuneCountInString(reason) < 1 || utf8.RuneCountInString(reason) > 200 {
		return nil, ErrScheduleInput
	}
	refs = append([]SlotReference(nil), refs...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	for i, ref := range refs {
		if ref.ID == "" || ref.Version < 1 || (i > 0 && refs[i-1].ID == ref.ID) {
			return nil, ErrScheduleInput
		}
	}
	started := time.Now()
	rows := make([]model.Slot, 0, len(refs))
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, ref := range refs {
			var row model.Slot
			if err := clinicScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"}), "clinic_id").First(&row, "id = ?", ref.ID).Error; err != nil {
				return followupDBError(err)
			}
			if row.Version != ref.Version {
				return ErrConflict
			}
			if !row.StartsAt.After(now.Add(time.Since(started))) {
				return ErrPast
			}
			if row.Status == "closed" {
				return ErrConflict
			}
			if err := tx.Model(&row).Updates(map[string]any{"status": "closed", "version": row.Version + 1}).Error; err != nil {
				return err
			}
			if err := tx.Create(&ScheduleEvent{ID: randomID("schedule-"), SlotID: row.ID, ActorID: employeeActor(ctx), Action: "suspended", Reason: reason, CreatedAt: now}).Error; err != nil {
				return err
			}
			if err := createSchedulePatientTasks(tx, ctx, row.ID, reason, now); err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}
