package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

type analyticsFake struct {
	fakeRepository
	role   string
	filter store.AnalyticsFilter
	now    time.Time
}

func (f *analyticsFake) AuthenticateEmployee(ctx context.Context, token string, now time.Time) (model.EmployeeAccount, error) {
	row, err := f.fakeRepository.AuthenticateEmployee(ctx, token, now)
	if f.role != "" {
		row.Role = f.role
	}
	row.ClinicIDs = []string{"clinic-demo"}
	return row, err
}

func (f *analyticsFake) Analytics(_ context.Context, filter store.AnalyticsFilter, now time.Time) (store.AnalyticsReport, error) {
	f.calls++
	f.filter, f.now = filter, now
	return store.AnalyticsReport{MetricVersion: "v1", Timezone: "Asia/Shanghai", GeneratedAt: now, Rows: []store.AnalyticsRow{}, Clinics: []store.AnalyticsClinic{}, Doctors: []store.AnalyticsDoctor{}}, f.err
}

func TestAnalyticsQueryValidation(t *testing.T) {
	for _, query := range []string{
		"", "dateFrom=2026-10-01", "dateTo=2026-10-01", "dateFrom=&dateTo=2026-10-01",
		"dateFrom=2026-10-02&dateTo=2026-10-01", "dateFrom=2026-02-30&dateTo=2026-10-01",
		"dateFrom=2026-1-01&dateTo=2026-10-01", "dateFrom=2026-01-01&dateTo=2027-01-02",
		"dateFrom=2026-10-01&dateTo=2026-10-01&dateTo=2026-10-02",
		"dateFrom=2026-10-01&dateTo=2026-10-01&groupBy=unknown",
		"dateFrom=2026-10-01&dateTo=2026-10-01&groupBy=",
		"dateFrom=2026-10-01&dateTo=2026-10-01&clinicId=",
		"dateFrom=2026-10-01&dateTo=2026-10-01&doctorId=%27",
		"dateFrom=2026-10-01&dateTo=2026-10-01&extra=1",
		"dateFrom=2026-10-01&dateTo=2026-10-01&doctorId=doctor-1;extra=1",
		"dateFrom=2026-10-01&dateTo=2026-10-01&clinicId=%ZZ",
	} {
		t.Run(query, func(t *testing.T) {
			repo := &analyticsFake{}
			w := serve(New(repo, ""), http.MethodGet, "/api/v1/admin/analytics?"+query, "", testToken, "")
			if w.Code != http.StatusBadRequest || repo.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, repo.calls, w.Body)
			}
		})
	}
}

func TestAnalyticsRangeDimensionsAndAuthorization(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, database.Shanghai)
	for _, groupBy := range []string{"", "doctor", "clinic", "day"} {
		repo := &analyticsFake{}
		r := newRouter(&API{repo: repo, employees: repo, now: func() time.Time { return now }})
		query := "/api/v1/admin/analytics?dateFrom=2026-01-01&dateTo=2027-01-01&clinicId=clinic-demo&doctorId=doctor-demo-1"
		if groupBy != "" {
			query += "&groupBy=" + groupBy
		}
		w := serve(r, http.MethodGet, query, "", testToken, "")
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("groupBy=%s status=%d body=%s", groupBy, w.Code, w.Body)
		}
		if repo.filter.ClinicID != "clinic-demo" || repo.filter.DoctorID != "doctor-demo-1" || repo.filter.DateRange.From.Format(time.RFC3339) != "2026-01-01T00:00:00+08:00" || repo.filter.DateRange.Until.Format(time.RFC3339) != "2027-01-02T00:00:00+08:00" || !repo.now.Equal(now) {
			t.Fatalf("incorrect filter or clock: %+v %s", repo.filter, repo.now)
		}
		if repo.filter.GroupBy != groupBy && !(groupBy == "" && repo.filter.GroupBy == "doctor") {
			t.Fatalf("incorrect dimension: %+v", repo.filter)
		}
		var payload struct{ Data store.AnalyticsReport }
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil || payload.Data.MetricVersion != "v1" || !strings.Contains(w.Body.String(), `"arrivalRate":null`) || !strings.Contains(w.Body.String(), `"rows":[]`) {
			t.Fatalf("unexpected report: %s err=%v", w.Body, err)
		}
	}
	for _, role := range []string{"manager", "operations", "reception", "doctor", "nurse", "clinician"} {
		repo := &analyticsFake{role: role}
		w := serve(New(repo, ""), http.MethodGet, "/api/v1/admin/analytics?dateFrom=2026-10-04&dateTo=2026-10-04", "", testToken, "")
		want := http.StatusForbidden
		if role == "manager" || role == "operations" {
			want = http.StatusOK
		}
		if w.Code != want || (want != http.StatusOK && repo.calls != 0) {
			t.Fatalf("role=%s status=%d calls=%d body=%s", role, w.Code, repo.calls, w.Body)
		}
	}
	w := serve(New(&analyticsFake{}, ""), http.MethodGet, "/api/v1/admin/analytics?dateFrom=2026-10-04&dateTo=2026-10-04", "", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", w.Code)
	}
}

func TestAnalyticsErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{store.ErrNotFound, 404, "NOT_FOUND"}, {store.ErrForbidden, 403, "FORBIDDEN"},
		{store.ErrAnalyticsInput, 400, "INVALID_QUERY"}, {errors.New("private SQL detail"), 503, "SERVICE_UNAVAILABLE"},
	} {
		repo := &analyticsFake{fakeRepository: fakeRepository{err: tc.err}}
		w := serve(New(repo, ""), http.MethodGet, "/api/v1/admin/analytics?dateFrom=2026-10-04&dateTo=2026-10-04", "", testToken, "")
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "private SQL detail") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
	}
}
