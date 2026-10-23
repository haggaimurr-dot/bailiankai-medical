package httpapi

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPatientBindingPermissions(t *testing.T) {
	for _, role := range []string{"manager", "doctor", "clinician", "nurse", "customer_service", "operations", "unknown"} {
		for _, method := range []string{"POST", "DELETE", "GET", "PUT"} {
			expected := (role == "manager" || role == "doctor" || role == "clinician") && (method == "POST" || method == "DELETE")
			if allowedEmployeeOperation(role, method, "/api/v1/admin/patients/:patientId/wechat-binding") != expected {
				t.Fatal("incorrect binding permission", role, method)
			}
		}
	}
}
func TestPatientBindingValidation(t *testing.T) {
	a := &API{}
	for _, tc := range []struct {
		body        string
		confirm, ok bool
	}{
		{`{"code":"0123456789abcdef0123456789abcdef"}`, false, true},
		{`{"code":"0123456789abcdef0123456789abcdef","version":1}`, true, true},
		{`{"code":"short"}`, false, false},
		{`{"code":null}`, false, false},
		{`{"code":"0123456789abcdef0123456789abcdef"}`, true, false},
		{`{"code":"0123456789abcdef0123456789abcdef","version":0}`, true, false},
		{`{"code":"0123456789abcdef0123456789abcdef","version":1,"patientId":"forged"}`, true, false},
	} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
		c.Request.Header.Set("Content-Type", "application/json")
		_, _, ok := a.decodeBinding(c, tc.confirm)
		if ok != tc.ok {
			t.Fatalf("%s: got %v", tc.body, ok)
		}
	}
}
