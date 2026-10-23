package store_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

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
