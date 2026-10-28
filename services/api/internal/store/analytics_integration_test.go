package store_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

type analyticsFixture struct {
	t       *testing.T
	db      *gorm.DB
	repo    *store.MySQL
	prefix  string
	now     time.Time
	clinic  string
	hidden  string
	doctor  string
	second  string
	other   string
	patient string
}

func newAnalyticsFixture(t *testing.T) *analyticsFixture {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set; analytics requires a dedicated *_test MySQL database")
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
	prefix := fmt.Sprintf("analytics-%d-", time.Now().UnixNano())
	f := &analyticsFixture{t: t, db: db, repo: store.New(db), prefix: prefix, now: analyticsDate(t, "2026-10-04T12:00:00+08:00"), clinic: prefix + "clinic-a", hidden: prefix + "clinic-b", doctor: prefix + "doctor-a", second: prefix + "doctor-b", other: prefix + "doctor-c", patient: prefix + "patient"}
	// This fixture only removes the rows it created, children before parents.
	t.Cleanup(func() {
		if err := db.Exec("DELETE FROM check_in_codes WHERE appointment_id LIKE ?", prefix+"%").Error; err != nil {
			t.Errorf("clean check_in_codes: %v", err)
		}
		for _, table := range []string{"followup_tasks", "treatment_revisions", "treatments", "care_plan_versions", "care_plans", "appointment_events", "appointments", "patients", "slots", "followup_staff", "doctors", "clinics"} {
			if err := db.Exec("DELETE FROM "+table+" WHERE id LIKE ?", prefix+"%").Error; err != nil {
				t.Errorf("clean %s: %v", table, err)
			}
		}
	})
	for _, clinic := range []model.Clinic{{ID: f.clinic, Name: "授权院区"}, {ID: f.hidden, Name: "隐藏院区"}} {
		clinic.DirectoryState = model.DirectoryState{Active: true, Version: 1}
		f.create(&clinic)
	}
	for _, doctor := range []model.Doctor{{ID: f.doctor, ClinicID: f.clinic, Name: "历史接诊医生"}, {ID: f.second, ClinicID: f.clinic, Name: "改派负责人"}, {ID: f.other, ClinicID: f.hidden, Name: "隐藏医生"}} {
		doctor.DirectoryState = model.DirectoryState{Active: doctor.ID != f.doctor, Version: 1}
		doctor.Focus = []string{}
		f.create(&doctor)
		f.create(&model.FollowupStaff{ID: doctor.ID, DisplayName: doctor.Name, Role: "doctor", Status: "on_duty", Version: 1})
	}
	f.create(&model.Patient{ID: f.patient, DisplayName: "统计测试患者", CreatedAt: f.now, Version: 1})
	return f
}

