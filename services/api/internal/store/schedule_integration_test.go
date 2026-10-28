package store_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func TestScheduleCreationAtomicityAndConcurrency(t *testing.T) {
	f := newBookingFixture(t)
	ctx := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "manager", "", []string{f.slot.ClinicID}), "schedule-manager")
	input := model.Slot{ClinicID: f.slot.ClinicID, DoctorID: f.slot.DoctorID, StartsAt: f.now.AddDate(0, 0, 15), EndsAt: f.now.AddDate(0, 0, 15).Add(30 * time.Minute), Capacity: 3, Status: "open"}
	next := input
	next.StartsAt = input.EndsAt
	next.EndsAt = next.StartsAt.Add(30 * time.Minute)
	rows, err := f.repo.CreateSlots(ctx, []model.Slot{input, next}, f.now)
	if err != nil || len(rows) != 2 {
		t.Fatalf("create: %v %v", rows, err)
	}
	partial := input
	partial.StartsAt = input.StartsAt.Add(15 * time.Minute)
	partial.EndsAt = input.EndsAt.Add(15 * time.Minute)
	if _, err = f.repo.CreateSlots(ctx, []model.Slot{partial}, f.now); !errors.Is(err, store.ErrSlotOverlap) {
		t.Fatalf("partial overlap: %v", err)
	}
	past := input
	past.StartsAt = f.now
	past.EndsAt = f.now.Add(time.Hour)
	if _, err = f.repo.CreateSlots(ctx, []model.Slot{past}, f.now); !errors.Is(err, store.ErrPast) {
		t.Fatalf("past: %v", err)
	}
	// An earlier new row must roll back when a later row overlaps.
	earlier := input
	earlier.StartsAt = input.StartsAt.Add(-time.Hour)
	earlier.EndsAt = earlier.StartsAt.Add(30 * time.Minute)
	if _, err = f.repo.CreateSlots(ctx, []model.Slot{earlier, input}, f.now); !errors.Is(err, store.ErrSlotOverlap) {
		t.Fatalf("overlap: %v", err)
	}
	var count int64
	f.db.Model(&model.Slot{}).Where("doctor_id = ? AND starts_at = ?", earlier.DoctorID, earlier.StartsAt).Count(&count)
	if count != 0 {
		t.Fatal("partial batch was committed")
	}
	unauthorized := store.WithEmployeeScope(context.Background(), "manager", "", []string{"other-clinic"})
	if _, err = f.repo.CreateSlots(unauthorized, []model.Slot{earlier}, f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("scope: %v", err)
	}
	// Concurrent creation of overlapping rows allows exactly one commit.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.repo.CreateSlots(ctx, []model.Slot{earlier}, f.now)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, store.ErrSlotOverlap) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
}

func TestScheduleSuspensionPreservesBookingsAndRollsBack(t *testing.T) {
	f := newBookingFixture(t)
	patient := f.patient()
	appointment := f.create(patient, "schedule-booking-preserved")
	ctx := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "manager", "", []string{f.slot.ClinicID}), "schedule-manager")
	var slot model.Slot
	f.db.First(&slot, "id = ?", f.slot.ID)
	extra := slot
	extra.ID = ""
	extra.StartsAt = f.now.AddDate(0, 0, 15)
	extra.EndsAt = extra.StartsAt.Add(time.Hour)
	created, err := f.repo.CreateSlots(ctx, []model.Slot{extra}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	refs := []store.SlotReference{{ID: slot.ID, Version: slot.Version}, {ID: created[0].ID, Version: created[0].Version}}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	latest := refs[1].Version
	refs[1].Version = 99
	if _, err = f.repo.SuspendSlots(ctx, refs, "临时停诊", f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("conflict: %v", err)
	}
	var unchanged model.Slot
	f.db.First(&unchanged, "id = ?", slot.ID)
	if unchanged.Status != "open" || unchanged.Version != slot.Version {
		t.Fatal("partial suspension committed")
	}
	var extraUnchanged model.Slot
	f.db.First(&extraUnchanged, "id = ?", created[0].ID)
	if extraUnchanged.Status != "open" || extraUnchanged.Version != created[0].Version {
		t.Fatal("partial suspension committed new slot")
	}
	refs[1].Version = latest
	saved, err := f.repo.SuspendSlots(ctx, refs, " 临时停诊 ", f.now)
	if err != nil || len(saved) != 2 {
		t.Fatalf("suspend: %v %v", saved, err)
	}
	f.db.First(&unchanged, "id = ?", slot.ID)
	if unchanged.Status != "closed" || unchanged.Reserved != 1 || unchanged.Version != slot.Version+1 {
		t.Fatalf("slot changed incorrectly: %+v", unchanged)
	}
	var current model.Appointment
	f.db.First(&current, "id = ?", appointment.ID)
	if current.Status != appointment.Status || current.Version != appointment.Version {
		t.Fatal("booking changed by suspension")
	}
	var events []store.ScheduleEvent
	f.db.Where("action = ?", "suspended").Find(&events)
	if len(events) != 2 || events[0].Reason != "临时停诊" || events[0].ActorID != "schedule-manager" {
		t.Fatalf("audit: %+v", events)
	}
	other := f.patient()
	if _, _, err = f.repo.CreateAppointment(context.Background(), other.Patient.ID, "schedule-closed-booking", f.input(), f.now); !errors.Is(err, store.ErrSlotClosed) {
		t.Fatalf("closed slot accepted new booking: %v", err)
	}
}

