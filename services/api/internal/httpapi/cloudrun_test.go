package httpapi

import (
	"net/http"
	"testing"
)

func TestCloudRunRoutesAndOrigins(t *testing.T) {
	t.Setenv("APP_ENV", "cloudrun")
	t.Setenv("ADMIN_ALLOWED_ORIGINS", "https://admin.example.com")
	r := New(&fakeRepository{}, "")
	for _, route := range r.Routes() {
		if route.Path == "/api/v1/dev/patient-sessions" {
			t.Fatal("cloud exposes development patient login")
		}
	}
	for _, tc := range []struct {
		origin string
		status int
	}{
		{"https://admin.example.com", http.StatusNoContent},
		{"https://evil.example.com", http.StatusForbidden},
		{"https://admin.example.com.evil.example", http.StatusForbidden},
		{"https://admin.example.com/path", http.StatusForbidden},
		{"http://admin.example.com", http.StatusForbidden},
		{"http://127.0.0.1:5173", http.StatusNoContent},
	} {
		w := serve(r, "OPTIONS", "/api/v1/clinics", "", "", tc.origin)
		if w.Code != tc.status {
			t.Errorf("origin %q: %d want %d", tc.origin, w.Code, tc.status)
		}
		if tc.status == http.StatusNoContent && w.Header().Get("Access-Control-Allow-Origin") != tc.origin {
			t.Error("missing exact allowed origin")
		}
	}
}
