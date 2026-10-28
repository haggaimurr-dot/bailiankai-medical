package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func exportEmployee(t *testing.T, f *bookingFixture, role, staffID string) model.EmployeeAccount {
	t.Helper()
	ctx := store.WithClinicAssignment(context.Background(), []string{"clinic-demo"})
	account, err := f.repo.CreateEmployee(ctx, fmt.Sprintf("export-%s-%d", role, time.Now().UnixNano()), "导出测试员工", "RecordExportTest123!", role, staffID, f.now)
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func exportClinicalRecords(t *testing.T, f *bookingFixture, patientID, appointmentID string) (model.CarePlanVersion, model.Treatment, model.FollowupTask) {
	t.Helper()
	private := "隐私哨兵 赵某 13812345678 住址"
	draft := `{"summary":"未发布草稿哨兵","stages":[],"instructions":""}`
	plan := model.CarePlan{ID: "archive-plan", AppointmentID: appointmentID, DraftVersion: 3, DraftJSON: &draft, UpdatedAt: f.now}
	if err := f.db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	content, _ := json.Marshal(store.CarePlanContent{Summary: private, Stages: []store.CarePlanStage{{Title: private, Body: private}}, Instructions: private})
	version := model.CarePlanVersion{ID: "archive-published-version", CarePlanID: plan.ID, Version: 1, ContentJSON: string(content), ChangeReason: private, PublishedBy: "private-publisher", PublishedAt: f.now}
	if err := f.db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	treatment := model.Treatment{ID: "archive-treatment", AppointmentID: appointmentID, PatientID: patientID, PlanVersionID: version.ID, Summary: private, Notes: private, TreatedAt: f.now, RecordedAt: f.now, Version: 1, RequestHash: strings.Repeat("f", 64), TreatmentFields: model.TreatmentFields{StageIndex: 1, Procedure: private, BodyArea: private, Device: private, Parameters: private}}
	if err := f.db.Create(&treatment).Error; err != nil {
		t.Fatal(err)
	}
	revision := model.TreatmentRevision{ID: "archive-revision", TreatmentID: treatment.ID, Version: 1, PlanVersionID: version.ID, Summary: private, Notes: private, TreatedAt: f.now, ChangedAt: f.now, ActorID: "private-author", CorrectionReason: private, TreatmentFields: treatment.TreatmentFields}
	if err := f.db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}
	task := model.FollowupTask{ID: "archive-followup", AppointmentID: appointmentID, TreatmentID: &treatment.ID, PatientID: patientID, AssigneeID: f.slot.DoctorID, AssignedAt: f.now, Title: private, DueAt: f.now.AddDate(0, 0, 2), Status: "pending", Version: 1, Symptoms: private, Response: private, Disposition: private, Severity: 2}
	if err := f.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	return version, treatment, task
}

func exportInput(patientIDs ...string) store.RecordExportInput {
	return store.RecordExportInput{PatientIDs: patientIDs, Sections: []string{"appointments", "consultations", "care_plans", "treatments", "followups"}, Masked: true, Reason: "临床交接"}
}

