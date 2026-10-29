package httpapi

import (
	"context"
	"encoding/json"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"net/http"
	"strings"
	"testing"
	"time"
)

type employeeAccessFake struct {
	fakeRepository
	role          string
	mutationCalls int
	mutationError error
	target        string
}

func (f *employeeAccessFake) AuthenticateEmployee(context.Context, string, time.Time) (model.EmployeeAccount, error) {
	return model.EmployeeAccount{ID: "employee-self", Role: f.role, Active: true, Version: 3, ClinicIDs: []string{"clinic-demo"}}, nil
}
func (f *employeeAccessFake) ChangeEmployeePassword(_ context.Context, id string, _ int, _, _ string, _ time.Time) (model.EmployeeAccount, error) {
	f.mutationCalls++
	f.target = id
	return model.EmployeeAccount{ID: id, Role: f.role}, f.mutationError
}
func (f *employeeAccessFake) SetEmployeeAccess(_ context.Context, id string, _ int, role, _ string, _ time.Time) (model.EmployeeAccount, error) {
	f.mutationCalls++
	return model.EmployeeAccount{ID: id, Role: role}, f.mutationError
}
func TestEmployeeCapabilitiesAndMatrix(t *testing.T) {
	roles := []string{"manager", "doctor", "nurse", "customer_service", "operations", "clinician"}
	for _, role := range roles {
		repo := &employeeAccessFake{role: role}
		router := New(repo, "")
		response := serve(router, "GET", "/api/v1/admin/session", "", testToken, "")
		var session struct {
			Data struct {
				Capabilities []string `json:"capabilities"`
			}
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &session) != nil || len(session.Data.Capabilities) == 0 {
			t.Fatalf("session %s: %s", role, response.Body)
		}
		matrix := serve(router, "GET", "/api/v1/admin/permissions", "", testToken, "")
		var result struct {
			Data []struct {
				Key   string          `json:"key"`
				Roles map[string]bool `json:"roles"`
			}
		}
		if matrix.Code != 200 || json.Unmarshal(matrix.Body.Bytes(), &result) != nil {
			t.Fatalf("matrix: %s", matrix.Body)
		}
		for _, row := range result.Data {
			found := false
			for _, key := range session.Data.Capabilities {
				if key == row.Key {
					found = true
				}
			}
			if row.Roles[role] != found {
				t.Fatalf("matrix/session mismatch %s %s", role, row.Key)
			}
		}
		wanted := map[string]bool{"manager": true, "customer_service": true}
		actual := false
		for _, key := range session.Data.Capabilities {
			if key == "appointment.review" {
				actual = true
			}
		}
		if actual != wanted[role] {
			t.Fatalf("review role %s", role)
		}
	}
	if len(employeeCapabilities("unknown")) != 0 {
		t.Fatal("unknown role grants capabilities")
	}
}
func TestEmployeeSelfPasswordValidationAndAuthorization(t *testing.T) {
	for _, role := range []string{"manager", "doctor", "nurse", "customer_service", "operations", "clinician"} {
		repo := &employeeAccessFake{role: role}
		router := New(repo, "")
		for _, body := range []string{`{"currentPassword":"old-password-123","password":"short"}`, `{"currentPassword":"old-password-123","password":"old-password-123"}`, `{"currentPassword":"old-password-123","password":"new-password-123","employeeId":"victim"}`, `{"currentPassword":"old-password-123","password":"` + strings.Repeat("长", 25) + `"}`} {
			if response := serve(router, "POST", "/api/v1/admin/session/password", body, testToken, ""); response.Code != 400 {
				t.Fatalf("validation: %d %s", response.Code, response.Body)
			}
		}
		if repo.mutationCalls != 0 {
			t.Fatal("invalid password reached repository")
		}
		repo.mutationError = store.ErrInvalidCredentials
		body := `{"currentPassword":"wrong-password-123","password":"new-password-123"}`
		response := serve(router, "POST", "/api/v1/admin/session/password", body, testToken, "")
		if response.Code != 400 || !strings.Contains(response.Body.String(), "INVALID_CURRENT_PASSWORD") {
			t.Fatalf("wrong original: %s", response.Body)
		}
		repo.mutationError = nil
		response = serve(router, "POST", "/api/v1/admin/session/password", body, testToken, "")
		if response.Code != http.StatusOK || repo.target != "employee-self" {
			t.Fatalf("own password %s: %s", role, response.Body)
		}
		if role != "manager" {
			response = serve(router, "POST", "/api/v1/admin/employees/victim/access", `{"version":1,"role":"operations","staffId":""}`, testToken, "")
			if response.Code != 403 {
				t.Fatalf("nonmanager changed access: %d", response.Code)
			}
		}
	}
}
func TestEmployeeAccessValidation(t *testing.T) {
	repo := &employeeAccessFake{role: "manager"}
	router := New(repo, "")
	for _, test := range []struct{ path, body string }{
		{"employee-self", `{"version":3,"role":"operations","staffId":""}`},
		{"victim", `{"version":3,"role":"doctor","staffId":""}`},
		{"victim", `{"version":0,"role":"operations","staffId":""}`},
		{"victim", `{"version":3,"role":"invalid","staffId":""}`},
	} {
		if response := serve(router, "POST", "/api/v1/admin/employees/"+test.path+"/access", test.body, testToken, ""); response.Code != 400 {
			t.Fatalf("access validation: %s", response.Body)
		}
	}
	if repo.mutationCalls != 0 {
		t.Fatal("invalid access reached repository")
	}
	repo.mutationError = store.ErrConflict
	if response := serve(router, "POST", "/api/v1/admin/employees/victim/access", `{"version":3,"role":"operations","staffId":""}`, testToken, ""); response.Code != 409 {
		t.Fatalf("conflict: %s", response.Body)
	}
}

func TestEmployeePageAccessByRole(t *testing.T) {
	expected := map[string][]string{
		"operations":       {"/overview", "/doctors", "/account"},
		"customer_service": {"/support", "/arrivals", "/appointments", "/waitlist", "/doctors", "/account"},
		"nurse":            {"/workbench", "/handoffs", "/appointments", "/patients", "/care", "/followups", "/doctors", "/account"},
		"doctor":           {"/assessments", "/knowledge", "/arrivals", "/workbench", "/handoffs", "/appointments", "/patients", "/care", "/followups", "/cases", "/doctors", "/account"},
		"clinician":        {"/knowledge", "/arrivals", "/workbench", "/handoffs", "/appointments", "/patients", "/care", "/followups", "/cases", "/doctors", "/account"},
	}
	for role, pages := range expected {
		wanted := make(map[string]bool)
		for _, page := range pages {
			wanted[page] = true
		}
		for _, key := range employeeCapabilities(role) {
			if !strings.HasPrefix(key, "/") {
				continue
			}
			if !wanted[key] {
				t.Fatalf("role %s exposed unexpected page %s", role, key)
			}
			delete(wanted, key)
		}
		if len(wanted) != 0 {
			t.Fatalf("role %s missing pages %v", role, wanted)
		}
	}
	seen := make(map[string]bool)
	routes := make(map[string]bool)
	for _, route := range New(&fakeRepository{}, "").Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, rule := range employeeAccessRules {
		if seen[rule.Key] {
			t.Fatalf("duplicate capability %s", rule.Key)
		}
		seen[rule.Key] = true
		if !routes[rule.Method+" "+rule.Path] {
			t.Fatalf("capability references missing operation: %+v", rule)
		}
	}
}
