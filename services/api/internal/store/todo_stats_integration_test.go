package store_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	driver "github.com/go-sql-driver/mysql"
)

func TestTodoStats(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set; todo stats requires a dedicated *_test MySQL database")
	}
	config, err := driver.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(config.DBName, "_test") {
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
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("todostats-%d-", time.Now().UnixNano())
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, database.Shanghai)
	clinic, hidden := prefix+"clinic", prefix+"hidden"
	staffA, staffB := prefix+"staff-a", prefix+"staff-b"
	patient := prefix + "patient"
	create := func(value any) {
		t.Helper()
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, table := range []string{"followup_handoff_receipts", "followup_tasks", "appointments", "patients", "slots", "followup_staff", "doctors", "clinics"} {
			if err := db.Exec("DELETE FROM "+table+" WHERE id LIKE ?", prefix+"%").Error; err != nil {
				t.Errorf("clean %s: %v", table, err)
			}
		}
	})
	for _, c := range []model.Clinic{{ID: clinic, Name: "授权院区"}, {ID: hidden, Name: "隐藏院区"}} {
		c.DirectoryState = model.DirectoryState{Active: true, Version: 1}
		create(&c)
	}
	doctor := prefix + "doctor"
	hiddenDoctor := prefix + "hidden-doctor"
	create(&model.Doctor{ID: doctor, ClinicID: clinic, Name: "接诊医生", DirectoryState: model.DirectoryState{Active: true, Version: 1}, Focus: []string{}})
	create(&model.Doctor{ID: hiddenDoctor, ClinicID: hidden, Name: "隐藏医生", DirectoryState: model.DirectoryState{Active: true, Version: 1}, Focus: []string{}})
	create(&model.FollowupStaff{ID: staffA, DisplayName: "负责人甲", Role: "doctor", Status: "on_duty", Version: 1})
	create(&model.FollowupStaff{ID: staffB, DisplayName: "负责人乙", Role: "doctor", Status: "on_duty", Version: 1})
	create(&model.Patient{ID: patient, DisplayName: "待办测试患者", CreatedAt: now, Version: 1})
	// The handoff overdue cutoff comes from the global setting row: pin it
	// to 24h for this test and restore the previous value afterwards.
	var prevSetting model.FollowupHandoffSetting
	prevErr := db.Where("id = 1").First(&prevSetting).Error
	if err := db.Save(&model.FollowupHandoffSetting{ID: 1, OverdueHours: 24, Version: 1, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if prevErr == nil {
			_ = db.Save(&prevSetting).Error
		} else {
			_ = db.Exec("DELETE FROM followup_handoff_settings WHERE id = 1").Error
		}
	})
	mkAppointment := func(label, clinicID string) model.Appointment {
		start := now.Add(24 * time.Hour)
		slotDoctor := doctor
		if label == "hidden-appt" {
			start = now.Add(26 * time.Hour)
			slotDoctor = hiddenDoctor
		}
		slot := model.Slot{ID: prefix + label + "-slot", ClinicID: clinicID, DoctorID: slotDoctor, StartsAt: start, EndsAt: start.Add(30 * time.Minute), Capacity: 3, Status: "open", Version: 1}
		create(&slot)
		appt := model.Appointment{ID: prefix + label, PatientID: patient, SlotID: slot.ID, ContactName: "待办测试", ContactPhone: "13800000000", Status: "confirmed", ConfirmationDeadline: now.Add(24 * time.Hour), RequestKey: label, RequestHash: strings.Repeat("0", 64), CreatedAt: now, UpdatedAt: now, Version: 1}
		create(&appt)
		return appt
	}
	mkTask := func(label, apptID, status string, dueAt time.Time) model.FollowupTask {
		task := model.FollowupTask{ID: prefix + label, AppointmentID: apptID, PatientID: patient, AssigneeID: staffA, AssignedAt: now, Title: "待办 " + label, DueAt: dueAt, Status: status, Version: 1}
		create(&task)
		return task
	}
	appt := mkAppointment("appt", clinic)
	mkTask("reviewed", appt.ID, "reviewed", now.Add(24*time.Hour))
	mkTask("handled", appt.ID, "handled", now.Add(24*time.Hour))
	mkTask("pending", appt.ID, "pending", now.Add(24*time.Hour))
	mkTask("pending-overdue", appt.ID, "pending", now.Add(-24*time.Hour))
	mkTask("submitted", appt.ID, "submitted", now.Add(24*time.Hour))
	mkTask("alert", appt.ID, "alert", now.Add(24*time.Hour))
	hiddenAppt := mkAppointment("hidden-appt", hidden)
	mkTask("hidden", hiddenAppt.ID, "pending", now.Add(24*time.Hour))
	mkReceipt := func(label, taskID, status, followupStatus string, createdAt time.Time) {
		create(&model.FollowupHandoffReceipt{ID: prefix + label, TaskID: taskID, FromStaffID: staffA, ToStaffID: staffA, Reason: "测试交接", AssignedBy: "test", CreatedAt: createdAt, Status: status, FollowupStatus: followupStatus, FollowupVersion: 1, Version: 1})
	}
	mkReceipt("confirmed", prefix+"reviewed", "confirmed", "resolved", now)
	mkReceipt("overdue", prefix+"pending-overdue", "pending", "untracked", now.Add(-100*time.Hour))
	mkReceipt("awaiting", prefix+"pending", "pending", "awaiting_reply", now)
	mkReceipt("resolved", prefix+"submitted", "pending", "resolved", now)
	mkReceipt("hidden", prefix+"hidden", "pending", "awaiting_reply", now)

	repo := store.New(db)
	stats, err := repo.TodoStats(store.WithEmployeeScope(context.Background(), "manager", "", []string{clinic}), now)
	if err != nil {
		t.Fatal(err)
	}
	f := stats.Followup
	if f.Total != 6 || f.Completed != 2 || f.Pending != 2 || f.Submitted != 1 || f.Alert != 1 || f.Overdue != 1 {
		t.Fatalf("followup = %+v, want total 6 completed 2 pending 2 submitted 1 alert 1 overdue 1", f)
	}
	if f.CompletionRate == nil || *f.CompletionRate < 33.3 || *f.CompletionRate > 33.4 {
		t.Fatalf("completionRate = %v, want ~33.33", f.CompletionRate)
	}
	h := stats.Handoffs
	if h.Total != 4 || h.Confirmed != 1 || h.Pending != 3 || h.Overdue != 1 || h.Todo != 2 {
		t.Fatalf("handoffs = %+v, want total 4 confirmed 1 pending 3 overdue 1 todo 2", h)
	}
	// A clinician linked to staff-b sees none of staff-a's scoped tasks or
	// receipts: the handoff counts are never folded into the followup rate,
	// and the clinician filter applies to both sections.
	empty, err := repo.TodoStats(store.WithEmployeeScope(context.Background(), "doctor", staffB, []string{clinic}), now)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Followup.Total != 0 || empty.Followup.CompletionRate != nil || empty.Handoffs.Total != 0 {
		t.Fatalf("clinician scope = %+v / %+v, want zero tasks and nil rate", empty.Followup, empty.Handoffs)
	}
}
