package store_test

import (
	"context"
	"errors"
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"testing"
	"time"
)

func TestConsultationRevisitsAndDateRange(t *testing.T) {
	f := newBookingFixture(t)
	patient := f.patient()
	appointment := f.create(patient, "clinical-record-request-1")
	other := f.patient()
	f.create(other, "clinical-record-request-2")
	ctx := store.WithEmployeeScope(context.Background(), "manager", "", []string{"clinic-demo"})
	day := time.Date(f.now.Year(), f.now.Month(), f.now.Day(), 0, 0, 0, 0, database.Shanghai)
	initial, err := f.repo.CreateConsultation(ctx, model.Consultation{PatientID: patient.Patient.ID, ChiefComplaint: "initial", VisitedAt: day.Add(-time.Hour), RecordedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	next := f.now.AddDate(0, 0, 7)
	revisit, err := f.repo.CreateConsultation(ctx, model.Consultation{PatientID: patient.Patient.ID, ChiefComplaint: "revisit", VisitType: "revisit", ParentConsultationID: &initial.ID, AppointmentID: &appointment.ID, VisitedAt: day, RecordedAt: f.now, NextVisitAt: &next, StageIndex: 1, StageStatus: "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := f.repo.PatientConsultations(ctx, patient.Patient.ID, store.DateRange{From: day, Until: day.AddDate(0, 0, 1)})
	if err != nil || len(rows) != 1 || rows[0].ID != revisit.ID || rows[0].ParentConsultationID == nil || rows[0].StageStatus != "in_progress" {
		t.Fatalf("date range and revisit: %+v %v", rows, err)
	}
	invalid := revisit
	invalid.PatientID = other.Patient.ID
	if _, err := f.repo.CreateConsultation(ctx, invalid); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("cross patient link: %v", err)
	}
	invalid = revisit
	invalid.AppointmentID = nil
	invalid.ParentConsultationID = nil
	if _, err := f.repo.CreateConsultation(ctx, invalid); !errors.Is(err, store.ErrCareState) {
		t.Fatalf("unlinked revisit: %v", err)
	}
	clinician := store.WithEmployeeScope(context.Background(), "doctor", f.slot.DoctorID, []string{"clinic-demo"})
	invalid = revisit
	if _, err := f.repo.CreateConsultation(clinician, invalid); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("hidden parent link: %v", err)
	}
	invalid = revisit
	invalid.VisitedAt = initial.VisitedAt.Add(-time.Hour)
	if _, err := f.repo.CreateConsultation(ctx, invalid); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("chronological link: %v", err)
	}
}
func TestStructuredTreatmentHistoryAndCalendarRange(t *testing.T) {
	f, patient, appointment, input := newFollowupFixture(t)
	ctx := context.Background()
	input.TreatmentFields = model.TreatmentFields{StageIndex: 2, Procedure: "test", BodyArea: "face", Device: "device", Parameters: "test parameters"}
	if _, err := f.repo.RecordTreatment(ctx, appointment.ID, input, f.now); !errors.Is(err, store.ErrCareState) {
		t.Fatalf("nonexistent stage: %v", err)
	}
	input.StageIndex = 1
	treatment, err := f.repo.RecordTreatment(ctx, appointment.ID, input, f.now)
	if err != nil {
		t.Fatal(err)
	}
	procedure := "corrected procedure"
	corrected, err := f.repo.CorrectTreatment(ctx, appointment.ID, store.TreatmentCorrectionInput{Version: treatment.Version, PlanVersionID: input.PlanVersionID, Summary: input.Summary, Notes: input.Notes, TreatedAt: input.TreatedAt, CorrectionReason: "field correction", Fields: store.TreatmentFieldPatch{Procedure: &procedure}}, f.now)
	if err != nil || corrected.Procedure != procedure || corrected.Device != input.Device {
		t.Fatalf("correction fields: %+v %v", corrected, err)
	}
	revisions, err := f.repo.TreatmentRevisions(ctx, appointment.ID, patient.Patient.ID)
	if err != nil || len(revisions) != 2 || revisions[1].Procedure != input.Procedure || revisions[0].Procedure != procedure {
		t.Fatalf("immutable history: %+v %v", revisions, err)
	}
	_, tasks, err := f.repo.FollowupWorkspace(ctx, appointment.ID, patient.Patient.ID)
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(f.now.Year(), f.now.Month(), f.now.Day(), 0, 0, 0, 0, database.Shanghai)
	// Due dates outside the window still match submissions and reviews inside it.
	for index, task := range tasks {
		if err := f.db.Model(&model.FollowupTask{}).Where("id = ?", task.ID).Updates(map[string]any{"due_at": day.AddDate(0, 0, -10).Add(time.Duration(index) * time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.Model(&model.FollowupTask{}).Where("id = ?", tasks[0].ID).Updates(map[string]any{"submitted_at": day, "reviewed_at": day.Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.FollowupTask{}).Where("id = ?", tasks[1].ID).Updates(map[string]any{"reviewed_at": day.AddDate(0, 0, 1).Add(-time.Millisecond)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.FollowupTask{}).Where("id = ?", tasks[2].ID).Updates(map[string]any{"reviewed_at": day.AddDate(0, 0, 1)}).Error; err != nil {
		t.Fatal(err)
	}
	filter := store.FollowupFilter{PatientID: patient.Patient.ID, Dates: store.DateRange{From: day, Until: day.AddDate(0, 0, 1)}, Page: 1, PageSize: 1, Now: f.now}
	rows, total, err := f.repo.FollowupQueue(ctx, filter)
	if err != nil || total != 2 || len(rows) != 1 {
		t.Fatalf("calendar page1: %+v %d %v", rows, total, err)
	}
	first := rows[0].ID
	filter.Page = 2
	rows, total, err = f.repo.FollowupQueue(ctx, filter)
	if err != nil || total != 2 || len(rows) != 1 || rows[0].ID == first {
		t.Fatalf("calendar page2: %+v %d %v", rows, total, err)
	}
	filter.PatientID = "another-patient"
	filter.Page = 1
	rows, total, err = f.repo.FollowupQueue(ctx, filter)
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatalf("OR predicate leaked another patient: %+v %d %v", rows, total, err)
	}
}
