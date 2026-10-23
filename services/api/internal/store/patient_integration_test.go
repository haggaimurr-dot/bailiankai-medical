package store_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/store"
	"example.com/bailiankai/api/internal/wechat"
)

type patientProvider struct{}

func (patientProvider) Login(_ context.Context, code string) (string, string, error) {
	if code == "invalid" {
		return "", "", wechat.ErrRejected
	}
	return "test-app", "verified-" + code, nil
}
func (patientProvider) Phone(_ context.Context, code, openID string) (string, error) {
	if code != "phone-code" || openID != "verified-alice" {
		return "", wechat.ErrRejected
	}
	return "13900000000", nil
}

func TestPatientIdentityAndProfile(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	f.repo.SetPatientProvider(patientProvider{})
	first, err := f.repo.LoginPatient(ctx, "alice", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.RecordPrivacyConsent(ctx, first.Patient.ID, store.PrivacyPolicyVersion, "accepted", f.now); err != nil {
		t.Fatal(err)
	}
	appointment := f.create(first, "patient-profile-request")
	second, err := f.repo.LoginPatient(ctx, "alice", f.now.Add(time.Minute))
	if err != nil || first.Patient.ID != second.Patient.ID || first.Token == second.Token {
		t.Fatalf("device recovery failed: %v", err)
	}
	other, err := f.repo.LoginPatient(ctx, "bob", f.now)
	if err != nil || other.Patient.ID == first.Patient.ID {
		t.Fatalf("identity isolation failed: %v", err)
	}
	if _, err := f.repo.GetAppointment(ctx, appointment.ID, other.Patient.ID, f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("another patient read the appointment", err)
	}
	if _, err := f.repo.GetAppointment(ctx, appointment.ID, second.Patient.ID, f.now); err != nil {
		t.Fatal("restored patient lost appointment", err)
	}
	patient, err := f.repo.UpdatePatientProfile(ctx, first.Patient.ID, "患者甲", first.Patient.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.UpdatePatientProfile(ctx, first.Patient.ID, "过期修改", first.Patient.Version); !errors.Is(err, store.ErrConflict) {
		t.Fatal("stale profile overwritten", err)
	}
	if _, err := f.repo.AuthorizePatientPhone(ctx, patient.ID, "invalid", patient.Version, f.now); !errors.Is(err, wechat.ErrRejected) {
		t.Fatal("invalid grant accepted", err)
	}
	patient, err = f.repo.AuthorizePatientPhone(ctx, patient.ID, "phone-code", patient.Version, f.now)
	if err != nil || patient.Phone != "13900000000" || patient.PhoneAuthorization != "granted" || patient.PhoneAuthorizedAt == nil {
		t.Fatalf("grant failed: %+v %v", patient, err)
	}
	restored, err := f.repo.AuthenticatePatient(ctx, second.Token, f.now)
	if err != nil || restored.DisplayName != "患者甲" || restored.Phone != patient.Phone {
		t.Fatal("second device did not see latest profile", err)
	}
	patient, err = f.repo.RevokePatientPhone(ctx, patient.ID, patient.Version)
	if err != nil || patient.Phone != "" || patient.PhoneAuthorization != "revoked" || patient.PhoneAuthorizedAt != nil {
		t.Fatalf("revoke failed: %+v %v", patient, err)
	}
	saved, err := f.repo.GetAppointment(ctx, appointment.ID, patient.ID, f.now)
	if err != nil || saved.ContactPhone != f.input().ContactPhone || saved.ContactName != f.input().ContactName {
		t.Fatal("profile changed booking contact", err)
	}
	if _, err := f.repo.AuthenticatePatient(ctx, first.Token, f.now.Add(8*24*time.Hour)); !errors.Is(err, store.ErrUnauthenticated) {
		t.Fatal("expired token accepted", err)
	}
	renewed, err := f.repo.LoginPatient(ctx, "alice", f.now.Add(8*24*time.Hour))
	if err != nil || renewed.Patient.ID != patient.ID {
		t.Fatal("expiry created different patient", err)
	}
	if err := f.repo.LogoutPatient(ctx, first.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.AuthenticatePatient(ctx, first.Token, f.now); !errors.Is(err, store.ErrUnauthenticated) {
		t.Fatal("logout token accepted", err)
	}
	if _, err := f.repo.AuthenticatePatient(ctx, second.Token, f.now); err != nil {
		t.Fatal("logout revoked another device", err)
	}
	requireHTTP(t, f.http("POST", "/api/v1/patient/session", `{"code":"invalid"}`, "", ""), 400, "WECHAT_CODE_INVALID")
	requireHTTP(t, f.http("POST", "/api/v1/patient/session", `{"code":"alice","patientId":"forged"}`, "", ""), 400, "INVALID_BODY")
	requireHTTP(t, f.http("PUT", "/api/v1/me/profile", `{"displayName":"甲","version":1,"phone":"13800000000"}`, second.Token, ""), 400, "INVALID_BODY")
	requireHTTP(t, f.http("POST", "/api/v1/me/phone", `{"phone":"13800000000","version":1}`, second.Token, ""), 400, "INVALID_BODY")
	requireHTTP(t, f.http("GET", "/api/v1/me", "", "", ""), 401, "PATIENT_SESSION_INVALID")
	response := f.http("GET", "/api/v1/me", "", second.Token, "")
	requireHTTP(t, response, 200, "")
	if strings.Contains(response.Body.String(), "verified-alice") || strings.Contains(response.Body.String(), "test-app") {
		t.Fatal("platform identity leaked")
	}
}

func TestConcurrentFirstPatientLogin(t *testing.T) {
	f := newBookingFixture(t)
	f.repo.SetPatientProvider(patientProvider{})
	var wg sync.WaitGroup
	results := make(chan store.SessionResult, 8)
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			result, err := f.repo.LoginPatient(context.Background(), "alice", f.now)
			results <- result
			failures <- err
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	id := ""
	tokens := map[string]bool{}
	for result := range results {
		if id == "" {
			id = result.Patient.ID
		}
		if id != result.Patient.ID || tokens[result.Token] {
			t.Fatal("concurrent login produced conflicting identities or duplicate tokens")
		}
		tokens[result.Token] = true
	}
}
