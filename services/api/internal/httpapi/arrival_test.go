package httpapi

import (
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
)

func TestArrivalAndAnalyticsPermissionMatrix(t *testing.T) {
	for _, role := range []string{"manager", "operations", "customer_service", "doctor", "clinician", "nurse", "unknown"} {
		wantArrival := role == "manager" || role == "customer_service" || role == "doctor" || role == "clinician"
		wantAnalytics := role == "manager" || role == "operations"
		if got := allowedEmployeeOperation(role, "POST", "/api/v1/admin/appointments/:appointmentId/arrival"); got != wantArrival {
			t.Errorf("arrival role %s: got %v want %v", role, got, wantArrival)
		}
		if got := allowedEmployeeOperation(role, "GET", "/api/v1/admin/analytics"); got != wantAnalytics {
			t.Errorf("analytics role %s: got %v want %v", role, got, wantAnalytics)
		}
	}
}

func TestAppointmentArrivalCapabilitiesAndCancellation(t *testing.T) {
	now := time.Now()
	row := model.Appointment{Status: "confirmed", StartsAt: now.Add(time.Hour), ConfirmationDeadline: now.Add(time.Hour)}
	confirmed := appointmentDTO(row, now, true)
	if confirmed.ArrivedAt != nil || confirmed.CanRegisterArrival == nil || !*confirmed.CanRegisterArrival {
		t.Fatal("confirmation must keep arrival unrecorded and allow separate registration")
	}
	row.Status, row.TreatmentRecorded = "completed", true
	legacy := appointmentDTO(row, now, true)
	if legacy.ArrivedAt != nil || !*legacy.CanRegisterArrival {
		t.Fatal("legacy treatment must not imply arrival; verified backfill is available")
	}
	row.Status, row.TreatmentRecorded, row.ArrivedAt = "confirmed", false, &now
	for _, admin := range []bool{false, true} {
		result := appointmentDTO(row, now, admin)
		if result.CanCancel || *result.CanRegisterArrival || result.ArrivedAt == nil {
			t.Fatal("arrival must disable cancellation and duplicate registration")
		}
	}
	row.ArrivedAt = nil
	for _, status := range []string{"pending", "cancelled", "rejected", "expired"} {
		row.Status = status
		if *appointmentDTO(row, now, true).CanRegisterArrival {
			t.Errorf("arrival allowed for %s", status)
		}
	}
}
