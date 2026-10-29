package store_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/httpapi"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const bookingAdminToken = "integration-operator-token"

type bookingFixture struct {
	t             *testing.T
	db            *gorm.DB
	repo          *store.MySQL
	now           time.Time
	slot          model.Slot
	router        http.Handler
	operatorToken string
}

func newBookingFixture(t *testing.T) *bookingFixture {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set; booking integration requires a dedicated *_test MySQL database")
	}
	cfg, err := driver.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("TEST_DB_DSN must select a dedicated *_test database")
	}
	db, err := database.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"patient_binding_codes", "consultation_templates", "patient_charts", "advice_receipts", "record_export_events", "record_export_jobs", "case_events", "case_images", "case_records"} {
		if err := db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"nursing_feedbacks", "followup_template_events", "followup_templates"} {
		if err := db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("UPDATE consultations SET parent_consultation_id = NULL").Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"support_attachments", "support_events", "support_tickets", "knowledge_favorites", "knowledge_events", "knowledge_articles", "knowledge_categories", "directory_revisions", "privacy_consent_events", "employee_account_events", "employee_sessions", "employee_accounts", "handoff_followup_events", "followup_handoff_receipts", "consultations", "followup_events", "followup_photos", "followup_assignment_events", "followup_staff_events", "followup_tasks", "treatment_revisions", "treatments", "followup_staff", "care_access_events", "care_plan_versions", "care_plans", "photo_consent_events", "care_photos", "check_in_codes", "waitlist_offers", "appointment_events", "appointments", "sessions", "patients", "slots", "doctors", "clinics"} {
		if err := db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().In(database.Shanghai).Truncate(time.Millisecond)
	if err := database.Seed(ctx, db, now); err != nil {
		t.Fatal(err)
	}
	var slot model.Slot
	if err := db.Where("starts_at > ?", now.Add(3*time.Hour)).Order("starts_at, doctor_id").First(&slot).Error; err != nil {
		t.Fatal(err)
	}
	repo := store.New(db)
	return &bookingFixture{t: t, db: db, repo: repo, now: now, slot: slot, router: httpapi.New(repo, bookingAdminToken)}
}

func (f *bookingFixture) patient() store.SessionResult {
	f.t.Helper()
	result, err := f.repo.NewPatientSession(context.Background(), f.now)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.repo.RecordPrivacyConsent(context.Background(), result.Patient.ID, store.PrivacyPolicyVersion, "accepted", f.now); err != nil {
		f.t.Fatal(err)
	}
	return result
}
func (f *bookingFixture) input() store.AppointmentInput {
	return store.AppointmentInput{SlotID: f.slot.ID, ContactName: "演示访客", ContactPhone: "13800000000", Note: "演示预约备注"}
}
func (f *bookingFixture) create(patient store.SessionResult, key string) model.Appointment {
	f.t.Helper()
	row, replayed, err := f.repo.CreateAppointment(context.Background(), patient.Patient.ID, key, f.input(), f.now)
	if err != nil || replayed {
		f.t.Fatalf("create failed: replay=%v err=%v", replayed, err)
	}
	return row
}
func (f *bookingFixture) savedSlot() model.Slot {
	f.t.Helper()
	var row model.Slot
	if err := f.db.First(&row, "id = ?", f.slot.ID).Error; err != nil {
		f.t.Fatal(err)
	}
	return row
}
func (f *bookingFixture) assertReserved(want int) {
	f.t.Helper()
	row := f.savedSlot()
	if row.Reserved != want {
		f.t.Fatalf("reserved=%d want=%d", row.Reserved, want)
	}
}
func (f *bookingFixture) http(method, path, body, token, key string) *httptest.ResponseRecorder {
	f.t.Helper()
	if token == bookingAdminToken && strings.HasPrefix(path, "/api/v1/admin/") {
		if f.operatorToken == "" {
			ctx := store.WithClinicAssignment(context.Background(), []string{f.slot.ClinicID})
			account, err := f.repo.CreateEmployee(ctx, "booking-rule-operator", "预约验收主管", "BookingRulesTest123!", "manager", "", f.now)
			if err != nil {
				f.t.Fatal(err)
			}
			_, f.operatorToken, err = f.repo.LoginEmployee(context.Background(), account.Username, "BookingRulesTest123!", f.now)
			if err != nil {
				f.t.Fatal(err)
			}
		}
		token = f.operatorToken
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, req)
	return response
}
func requireHTTP(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status || (code != "" && !strings.Contains(w.Body.String(), code)) {
		t.Fatalf("response %d want=%d code=%s body=%s", w.Code, status, code, w.Body)
	}
}

