package httpapi

import (
	"context"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEmployeePermissionMatrix(t *testing.T) {
	tests := []struct {
		role, method, path string
		allowed            bool
	}{
		{"customer_service", "GET", "/api/v1/admin/appointments", true},
		{"customer_service", "POST", "/api/v1/admin/appointments/:appointmentId/review", true},
		{"customer_service", "GET", "/api/v1/admin/appointments/:appointmentId/photos", false},
		{"customer_service", "GET", "/api/v1/admin/patients/:patientId", false},
		{"customer_service", "GET", "/api/v1/admin/followups", false},
		{"operations", "GET", "/api/v1/admin/overview", true},
		{"operations", "GET", "/api/v1/admin/appointments", false},
		{"operations", "POST", "/api/v1/admin/appointments/:appointmentId/review", false},
		{"nurse", "POST", "/api/v1/admin/followups/:taskId/review", true},
		{"nurse", "GET", "/api/v1/admin/appointments/:appointmentId/care-plan", true},
		{"nurse", "PUT", "/api/v1/admin/appointments/:appointmentId/care-plan/draft", false},
		{"nurse", "POST", "/api/v1/admin/patients/:patientId/consultations", false},
		{"nurse", "POST", "/api/v1/admin/appointments/:appointmentId/treatment", false},
		{"doctor", "POST", "/api/v1/admin/appointments/:appointmentId/treatment", true},
		{"doctor", "POST", "/api/v1/admin/patients/:patientId/consultations", true},
		{"doctor", "GET", "/api/v1/admin/overview", false},
		{"doctor", "POST", "/api/v1/admin/employees", false},
		{"doctor", "GET", "/api/v1/admin/new-sensitive-endpoint", false},
		{"clinician", "PUT", "/api/v1/admin/appointments/:appointmentId/care-plan/draft", true},
		{"unknown", "GET", "/api/v1/admin/session", false},
	}
	for _, test := range tests {
		t.Run(test.role+test.method+test.path, func(t *testing.T) {
			if got := allowedEmployeeOperation(test.role, test.method, test.path); got != test.allowed {
				t.Fatalf("allowed=%v want=%v", got, test.allowed)
			}
		})
	}
}

type privacyTestRepository struct {
	store.Repository
	store.BookingRepository
	events           []model.PrivacyConsentEvent
	requestedPatient string
}

func (repo *privacyTestRepository) AuthenticatePatient(context.Context, string, time.Time) (model.Patient, error) {
	return model.Patient{ID: "patient-self"}, nil
}
func (repo *privacyTestRepository) PrivacyConsentEvents(_ context.Context, patientID string) ([]model.PrivacyConsentEvent, error) {
	repo.requestedPatient = patientID
	return repo.events, nil
}
func (repo *privacyTestRepository) RecordPrivacyConsent(_ context.Context, patientID, version, action string, now time.Time) (model.PrivacyConsentEvent, error) {
	repo.requestedPatient = patientID
	if version != store.PrivacyPolicyVersion {
		return model.PrivacyConsentEvent{}, store.ErrPrivacyVersion
	}
	return model.PrivacyConsentEvent{ID: "privacy-test", PatientID: patientID, PolicyVersion: version, Action: action, RecordedAt: now, Source: "miniprogram"}, nil
}
func TestPrivacyHTTPAndSubmissionGate(t *testing.T) {
	repo := &privacyTestRepository{}
	router := New(repo, "")
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer patient-test-token")
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	if response := request(http.MethodGet, "/api/v1/privacy-policy", ""); response.Code != 200 || !strings.Contains(response.Body.String(), store.PrivacyPolicyVersion) {
		t.Fatalf("policy: %d %s", response.Code, response.Body)
	}
	if response := request(http.MethodGet, "/api/v1/me/privacy-consent", ""); response.Code != 200 || repo.requestedPatient != "patient-self" {
		t.Fatalf("own consent: %d %s", response.Code, response.Body)
	}
	for _, events := range [][]model.PrivacyConsentEvent{nil, {{Action: "revoked", PolicyVersion: store.PrivacyPolicyVersion}}, {{Action: "accepted", PolicyVersion: "old-version"}}} {
		repo.events = events
		response := request(http.MethodPost, "/api/v1/me/appointments", "{}")
		if response.Code != 403 || !strings.Contains(response.Body.String(), "PRIVACY_CONSENT_REQUIRED") {
			t.Fatalf("gate: %d %s", response.Code, response.Body)
		}
	}
	repo.events = []model.PrivacyConsentEvent{{Action: "accepted", PolicyVersion: store.PrivacyPolicyVersion}}
	if response := request(http.MethodPost, "/api/v1/me/appointments", "{}"); response.Code != 400 {
		t.Fatalf("accepted request did not reach validation: %d %s", response.Code, response.Body)
	}
	if response := request(http.MethodPost, "/api/v1/me/privacy-consent", `{"policyVersion":"old-version","action":"accepted"}`); response.Code != 409 {
		t.Fatalf("stale version: %d %s", response.Code, response.Body)
	}
	if response := request(http.MethodPost, "/api/v1/me/privacy-consent", `{"policyVersion":"`+store.PrivacyPolicyVersion+`","action":"revoked"}`); response.Code != 200 {
		t.Fatalf("revoke: %d %s", response.Code, response.Body)
	}
	if response := request(http.MethodGet, "/api/v1/me/privacy-consent?patientId=other", ""); response.Code != 400 || repo.requestedPatient != "patient-self" {
		t.Fatalf("identity override: %d %s", response.Code, response.Body)
	}
}
func TestPrivacyRevocationsDoNotRequireConsent(t *testing.T) {
	for _, path := range []string{"/api/v1/me/privacy-consent", "/api/v1/me/phone/revoke", "/api/v1/me/logout", "/api/v1/me/appointments/:appointmentId/cancel", "/api/v1/me/appointments/:appointmentId/photo-consent"} {
		if requiresPrivacyConsent("POST", path) {
			t.Fatalf("revocation blocked: %s", path)
		}
	}
}
