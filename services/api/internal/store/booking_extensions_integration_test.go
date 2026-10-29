package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func TestBookingExtensions(t *testing.T) {
	t.Run("patient HTTP waitlist claim timeline and ownership", func(t *testing.T) {
		f := newBookingFixture(t)
		if err := f.db.Model(&f.slot).Update("capacity", 1).Error; err != nil {
			t.Fatal(err)
		}
		owner, candidate, other := f.patient(), f.patient(), f.patient()
		booked := f.create(owner, "http-occupied-0001")
		body := fmt.Sprintf(`{"slotId":%q,"contactName":"演示候补","contactPhone":"13800000000","waitlist":true}`, f.slot.ID)
		response := f.http("POST", "/api/v1/me/appointments", body, candidate.Token, "http-waiting-0001")
		requireHTTP(t, response, 201, "")
		var result struct {
			Data struct {
				ID      string `json:"id"`
				Status  string `json:"status"`
				Version int    `json:"version"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Data.Status != "waiting" {
			t.Fatal(response.Body.String())
		}
		path := "/api/v1/me/appointments/" + result.Data.ID
		claim := fmt.Sprintf(`{"slotId":%q,"version":%d}`, f.slot.ID, result.Data.Version)
		requireHTTP(t, f.http("GET", path+"/timeline", "", other.Token, ""), 404, "APPOINTMENT_NOT_FOUND")
		requireHTTP(t, f.http("POST", path+"/reschedule", claim, other.Token, "http-claim-key-0001"), 404, "APPOINTMENT_NOT_FOUND")
		requireHTTP(t, f.http("POST", path+"/reschedule", claim, candidate.Token, "http-claim-key-0001"), 409, "SLOT_FULL")
		if _, err := f.repo.CancelAppointment(context.Background(), booked.ID, owner.Patient.ID, booked.Version, "", "", f.now); err != nil {
			t.Fatal(err)
		}
		requireHTTP(t, f.http("POST", path+"/reschedule", claim, candidate.Token, "http-claim-key-0001"), 200, "")
		requireHTTP(t, f.http("POST", path+"/reschedule", claim, candidate.Token, "http-claim-key-0001"), 200, "")
		response = f.http("GET", path+"/timeline", "", candidate.Token, "")
		requireHTTP(t, response, 200, "")
		if !strings.Contains(response.Body.String(), `"action":"claim"`) || strings.Contains(response.Body.String(), "actorId") || strings.Contains(response.Body.String(), "requestKey") {
			t.Fatal(response.Body.String())
		}
		f.assertReserved(1)
	})
	t.Run("atomic reschedule rollback replay ownership and timeline", func(t *testing.T) {
		f := newBookingFixture(t)
		ctx := context.Background()
		patient, other := f.patient(), f.patient()
		original := f.create(patient, "original-booking-0001")
		original, err := f.repo.ReviewAppointment(ctx, original.ID, store.ReviewInput{Action: "confirm", Version: original.Version}, f.now)
		if err != nil {
			t.Fatal(err)
		}
		var target model.Slot
		if err := f.db.Where("doctor_id = ? AND starts_at > ? AND id <> ?", f.slot.DoctorID, f.now.Add(3*time.Hour), f.slot.ID).Order("starts_at").First(&target).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.db.Model(&target).Updates(map[string]any{"capacity": 1, "reserved": 1}).Error; err != nil {
			t.Fatal(err)
		}
		in := store.RescheduleInput{SlotID: target.ID, Version: original.Version}
		if _, err := f.repo.RescheduleAppointment(ctx, original.ID, patient.Patient.ID, "reschedule-test-0001", in, f.now); !errors.Is(err, store.ErrSlotFull) {
			t.Fatalf("full target: %v", err)
		}
		f.assertReserved(1)
		unchanged, err := f.repo.GetAppointment(ctx, original.ID, patient.Patient.ID, f.now)
		if err != nil || unchanged.SlotID != original.SlotID || unchanged.Version != original.Version {
			t.Fatalf("failed move changed original: %+v %v", unchanged, err)
		}
		if err := f.db.Model(&target).Update("reserved", 0).Error; err != nil {
			t.Fatal(err)
		}
		moved, err := f.repo.RescheduleAppointment(ctx, original.ID, patient.Patient.ID, "reschedule-test-0001", in, f.now)
		if err != nil || moved.SlotID != target.ID || moved.Status != "pending" || moved.Version != original.Version+1 {
			t.Fatalf("move: %+v %v", moved, err)
		}
		f.assertReserved(0)
		replay, err := f.repo.RescheduleAppointment(ctx, original.ID, patient.Patient.ID, "reschedule-test-0001", in, f.now)
		if err != nil || replay.Version != moved.Version {
			t.Fatalf("replay: %+v %v", replay, err)
		}
		in.Version++
		if _, err := f.repo.RescheduleAppointment(ctx, original.ID, patient.Patient.ID, "reschedule-test-0001", in, f.now); !errors.Is(err, store.ErrIdempotency) {
			t.Fatalf("changed replay: %v", err)
		}
		if _, err := f.repo.AppointmentTimeline(ctx, original.ID, other.Patient.ID, f.now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("timeline ownership: %v", err)
		}
		events, err := f.repo.AppointmentTimeline(ctx, original.ID, patient.Patient.ID, f.now)
		if err != nil || len(events) != 3 || events[0].Action != "submit" || events[1].Action != "confirm" || events[2].Action != "reschedule" || events[2].Detail == "" {
			t.Fatalf("timeline: %+v %v", events, err)
		}
		if _, err := f.repo.CancelAppointment(ctx, moved.ID, patient.Patient.ID, original.Version, "", "", f.now); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("stale version: %v", err)
		}
		if _, err := f.repo.CancelAppointment(ctx, moved.ID, patient.Patient.ID, moved.Version, "", "", f.now); err != nil {
			t.Fatal(err)
		}
		if err := f.db.First(&target, "id = ?", target.ID).Error; err != nil || target.Reserved != 0 {
			t.Fatalf("cancel moved slot: %+v %v", target, err)
		}
	})
	t.Run("waiting consumes no reservation and only one patient claims vacancy", func(t *testing.T) {
		f := newBookingFixture(t)
		ctx := context.Background()
		if err := f.db.Model(&f.slot).Update("capacity", 1).Error; err != nil {
			t.Fatal(err)
		}
		owner := f.patient()
		booked := f.create(owner, "occupied-booking-0001")
		patients := []store.SessionResult{f.patient(), f.patient()}
		waiting := make([]model.Appointment, 2)
		for i, patient := range patients {
			in := f.input()
			in.Waitlist = true
			row, _, err := f.repo.CreateAppointment(ctx, patient.Patient.ID, fmt.Sprintf("waitlist-request-%04d", i), in, f.now)
			if err != nil || row.Status != "waiting" {
				t.Fatalf("waitlist: %+v %v", row, err)
			}
			waiting[i] = row
		}
		f.assertReserved(1)
		if _, err := f.repo.CancelAppointment(ctx, booked.ID, owner.Patient.ID, booked.Version, "", "", f.now); err != nil {
			t.Fatal(err)
		}
		f.assertReserved(0)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range patients {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, errs[i] = f.repo.RescheduleAppointment(ctx, waiting[i].ID, patients[i].Patient.ID, fmt.Sprintf("claim-request-%04d", i), store.RescheduleInput{SlotID: f.slot.ID, Version: waiting[i].Version}, f.now)
			}(i)
		}
		wg.Wait()
		successes := 0
		for _, err := range errs {
			if err == nil {
				successes++
			} else if !errors.Is(err, store.ErrSlotFull) {
				t.Fatal(err)
			}
		}
		if successes != 1 {
			t.Fatalf("claim successes=%d errors=%v", successes, errs)
		}
		f.assertReserved(1)
	})
	t.Run("cancel and expire waiting do not release occupied capacity", func(t *testing.T) {
		f := newBookingFixture(t)
		ctx := context.Background()
		if err := f.db.Model(&f.slot).Update("capacity", 1).Error; err != nil {
			t.Fatal(err)
		}
		f.create(f.patient(), "keep-capacity-0001")
		patient := f.patient()
		in := f.input()
		in.Waitlist = true
		row, _, err := f.repo.CreateAppointment(ctx, patient.Patient.ID, "cancel-waiting-0001", in, f.now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.CancelAppointment(ctx, row.ID, patient.Patient.ID, row.Version, "", "", f.now); err != nil {
			t.Fatal(err)
		}
		f.assertReserved(1)
		row, _, err = f.repo.CreateAppointment(ctx, patient.Patient.ID, "expire-waiting-0001", in, f.now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.ExpirePending(ctx, f.slot.StartsAt); err != nil {
			t.Fatal(err)
		}
		saved, err := f.repo.GetAppointment(ctx, row.ID, patient.Patient.ID, f.slot.StartsAt)
		if err != nil || saved.Status != "expired" {
			t.Fatalf("waiting expiry: %+v %v", saved, err)
		}
		f.assertReserved(0) // Only the original pending reservation is released.
	})
}