func analyticsDate(t *testing.T, value string) time.Time {
	t.Helper()
	date, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func (f *analyticsFixture) create(value any) {
	f.t.Helper()
	if err := f.db.Create(value).Error; err != nil {
		f.t.Fatal(err)
	}
}

func (f *analyticsFixture) appointment(label, doctor, startsAt, status string, deadline time.Time, arrived bool) model.Appointment {
	f.t.Helper()
	clinic := f.clinic
	if doctor == f.other {
		clinic = f.hidden
	}
	start := analyticsDate(f.t, startsAt)
	slot := model.Slot{ID: f.prefix + label + "-slot", ClinicID: clinic, DoctorID: doctor, StartsAt: start, EndsAt: start.Add(30 * time.Minute), Capacity: 3, Status: "open", Version: 1}
	f.create(&slot)
	appointment := model.Appointment{ID: f.prefix + label, PatientID: f.patient, SlotID: slot.ID, ContactName: "统计测试", ContactPhone: "13800000000", Status: status, ConfirmationDeadline: deadline, RequestKey: label, RequestHash: strings.Repeat("0", 64), CreatedAt: f.now.AddDate(0, -1, 0), UpdatedAt: f.now, Version: 1}
	f.create(&appointment)
	if arrived {
		if err := f.db.Table("appointments").Where("id = ?", appointment.ID).Updates(map[string]any{"arrived_at": start.Add(5 * time.Minute), "arrived_by": "analytics-test"}).Error; err != nil {
			f.t.Fatal(err)
		}
	}
	return appointment
}

func (f *analyticsFixture) treatment(appointment model.Appointment) model.Treatment {
	f.t.Helper()
	plan := model.CarePlan{ID: appointment.ID + "-plan", AppointmentID: appointment.ID, DraftVersion: 1, UpdatedAt: f.now}
	f.create(&plan)
	version := model.CarePlanVersion{ID: appointment.ID + "-version", CarePlanID: plan.ID, Version: 1, ContentJSON: "{}", PublishedBy: "analytics-test", PublishedAt: f.now}
	f.create(&version)
	treatment := model.Treatment{ID: appointment.ID + "-treatment", AppointmentID: appointment.ID, PatientID: f.patient, PlanVersionID: version.ID, Summary: "实际治疗", TreatedAt: f.now, RecordedAt: f.now, Version: 2, RequestHash: strings.Repeat("1", 64)}
	f.create(&treatment)
	for revision := 1; revision <= 2; revision++ {
		f.create(&model.TreatmentRevision{ID: fmt.Sprintf("%s-revision-%d", appointment.ID, revision), TreatmentID: treatment.ID, Version: revision, PlanVersionID: version.ID, Summary: "治疗更正", TreatedAt: treatment.TreatedAt, ChangedAt: f.now, ActorID: "analytics-test", CorrectionReason: "测试去重"})
	}
	return treatment
}

func (f *analyticsFixture) task(label string, treatment model.Treatment, dueAt, status string) {
	f.t.Helper()
	f.create(&model.FollowupTask{ID: f.prefix + "task-" + label, AppointmentID: treatment.AppointmentID, TreatmentID: &treatment.ID, PatientID: f.patient, AssigneeID: f.second, AssignedAt: f.now, Title: "改派随访", DueAt: analyticsDate(f.t, dueAt), Status: status, Version: 1})
}

func (f *analyticsFixture) ctx(clinics ...string) context.Context {
	return store.WithEmployeeScope(context.Background(), "manager", "", clinics)
}

func (f *analyticsFixture) report(ctx context.Context, from, to, groupBy, clinicID, doctorID string) store.AnalyticsReport {
	f.t.Helper()
	start, err := time.ParseInLocation("2006-01-02", from, database.Shanghai)
	if err != nil {
		f.t.Fatal(err)
	}
	end, err := time.ParseInLocation("2006-01-02", to, database.Shanghai)
	if err != nil {
		f.t.Fatal(err)
	}
	report, err := f.repo.Analytics(ctx, store.AnalyticsFilter{DateRange: store.DateRange{From: start, Until: end.AddDate(0, 0, 1)}, GroupBy: groupBy, ClinicID: clinicID, DoctorID: doctorID}, f.now)
	if err != nil {
		f.t.Fatal(err)
	}
	return report
}

func assertAnalyticsMetrics(t *testing.T, got, want store.AnalyticsMetrics) {
	t.Helper()
	if got.AppointmentCount != want.AppointmentCount || got.PendingAppointmentCount != want.PendingAppointmentCount || got.ConfirmedAppointmentCount != want.ConfirmedAppointmentCount || got.ArrivedAppointmentCount != want.ArrivedAppointmentCount || got.TreatmentCount != want.TreatmentCount || got.FollowupCount != want.FollowupCount || got.FollowupCompletedCount != want.FollowupCompletedCount || got.FollowupOverdueCount != want.FollowupOverdueCount || got.AlertCount != want.AlertCount || got.AlertHandledCount != want.AlertHandledCount || got.PendingAlertCount != want.PendingAlertCount {
		t.Fatalf("metrics=%+v want=%+v", got, want)
	}
}

func TestAnalyticsMySQLCohortsAndScope(t *testing.T) {
	f := newAnalyticsFixture(t)
	a := f.appointment("a", f.doctor, "2026-10-02T16:05:00Z", "completed", f.now, true)
	b := f.appointment("b", f.doctor, "2026-10-03T23:50:00+08:00", "completed", f.now, false)
	c := f.appointment("c", f.doctor, "2026-10-04T09:00:00+08:00", "cancelled", f.now, false)
	f.appointment("d", f.doctor, "2026-10-04T10:00:00+08:00", "pending", f.now.Add(time.Hour), false)
	f.appointment("e", f.doctor, "2026-10-04T11:00:00+08:00", "pending", f.now.Add(-time.Minute), false)
	f.appointment("f", f.doctor, "2026-10-04T11:30:00+08:00", "pending", f.now, false)
	f.appointment("g", f.second, "2026-10-04T11:15:00+08:00", "confirmed", f.now, false)
	hidden := f.appointment("h", f.other, "2026-10-04T09:00:00+08:00", "confirmed", f.now, true)
	outside := f.appointment("i", f.doctor, "2026-10-02T23:59:59+08:00", "completed", f.now, false)
	f.appointment("j", f.doctor, "2026-10-05T00:00:00+08:00", "confirmed", f.now, false)
	f.appointment("k", f.doctor, "2026-10-04T08:00:00+08:00", "rejected", f.now, false)
	f.appointment("l", f.doctor, "2026-10-04T07:00:00+08:00", "completed", f.now, false)
	for _, appointment := range []model.Appointment{a, c} {
		for duplicate := 0; duplicate < 3; duplicate++ {
			f.create(&model.AppointmentEvent{ID: fmt.Sprintf("%s-confirm-%d", appointment.ID, duplicate), AppointmentID: appointment.ID, ActorType: "operator", ActorID: "analytics-test", Action: "confirm", FromStatus: "pending", ToStatus: "confirmed", CreatedAt: f.now})
		}
	}
	treatment := f.treatment(a)
	f.task("reviewed", treatment, "2026-10-03T23:59:59+08:00", "reviewed")
	f.task("handled", treatment, "2026-10-03T16:00:00Z", "handled")
	f.task("alert", treatment, "2026-10-04T01:00:00+08:00", "alert")
	f.task("overdue", treatment, "2026-10-04T08:00:00+08:00", "pending")
	f.task("future", treatment, "2026-10-04T20:00:00+08:00", "pending")
	f.task("submitted", treatment, "2026-10-04T11:00:00+08:00", "submitted")
	f.task("after", treatment, "2026-10-05T00:00:00+08:00", "reviewed")
	f.task("before", treatment, "2026-10-02T23:59:59+08:00", "reviewed")
	f.task("second-reviewed", f.treatment(b), "2026-10-04T23:59:59+08:00", "reviewed")
	f.task("outside-slot", f.treatment(outside), "2026-10-04T12:00:00+08:00", "pending")
	f.task("hidden", f.treatment(hidden), "2026-10-04T02:00:00+08:00", "handled")

	want := store.AnalyticsMetrics{AppointmentCount: 9, PendingAppointmentCount: 1, ConfirmedAppointmentCount: 5, ArrivedAppointmentCount: 1, TreatmentCount: 2, FollowupCount: 8, FollowupCompletedCount: 3, FollowupOverdueCount: 1, AlertCount: 2, AlertHandledCount: 1, PendingAlertCount: 1}
	report := f.report(f.ctx(f.clinic), "2026-10-03", "2026-10-04", "doctor", "", "")
	assertAnalyticsMetrics(t, report.Summary, want)
	if report.MetricVersion != "v1" || report.Timezone != "Asia/Shanghai" || report.GeneratedAt.Format(time.RFC3339) != "2026-10-04T12:00:00+08:00" || len(report.Rows) != 2 || len(report.Clinics) != 1 || len(report.Doctors) != 2 {
		t.Fatalf("report metadata or scope=%+v", report)
	}
	if report.Summary.ArrivalRate == nil || *report.Summary.ArrivalRate != 20 || report.Summary.FollowupCompletionRate == nil || *report.Summary.FollowupCompletionRate != 37.5 || report.Summary.AlertHandlingRate == nil || *report.Summary.AlertHandlingRate != 50 {
		t.Fatalf("incorrect rates=%+v", report.Summary)
	}
	for _, row := range report.Rows {
		if row.ClinicID != f.clinic || row.ClinicName != "授权院区" {
			t.Fatalf("hidden clinic or wrong ownership: %+v", row)
		}
		if row.DoctorID == f.doctor {
			if row.DoctorName != "历史接诊医生" || row.Metrics.FollowupCount != 8 || row.Metrics.AppointmentCount != 8 {
				t.Fatalf("inactive or reassigned doctor attribution incorrect: %+v", row)
			}
		} else if row.Metrics.FollowupCount != 0 || row.Metrics.AppointmentCount != 1 {
			t.Fatalf("tasks attributed to assignee: %+v", row)
		}
	}

	t.Run("daily boundaries and independent due dates", func(t *testing.T) {
		day := f.report(f.ctx(f.clinic), "2026-10-03", "2026-10-05", "day", "", "")
		if len(day.Rows) != 3 || day.Rows[0].Date != "2026-10-03" || day.Rows[1].Date != "2026-10-04" || day.Rows[2].Date != "2026-10-05" {
			t.Fatalf("days=%+v", day.Rows)
		}
		assertAnalyticsMetrics(t, day.Rows[0].Metrics, store.AnalyticsMetrics{AppointmentCount: 2, ConfirmedAppointmentCount: 2, ArrivedAppointmentCount: 1, TreatmentCount: 2, FollowupCount: 1, FollowupCompletedCount: 1})
		assertAnalyticsMetrics(t, day.Rows[1].Metrics, store.AnalyticsMetrics{AppointmentCount: 7, PendingAppointmentCount: 1, ConfirmedAppointmentCount: 3, FollowupCount: 7, FollowupCompletedCount: 2, FollowupOverdueCount: 1, AlertCount: 2, AlertHandledCount: 1, PendingAlertCount: 1})
		empty := f.report(f.ctx(f.clinic), "2026-10-06", "2026-10-08", "day", "", "")
		if len(empty.Rows) != 3 || empty.Rows[0].Metrics.ArrivalRate != nil || empty.Summary.FollowupCompletionRate != nil || empty.Summary.AlertHandlingRate != nil {
			t.Fatalf("zero days or undefined rates=%+v", empty)
		}
		assertAnalyticsMetrics(t, empty.Summary, store.AnalyticsMetrics{})
	})

	t.Run("filters and rate denominator", func(t *testing.T) {
		filtered := f.report(f.ctx(f.clinic, f.hidden), "2026-10-03", "2026-10-04", "clinic", f.clinic, "")
		assertAnalyticsMetrics(t, filtered.Summary, want)
		if len(filtered.Rows) != 1 || filtered.Rows[0].DoctorID != "" || filtered.Rows[0].ClinicID != f.clinic || len(filtered.Clinics) != 2 || len(filtered.Doctors) != 3 {
			t.Fatalf("filter options or dimensions=%+v", filtered)
		}
		doctor := f.report(f.ctx(f.clinic), "2026-10-03", "2026-10-04", "doctor", "", f.doctor)
		wantDoctor := want
		wantDoctor.AppointmentCount--
		wantDoctor.ConfirmedAppointmentCount--
		assertAnalyticsMetrics(t, doctor.Summary, wantDoctor)
		if len(doctor.Rows) != 1 || doctor.Summary.ArrivalRate == nil || math.Abs(*doctor.Summary.ArrivalRate-25) > 1e-9 {
			t.Fatalf("doctor filter=%+v", doctor)
		}
	})

	t.Run("empty scope and forbidden roles", func(t *testing.T) {
		empty := f.report(f.ctx(), "2026-10-03", "2026-10-04", "day", "", "")
		assertAnalyticsMetrics(t, empty.Summary, store.AnalyticsMetrics{})
		if len(empty.Rows) != 0 || empty.Rows == nil || len(empty.Clinics) != 0 || empty.Clinics == nil || len(empty.Doctors) != 0 || empty.Doctors == nil {
			t.Fatalf("empty scope not fail closed=%+v", empty)
		}
		filter := store.AnalyticsFilter{DateRange: store.DateRange{From: analyticsDate(t, "2026-10-03T00:00:00+08:00"), Until: analyticsDate(t, "2026-10-05T00:00:00+08:00")}}
		operations := f.report(store.WithEmployeeScope(context.Background(), "operations", "", []string{f.clinic}), "2026-10-03", "2026-10-04", "", "", "")
		assertAnalyticsMetrics(t, operations.Summary, want)
		if operations.GroupBy != "doctor" {
			t.Fatalf("default grouping=%s", operations.GroupBy)
		}
		for _, ctx := range []context.Context{context.Background(), store.WithEmployeeScope(context.Background(), "doctor", f.doctor, []string{f.clinic}), store.WithEmployeeScope(context.Background(), "reception", "", []string{f.clinic})} {
			if _, err := f.repo.Analytics(ctx, filter, f.now); !errors.Is(err, store.ErrForbidden) {
				t.Fatalf("role accepted: %v", err)
			}
		}
		for _, filterID := range []struct{ clinic, doctor string }{{f.hidden, ""}, {"", f.other}, {"missing-clinic", ""}, {"", "missing-doctor"}} {
			filter.ClinicID, filter.DoctorID = filterID.clinic, filterID.doctor
			if _, err := f.repo.Analytics(f.ctx(f.clinic), filter, f.now); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("unauthorized/missing filter accepted: %+v err=%v", filterID, err)
			}
		}
	})
}

func TestAnalyticsStoreRejectsInvalidInputBeforeDatabase(t *testing.T) {
	repo := store.New(nil)
	ctx := store.WithEmployeeScope(context.Background(), "manager", "", []string{"clinic-demo"})
	from := analyticsDate(t, "2026-10-03T00:00:00+08:00")
	for _, filter := range []store.AnalyticsFilter{
		{},
		{DateRange: store.DateRange{From: from, Until: from}},
		{DateRange: store.DateRange{From: from, Until: from.AddDate(0, 0, 367)}},
		{DateRange: store.DateRange{From: from.Add(time.Minute), Until: from.AddDate(0, 0, 1)}},
		{DateRange: store.DateRange{From: from, Until: from.AddDate(0, 0, 1)}, GroupBy: "assignee"},
		{DateRange: store.DateRange{From: from, Until: from.AddDate(0, 0, 1)}, DoctorID: "'"},
	} {
		if _, err := repo.Analytics(ctx, filter, from); !errors.Is(err, store.ErrAnalyticsInput) {
			t.Fatalf("filter=%+v err=%v", filter, err)
		}
	}
	if _, err := repo.Analytics(context.Background(), store.AnalyticsFilter{}, from); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("missing employee scope err=%v", err)
	}
}