func TestRecordExportDurabilityPrivacyAndExpiry(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	patient := f.patient()
	appointment := f.create(patient, "archive-patient")
	manager := exportEmployee(t, f, "manager", "")
	_, treatment, task := exportClinicalRecords(t, f, patient.Patient.ID, appointment.ID)
	managerCtx := store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"})
	consultation, err := f.repo.CreateConsultation(managerCtx, model.Consultation{PatientID: patient.Patient.ID, ChiefComplaint: "隐私哨兵 13812345678", RecordedBy: manager.ID, RecordedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	job, err := f.repo.CreateRecordExport(ctx, manager.ID, exportInput(patient.Patient.ID), f.now)
	if err != nil || job.Status != "queued" {
		t.Fatalf("queue job: %+v %v", job, err)
	}
	metadata, _ := json.Marshal(job)
	if strings.Contains(string(metadata), patient.Patient.ID) || strings.Contains(string(metadata), "snapshotJson") {
		t.Fatal("metadata leaked patient selection")
	}
	if _, _, err := f.repo.DownloadRecordExport(ctx, manager.ID, job.ID, f.now); !errors.Is(err, store.ErrRecordExportNotReady) {
		t.Fatal("queued job was downloadable", err)
	}
	// Simulate a crashed processing lease, then create a fresh repository instance.
	if err := f.db.Model(&model.RecordExportJob{}).Where("id = ?", job.ID).Updates(map[string]any{"status": "processing", "lease_until": f.now.Add(-time.Minute), "lease_token": "stale-worker"}).Error; err != nil {
		t.Fatal(err)
	}
	restarted := store.New(f.db)
	if err := restarted.ProcessRecordExports(ctx, f.now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	ready, payload, err := restarted.DownloadRecordExport(ctx, manager.ID, job.ID, f.now.Add(2*time.Second))
	if err != nil || ready.Status != "ready" || ready.DownloadCount != 1 || ready.ExpiresAt == nil || ready.CompletedAt == nil || ready.ExpiresAt.Sub(*ready.CompletedAt) != 24*time.Hour || ready.SizeBytes != int64(len(payload)) {
		t.Fatalf("ready archive: %+v %v", ready, err)
	}
	for _, private := range []string{patient.Patient.ID, appointment.ID, treatment.ID, task.ID, consultation.ID, "隐私哨兵", "13812345678", "未发布草稿哨兵", strings.Repeat("f", 64), "photo", "draft", "snapshotJson", "referenceKey"} {
		if strings.Contains(string(payload), private) {
			t.Fatalf("archive leaked %s", private)
		}
	}
	if !strings.Contains(string(payload), `"revisions": [`) {
		t.Fatal("treatment history missing")
	}
	if _, _, err := restarted.DownloadRecordExport(ctx, "another-employee", job.ID, f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("job ownership not enforced", err)
	}
	expired, _, err := restarted.DownloadRecordExport(ctx, manager.ID, job.ID, *ready.ExpiresAt)
	if !errors.Is(err, store.ErrRecordExportExpired) || expired.Status != "expired" {
		t.Fatal("deadline download accepted", err)
	}
	var saved model.RecordExportJob
	if err := f.db.First(&saved, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(saved.Payload) != 0 || saved.Status != "expired" {
		t.Fatal("expired archive bytes not deleted")
	}
	events, err := restarted.RecordExportEvents(ctx, manager.ID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]int{}
	for _, event := range events {
		actions[event.Action]++
	}
	for _, action := range []string{"requested", "generated", "downloaded", "download_denied", "expired"} {
		if actions[action] == 0 {
			t.Fatalf("missing %s audit", action)
		}
	}
	if actions["expired"] != 1 {
		t.Fatal("expiry audit duplicated")
	}
	encoded, _ := json.Marshal(events)
	if strings.Contains(string(encoded), patient.Patient.ID) || strings.Contains(string(encoded), "隐私哨兵") {
		t.Fatal("audit leaked patient data")
	}
}

func TestRecordExportAuthorizationQuotaAndGenerationRevocation(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	patient := f.patient()
	f.create(patient, "archive-auth")
	manager := exportEmployee(t, f, "manager", "")
	operations := exportEmployee(t, f, "operations", "")
	doctor := exportEmployee(t, f, "doctor", f.slot.DoctorID)
	fakeScope := store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"})
	if _, err := f.repo.CreateRecordExport(fakeScope, operations.ID, exportInput(patient.Patient.ID), f.now); !errors.Is(err, store.ErrForbidden) {
		t.Fatal("forged manager context bypassed database role", err)
	}
	full := exportInput(patient.Patient.ID)
	full.Masked = false
	if _, err := f.repo.CreateRecordExport(fakeScope, doctor.ID, full, f.now); !errors.Is(err, store.ErrForbidden) {
		t.Fatal("clinical full export allowed", err)
	}
	if _, err := f.repo.CreateRecordExport(ctx, manager.ID, exportInput(patient.Patient.ID, "inaccessible-patient"), f.now); !errors.Is(err, store.ErrForbidden) {
		t.Fatal("partial inaccessible selection accepted", err)
	}
	jobs := []model.RecordExportJob{}
	for i := 0; i < 3; i++ {
		job, err := f.repo.CreateRecordExport(ctx, manager.ID, exportInput(patient.Patient.ID), f.now)
		if err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, job)
	}
	if _, err := f.repo.CreateRecordExport(ctx, manager.ID, exportInput(patient.Patient.ID), f.now); !errors.Is(err, store.ErrRecordExportQuota) {
		t.Fatal("quota not enforced", err)
	}
	if err := f.db.Model(&model.EmployeeAccount{}).Where("id = ?", manager.ID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.repo.ProcessRecordExports(ctx, f.now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		var saved model.RecordExportJob
		if err := f.db.First(&saved, "id = ?", job.ID).Error; err != nil {
			t.Fatal(err)
		}
		if saved.Status != "failed" || saved.ErrorCode != "ACCESS_REVOKED" || len(saved.Payload) != 0 {
			t.Fatalf("revoked worker generated archive %+v", saved)
		}
	}
	if _, _, err := f.repo.DownloadRecordExport(ctx, manager.ID, jobs[0].ID, f.now); !errors.Is(err, store.ErrForbidden) {
		t.Fatal("inactive owner downloaded", err)
	}
	var denied int64
	if err := f.db.Model(&model.RecordExportEvent{}).Where("job_id = ? AND action = ?", jobs[0].ID, "download_denied").Count(&denied).Error; err != nil || denied != 1 {
		t.Fatal("revoked download denial not audited", err)
	}
}

func TestRecordExportClinicalTaskAccessRecheckedOnDownload(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	patient := f.patient()
	appointment := f.create(patient, "archive-clinical")
	doctor := exportEmployee(t, f, "doctor", f.slot.DoctorID)
	_, _, task := exportClinicalRecords(t, f, patient.Patient.ID, appointment.ID)
	otherTask := task
	otherTask.ID = "archive-other-staff-task"
	otherTask.AssigneeID = "nurse-demo-1"
	otherTask.DueAt = otherTask.DueAt.Add(time.Hour)
	if err := f.db.Create(&otherTask).Error; err != nil {
		t.Fatal(err)
	}
	input := exportInput(patient.Patient.ID)
	input.Sections = []string{"followups"}
	job, err := f.repo.CreateRecordExport(ctx, doctor.ID, input, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.repo.ProcessRecordExports(ctx, f.now); err != nil {
		t.Fatal(err)
	}
	_, payload, err := f.repo.DownloadRecordExport(ctx, doctor.ID, job.ID, f.now)
	if err != nil {
		t.Fatal(err)
	}
	var archive struct {
		Patients []struct {
			Followups []json.RawMessage `json:"followups"`
		} `json:"patients"`
	}
	if err := json.Unmarshal(payload, &archive); err != nil || len(archive.Patients) != 1 || len(archive.Patients[0].Followups) != 1 {
		t.Fatal("clinical export included another assignee task", err)
	}
	// Treating doctor retains appointment access, but loses the exported task.
	if err := f.db.Model(&model.FollowupTask{}).Where("id = ?", task.ID).Update("assignee_id", "nurse-demo-1").Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.repo.DownloadRecordExport(ctx, doctor.ID, job.ID, f.now); !errors.Is(err, store.ErrForbidden) {
		t.Fatal("captured task reassignment did not revoke download", err)
	}
	var events []model.RecordExportEvent
	if err := f.db.Where("job_id = ? AND action = ?", job.ID, "download_denied").Find(&events).Error; err != nil || len(events) != 1 || events[0].DetailCode != "ACCESS_REVOKED" {
		t.Fatal("resource revocation not audited", err)
	}
}

func TestRecordExportIndependentDatesAndFrozenSelection(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	patient := f.patient()
	appointment := f.create(patient, "archive-dates")
	manager := exportEmployee(t, f, "manager", "")
	_, _, task := exportClinicalRecords(t, f, patient.Patient.ID, appointment.ID)
	date := task.DueAt.In(f.now.Location()).Format("2006-01-02")
	input := exportInput(patient.Patient.ID)
	input.Sections = []string{"appointments", "followups"}
	input.DateFrom, input.DateTo = date, date
	job, err := f.repo.CreateRecordExport(ctx, manager.ID, input, f.now)
	if err != nil {
		t.Fatal(err)
	}
	// New records after enqueue must not expand the captured selection.
	newTask := task
	newTask.ID = "archive-new-task"
	newTask.DueAt = newTask.DueAt.Add(time.Hour)
	if err := f.db.Create(&newTask).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.repo.ProcessRecordExports(ctx, f.now); err != nil {
		t.Fatal(err)
	}
	_, payload, err := f.repo.DownloadRecordExport(ctx, manager.ID, job.ID, f.now)
	if err != nil {
		t.Fatal(err)
	}
	var archive struct {
		Patients []struct {
			Appointments []json.RawMessage `json:"appointments"`
			Followups    []json.RawMessage `json:"followups"`
		} `json:"patients"`
	}
	if err := json.Unmarshal(payload, &archive); err != nil || len(archive.Patients) != 1 || len(archive.Patients[0].Appointments) != 0 || len(archive.Patients[0].Followups) != 1 {
		t.Fatalf("independent dates/frozen selection incorrect: %s %v", payload, err)
	}
	full := exportInput(patient.Patient.ID)
	full.Masked = false
	job, err = f.repo.CreateRecordExport(ctx, manager.ID, full, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.repo.ProcessRecordExports(ctx, f.now); err != nil {
		t.Fatal(err)
	}
	_, payload, err = f.repo.DownloadRecordExport(ctx, manager.ID, job.ID, f.now)
	if err != nil || !strings.Contains(string(payload), "隐私哨兵") || !strings.Contains(string(payload), patient.Patient.ID) {
		t.Fatal("authorized full archive failed", err)
	}
	if err := f.db.Model(&model.EmployeeAccount{}).Where("id = ?", manager.ID).Update("clinic_ids", "[]").Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.repo.DownloadRecordExport(ctx, manager.ID, job.ID, f.now); !errors.Is(err, store.ErrForbidden) {
		t.Fatal("clinic revocation did not revoke existing payload", err)
	}
}
