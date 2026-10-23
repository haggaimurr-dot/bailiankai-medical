package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

type recordExportHTTPRepo struct {
	fakeRepository
	role     string
	input    store.RecordExportInput
	actor    string
	exportID string
}

func (r *recordExportHTTPRepo) AuthenticateEmployee(_ context.Context, token string, _ time.Time) (model.EmployeeAccount, error) {
	if token != testToken {
		return model.EmployeeAccount{}, store.ErrNotFound
	}
	return model.EmployeeAccount{ID: "employee-test", Role: r.role, Active: true, ClinicIDs: []string{"clinic-demo"}}, nil
}
func (r *recordExportHTTPRepo) CreateRecordExport(_ context.Context, actor string, input store.RecordExportInput, now time.Time) (model.RecordExportJob, error) {
	r.calls++
	r.actor, r.input = actor, input
	return model.RecordExportJob{ID: "export-test", EmployeeID: actor, Masked: input.Masked, Sections: input.Sections, Reason: input.Reason, PatientCount: len(input.PatientIDs), Status: "queued", CreatedAt: now}, r.err
}
func (r *recordExportHTTPRepo) ListRecordExports(_ context.Context, actor string, _, _ int, _ time.Time) ([]model.RecordExportJob, int64, error) {
	r.calls++
	r.actor = actor
	return []model.RecordExportJob{}, 0, r.err
}
func (r *recordExportHTTPRepo) RecordExport(_ context.Context, actor, id string, _ time.Time) (model.RecordExportJob, error) {
	r.calls++
	r.actor, r.exportID = actor, id
	return model.RecordExportJob{ID: id, EmployeeID: actor}, r.err
}
func (r *recordExportHTTPRepo) DownloadRecordExport(_ context.Context, actor, id string, _ time.Time) (model.RecordExportJob, []byte, error) {
	r.calls++
	r.actor, r.exportID = actor, id
	return model.RecordExportJob{ID: id, Status: "ready"}, []byte(`{"records":[],"masked":true}`), r.err
}
func (r *recordExportHTTPRepo) RecordExportEvents(_ context.Context, actor, id string) ([]model.RecordExportEvent, error) {
	r.calls++
	r.actor, r.exportID = actor, id
	return []model.RecordExportEvent{}, r.err
}

func TestRecordExportHTTPCreate(t *testing.T) {
	valid := `{"patientIds":["patient-a"],"sections":["appointments"],"reason":"  诊疗交接  "}`
	tests := []struct {
		name, role, body string
		status           int
		masked           bool
	}{
		{"default masking", "manager", valid, 202, true},
		{"doctor masked", "doctor", valid, 202, true},
		{"nurse masked", "nurse", valid, 202, true},
		{"manager full", "manager", strings.TrimSuffix(valid, "}") + `,"masked":false}`, 202, false},
		{"doctor full denied", "doctor", strings.TrimSuffix(valid, "}") + `,"masked":false}`, 403, false},
		{"nurse full denied", "nurse", strings.TrimSuffix(valid, "}") + `,"masked":false}`, 403, false},
		{"service denied", "customer_service", valid, 403, false},
		{"operations denied", "operations", valid, 403, false},
		{"empty patients", "manager", `{"patientIds":[],"sections":["appointments"],"reason":"交接"}`, 400, false},
		{"duplicate patients", "manager", `{"patientIds":["patient-a","patient-a"],"sections":["appointments"],"reason":"交接"}`, 400, false},
		{"empty id", "manager", `{"patientIds":[""],"sections":["appointments"],"reason":"交接"}`, 400, false},
		{"invalid id", "manager", `{"patientIds":["patient/a"],"sections":["appointments"],"reason":"交接"}`, 400, false},
		{"missing purpose", "manager", `{"patientIds":["patient-a"],"sections":["appointments"]}`, 400, false},
		{"blank purpose", "manager", `{"patientIds":["patient-a"],"sections":["appointments"],"reason":"  "}`, 400, false},
		{"null masked", "manager", strings.TrimSuffix(valid, "}") + `,"masked":null}`, 400, false},
		{"unknown section", "manager", `{"patientIds":["patient-a"],"sections":["photos"],"reason":"交接"}`, 400, false},
		{"duplicate sections", "manager", `{"patientIds":["patient-a"],"sections":["treatments","treatments"],"reason":"交接"}`, 400, false},
		{"empty sections", "manager", `{"patientIds":["patient-a"],"sections":[],"reason":"交接"}`, 400, false},
		{"extra actor", "manager", strings.TrimSuffix(valid, "}") + `,"employeeId":"other"}`, 400, false},
		{"two objects", "manager", valid + `{}`, 400, false},
		{"one date", "manager", strings.TrimSuffix(valid, "}") + `,"dateFrom":"2026-10-01"}`, 400, false},
		{"empty dates", "manager", strings.TrimSuffix(valid, "}") + `,"dateFrom":"","dateTo":""}`, 400, false},
		{"invalid calendar", "manager", strings.TrimSuffix(valid, "}") + `,"dateFrom":"2026-02-30","dateTo":"2026-03-02"}`, 400, false},
		{"reverse dates", "manager", strings.TrimSuffix(valid, "}") + `,"dateFrom":"2026-10-02","dateTo":"2026-10-01"}`, 400, false},
		{"366 days inclusive", "manager", strings.TrimSuffix(valid, "}") + `,"dateFrom":"2026-01-01","dateTo":"2027-01-01"}`, 202, true},
		{"367 days rejected", "manager", strings.TrimSuffix(valid, "}") + `,"dateFrom":"2026-01-01","dateTo":"2027-01-02"}`, 400, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &recordExportHTTPRepo{role: tc.role}
			w := serve(New(repo, ""), http.MethodPost, "/api/v1/admin/record-exports", tc.body, testToken, "")
			if w.Code != tc.status {
				t.Fatalf("status %d want %d: %s", w.Code, tc.status, w.Body)
			}
			if tc.status < 400 {
				if repo.calls != 1 || repo.actor != "employee-test" || repo.input.Masked != tc.masked || repo.input.Reason != "诊疗交接" {
					t.Fatalf("incorrect export request: %+v", repo)
				}
			} else if repo.calls != 0 {
				t.Fatal("invalid or forbidden request reached store")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("export response allowed caching")
			}
		})
	}
	for _, count := range []int{20, 21} {
		ids := make([]string, count)
		for i := range ids {
			ids[i] = fmt.Sprintf("patient-%d", i)
		}
		body, _ := json.Marshal(map[string]any{"patientIds": ids, "sections": []string{"appointments"}, "reason": "交接"})
		w := serve(New(&recordExportHTTPRepo{role: "manager"}, ""), http.MethodPost, "/api/v1/admin/record-exports", string(body), testToken, "")
		if (count == 20 && w.Code != 202) || (count == 21 && w.Code != 400) {
			t.Fatalf("patient limit %d: %d", count, w.Code)
		}
	}
}

