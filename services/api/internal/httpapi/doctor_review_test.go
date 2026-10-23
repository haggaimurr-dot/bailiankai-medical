package httpapi

import (
	"encoding/json"
	"example.com/bailiankai/api/internal/model"
	"strings"
	"testing"
	"time"
)

func TestDoctorQualificationReviewPublicPrivacy(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	row := model.Doctor{QualificationReview: &model.DoctorQualificationReview{Status: "approved", Note: "private verification evidence", ReviewerID: "private-reviewer", ReviewedAt: now}}
	data, err := json.Marshal(doctorDTO(row))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private") || !strings.Contains(string(data), "qualificationReviewedAt") {
		t.Fatalf("invalid public review: %s", data)
	}
	row.QualificationReview.Status = "rejected"
	if doctorDTO(row).QualificationReviewedAt != nil {
		t.Fatal("rejected review shown as verified")
	}
}

func TestDoctorQualificationReviewPermissions(t *testing.T) {
	for _, role := range []string{"manager", "doctor", "clinician", "nurse", "operations", "customer_service"} {
		if got := allowedEmployeeOperation(role, "POST", "/api/v1/admin/doctors/:resourceId/review"); got != (role == "manager") {
			t.Fatalf("review permission for %s = %v", role, got)
		}
	}
}
