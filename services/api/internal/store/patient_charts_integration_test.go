package store_test

import (
	"context"
	"errors"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"testing"
)

func TestPatientChartsWithoutAppointments(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	doctor := store.WithEmployeeScope(ctx, "doctor", f.slot.DoctorID, []string{"clinic-demo"})
	manager := store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"})
	nurse := store.WithEmployeeScope(ctx, "nurse", f.slot.DoctorID, []string{"clinic-demo"})
	outsider := store.WithEmployeeScope(ctx, "doctor", "unassigned-doctor", []string{"clinic-demo"})
	otherClinic := store.WithEmployeeScope(ctx, "manager", "", []string{"other-clinic"})
	patient, err := f.repo.CreatePatientChart(doctor, "独立档案", model.PatientChart{ClinicID: "clinic-demo", Gender: "female", Tags: []string{"敏感肌"}}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.db.Where("patient_id = ?", patient.ID).Delete(&model.Consultation{})
		f.db.Where("patient_id = ?", patient.ID).Delete(&model.PatientChart{})
		f.db.Where("id = ?", patient.ID).Delete(&model.Patient{})
	})
	for _, allowed := range []context.Context{doctor, manager} {
		rows, total, err := f.repo.PatientDirectory(allowed, "独立档案", 1, 20, "")
		if err != nil || total != 1 || len(rows) != 1 || rows[0].Profile == nil || rows[0].AppointmentCount != 0 {
			t.Fatalf("direct directory: %+v %d %v", rows, total, err)
		}
	}
	for _, denied := range []context.Context{outsider, otherClinic} {
		if _, _, err := f.repo.PatientRecordAppointments(denied, patient.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("scope leaked: %v", err)
		}
	}
	if _, err := f.repo.CreatePatientChart(nurse, "不允许", model.PatientChart{ClinicID: "clinic-demo"}, f.now); !errors.Is(err, store.ErrForbidden) {
		t.Fatal("nurse created chart", err)
	}
	consultation, err := f.repo.CreateConsultation(doctor, model.Consultation{PatientID: patient.ID, ClinicID: "clinic-demo", ChiefComplaint: "直接问诊", AllergyHistory: "已核对", RecordedBy: "doctor-chart", RecordedAt: f.now})
	if err != nil || consultation.ID == "" {
		t.Fatal("independent consultation failed", err)
	}
	if _, err := f.repo.CreateConsultation(outsider, model.Consultation{PatientID: patient.ID, ClinicID: "clinic-demo", RecordedAt: f.now}); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("outsider wrote consultation", err)
	}
	tagged, total, err := f.repo.PatientDirectoryWithTag(manager, "", 1, 20, "敏感肌", "")
	if err != nil || total != 1 || len(tagged) != 1 {
		t.Fatal("tag filter failed", err)
	}
	_, total, err = f.repo.PatientDirectoryWithTag(manager, "", 1, 20, "不存在", "")
	if err != nil || total != 0 {
		t.Fatal("tag filter returned unmatched patients", err)
	}
	profile := *patient.Profile
	profile.Tags = []string{"敏感肌", "复诊跟进"}
	saved, err := f.repo.SavePatientChart(doctor, "更新姓名", profile)
	if err != nil || saved.Version != 2 || len(saved.Tags) != 2 {
		t.Fatalf("save chart: %+v %v", saved, err)
	}
	if _, err := f.repo.SavePatientChart(doctor, "过期更新", profile); !errors.Is(err, store.ErrConflict) {
		t.Fatal("stale update accepted", err)
	}
	latest, _, err := f.repo.PatientRecordAppointments(manager, patient.ID)
	if err != nil || latest.DisplayName != "更新姓名" || latest.Profile.Version != 2 {
		t.Fatalf("chart not persisted: %+v %v", latest, err)
	}
	template, err := f.repo.CreateConsultationTemplate(manager, model.ConsultationTemplate{ClinicID: "clinic-demo", Name: "初诊模板", VisitType: "initial", RecordedBy: "chart-manager", CreatedAt: f.now, AllergyHistory: "过敏史待填写"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.db.Where("id = ?", template.ID).Delete(&model.ConsultationTemplate{}) })
	rows, err := f.repo.ConsultationTemplates(manager)
	if err != nil || len(rows) != 1 || rows[0].AllergyHistory != "过敏史待填写" {
		t.Fatal("template not persisted", err)
	}
	if err := f.repo.DeleteConsultationTemplate(otherClinic, template.ID, "chart-manager"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("cross clinic deleted template", err)
	}
	if err := f.repo.DeleteConsultationTemplate(manager, template.ID, "chart-manager"); err != nil {
		t.Fatal(err)
	}
}
