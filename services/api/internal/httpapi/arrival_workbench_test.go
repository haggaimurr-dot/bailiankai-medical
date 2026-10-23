package httpapi

import "testing"

func TestArrivalWorkbenchPermissions(t *testing.T) {
	for _, role := range []string{"manager", "customer_service", "doctor", "clinician", "nurse", "operations", "unknown"} {
		for _, path := range []string{"/api/v1/admin/arrival-scans/resolve", "/api/v1/admin/arrival-scans/confirm"} {
			want := role == "manager" || role == "customer_service" || role == "doctor" || role == "clinician"
			if got := allowedEmployeeOperation(role, "POST", path); got != want {
				t.Errorf("scan %s %s: %v", role, path, got)
			}
			if allowedEmployeeOperation(role, "GET", path) {
				t.Errorf("scan GET permitted: %s", role)
			}
		}
		want := role == "manager" || role == "customer_service"
		if got := allowedEmployeeOperation(role, "POST", "/api/v1/admin/appointments/:appointmentId/suspension-reschedule"); got != want {
			t.Errorf("reschedule %s: %v", role, got)
		}
	}
}