func TestScheduleBatchPreviewAndPartialCreate(t *testing.T) {
	f := newBookingFixture(t)
	ctx := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "manager", "", []string{f.slot.ClinicID}), "schedule-manager")
	day := f.slot.StartsAt.In(database.Shanghai).Format("2006-01-02")
	clock := f.slot.StartsAt.In(database.Shanghai).Format("15:04")
	grid := store.SlotBatchGrid{ClinicID: f.slot.ClinicID, DoctorID: f.slot.DoctorID, StartDate: day, EndDate: day, StartTimes: []string{clock, "23:30"}, DurationMinutes: 30, Capacity: 3}
	toCreate, conflicts, err := f.repo.PreviewBatchSlots(ctx, grid, f.now)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(toCreate) != 1 || len(conflicts) != 1 {
		t.Fatalf("preview toCreate=%d conflicts=%d", len(toCreate), len(conflicts))
	}
	if conflicts[0].Reason != store.SlotConflictOverlap || conflicts[0].ExistingSlotID != f.slot.ID || conflicts[0].ExistingCapacity != f.slot.Capacity {
		t.Fatalf("conflict=%+v want overlap of %s", conflicts[0], f.slot.ID)
	}
	created, skipped, err := f.repo.CreateBatchSlots(ctx, grid, f.now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(created) != 1 || len(skipped) != 1 || skipped[0].Reason != store.SlotConflictOverlap {
		t.Fatalf("created=%d skipped=%d %+v", len(created), len(skipped), skipped)
	}
	if created[0].Capacity != 3 || created[0].Reserved != 0 || created[0].Status != "open" {
		t.Fatalf("created slot=%+v", created[0])
	}
	// The existing slot must be untouched: no capacity change, no version bump.
	after := f.savedSlot()
	if after.Capacity != f.slot.Capacity || after.Reserved != f.slot.Reserved || after.Version != f.slot.Version {
		t.Fatalf("existing slot changed: %+v want %+v", after, f.slot)
	}
	// A second run creates nothing: both drafts now overlap.
	created, skipped, err = f.repo.CreateBatchSlots(ctx, grid, f.now)
	if err != nil || len(created) != 0 || len(skipped) != 2 {
		t.Fatalf("second create: created=%d skipped=%d err=%v", len(created), len(skipped), err)
	}
	// Past dates are conflicts, never created.
	yesterday := f.now.AddDate(0, 0, -1).Format("2006-01-02")
	past := store.SlotBatchGrid{ClinicID: f.slot.ClinicID, DoctorID: f.slot.DoctorID, StartDate: yesterday, EndDate: yesterday, StartTimes: []string{"09:00"}, DurationMinutes: 30, Capacity: 3}
	_, pastConflicts, err := f.repo.PreviewBatchSlots(ctx, past, f.now)
	if err != nil || len(pastConflicts) != 1 || pastConflicts[0].Reason != store.SlotConflictPast {
		t.Fatalf("past preview: conflicts=%+v err=%v", pastConflicts, err)
	}
	created, skipped, err = f.repo.CreateBatchSlots(ctx, past, f.now)
	if err != nil || len(created) != 0 || len(skipped) != 1 {
		t.Fatalf("past create: created=%d skipped=%d err=%v", len(created), len(skipped), err)
	}
	// Invalid grids are rejected wholesale.
	for _, bad := range []store.SlotBatchGrid{
		{ClinicID: f.slot.ClinicID, DoctorID: f.slot.DoctorID, StartDate: day, EndDate: f.now.AddDate(0, 0, -1).Format("2006-01-02"), StartTimes: []string{"09:00"}, DurationMinutes: 30, Capacity: 3},
		{ClinicID: f.slot.ClinicID, DoctorID: f.slot.DoctorID, StartDate: day, EndDate: day, StartTimes: []string{"09:00"}, DurationMinutes: 30, Capacity: 0},
		{ClinicID: f.slot.ClinicID, DoctorID: f.slot.DoctorID, StartDate: day, EndDate: day, StartTimes: []string{"25:00"}, DurationMinutes: 30, Capacity: 3},
	} {
		if _, _, err := f.repo.PreviewBatchSlots(ctx, bad, f.now); !errors.Is(err, store.ErrScheduleInput) {
			t.Fatalf("bad grid %+v err=%v", bad, err)
		}
	}
}
