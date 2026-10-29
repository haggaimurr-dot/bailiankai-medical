package httpapi

import (
	"context"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type caseHTTPRepo struct {
	fakeRepository
	store.CaseRepository
	role   string
	calls  int
	filter store.CaseFilter
	change store.CaseChange
}

func (r *caseHTTPRepo) AuthenticateEmployee(context.Context, string, time.Time) (model.EmployeeAccount, error) {
	return model.EmployeeAccount{ID: "case-employee", Role: r.role, StaffID: strPtr("doctor-demo-1"), ClinicIDs: []string{"clinic-demo"}}, nil
}
func (r *caseHTTPRepo) ChangeCase(_ context.Context, _, _, _ string, in store.CaseChange, _ store.Media, _ time.Time) (store.CaseView, error) {
	r.calls++
	r.change = in
	return store.CaseView{}, nil
}
func (r *caseHTTPRepo) Cases(_ context.Context, _, _ string, filter store.CaseFilter) ([]store.CaseView, int64, error) {
	r.calls++
	r.filter = filter
	return []store.CaseView{}, 0, nil
}
func TestCaseHTTPPermissionsAndValidation(t *testing.T) {
	for _, tc := range []struct {
		role, method, path, body string
		status                   int
	}{
		{"manager", "POST", "", `{"appointmentId":"a-1","title":"案例","summary":"","body":"正文","pairs":[{"beforeSourceId":"before-1","afterSourceId":"after-1"}]}`, 201},
		{"manager", "POST", "", `{"appointmentId":"a-1","title":"案例","summary":"","body":"正文","pairs":[{"beforeSourceId":"before-1","afterSourceId":"after-1","patientId":"forged"}]}`, 400},
		{"manager", "POST", "/c-1/review", `{"version":1,"decision":"approved","note":""}`, 403},
		{"doctor", "POST", "/c-1/review", `{"version":1,"decision":"approved","note":""}`, 200},
		{"doctor", "POST", "/c-1/review", `{"version":1,"decision":"rejected","note":""}`, 400},
		{"doctor", "POST", "/c-1/publish", `{"version":1}`, 403},
		{"nurse", "GET", "", "", 403},
		{"operations", "GET", "", "", 403},
		{"customer_service", "GET", "", "", 403},
		{"manager", "POST", "/c-1/publish", `{"version":1,"consentRevision":1}`, 400},
		{"manager", "PUT", "/c-1", `{"version":1,"title":"案例","summary":"","body":"正文","pairs":[]}`, 400},
		{"manager", "POST", "/c-1/publish", `{"version":0}`, 400},
	} {
		t.Run(tc.role+tc.method+tc.path+tc.body, func(t *testing.T) {
			repo := &caseHTTPRepo{role: tc.role}
			w := serve(New(repo, ""), tc.method, "/api/v1/admin/cases"+tc.path, tc.body, testToken, "")
			if w.Code != tc.status {
				t.Fatalf("%d want %d: %s", w.Code, tc.status, w.Body)
			}
			if tc.status >= 400 && repo.calls != 0 {
				t.Fatal("invalid request reached store")
			}
		})
	}
	repo := &caseHTTPRepo{}
	w := serve(New(repo, ""), "GET", "/api/v1/cases", "", "", "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body)
	}
}

func TestCaseMetadataValidationAndFilters(t *testing.T) {
	body := `{"appointmentId":"a-1","title":"案例","summary":"","body":"正文","pairs":[{"beforeSourceId":"before-1","afterSourceId":"after-1"}],"category":" 瘢痕修复 ","tags":[" 正面 ","正面","随访"]}`
	repo := &caseHTTPRepo{role: "manager"}
	w := serve(New(repo, ""), "POST", "/api/v1/admin/cases", body, testToken, "")
	if w.Code != 201 || repo.change.Category != "瘢痕修复" || len(repo.change.Tags) != 2 {
		t.Fatal(w.Code, w.Body, repo.change)
	}
	invalid := strings.Replace(body, `" 正面 "`, `" "`, 1)
	w = serve(New(repo, ""), "POST", "/api/v1/admin/cases", invalid, testToken, "")
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	w = serve(New(repo, ""), "GET", "/api/v1/cases?category="+url.QueryEscape("瘢痕修复")+"&tag="+url.QueryEscape("正面")+"&page=2&pageSize=5", "", "", "")
	if w.Code != 200 || repo.filter.Category != "瘢痕修复" || repo.filter.Tag != "正面" || repo.filter.Page != 2 || repo.filter.PageSize != 5 {
		t.Fatal(w.Code, w.Body, repo.filter)
	}
	w = serve(New(repo, ""), "GET", "/api/v1/cases?tag="+url.QueryEscape(strings.Repeat("字", 31)), "", "", "")
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	// Row 92: course filter accepts Chinese + spaces (not just validID chars).
	w = serve(New(repo, ""), "GET", "/api/v1/cases?course="+url.QueryEscape("第 2 疗程"), "", "", "")
	if w.Code != 200 || repo.filter.Course != "第 2 疗程" {
		t.Fatal("course filter rejected:", w.Code, w.Body, repo.filter)
	}
	w = serve(New(repo, ""), "GET", "/api/v1/cases?course="+url.QueryEscape(strings.Repeat("字", 21)), "", "", "")
	if w.Code != 400 {
		t.Fatal("overlong course accepted:", w.Code)
	}
}

func TestCandidateAppointmentQuery(t *testing.T) {
	for _, query := range []string{"张", "138001", "a-1", "%", "_"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/api/v1/admin/appointments?status=completed&q="+url.QueryEscape(query)+"&page=6&pageSize=20", nil)
		filter, ok := parseAppointmentFilter(c, true)
		if !ok || filter.Query != query || filter.Page != 6 || filter.PageSize != 20 || filter.Status != "completed" {
			t.Fatal("candidate query", query, ok, filter)
		}
	}
}
