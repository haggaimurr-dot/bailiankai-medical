package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func TestPatientBindingLifecycle(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	manager := store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"})
	f.repo.SetPatientProvider(patientProvider{})
	chart, err := f.repo.CreatePatientChart(manager, "人工档案患者", model.PatientChart{ClinicID: "clinic-demo", Gender: "female", ContactPhone: "13800000000", Tags: []string{"敏感肌"}}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	consultation, err := f.repo.CreateConsultation(manager, model.Consultation{PatientID: chart.ID, ClinicID: "clinic-demo", ChiefComplaint: "建档时问诊", RecordedBy: "binding-doctor", RecordedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	source, err := f.repo.LoginPatient(ctx, "alice", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.RecordPrivacyConsent(ctx, source.Patient.ID, store.PrivacyPolicyVersion, "accepted", f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.AuthorizePatientPhone(ctx, source.Patient.ID, "phone-code", 1, f.now); err != nil {
		t.Fatal(err)
	}
	second, err := f.repo.LoginPatient(ctx, "alice", f.now)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/patients/" + chart.ID + "/wechat-binding"
	response := f.http("POST", path, "", bookingAdminToken, "")
	requireHTTP(t, response, 201, "")
	var generated struct {
		Data struct {
			Code      string
			ExpiresAt time.Time
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &generated); err != nil {
		t.Fatal(err)
	}
	code := generated.Data.Code
	if len(code) != 32 || !generated.Data.ExpiresAt.After(f.now) {
		t.Fatal("invalid generated code")
	}
	var stored store.PatientBindingCode
	if err := f.db.First(&stored, "patient_id = ?", chart.ID).Error; err != nil || stored.TokenHash == code || stored.CreatedBy == "" {
		t.Fatal("code or audit not safely persisted", err)
	}
	previewResponse := f.http("POST", "/api/v1/me/patient-binding/preview", `{"code":"`+code+`"}`, source.Token, "")
	requireHTTP(t, previewResponse, 200, "")
	if strings.Contains(previewResponse.Body.String(), "13800000000") || strings.Contains(previewResponse.Body.String(), "verified-alice") {
		t.Fatal("private fields leaked in preview")
	}
	var preview struct{ Data store.PatientBindingPreview }
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Data.PatientID != chart.ID || preview.Data.DisplayName != "人工档案患者" || preview.Data.ClinicName == "" {
		t.Fatal("incorrect preview", preview)
	}
	body := fmt.Sprintf(`{"code":"%s","version":%d}`, code, preview.Data.Version)
	requireHTTP(t, f.http("POST", "/api/v1/me/patient-binding", body, source.Token, ""), 200, "")
	for _, token := range []string{source.Token, second.Token} {
		if _, err := f.repo.AuthenticatePatient(ctx, token, f.now); !errors.Is(err, store.ErrUnauthenticated) {
			t.Fatal("old session survived", err)
		}
	}
	renewed, err := f.repo.LoginPatient(ctx, "alice", f.now.Add(time.Minute))
	if err != nil || renewed.Patient.ID != chart.ID || renewed.Patient.DisplayName != "人工档案患者" || renewed.Patient.Phone != "13900000000" {
		t.Fatal("wechat identity not restored to chart", err, renewed.Patient)
	}
	latest, _, err := f.repo.PatientRecordAppointments(manager, chart.ID)
	if err != nil || !latest.WechatBound || latest.Profile.ContactPhone != "13800000000" {
		t.Fatal("chart overwritten", err, latest)
	}
	records, err := f.repo.PatientConsultations(manager, chart.ID)
	if err != nil || len(records) != 1 || records[0].ID != consultation.ID {
		t.Fatal("consultation identity changed", err)
	}
	consents, err := f.repo.PrivacyConsentEvents(ctx, chart.ID)
	if err != nil || len(consents) != 1 || consents[0].Action != "accepted" {
		t.Fatal("privacy history lost", err)
	}
	requireHTTP(t, f.http("POST", "/api/v1/me/patient-binding", body, renewed.Token, ""), 400, "BINDING_CODE_INVALID")
	requireHTTP(t, f.http("POST", path, "", bookingAdminToken, ""), 409, "PATIENT_BINDING_CONFLICT")
}

func TestPatientBindingConflictsAndPermissions(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	manager := store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"})
	f.repo.SetPatientProvider(patientProvider{})
	chart, err := f.repo.CreatePatientChart(manager, "人工患者", model.PatientChart{ClinicID: "clinic-demo", Gender: "unknown", Tags: []string{}}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []context.Context{store.WithEmployeeScope(ctx, "nurse", f.slot.DoctorID, []string{"clinic-demo"}), store.WithEmployeeScope(ctx, "manager", "", []string{"other-clinic"}), store.WithEmployeeScope(ctx, "doctor", "outsider", []string{"clinic-demo"})} {
		if _, _, err := f.repo.CreatePatientBindingCode(scope, chart.ID, "actor", f.now); err == nil {
			t.Fatal("unauthorized code creation")
		}
		if err := f.repo.RevokePatientBindingCodes(scope, chart.ID, f.now); err == nil {
			t.Fatal("unauthorized revocation")
		}
	}
	first, _, err := f.repo.CreatePatientBindingCode(manager, chart.ID, "actor", f.now)
	if err != nil {
		t.Fatal(err)
	}
	code, _, err := f.repo.CreatePatientBindingCode(manager, chart.ID, "actor", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.PreviewPatientBinding(ctx, first, f.now); !errors.Is(err, store.ErrBindingInvalid) {
		t.Fatal("old code valid", err)
	}
	if _, err := f.repo.PreviewPatientBinding(ctx, code, f.now.Add(15*time.Minute)); !errors.Is(err, store.ErrBindingInvalid) {
		t.Fatal("expired code valid", err)
	}
	if err := f.repo.RevokePatientBindingCodes(manager, chart.ID, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.PreviewPatientBinding(ctx, code, f.now); !errors.Is(err, store.ErrBindingInvalid) {
		t.Fatal("revoked code valid", err)
	}
	code, _, err = f.repo.CreatePatientBindingCode(manager, chart.ID, "actor", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.SavePatientChart(manager, "修改过的姓名", *chart.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.PreviewPatientBinding(ctx, code, f.now); !errors.Is(err, store.ErrBindingConflict) {
		t.Fatal("edited chart accepted old invite", err)
	}
	code, _, err = f.repo.CreatePatientBindingCode(manager, chart.ID, "actor", f.now)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := f.repo.PreviewPatientBinding(ctx, code, f.now)
	if err != nil {
		t.Fatal(err)
	}
	source, err := f.repo.LoginPatient(ctx, "alice", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.RecordPrivacyConsent(ctx, source.Patient.ID, store.PrivacyPolicyVersion, "accepted", f.now); err != nil {
		t.Fatal(err)
	}
	appointment := f.create(source, "binding-existing-record")
	if _, err := f.repo.BindPatient(ctx, source.Patient.ID, code, preview.Version, f.now); !errors.Is(err, store.ErrBindingConflict) {
		t.Fatal("business records merged", err)
	}
	if _, err := f.repo.AuthenticatePatient(ctx, source.Token, f.now); err != nil {
		t.Fatal("failed binding invalidated source session", err)
	}
	if saved, err := f.repo.GetAppointment(ctx, appointment.ID, source.Patient.ID, f.now); err != nil || saved.PatientID != source.Patient.ID {
		t.Fatal("appointment moved", err)
	}
	if events, err := f.repo.PrivacyConsentEvents(ctx, source.Patient.ID); err != nil || len(events) != 1 {
		t.Fatal("failed binding moved consent", err)
	}
	if _, err := f.repo.PreviewPatientBinding(ctx, code, f.now); err != nil {
		t.Fatal("conflict consumed code", err)
	}
	demo := f.patient()
	if _, err := f.repo.BindPatient(ctx, demo.Patient.ID, code, preview.Version, f.now); !errors.Is(err, store.ErrUnauthenticated) {
		t.Fatal("demo identity bound", err)
	}
	requireHTTP(t, f.http("POST", "/api/v1/me/patient-binding", `{"code":"`+code+`","version":1,"patientId":"forged"}`, source.Token, ""), 400, "INVALID_BODY")
}

func TestPatientBindingConcurrentConsumption(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	manager := store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"})
	f.repo.SetPatientProvider(patientProvider{})
	chart, err := f.repo.CreatePatientChart(manager, "并发绑定患者", model.PatientChart{ClinicID: "clinic-demo", Gender: "unknown", Tags: []string{}}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	code, _, err := f.repo.CreatePatientBindingCode(manager, chart.ID, "actor", f.now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.repo.LoginPatient(ctx, "first", f.now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.repo.LoginPatient(ctx, "second", f.now)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, source := range []store.SessionResult{first, second} {
		wg.Add(1)
		go func(source store.SessionResult) {
			defer wg.Done()
			_, err := f.repo.BindPatient(ctx, source.Patient.ID, code, 1, f.now)
			results <- err
		}(source)
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, store.ErrBindingInvalid) && !errors.Is(err, store.ErrBindingConflict) {
			t.Fatal("unexpected concurrency failure", err)
		}
	}
	if successes != 1 {
		t.Fatal("code did not have exactly one consumer", successes)
	}
}
