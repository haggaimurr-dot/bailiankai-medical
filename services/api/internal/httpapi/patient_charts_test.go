package httpapi

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPatientChartPermissions(t *testing.T) {
	for _, role := range []string{"manager", "doctor", "clinician", "nurse", "customer_service", "operations", "unknown"} {
		allowed := role == "manager" || role == "doctor" || role == "clinician"
		for _, op := range [][2]string{{"POST", "/api/v1/admin/patients"}, {"PUT", "/api/v1/admin/patients/:patientId/profile"}, {"GET", "/api/v1/admin/consultation-templates"}, {"POST", "/api/v1/admin/consultation-templates"}, {"DELETE", "/api/v1/admin/consultation-templates/:templateId"}} {
			if got := allowedEmployeeOperation(role, op[0], op[1]); got != allowed {
				t.Errorf("%s %v: %v", role, op, got)
			}
		}
	}
}
func TestPatientChartValidation(t *testing.T) {
	a := &API{now: func() time.Time { return time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC) }}
	valid := map[string]any{"displayName": "患者", "clinicId": "clinic-demo", "gender": "unknown", "birthDate": "", "contactPhone": "", "tags": []string{"敏感肌"}}
	for _, tc := range []struct {
		name, key  string
		value      any
		update, ok bool
	}{
		{"valid", "contactPhone", "13800138000", false, true},
		{"blank name", "displayName", " ", false, false},
		{"invalid phone", "contactPhone", "123", false, false},
		{"empty clinic", "clinicId", "", false, false},
		{"invalid gender", "gender", "other", false, false},
		{"future birth", "birthDate", "2026-10-08", false, false},
		{"invalid day", "birthDate", "2026-02-30", false, false},
		{"duplicate tags", "tags", []string{"敏感肌", " 敏感肌 "}, false, false},
		{"long tag", "tags", []string{strings.Repeat("字", 25)}, false, false},
		{"too many tags", "tags", make([]string, 21), false, false},
		{"null tags", "tags", nil, false, false},
		{"missing version", "gender", "female", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{}
			for k, v := range valid {
				body[k] = v
			}
			body[tc.key] = tc.value
			b, _ := json.Marshal(body)
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest("POST", "/", strings.NewReader(string(b)))
			c.Request.Header.Set("Content-Type", "application/json")
			_, ok := a.decodeChart(c, tc.update)
			if ok != tc.ok {
				t.Fatalf("validation %v %s", ok, response.Body.String())
			}
		})
	}
}
func TestStructuredConsultationLimits(t *testing.T) {
	if validStructuredFields("", "", "", strings.Repeat("字", 33), "") || validStructuredFields(strings.Repeat("字", 5001), "", "", "", "") {
		t.Fatal("oversized field accepted")
	}
	if !validStructuredFields("已核对", "已核对", "已核对", "敏感性", "改善肤质") {
		t.Fatal("valid fields rejected")
	}
}