func TestRecordExportHTTPAccessDownloadAndErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{store.ErrNotFound, 404, "NOT_FOUND"},
		{store.ErrForbidden, 403, "EXPORT_FORBIDDEN"},
		{store.ErrRecordExportNotReady, 409, "EXPORT_NOT_READY"},
		{store.ErrRecordExportExpired, 410, "EXPORT_EXPIRED"},
		{store.ErrRecordExportQuota, 429, "EXPORT_QUOTA"},
		{errors.New("private export storage detail"), 503, "SERVICE_UNAVAILABLE"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			repo := &recordExportHTTPRepo{role: "manager", fakeRepository: fakeRepository{err: tc.err}}
			w := serve(New(repo, ""), http.MethodGet, "/api/v1/admin/record-exports/export-a/download", "", testToken, "")
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "private export") || strings.Contains(w.Body.String(), `"records"`) {
				t.Fatalf("download error: %d %s", w.Code, w.Body)
			}
			if repo.actor != "employee-test" || repo.exportID != "export-a" {
				t.Fatal("download failed to use authenticated owner")
			}
		})
	}
	repo := &recordExportHTTPRepo{role: "manager"}
	router := New(repo, "")
	w := serve(router, http.MethodGet, "/api/v1/admin/record-exports/export-a/download", "", testToken, "")
	if w.Code != 200 || w.Header().Get("Content-Disposition") != `attachment; filename="record-export-export-a.json"` || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Body.String() != `{"records":[],"masked":true}` {
		t.Fatalf("download: %d %s %v", w.Code, w.Body, w.Header())
	}
	for _, path := range []string{"", "/export-a", "/export-a/events"} {
		w := serve(router, http.MethodGet, "/api/v1/admin/record-exports"+path, "", "", "")
		if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("unauthenticated export %s: %d", path, w.Code)
		}
	}
	for _, suffix := range []string{"?page=0", "?pageSize=101", "?page=1&page=2", "?employeeId=other", "/export-a/download?token=secret", "/export-a/events?patientId=other"} {
		before := repo.calls
		w := serve(router, http.MethodGet, "/api/v1/admin/record-exports"+suffix, "", testToken, "")
		if w.Code != 400 || repo.calls != before {
			t.Fatalf("invalid query %s: %d %s", suffix, w.Code, w.Body)
		}
	}
	for _, path := range []string{"", "/export-a", "/export-a/download", "/export-a/events"} {
		w := serve(New(&recordExportHTTPRepo{role: "customer_service"}, ""), http.MethodGet, "/api/v1/admin/record-exports"+path, "", testToken, "")
		if w.Code != 403 {
			t.Fatalf("service export access %s: %d", path, w.Code)
		}
	}
}
