package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPatientRecordsAggregateAndScope(t *testing.T) {
	fixture := newBookingFixture(t)
	ctx := context.Background()
	patient := fixture.patient()
	other := fixture.patient()
	first := fixture.create(patient, "patient-record-first")
	fixture.create(other, "patient-record-other")
	originalDoctor := fixture.slot.DoctorID
	fixture.slot = model.Slot{}
	if err := fixture.db.Where("doctor_id <> ? AND starts_at > ?", originalDoctor, fixture.now.Add(3*time.Hour)).Order("starts_at").First(&fixture.slot).Error; err != nil {
		t.Fatal(err)
	}
	second := fixture.create(patient, "patient-record-second")
	profile, err := fixture.repo.UpdatePatientProfile(ctx, patient.Patient.ID, "档案患者", patient.Patient.Version)
	if err != nil {
		t.Fatal(err)
	}
	manager := store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"})
	summary, appointments, err := fixture.repo.PatientRecordAppointments(manager, patient.Patient.ID)
	if err != nil || summary.AppointmentCount != 2 || len(appointments) != 2 {
		t.Fatalf("patient aggregate: %+v %v", summary, err)
	}
	if appointments[0].ID != first.ID && appointments[0].ID != second.ID {
		t.Fatal("unexpected appointment")
	}
	rows, total, err := fixture.repo.PatientDirectory(manager, "档案", 1, 20, "")
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != profile.ID || rows[0].AppointmentCount != 2 {
		t.Fatalf("directory: %+v %d %v", rows, total, err)
	}
	rows, total, err = fixture.repo.PatientDirectory(manager, "%", 1, 20, "")
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatal("search wildcard escaped incorrectly", err)
	}
	clinician := store.WithEmployeeScope(ctx, "clinician", originalDoctor, []string{"clinic-demo"})
	_, appointments, err = fixture.repo.PatientRecordAppointments(clinician, patient.Patient.ID)
	if err != nil || len(appointments) != 1 || appointments[0].ID != first.ID {
		t.Fatal("cross-doctor appointment leaked", err)
	}
	unavailable := store.WithEmployeeScope(ctx, "clinician", "unknown-doctor", []string{"clinic-demo"})
	if _, _, err := fixture.repo.PatientRecordAppointments(unavailable, patient.Patient.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("unassigned clinician read patient", err)
	}
	row, err := fixture.repo.CreateConsultation(manager, model.Consultation{PatientID: patient.Patient.ID, ChiefComplaint: "痘坑问诊", History: "无过敏史", RecordedBy: "manager-record", RecordedAt: fixture.now})
	if err != nil || row.ID == "" {
		t.Fatal("consultation not persisted", err)
	}
	records, err := fixture.repo.PatientConsultations(manager, patient.Patient.ID)
	if err != nil || len(records) != 1 || records[0].ChiefComplaint != "痘坑问诊" {
		t.Fatal("consultation read failed", err)
	}
	records, err = fixture.repo.PatientConsultations(manager, other.Patient.ID)
	if err != nil || len(records) != 0 {
		t.Fatal("other patient consultation leaked", err)
	}
	records, err = fixture.repo.PatientConsultations(clinician, patient.Patient.ID)
	if err != nil || len(records) != 0 {
		t.Fatal("other author's consultation leaked", err)
	}
	if _, err := fixture.repo.CreateConsultation(unavailable, model.Consultation{PatientID: patient.Patient.ID}); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("unassigned clinician wrote consultation", err)
	}
	username := fmt.Sprintf("records-%d", time.Now().UnixNano())
	account, err := fixture.repo.CreateEmployee(ctx, username, "档案管理员", "PatientRecordsTest123", "manager", "", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		fixture.db.Where("employee_id = ?", account.ID).Delete(&model.EmployeeSession{})
		fixture.db.Where("employee_id = ?", account.ID).Delete(&model.EmployeeAccountEvent{})
		fixture.db.Where("id = ?", account.ID).Delete(&model.EmployeeAccount{})
	})
	_, token, err := fixture.repo.LoginEmployee(ctx, username, "PatientRecordsTest123", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/v1/admin/patients/"+patient.Patient.ID, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("patient record API: %d %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data struct {
			Encounters    []json.RawMessage
			Consultations []model.Consultation
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || len(payload.Data.Encounters) != 2 || len(payload.Data.Consultations) != 1 {
		t.Fatal("incorrect API aggregate", err)
	}
	request = httptest.NewRequest("POST", "/api/v1/admin/patients/"+patient.Patient.ID+"/consultations", strings.NewReader(`{"chiefComplaint":"独立问诊","history":"","assessment":"","advice":""}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != 201 {
		t.Fatalf("consultation API: %d %s", response.Code, response.Body.String())
	}
	idleAccount := fixture.patient()
	if _, _, err := fixture.repo.PatientRecordAppointments(manager, idleAccount.Patient.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("unassigned login account visible in directory", err)
	}
	idlePatient, err := fixture.repo.CreatePatientChart(manager, "无预约人工档案", model.PatientChart{ClinicID: "clinic-demo", Gender: "unknown", Tags: []string{}}, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	_, appointments, err = fixture.repo.PatientRecordAppointments(manager, idlePatient.ID)
	if err != nil || len(appointments) != 0 {
		t.Fatal("patient without appointments missing", err)
	}
	if _, err := fixture.repo.CreateConsultation(manager, model.Consultation{PatientID: idlePatient.ID, ChiefComplaint: "无预约问诊", RecordedBy: account.ID, RecordedAt: fixture.now}); err != nil {
		t.Fatal("consultation requires appointment", err)
	}
}

func TestPatientDirectoryPhoneLast4(t *testing.T) {
	fixture := newBookingFixture(t)
	ctx := context.Background()
	patient := fixture.patient()
	if _, err := fixture.repo.UpdatePatientProfile(ctx, patient.Patient.ID, "后四位患者", patient.Patient.Version); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.Patient{}).Where("id = ?", patient.Patient.ID).Update("phone", "13800001005").Error; err != nil {
		t.Fatal(err)
	}
	manager := store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"})
	rows, total, err := fixture.repo.PatientDirectory(manager, "", 1, 20, "1005")
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != patient.Patient.ID {
		t.Fatalf("phone last4 match: %+v %d %v", rows, total, err)
	}
	if rows[0].PhoneMasked != "138****1005" {
		t.Fatalf("phone not masked: %q", rows[0].PhoneMasked)
	}
	if rows[0].Phone != "" {
		t.Fatal("raw phone leaked in directory response")
	}
	rows, total, err = fixture.repo.PatientDirectory(manager, "", 1, 20, "9999")
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatalf("wrong last4 should match none: %+v %d %v", rows, total, err)
	}
	// 组合条件：姓名 + 后四位同时命中才返回
	rows, total, err = fixture.repo.PatientDirectory(manager, "后四位", 1, 20, "1005")
	if err != nil || total != 1 {
		t.Fatalf("combined name+last4 should match: %+v %d %v", rows, total, err)
	}
	rows, total, err = fixture.repo.PatientDirectory(manager, "后四位", 1, 20, "1006")
	if err != nil || total != 0 {
		t.Fatalf("combined name+wrong last4 should match none: %+v %d %v", rows, total, err)
	}
}
