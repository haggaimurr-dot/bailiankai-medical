package httpapi

import "testing"

func TestDirectoryMaintenanceCapabilities(t *testing.T) {
	for _, role := range []string{"manager", "doctor", "nurse", "clinician", "customer_service", "operations"} {
		t.Run(role, func(t *testing.T) {
			hasClinicPage := false
			for _, key := range employeeCapabilities(role) {
				if key == "/clinics" {
					hasClinicPage = true
				}
			}
			if hasClinicPage != (role == "manager") {
				t.Fatalf("clinic maintenance capability for %s = %v", role, hasClinicPage)
			}
			for _, path := range []string{"/api/v1/admin/clinics", "/api/v1/admin/doctors", "/api/v1/admin/doctors/:resourceId/history"} {
				if got := allowedEmployeeOperation(role, "GET", path); got != (role == "manager") {
					t.Fatalf("directory read %s for %s = %v", path, role, got)
				}
			}
		})
	}
}
