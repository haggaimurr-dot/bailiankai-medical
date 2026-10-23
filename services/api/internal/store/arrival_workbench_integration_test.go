package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func TestCheckInHTTPPatientOwnershipAndCustomerServiceConfirmation(t *testing.T) {
	f, patient, row := confirmedArrivalFixture(t)
	if err := f.db.Model(&model.Slot{}).Where("id = ?", row.SlotID).Update("starts_at", f.now.Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	ctx := store.WithClinicAssignment(context.Background(), []string{row.ClinicID})
	account, err := f.repo.CreateEmployee(ctx, "checkin-customer", "到诊客服", "CheckInTestPassword123", "customer_service", "", f.now)
	if err != nil {
		t.Fatal(err)
	}
	_, employeeToken, err := f.repo.LoginEmployee(context.Background(), account.Username, "CheckInTestPassword123", f.now)
	if err != nil {
		t.Fatal(err)
	}
	codeResponse := f.http(http.MethodPost, "/api/v1/me/appointments/"+row.ID+"/check-in-code", "{}", patient.Token, "")
	if codeResponse.Code != 200 {
		t.Fatalf("code: %d %s", codeResponse.Code, codeResponse.Body.String())
	}
	var payload struct {
		Data struct {
			Token string `json:"token"`
			Image string `json:"imageDataUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(codeResponse.Body.Bytes(), &payload); err != nil || len(payload.Data.Image) < 100 {
		t.Fatal("missing QR image", err)
	}
	raw, _ := json.Marshal(map[string]string{"token": payload.Data.Token})
	for _, action := range []string{"resolve", "confirm", "confirm"} {
		response := f.http(http.MethodPost, "/api/v1/admin/arrival-scans/"+action, string(raw), employeeToken, "")
		if response.Code != 200 {
			t.Fatalf("%s: %d %s", action, response.Code, response.Body.String())
		}
	}
	if got := arrivalAuditCount(t, f, row.ID); got != 1 {
		t.Fatalf("audit count %d", got)
	}
	filtered := f.http(http.MethodGet, "/api/v1/admin/appointments?arrival=arrived&clinicId="+row.ClinicID, "", employeeToken, "")
	if filtered.Code != 200 {
		t.Fatalf("workbench filters: %d %s", filtered.Code, filtered.Body.String())
	}
}

func TestCheckInCodeScopeExpiryAndConcurrentRetry(t *testing.T) {
	f, patient, row := confirmedArrivalFixture(t)
	// Set the booked visit to today, preserving a future start even near midnight.
	todayStart := f.now.Add(-time.Minute)
	if err := f.db.Model(&model.Slot{}).Where("id = ?", row.SlotID).Updates(map[string]any{"starts_at": todayStart, "ends_at": f.now.Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	token, expires, err := f.repo.NewCheckInCode(context.Background(), row.ID, patient.Patient.ID, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.repo.NewCheckInCode(context.Background(), row.ID, "other-patient", f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ownership: %v", err)
	}
	actor := store.WithEmployeeScope(context.Background(), "customer_service", "", []string{row.ClinicID})
	if _, err := f.repo.ResolveCheckInCode(actor, token, expires); !errors.Is(err, store.ErrCheckInCode) {
		t.Fatalf("expired: %v", err)
	}
	outsider := store.WithEmployeeScope(context.Background(), "customer_service", "", []string{"outside-clinic"})
	if _, err := f.repo.ResolveCheckInCode(outsider, token, f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("scope: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.repo.RegisterArrival(actor, row.ID, store.ArrivalInput{Version: row.Version, ArrivedAt: f.now, CheckInToken: token}, f.now)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := arrivalAuditCount(t, f, row.ID); got != 1 {
		t.Fatalf("duplicate audit: %d", got)
	}
	if _, err := f.repo.ResolveCheckInCode(actor, token, f.now); err != nil {
		t.Fatalf("lost response retry: %v", err)
	}
}

func TestSuspensionRescheduleCountersAuditAndRollback(t *testing.T) {
	f, _, row := confirmedArrivalFixture(t)
	var original model.Slot
	if err := f.db.First(&original, "id = ?", row.SlotID).Error; err != nil {
		t.Fatal(err)
	}
	target := original
	target.ID = "arrival-reschedule-target"
	target.StartsAt = original.StartsAt.Add(90 * 24 * time.Hour)
	target.EndsAt = original.EndsAt.Add(90 * 24 * time.Hour)
	target.Reserved = 0
	target.Version = 1
	if err := f.db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	actor := store.WithEmployeeScope(context.Background(), "customer_service", "", []string{row.ClinicID})
	input := store.AdminRescheduleInput{SlotID: target.ID, Version: row.Version, Reason: "患者电话同意新时段", RequestKey: "suspension-reschedule-request-1"}
	if _, err := f.repo.AdminRescheduleAppointment(actor, row.ID, input, f.now); !errors.Is(err, store.ErrTransition) {
		t.Fatalf("open original: %v", err)
	}
	if _, err := f.repo.SuspendSlots(context.Background(), []store.SlotReference{{ID: original.ID, Version: original.Version}}, "医生临时停诊", f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.Slot{}).Where("id = ?", target.ID).Update("capacity", 0).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.AdminRescheduleAppointment(actor, row.ID, input, f.now); !errors.Is(err, store.ErrSlotFull) {
		t.Fatalf("full target: %v", err)
	}
	var untouched model.Slot
	if err := f.db.First(&untouched, "id = ?", original.ID).Error; err != nil || untouched.Reserved != original.Reserved {
		t.Fatal("failed move released original reservation", err)
	}
	if err := f.db.Model(&model.Slot{}).Where("id = ?", target.ID).Update("capacity", 2).Error; err != nil {
		t.Fatal(err)
	}
	doctor := store.WithEmployeeScope(context.Background(), "doctor", row.DoctorID, []string{row.ClinicID})
	if _, err := f.repo.AdminRescheduleAppointment(doctor, row.ID, input, f.now); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("doctor role: %v", err)
	}
	moved, err := f.repo.AdminRescheduleAppointment(actor, row.ID, input, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if moved.ID != row.ID || moved.Status != "confirmed" || moved.SlotID != target.ID {
		t.Fatalf("unexpected move: %+v", moved)
	}
	if _, err := f.repo.AdminRescheduleAppointment(actor, row.ID, input, f.now); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	var count int64
	f.db.Model(&model.AppointmentEvent{}).Where("appointment_id = ? AND action = ?", row.ID, "suspend-move").Count(&count)
	if count != 1 {
		t.Fatalf("audit count %d", count)
	}
	f.db.First(&original, "id = ?", original.ID)
	f.db.First(&target, "id = ?", target.ID)
	if original.Reserved != 0 || target.Reserved != 1 {
		t.Fatalf("counters: %d %d", original.Reserved, target.Reserved)
	}
	input.Reason = "另一份沟通记录"
	if _, err := f.repo.AdminRescheduleAppointment(actor, row.ID, input, f.now); !errors.Is(err, store.ErrIdempotency) {
		t.Fatalf("key conflict: %v", err)
	}
}