// Keep the first lock in the documented order held long enough to cross a
// business deadline, and prove that the operation actually waited for it.
func holdPatientAcrossDeadline(t *testing.T, db *gorm.DB, patientID string, operation func() error) error {
	t.Helper()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	var row model.Patient
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", patientID).Error; err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() { close(started); result <- operation() }()
	<-started
	select {
	case err := <-result:
		t.Fatalf("operation did not wait for patient lock: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not complete after lock release")
		return nil
	}
}

func TestBookingMySQLIntegration(t *testing.T) {
	t.Run("waiting past slot start blocks creation", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		if err := f.db.Model(&model.Slot{}).Where("id = ?", f.slot.ID).Updates(map[string]any{"starts_at": f.now.Add(100 * time.Millisecond), "ends_at": f.now.Add(30 * time.Minute)}).Error; err != nil {
			t.Fatal(err)
		}
		err := holdPatientAcrossDeadline(t, f.db, patient.Patient.ID, func() error {
			_, _, err := f.repo.CreateAppointment(context.Background(), patient.Patient.ID, "blocked-create-request-01", f.input(), f.now)
			return err
		})
		if !errors.Is(err, store.ErrPast) {
			t.Fatalf("creation crossed start time: %v", err)
		}
		f.assertReserved(0)
		var count int64
		if err := f.db.Model(&model.Appointment{}).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("late request persisted: %d %v", count, err)
		}
	})

	t.Run("waiting past confirmation deadline expires in same transaction", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		row := f.create(patient, "blocked-confirm-request")
		deadline := f.now.Add(100 * time.Millisecond)
		if err := f.db.Model(&model.Appointment{}).Where("id = ?", row.ID).Update("confirmation_deadline", deadline).Error; err != nil {
			t.Fatal(err)
		}
		err := holdPatientAcrossDeadline(t, f.db, patient.Patient.ID, func() error {
			_, err := f.repo.ReviewAppointment(context.Background(), row.ID, store.ReviewInput{Action: "confirm", Version: 1}, f.now)
			return err
		})
		if !errors.Is(err, store.ErrTransition) {
			t.Fatalf("confirmation crossed cutoff: %v", err)
		}
		f.assertReserved(0)
		var saved model.Appointment
		if err := f.db.First(&saved, "id = ?", row.ID).Error; err != nil {
			t.Fatal(err)
		}
		if saved.Status != "expired" || saved.Version != 2 || saved.UpdatedAt.Before(deadline) {
			t.Fatalf("late transition did not persist expiry: %+v", saved)
		}
	})

	t.Run("waiting past slot start blocks confirmed cancellation", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		row := f.create(patient, "blocked-cancel-request-1")
		if _, err := f.repo.ReviewAppointment(context.Background(), row.ID, store.ReviewInput{Action: "confirm", Version: 1}, f.now); err != nil {
			t.Fatal(err)
		}
		if err := f.db.Model(&model.Slot{}).Where("id = ?", f.slot.ID).Updates(map[string]any{"starts_at": f.now.Add(100 * time.Millisecond), "ends_at": f.now.Add(30 * time.Minute)}).Error; err != nil {
			t.Fatal(err)
		}
		err := holdPatientAcrossDeadline(t, f.db, patient.Patient.ID, func() error {
			_, err := f.repo.CancelAppointment(context.Background(), row.ID, patient.Patient.ID, 2, "", "", f.now)
			return err
		})
		if !errors.Is(err, store.ErrTransition) {
			t.Fatalf("cancellation crossed start: %v", err)
		}
		f.assertReserved(1)
	})

	t.Run("sessions are independent hashed and expire", func(t *testing.T) {
		f := newBookingFixture(t)
		ctx := context.Background()
		a, b := f.patient(), f.patient()
		if a.Token == b.Token || a.Patient.ID == b.Patient.ID || len(a.Token) < 40 {
			t.Fatal("sessions are not independent")
		}
		var saved model.PatientSession
		if err := f.db.First(&saved, "patient_id = ?", a.Patient.ID).Error; err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(a.Token))
		if saved.TokenHash != hex.EncodeToString(sum[:]) || saved.TokenHash == a.Token {
			t.Fatal("token not hashed")
		}
		if saved.ExpiresAt.Sub(f.now) != 7*24*time.Hour {
			t.Fatal("wrong session lifetime")
		}
		if _, err := f.repo.AuthenticatePatient(ctx, a.Token, f.now); err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.AuthenticatePatient(ctx, a.Token, a.ExpiresAt); !errors.Is(err, store.ErrUnauthenticated) {
			t.Fatalf("expired session accepted: %v", err)
		}
		if _, err := f.repo.AuthenticatePatient(ctx, bookingAdminToken, f.now); !errors.Is(err, store.ErrUnauthenticated) {
			t.Fatal("admin token became patient")
		}
		w := f.http("POST", "/api/v1/dev/patient-sessions", `{"patientId":"`+a.Patient.ID+`"}`, "", "")
		requireHTTP(t, w, 400, "INVALID_BODY")
		w = f.http("POST", "/api/v1/dev/patient-sessions", `{}`, "", "")
		requireHTTP(t, w, 201, "")
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("session response must disable caches")
		}
	})

	t.Run("idempotency normalization recovery and changed body", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		key := "request-normalized-0001"
		first := f.create(patient, key)
		if first.Status != "pending" || first.Version != 1 || first.ConfirmationDeadline.Sub(first.CreatedAt) != 120*time.Minute {
			t.Fatalf("unexpected initial state: %+v", first)
		}
		input := f.input()
		input.ContactName = "  " + input.ContactName + "  "
		input.Note += "\n"
		again, replayed, err := f.repo.CreateAppointment(context.Background(), patient.Patient.ID, key, input, f.now)
		if err != nil || !replayed || again.ID != first.ID {
			t.Fatalf("replay failed: %v %v", replayed, err)
		}
		input.Note = "另一份备注"
		if _, _, err := f.repo.CreateAppointment(context.Background(), patient.Patient.ID, key, input, f.now); !errors.Is(err, store.ErrIdempotency) {
			t.Fatalf("different body not rejected: %v", err)
		}
		found, err := f.repo.AppointmentByRequest(context.Background(), patient.Patient.ID, key, f.now)
		if err != nil || found.ID != first.ID {
			t.Fatal("request recovery failed", err)
		}
		if _, _, err := f.repo.CreateAppointment(context.Background(), patient.Patient.ID, "request-second-key-0001", f.input(), f.now); !errors.Is(err, store.ErrActiveAppointment) {
			t.Fatalf("duplicate active booking accepted: %v", err)
		}
		f.assertReserved(1)
		var events int64
		if err := f.db.Model(&model.AppointmentEvent{}).Count(&events).Error; err != nil || events != 1 {
			t.Fatalf("replay created event: %d %v", events, err)
		}
	})

	t.Run("simultaneous same key produces one appointment", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		start := make(chan struct{})
		var wg sync.WaitGroup
		type outcome struct {
			id     string
			replay bool
			err    error
		}
		results := make(chan outcome, 8)
		for range 8 {
			wg.Go(func() {
				<-start
				row, replay, err := f.repo.CreateAppointment(context.Background(), patient.Patient.ID, "concurrent-same-key-0001", f.input(), f.now)
				results <- outcome{row.ID, replay, err}
			})
		}
		close(start)
		wg.Wait()
		close(results)
		id := ""
		created := 0
		for result := range results {
			if result.err != nil {
				t.Fatal(result.err)
			}
			if id == "" {
				id = result.id
			}
			if result.id != id {
				t.Fatal("different appointments for same key")
			}
			if !result.replay {
				created++
			}
		}
		if created != 1 {
			t.Fatalf("created=%d", created)
		}
		f.assertReserved(1)
	})

	t.Run("same patient different keys races safely", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, key := range []string{"concurrent-key-alpha-01", "concurrent-key-bravo-02"} {
			wg.Go(func() {
				<-start
				_, _, err := f.repo.CreateAppointment(context.Background(), patient.Patient.ID, key, f.input(), f.now)
				results <- err
			})
		}
		close(start)
		wg.Wait()
		close(results)
		success, duplicates := 0, 0
		for err := range results {
			if err == nil {
				success++
			} else if errors.Is(err, store.ErrActiveAppointment) {
				duplicates++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || duplicates != 1 {
			t.Fatalf("success=%d duplicates=%d", success, duplicates)
		}
		f.assertReserved(1)
	})

	t.Run("different patients compete for final place", func(t *testing.T) {
		f := newBookingFixture(t)
		if err := f.db.Model(&model.Slot{}).Where("id = ?", f.slot.ID).Update("capacity", 1).Error; err != nil {
			t.Fatal(err)
		}
		patients := []store.SessionResult{f.patient(), f.patient(), f.patient(), f.patient()}
		start := make(chan struct{})
		results := make(chan error, len(patients))
		var wg sync.WaitGroup
		for _, patient := range patients {
			wg.Go(func() {
				<-start
				_, _, err := f.repo.CreateAppointment(context.Background(), patient.Patient.ID, "last-place-request-0001", f.input(), f.now)
				results <- err
			})
		}
		close(start)
		wg.Wait()
		close(results)
		success, full := 0, 0
		for err := range results {
			if err == nil {
				success++
			} else if errors.Is(err, store.ErrSlotFull) {
				full++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || full != len(patients)-1 {
			t.Fatalf("success=%d full=%d", success, full)
		}
		f.assertReserved(1)
	})

	t.Run("confirmation preserves reservation cancellation releases once", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		row := f.create(patient, "confirm-cancel-request-01")
		before := f.savedSlot()
		confirmed, err := f.repo.ReviewAppointment(context.Background(), row.ID, store.ReviewInput{Action: "confirm", Version: 1}, f.now)
		if err != nil || confirmed.Status != "confirmed" || confirmed.Version != 2 {
			t.Fatal("confirmation failed", err)
		}
		after := f.savedSlot()
		if after.Reserved != 1 || after.Version != before.Version {
			t.Fatal("confirmation reserved twice or changed slot version")
		}
		cancelled, err := f.repo.CancelAppointment(context.Background(), row.ID, patient.Patient.ID, 2, "", "", f.now)
		if err != nil || cancelled.Status != "cancelled" || cancelled.Version != 3 {
			t.Fatal("cancellation failed", err)
		}
		f.assertReserved(0)
		if _, err := f.repo.CancelAppointment(context.Background(), row.ID, patient.Patient.ID, 3, "", "", f.now); !errors.Is(err, store.ErrTransition) {
			t.Fatalf("repeat cancellation accepted: %v", err)
		}
		f.assertReserved(0)
		f.create(patient, "new-booking-after-cancel")
		f.assertReserved(1)
	})

	t.Run("rejection frees capacity and writes safe audit", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		row := f.create(patient, "reject-request-key-0001")
		rejected, err := f.repo.ReviewAppointment(context.Background(), row.ID, store.ReviewInput{Action: "reject", Reason: "该时段安排调整", Version: 1}, f.now)
		if err != nil || rejected.Status != "rejected" || rejected.RejectionReason != "该时段安排调整" {
			t.Fatal("rejection failed", err)
		}
		f.assertReserved(0)
		var events []model.AppointmentEvent
		if err := f.db.Where("appointment_id = ?", row.ID).Order("created_at, id").Find(&events).Error; err != nil {
			t.Fatal(err)
		}
		if len(events) != 2 {
			t.Fatalf("audit events=%d", len(events))
		}
		for _, event := range events {
			raw, _ := json.Marshal(event)
			if strings.Contains(string(raw), f.input().ContactPhone) || strings.Contains(string(raw), f.input().Note) || strings.Contains(string(raw), patient.Token) {
				t.Fatal("sensitive data in audit")
			}
		}
	})

	t.Run("expiry survives new repository and releases once", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		row := f.create(patient, "expire-request-key-0001")
		restarted := store.New(f.db)
		count, err := restarted.ExpirePending(context.Background(), row.ConfirmationDeadline)
		if err != nil || count != 1 {
			t.Fatalf("expiry count=%d err=%v", count, err)
		}
		f.assertReserved(0)
		count, err = restarted.ExpirePending(context.Background(), row.ConfirmationDeadline.Add(time.Minute))
		if err != nil || count != 0 {
			t.Fatalf("repeated expiry count=%d err=%v", count, err)
		}
		f.assertReserved(0)
		latest, err := restarted.GetAppointment(context.Background(), row.ID, patient.Patient.ID, row.ConfirmationDeadline)
		if err != nil || latest.Status != "expired" || latest.Version != 2 {
			t.Fatal("expiry not persisted", err)
		}
		if _, err := restarted.ReviewAppointment(context.Background(), row.ID, store.ReviewInput{Action: "confirm", Version: 2}, row.ConfirmationDeadline); !errors.Is(err, store.ErrTransition) {
			t.Fatal("expired appointment confirmed", err)
		}
	})

	t.Run("expiry cutoff and configuration only affect new deadlines", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		soon := f.slot
		soon.ID = "slot-expiry-soon"
		soon.StartsAt = f.now.Add(30 * time.Minute)
		soon.EndsAt = soon.StartsAt.Add(30 * time.Minute)
		if err := f.db.Create(&soon).Error; err != nil {
			t.Fatal(err)
		}
		input := f.input()
		input.SlotID = soon.ID
		row, _, err := f.repo.CreateAppointment(context.Background(), patient.Patient.ID, "soon-request-key-00001", input, f.now)
		if err != nil || !row.ConfirmationDeadline.Equal(soon.StartsAt) {
			t.Fatal("deadline exceeds slot start", err)
		}
		if err := f.repo.SetPendingConfirmation(10 * time.Minute); err != nil {
			t.Fatal(err)
		}
		old, err := f.repo.GetAppointment(context.Background(), row.ID, patient.Patient.ID, f.now)
		if err != nil || !old.ConfirmationDeadline.Equal(soon.StartsAt) {
			t.Fatal("configuration changed persisted deadline", err)
		}
		newRow := f.create(patient, "new-short-deadline-0001")
		if newRow.ConfirmationDeadline.Sub(newRow.CreatedAt) != 10*time.Minute {
			t.Fatal("new configuration ignored")
		}
	})

	t.Run("list cleans expired pending and exposes current capacity", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		row := f.create(patient, "list-expired-request-01")
		page, err := f.repo.ListAppointments(context.Background(), store.AppointmentFilter{PatientID: patient.Patient.ID, Status: "expired"}, row.ConfirmationDeadline)
		if err != nil || page.Total != 1 || len(page.Data) != 1 || page.Data[0].Status != "expired" {
			t.Fatal("list did not compensate missed expiry", err)
		}
		f.assertReserved(0)
	})

	t.Run("closed slots block new requests and confirmation but allow rejection", func(t *testing.T) {
		f := newBookingFixture(t)
		first, second := f.patient(), f.patient()
		row := f.create(first, "before-close-request-01")
		slot := f.savedSlot()
		if _, err := f.repo.UpdateSlot(context.Background(), slot.ID, store.SlotUpdate{Capacity: slot.Capacity, Status: "closed", Version: slot.Version}, f.now); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.repo.CreateAppointment(context.Background(), second.Patient.ID, "after-close-request-001", f.input(), f.now); !errors.Is(err, store.ErrSlotClosed) {
			t.Fatal("closed slot accepted booking", err)
		}
		if _, err := f.repo.ReviewAppointment(context.Background(), row.ID, store.ReviewInput{Action: "confirm", Version: 1}, f.now); !errors.Is(err, store.ErrTransition) {
			t.Fatal("closed slot allowed confirmation", err)
		}
		f.assertReserved(1)
		if _, err := f.repo.ReviewAppointment(context.Background(), row.ID, store.ReviewInput{Action: "reject", Reason: "停诊需重新安排", Version: 1}, f.now); err != nil {
			t.Fatal(err)
		}
		f.assertReserved(0)
	})

	t.Run("concurrent cancel and confirm apply a single transition", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		row := f.create(patient, "race-cancel-confirm-001")
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			_, err := f.repo.CancelAppointment(context.Background(), row.ID, patient.Patient.ID, 1, "", "", f.now)
			results <- err
		})
		wg.Go(func() {
			<-start
			_, err := f.repo.ReviewAppointment(context.Background(), row.ID, store.ReviewInput{Action: "confirm", Version: 1}, f.now)
			results <- err
		})
		close(start)
		wg.Wait()
		close(results)
		success, conflict := 0, 0
		for err := range results {
			if err == nil {
				success++
			} else if errors.Is(err, store.ErrConflict) {
				conflict++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatalf("success=%d conflict=%d", success, conflict)
		}
		latest, err := f.repo.GetAppointment(context.Background(), row.ID, patient.Patient.ID, f.now)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if latest.Status == "confirmed" {
			want = 1
		}
		f.assertReserved(want)
	})

	t.Run("concurrent expiry cancel reject never double release", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		row := f.create(patient, "race-expire-cancel-001")
		start := make(chan struct{})
		results := make(chan error, 3)
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			_, err := f.repo.ExpirePending(context.Background(), row.ConfirmationDeadline)
			results <- err
		})
		wg.Go(func() {
			<-start
			_, err := f.repo.CancelAppointment(context.Background(), row.ID, patient.Patient.ID, 1, "", "", row.ConfirmationDeadline)
			results <- err
		})
		wg.Go(func() {
			<-start
			_, err := f.repo.ReviewAppointment(context.Background(), row.ID, store.ReviewInput{Action: "reject", Reason: "演示拒绝", Version: 1}, row.ConfirmationDeadline)
			results <- err
		})
		close(start)
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil && !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrTransition) {
				t.Fatal(err)
			}
		}
		f.assertReserved(0)
		var count int64
		if err := f.db.Model(&model.AppointmentEvent{}).Where("appointment_id = ? AND action = ?", row.ID, "expire").Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("expire events=%d err=%v", count, err)
		}
	})

	t.Run("HTTP ownership roles pagination and persisted workflow", func(t *testing.T) {
		f := newBookingFixture(t)
		owner, other := f.patient(), f.patient()
		input := f.input()
		raw, _ := json.Marshal(input)
		w := f.http("POST", "/api/v1/me/appointments", string(raw), owner.Token, "http-flow-request-0001")
		requireHTTP(t, w, 201, "")
		var response apitypes.AppointmentResult
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		row := response.Data
		if !row.CanCancel || row.CanReview || row.ContactName != input.ContactName {
			t.Fatal("incorrect patient response")
		}
		w = f.http("POST", "/api/v1/me/appointments", string(raw), owner.Token, "http-flow-request-0001")
		requireHTTP(t, w, 200, "")
		path := "/api/v1/me/appointments/" + row.Id
		requireHTTP(t, f.http("GET", path, "", other.Token, ""), 404, "APPOINTMENT_NOT_FOUND")
		requireHTTP(t, f.http("POST", path+"/cancel", `{"version":1}`, other.Token, ""), 404, "APPOINTMENT_NOT_FOUND")
		requireHTTP(t, f.http("GET", "/api/v1/me/appointments/by-request/http-flow-request-0001", "", other.Token, ""), 404, "APPOINTMENT_NOT_FOUND")
		requireHTTP(t, f.http("GET", "/api/v1/me", "", bookingAdminToken, ""), 401, "PATIENT_SESSION_INVALID")
		requireHTTP(t, f.http("GET", "/api/v1/admin/appointments", "", owner.Token, ""), 401, "UNAUTHORIZED")
		w = f.http("GET", "/api/v1/me/appointments?page=1&pageSize=1", "", owner.Token, "")
		requireHTTP(t, w, 200, "")
		var page apitypes.AppointmentPage
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.Meta.Total != 1 || page.Meta.PageSize != 1 || len(page.Data) != 1 {
			t.Fatal("bad pagination")
		}
		w = f.http("GET", "/api/v1/me/appointments", "", other.Token, "")
		requireHTTP(t, w, 200, "")
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.Meta.Total != 0 || len(page.Data) != 0 {
			t.Fatal("another patient's list exposed data")
		}
		adminPath := "/api/v1/admin/appointments/" + row.Id
		w = f.http("GET", adminPath, "", bookingAdminToken, "")
		requireHTTP(t, w, 200, "")
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if !response.Data.CanReview || response.Data.CanCancel {
			t.Fatal("incorrect admin actions")
		}
		requireHTTP(t, f.http("POST", adminPath+"/review", `{"action":"confirm","version":1}`, bookingAdminToken, ""), 200, "")
		w = f.http("GET", path, "", owner.Token, "")
		requireHTTP(t, w, 200, "")
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Data.Status != apitypes.AppointmentStatusConfirmed || response.Data.Version != 2 {
			t.Fatal("patient cannot see admin confirmation")
		}
		requireHTTP(t, f.http("POST", path+"/cancel", `{"version":2}`, owner.Token, ""), 200, "")
		f.assertReserved(0)
		if err := f.db.Model(&model.PatientSession{}).Where("patient_id = ?", owner.Patient.ID).Update("expires_at", f.now.Add(-time.Minute)).Error; err != nil {
			t.Fatal(err)
		}
		requireHTTP(t, f.http("GET", path, "", owner.Token, ""), 401, "PATIENT_SESSION_INVALID")
	})

	t.Run("HTTP input failures cannot reserve or impersonate", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		valid := fmt.Sprintf(`{"slotId":%q,"contactName":"演示访客","contactPhone":"13800000000"}`, f.slot.ID)
		for _, body := range []string{`{}`, `null`, strings.Replace(valid, `"13800000000"`, `"123"`, 1), strings.Replace(valid, `"演示访客"`, `"  "`, 1), strings.TrimSuffix(valid, "}") + `,"patientId":"another"}`, strings.TrimSuffix(valid, "}") + `,"note":null}`} {
			requireHTTP(t, f.http("POST", "/api/v1/me/appointments", body, patient.Token, "validation-request-0001"), 400, "INVALID_BODY")
		}
		requireHTTP(t, f.http("POST", "/api/v1/me/appointments", valid, patient.Token, "short"), 400, "INVALID_IDEMPOTENCY_KEY")
		for _, query := range []string{"?page=0", "?page=100001", "?pageSize=101", "?status=unknown", "?patientId=another", "?page=1&page=2"} {
			requireHTTP(t, f.http("GET", "/api/v1/me/appointments"+query, "", patient.Token, ""), 400, "INVALID_QUERY")
		}
		f.assertReserved(0)
		row := f.create(patient, "review-invalid-body-001")
		for _, body := range []string{`{"action":"reject","version":1}`, `{"action":"reject","reason":"  ","version":1}`, `{"action":"confirm","reason":"not empty","version":1}`, `{"action":"confirm","version":0}`} {
			requireHTTP(t, f.http("POST", "/api/v1/admin/appointments/"+row.ID+"/review", body, bookingAdminToken, ""), 400, "INVALID_BODY")
		}
		f.assertReserved(1)
	})
}
