package httpapi

import "testing"

func TestAssessmentPermissionMatrix(t *testing.T) {
	for _, role := range []string{"manager", "doctor", "nurse", "customer_service", "operations", "clinician", "unknown"} {
		for _, tc := range []struct {
			method, path string
			allowed      bool
		}{
			{"GET", "/api/v1/admin/assessments", role == "manager" || role == "doctor"},
			{"GET", "/api/v1/admin/assessments/:assessmentId/photos/:photoRef/content", role == "manager" || role == "doctor"},
			{"POST", "/api/v1/admin/assessments/:assessmentId/assignment", role == "manager"},
			{"POST", "/api/v1/admin/assessments/:assessmentId/reviews", role == "doctor"},
			{"POST", "/api/v1/admin/assessments/new-unknown-operation", false},
		} {
			t.Run(role+tc.method+tc.path, func(t *testing.T) {
				if got := allowedEmployeeOperation(role, tc.method, tc.path); got != tc.allowed {
					t.Fatalf("allowed=%v want=%v", got, tc.allowed)
				}
			})
		}
	}
}
