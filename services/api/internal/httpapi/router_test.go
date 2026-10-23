package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

const testToken = "test-only-token-at-least-16"

type fakeRepository struct {
	store.EmployeeRepository
	err    error
	calls  int
	filter store.SlotFilter
}

func (f *fakeRepository) AuthenticateEmployee(_ context.Context, token string, _ time.Time) (model.EmployeeAccount, error) {
	if token != testToken {
		return model.EmployeeAccount{}, store.ErrNotFound
	}
	return model.EmployeeAccount{ID: "employee-test", Role: "manager", Active: true}, nil
}

func (f *fakeRepository) Ready(context.Context) error { return f.err }
func (f *fakeRepository) Clinics(context.Context) ([]model.Clinic, error) {
	return []model.Clinic{}, f.err
}
func (f *fakeRepository) Doctors(context.Context, string) ([]model.Doctor, error) {
	return []model.Doctor{}, f.err
}
func (f *fakeRepository) Slots(_ context.Context, filter store.SlotFilter) ([]model.Slot, error) {
	f.filter = filter
	return []model.Slot{}, f.err
}
func (f *fakeRepository) UpdateSlot(_ context.Context, id string, u store.SlotUpdate, _ time.Time) (model.Slot, error) {
	f.calls++
	return model.Slot{ID: id, Capacity: u.Capacity, Status: u.Status, Version: u.Version + 1}, f.err
}

func serve(r http.Handler, method, path, body, token, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAdminValidationAndAuthentication(t *testing.T) {
	tests := []struct {
		name, body, token string
		want              int
	}{
		{"missing authentication", `{"capacity":2,"status":"open","version":1}`, "", 401},
		{"incorrect authentication", `{"capacity":2,"status":"open","version":1}`, "wrong", 401},
		{"missing capacity", `{"status":"open","version":1}`, testToken, 400},
		{"null capacity", `{"capacity":null,"status":"open","version":1}`, testToken, 400},
		{"negative capacity", `{"capacity":-1,"status":"open","version":1}`, testToken, 400},
		{"excessive capacity", `{"capacity":101,"status":"open","version":1}`, testToken, 400},
		{"fractional capacity", `{"capacity":1.5,"status":"open","version":1}`, testToken, 400},
		{"invalid status", `{"capacity":2,"status":"unknown","version":1}`, testToken, 400},
		{"missing version", `{"capacity":2,"status":"open"}`, testToken, 400},
		{"invalid version", `{"capacity":2,"status":"open","version":0}`, testToken, 400},
		{"unknown field", `{"capacity":2,"status":"open","version":1,"reserved":0}`, testToken, 400},
		{"trailing object", `{"capacity":2,"status":"open","version":1}{}`, testToken, 400},
		{"zero capacity accepted", `{"capacity":0,"status":"closed","version":1}`, testToken, 200},
		{"maximum capacity accepted", `{"capacity":100,"status":"open","version":1}`, testToken, 200},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{}
			w := serve(New(repo, testToken), http.MethodPatch, "/api/v1/admin/slots/slot-demo", tc.body, tc.token, "")
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body)
			}
			if tc.want != 200 && repo.calls != 0 {
				t.Fatal("invalid request reached repository")
			}
		})
	}
}

func TestErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{store.ErrNotFound, 404, "SLOT_NOT_FOUND"}, {store.ErrConflict, 409, "VERSION_CONFLICT"},
		{store.ErrCapacity, 409, "CAPACITY_BELOW_RESERVED"}, {store.ErrPast, 409, "SLOT_ALREADY_STARTED"},
		{errors.New("private database detail"), 503, "SERVICE_UNAVAILABLE"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			w := serve(New(&fakeRepository{err: tc.err}, testToken), http.MethodPatch, "/api/v1/admin/slots/demo", `{"capacity":2,"status":"open","version":1}`, testToken, "")
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("unexpected response %d %s", w.Code, w.Body)
			}
			if strings.Contains(w.Body.String(), "private database detail") {
				t.Fatal("internal error leaked")
			}
		})
	}
}

func TestDatesAndQueries(t *testing.T) {
	for _, path := range []string{"/api/v1/slots?date=2026-02-30", "/api/v1/slots?date=2026-2-01", "/api/v1/slots?date=", "/api/v1/slots?date=2026-09-28&date=2026-09-29", "/api/v1/slots?unknown=x", "/api/v1/doctors?clinicId=%27"} {
		w := serve(New(&fakeRepository{}, testToken), http.MethodGet, path, "", "", "")
		if w.Code != 400 {
			t.Fatalf("%s got status=%d", path, w.Code)
		}
	}
	repo := &fakeRepository{}
	now := time.Date(2026, 9, 28, 15, 0, 0, 0, database.Shanghai)
	r := newRouter(&API{repo: repo, now: func() time.Time { return now }})
	w := serve(r, http.MethodGet, "/api/v1/slots?date=2026-09-29&clinicId=clinic-demo&doctorId=doctor-demo-1", "", "", "")
	if w.Code != 200 || w.Body.String() != `{"data":[]}` {
		t.Fatalf("unexpected list %d %s", w.Code, w.Body)
	}
	if repo.filter.Date.Format(time.RFC3339) != "2026-09-29T00:00:00+08:00" || repo.filter.DoctorID != "doctor-demo-1" || !repo.filter.Now.Equal(now) {
		t.Fatalf("incorrect filter: %+v", repo.filter)
	}
}

func TestHealthReadinessAndCORS(t *testing.T) {
	r := New(&fakeRepository{err: errors.New("database unavailable")}, testToken)
	for _, tc := range []struct {
		path   string
		status int
	}{{"/healthz", 200}, {"/readyz", 503}, {"/api/v1/clinics", 503}} {
		w := serve(r, http.MethodGet, tc.path, "", "", "")
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.path, w.Code)
		}
	}
	for _, tc := range []struct {
		origin string
		status int
	}{
		{"http://localhost:5173", 204}, {"http://127.0.0.1:5173", 204}, {"http://[::1]:5173", 204},
		{"https://attacker.example", 403}, {"http://localhost.attacker.example", 403}, {"http://localhost:5173/invalid", 403}, {"null", 403},
	} {
		w := serve(r, http.MethodOptions, "/api/v1/clinics", "", "", tc.origin)
		if w.Code != tc.status {
			t.Fatalf("origin %s: %d", tc.origin, w.Code)
		}
		if tc.status == 204 && w.Header().Get("Access-Control-Allow-Origin") != tc.origin {
			t.Fatal("missing exact origin")
		}
		if tc.status == 403 && w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("untrusted origin permitted")
		}
	}
}
