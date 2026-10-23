package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
)

func TestRecordExportMaskedAllowlistRemovesEmbeddedPII(t *testing.T) {
	job := model.RecordExportJob{ID: "public-export-id", Masked: true, ReferenceKey: strings.Repeat("a", 64)}
	private := "赵某 13812345678 地址 身份证 patient-source-id"
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.FixedZone("Shanghai", 8*60*60))
	consultation := model.Consultation{ID: "consultation-source-id", ChiefComplaint: private, History: private, Assessment: private, Advice: private, VisitType: "revisit", StageStatus: "completed", StageIndex: 2, VisitedAt: now, RecordedAt: now}
	planContent, _ := json.Marshal(CarePlanContent{Summary: private, Instructions: private, Stages: []CarePlanStage{{Title: private, Body: private}}})
	plan, err := archivePlanDTO(job, model.CarePlanVersion{ID: "plan-source-id", ContentJSON: string(planContent), Version: 2, ChangeReason: private, PublishedBy: private, PublishedAt: now}, "appointment-source-id")
	if err != nil {
		t.Fatal(err)
	}
	fields := model.TreatmentFields{StageIndex: 2, Procedure: private, BodyArea: private, Device: private, Parameters: private}
	values := []any{
		archiveAppointmentDTO(job, model.Appointment{ID: "appointment-source-id", ContactName: private, ContactPhone: private, Note: private, RejectionReason: private, ClinicName: private, DoctorName: private, RequestKey: private, RequestHash: private, Status: "completed", StartsAt: now}),
		archiveConsultationDTO(job, consultation),
		plan,
		archiveTreatmentDTO(job, model.Treatment{ID: "treatment-source-id", AppointmentID: "appointment-source-id", PlanVersionID: "plan-source-id", TreatmentFields: fields, Summary: private, Notes: private, RequestHash: private, Version: 2, TreatedAt: now}, []model.TreatmentRevision{{ID: "revision-source-id", PlanVersionID: "plan-source-id", TreatmentFields: fields, Summary: private, Notes: private, CorrectionReason: private, ActorID: private, Version: 1}}),
		archiveFollowupDTO(job, model.FollowupTask{ID: "task-source-id", AppointmentID: "appointment-source-id", TreatmentID: "treatment-source-id", Title: private, Symptoms: private, Response: private, Disposition: private, AssigneeID: private, PatientName: private, Status: "reviewed", Severity: 2, Concern: true, DueAt: now}),
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{private, "-source-id", "requestHash", "requestKey", "content", "correctionReason", "assigneeId", "contactName", "contactPhone", "procedure", "parameters", "referenceKey"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("masked archive leaked %s: %s", forbidden, encoded)
		}
	}
	for _, expected := range []string{`"stageIndex":2`, `"severity":2`, `"concern":true`, `"hasFreeText":true`, `"revisions":[`, `"stageCount":1`, `"reference":"ref-`} {
		if !strings.Contains(string(encoded), expected) {
			t.Fatalf("structured export missing %s", expected)
		}
	}
	job.Masked = false
	full, err := json.Marshal(archiveConsultationDTO(job, consultation))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(full), private) || !strings.Contains(string(full), consultation.ID) {
		t.Fatal("authorized full archive omitted clinical data")
	}
}

func TestRecordExportReferencesRequirePrivatePerJobKey(t *testing.T) {
	a := archiveReference(strings.Repeat("a", 64), "patient-id")
	if a == archiveReference(strings.Repeat("b", 64), "patient-id") || a == archiveReference(strings.Repeat("a", 64), "other-id") {
		t.Fatal("references must differ between jobs and records")
	}
	if a != archiveReference(strings.Repeat("a", 64), "patient-id") {
		t.Fatal("same archive relationships must be stable")
	}
	if archiveReference("key", "") != "" {
		t.Fatal("missing relationships should stay absent")
	}
	job := model.RecordExportJob{ReferenceKey: strings.Repeat("a", 64), SnapshotJSON: "secret-snapshot", Payload: []byte("private-file"), LeaseToken: "private-lease"}
	encoded, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"secret-snapshot", "private-file", "private-lease", strings.Repeat("a", 64)} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("metadata leaked private state")
		}
	}
	if !strings.Contains(string(encoded), `"completedAt":null`) || !strings.Contains(string(encoded), `"expiresAt":null`) {
		t.Fatal("queued timestamps must be nullable")
	}
}

func TestRecordExportInputUsesBoundedShanghaiDates(t *testing.T) {
	base := RecordExportInput{PatientIDs: []string{"patient"}, Sections: []string{"appointments"}, Reason: "临床交接", Masked: true}
	for _, dates := range [][2]string{{"2026-01-01", ""}, {"", "2026-01-01"}, {"2026-02-30", "2026-03-01"}, {"2026-03-01", "2026-02-01"}, {"2026-01-01", "2027-01-02"}} {
		input := base
		input.DateFrom, input.DateTo = dates[0], dates[1]
		if _, _, err := validateRecordExport(input); err != ErrRecordExportInput {
			t.Fatalf("invalid dates allowed: %v %v", dates, err)
		}
	}
	input := base
	input.DateFrom, input.DateTo = "2026-01-01", "2027-01-01"
	_, dates, err := validateRecordExport(input)
	if err != nil || dates.Until.Sub(dates.From) != 366*24*time.Hour || dates.From.Format("-07:00") != "+08:00" {
		t.Fatalf("inclusive 366-day Shanghai range: %+v %v", dates, err)
	}
	input = base
	input.DateFrom, input.DateTo = "1991-06-01", "1992-05-31"
	if _, _, err := validateRecordExport(input); err != nil {
		t.Fatal("366 natural days across historical Shanghai DST rejected", err)
	}
	input = base
	input.Sections = []string{"appointments", "appointments"}
	if _, _, err := validateRecordExport(input); err != ErrRecordExportInput {
		t.Fatal("duplicate section accepted")
	}
	input = base
	input.PatientIDs = []string{"patient", "patient"}
	if _, _, err := validateRecordExport(input); err != ErrRecordExportInput {
		t.Fatal("duplicate patient accepted")
	}
	input = base
	input.Reason = strings.Repeat("中", 201)
	if _, _, err := validateRecordExport(input); err != ErrRecordExportInput {
		t.Fatal("reason limit must count characters")
	}
}
