package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/httpapi"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	driver "github.com/go-sql-driver/mysql"
)

func TestMySQLIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set; real MySQL integration test requires a dedicated *_test database")
	}
	cfg, err := driver.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("TEST_DB_DSN must name a dedicated database ending in _test")
	}
	db, err := database.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	// Only the explicit *_test schema is reset, children before their parents.
	for _, table := range []string{"patient_binding_codes", "nursing_feedbacks", "followup_template_events", "followup_templates", "advice_receipts", "record_export_events", "record_export_jobs", "case_events", "case_images", "case_records"} {
		if err := db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("UPDATE consultations SET parent_consultation_id = NULL").Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"support_attachments", "support_events", "support_tickets", "knowledge_events", "knowledge_articles", "knowledge_categories", "directory_revisions", "privacy_consent_events", "employee_account_events", "employee_sessions", "employee_accounts", "handoff_followup_events", "followup_handoff_receipts", "consultations", "followup_events", "followup_photos", "followup_assignment_events", "followup_staff_events", "followup_tasks", "treatment_revisions", "treatments", "followup_staff", "care_access_events", "care_plan_versions", "care_plans", "photo_consent_events", "care_photos", "check_in_codes", "appointment_events", "appointments", "sessions", "patients", "slots", "doctors", "clinics"} {
		if err := db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().In(database.Shanghai)
	if err := database.Seed(ctx, db, now); err != nil {
		t.Fatal(err)
	}
	repo := store.New(db)
	if err := repo.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	doctors, err := repo.Doctors(ctx, "clinic-demo")
	if err != nil || len(doctors) != 2 {
		t.Fatalf("doctors=%v err=%v", doctors, err)
	}
	slots, err := repo.Slots(ctx, store.SlotFilter{DoctorID: "doctor-demo-1", Now: now})
	if err != nil || len(slots) == 0 {
		t.Fatalf("slots=%v err=%v", slots, err)
	}
	first := slots[0]
	date := time.Date(first.StartsAt.Year(), first.StartsAt.Month(), first.StartsAt.Day(), 0, 0, 0, 0, database.Shanghai)
	filtered, err := repo.Slots(ctx, store.SlotFilter{DoctorID: "doctor-demo-1", ClinicID: "clinic-demo", Date: date, Now: now})
	if err != nil || len(filtered) == 0 {
		t.Fatal("date filter returned no slots", err)
	}
	for _, s := range filtered {
		if s.StartsAt.Format("2006-01-02") != date.Format("2006-01-02") || s.DoctorID != "doctor-demo-1" {
			t.Fatalf("wrong slot %+v", s)
		}
	}

	t.Run("concurrent version updates", func(t *testing.T) {
		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, capacity := range []int{4, 5} {
			wg.Go(func() {
				<-start
				_, err := repo.UpdateSlot(ctx, first.ID, store.SlotUpdate{Capacity: capacity, Status: "open", Version: 1}, now)
				results <- err
			})
		}
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
	})

	t.Run("seed preserves saved schedule", func(t *testing.T) {
		if err := database.Seed(ctx, db, now); err != nil {
			t.Fatal(err)
		}
		var saved model.Slot
		if err := db.First(&saved, "id = ?", first.ID).Error; err != nil {
			t.Fatal(err)
		}
		if saved.Version != 2 || saved.Capacity < 4 {
			t.Fatalf("seed overwrote edit: %+v", saved)
		}
		var count int64
		if err := db.Model(&model.Doctor{}).Count(&count).Error; err != nil || count != 2 {
			t.Fatalf("duplicate doctors count=%d err=%v", count, err)
		}
		again, err := repo.Slots(ctx, store.SlotFilter{DoctorID: "doctor-demo-1", Now: now})
		if err != nil || len(again) != len(slots) {
			t.Fatal("seed changed slot count", err)
		}
	})

	t.Run("capacity and expired slot checks", func(t *testing.T) {
		if err := db.Model(&model.Slot{}).Where("id = ?", first.ID).Update("reserved", 2).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := repo.UpdateSlot(ctx, first.ID, store.SlotUpdate{Capacity: 1, Status: "open", Version: 2}, now); !errors.Is(err, store.ErrCapacity) {
			t.Fatalf("expected capacity conflict, got %v", err)
		}
		if _, err := repo.UpdateSlot(ctx, first.ID, store.SlotUpdate{Capacity: 3, Status: "open", Version: 2}, first.StartsAt); !errors.Is(err, store.ErrPast) {
			t.Fatalf("expected past slot conflict, got %v", err)
		}
		if _, err := repo.UpdateSlot(ctx, "missing", store.SlotUpdate{Capacity: 3, Status: "open", Version: 1}, now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("expected not found, got %v", err)
		}
	})

	t.Run("HTTP write then read MySQL", func(t *testing.T) {
		account, err := repo.CreateEmployee(store.WithClinicAssignment(ctx, []string{"clinic-demo"}), "mysql-integration-manager", "数据库验收主管", "IntegrationTest123!", "manager", "", now)
		if err != nil {
			t.Fatal(err)
		}
		_, token, err := repo.LoginEmployee(ctx, account.Username, "IntegrationTest123!", now)
		if err != nil {
			t.Fatal(err)
		}
		r := httpapi.New(repo, "integration-demo-token")
		body := `{"capacity":6,"status":"closed","version":2}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/slots/"+first.ID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("PATCH %d %s", w.Code, w.Body)
		}
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/slots?doctorId=doctor-demo-1&date="+date.Format("2006-01-02"), nil))
		var response struct {
			Data []model.Slot `json:"data"`
		}
		if w.Code != 200 {
			t.Fatalf("GET %d %s", w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, s := range response.Data {
			if s.ID == first.ID {
				found = true
				if s.Capacity != 6 || s.Status != "closed" || s.Version != 3 || s.Reserved != 2 {
					t.Fatalf("update not visible: %+v", s)
				}
			}
		}
		if !found {
			t.Fatal("updated slot missing from read")
		}
		if !strings.Contains(w.Body.String(), "+08:00") {
			t.Fatal("timestamps must carry Shanghai offset")
		}
	})
}
