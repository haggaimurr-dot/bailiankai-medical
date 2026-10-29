package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

type arrivalResponse struct {
	Data struct {
		ID                 string     `json:"id"`
		Status             string     `json:"status"`
		Version            int        `json:"version"`
		ArrivedAt          *time.Time `json:"arrivedAt"`
		CanCancel          bool       `json:"canCancel"`
		CanRegisterArrival bool       `json:"canRegisterArrival"`
	} `json:"data"`
}

func readArrivalResponse(t *testing.T, response *httptest.ResponseRecorder) arrivalResponse {
	t.Helper()
	var result arrivalResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func arrivalRequestBody(t *testing.T, version int, at time.Time) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"version": version, "arrivedAt": at})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func confirmedArrivalFixture(t *testing.T) (*bookingFixture, store.SessionResult, model.Appointment) {
	t.Helper()
	f := newBookingFixture(t)
	patient := f.patient()
	row := f.create(patient, "actual-arrival-request-1")
	// Backdated creation leaves a valid interval for recording a past visit;
	// the future slot also proves arrival does not depend on its scheduled start.
	if err := f.db.Model(&model.Appointment{}).Where("id = ?", row.ID).Update("created_at", f.now.Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	confirmed, err := f.repo.ReviewAppointment(context.Background(), row.ID, store.ReviewInput{Action: "confirm", Version: row.Version}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	return f, patient, confirmed
}

func arrivalAuditCount(t *testing.T, f *bookingFixture, id string) int64 {
	t.Helper()
	var count int64
	if err := f.db.Model(&model.AppointmentEvent{}).Where("appointment_id = ? AND action = ?", id, "arrive").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func TestActualArrivalMySQLIntegration(t *testing.T) {
	t.Run("confirmation is independent and recording is immutable and idempotent", func(t *testing.T) {
		f, patient, row := confirmedArrivalFixture(t)
		path := "/api/v1/admin/appointments/" + row.ID
		before := f.http("GET", path, "", bookingAdminToken, "")
		requireHTTP(t, before, 200, "")
		unarrived := readArrivalResponse(t, before)
		if unarrived.Data.ArrivedAt != nil || !unarrived.Data.CanRegisterArrival {
			t.Fatalf("confirmed booking implied arrival: %+v", unarrived.Data)
		}
		at := f.now.Add(-30 * time.Minute)
		body := arrivalRequestBody(t, row.Version, at)
		response := f.http("POST", path+"/arrival", body, bookingAdminToken, "")
		requireHTTP(t, response, 200, "")
		registered := readArrivalResponse(t, response)
		if registered.Data.ArrivedAt == nil || !registered.Data.ArrivedAt.Equal(at) || registered.Data.Version != row.Version+1 || registered.Data.Status != "confirmed" || registered.Data.CanRegisterArrival {
			t.Fatalf("actual arrival response: %+v", registered.Data)
		}
		f.assertReserved(1)
		// Equivalent UTC and Shanghai timestamps with the original version replay
		// the same visit instead of adding another event or incrementing version.
		response = f.http("POST", path+"/arrival", arrivalRequestBody(t, row.Version, at.UTC()), bookingAdminToken, "")
		requireHTTP(t, response, 200, "")
		if retry := readArrivalResponse(t, response); retry.Data.Version != registered.Data.Version || retry.Data.ArrivedAt == nil || !retry.Data.ArrivedAt.Equal(at) {
			t.Fatalf("retry changed arrival: %+v", retry.Data)
		}
		if count := arrivalAuditCount(t, f, row.ID); count != 1 {
			t.Fatalf("duplicate arrival events: %d", count)
		}
		var event model.AppointmentEvent
		if err := f.db.Where("appointment_id = ? AND action = ?", row.ID, "arrive").First(&event).Error; err != nil || event.ActorID == "" || event.FromStatus != "confirmed" || event.ToStatus != "confirmed" {
			t.Fatalf("arrival audit: %+v %v", event, err)
		}
		requireHTTP(t, f.http("POST", path+"/arrival", arrivalRequestBody(t, registered.Data.Version, at.Add(time.Minute)), bookingAdminToken, ""), 409, "VERSION_CONFLICT")
		patientPath := "/api/v1/me/appointments/" + row.ID
		patientResponse := f.http("GET", patientPath, "", patient.Token, "")
		requireHTTP(t, patientResponse, 200, "")
		if current := readArrivalResponse(t, patientResponse); current.Data.CanCancel || current.Data.CanRegisterArrival || current.Data.ArrivedAt == nil {
			t.Fatalf("patient arrival/cancel capability: %+v", current.Data)
		}
		requireHTTP(t, f.http("POST", patientPath+"/cancel", fmt.Sprintf(`{"version":%d}`, registered.Data.Version), patient.Token, ""), 409, "APPOINTMENT_STATE_CONFLICT")
		f.assertReserved(1)
	})

	t.Run("invalid time transition version and identity cannot persist arrival", func(t *testing.T) {
		f := newBookingFixture(t)
		patient := f.patient()
		row := f.create(patient, "actual-arrival-invalid-1")
		path := "/api/v1/admin/appointments/" + row.ID + "/arrival"
		valid := arrivalRequestBody(t, row.Version, f.now)
		requireHTTP(t, f.http("POST", path, valid, patient.Token, ""), 401, "")
		requireHTTP(t, f.http("POST", path, valid, bookingAdminToken, ""), 409, "APPOINTMENT_STATE_CONFLICT")
		confirmed, err := f.repo.ReviewAppointment(context.Background(), row.ID, store.ReviewInput{Action: "confirm", Version: row.Version}, f.now)
		if err != nil {
			t.Fatal(err)
		}
		requireHTTP(t, f.http("POST", path, valid, bookingAdminToken, ""), 409, "VERSION_CONFLICT")
		for _, at := range []time.Time{row.CreatedAt.Add(-time.Millisecond), f.now.Add(24 * time.Hour)} {
			requireHTTP(t, f.http("POST", path, arrivalRequestBody(t, confirmed.Version, at), bookingAdminToken, ""), 400, "INVALID_ARRIVAL_TIME")
		}
		for _, body := range []string{`{}`, `{"version":2}`, `{"version":2,"arrivedAt":null}`, `{"version":0,"arrivedAt":"2026-10-01T09:00:00+08:00"}`, `{"version":2,"arrivedAt":"not-a-date"}`, `{"version":2,"arrivedAt":"2026-10-01T09:00:00+08:00","arrivedBy":"spoofed"}`} {
			requireHTTP(t, f.http("POST", path, body, bookingAdminToken, ""), 400, "")
		}
		if count := arrivalAuditCount(t, f, row.ID); count != 0 {
			t.Fatalf("invalid request persisted arrival: %d", count)
		}
		cancelled, err := f.repo.CancelAppointment(context.Background(), row.ID, patient.Patient.ID, confirmed.Version, "", "", f.now)
		if err != nil {
			t.Fatal(err)
		}
		requireHTTP(t, f.http("POST", path, arrivalRequestBody(t, cancelled.Version, f.now), bookingAdminToken, ""), 409, "APPOINTMENT_STATE_CONFLICT")
	})

	t.Run("completed treatment does not imply arrival and permits chronological backfill", func(t *testing.T) {
		f, patient, row, treatment := newFollowupFixture(t)
		createdAt := f.now.Add(-72 * time.Hour)
		if err := f.db.Model(&model.Appointment{}).Where("id = ?", row.ID).Update("created_at", createdAt).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.RecordTreatment(context.Background(), row.ID, treatment, f.now); err != nil {
			t.Fatal(err)
		}
		path := "/api/v1/admin/appointments/" + row.ID
		response := f.http("GET", path, "", bookingAdminToken, "")
		requireHTTP(t, response, 200, "")
		completed := readArrivalResponse(t, response)
		if completed.Data.Status != "completed" || completed.Data.ArrivedAt != nil || !completed.Data.CanRegisterArrival {
			t.Fatalf("treatment implied arrival: %+v", completed.Data)
		}
		requireHTTP(t, f.http("POST", path+"/arrival", arrivalRequestBody(t, completed.Data.Version, treatment.TreatedAt.Add(time.Minute)), bookingAdminToken, ""), 400, "INVALID_ARRIVAL_TIME")
		// Arrival at exactly the treatment time is allowed for legacy backfill.
		response = f.http("POST", path+"/arrival", arrivalRequestBody(t, completed.Data.Version, treatment.TreatedAt), bookingAdminToken, "")
		requireHTTP(t, response, 200, "")
		backfilled := readArrivalResponse(t, response)
		if backfilled.Data.ArrivedAt == nil || !backfilled.Data.ArrivedAt.Equal(treatment.TreatedAt) || backfilled.Data.Status != "completed" {
			t.Fatalf("completed arrival backfill: %+v", backfilled.Data)
		}
		_, tasks, err := f.repo.FollowupWorkspace(context.Background(), row.ID, patient.Patient.ID)
		if err != nil || len(tasks) != 3 {
			t.Fatalf("arrival changed treatment followups: %d %v", len(tasks), err)
		}
	})

	t.Run("recorded arrival constrains treatment and correction chronology", func(t *testing.T) {
		f, _, row, input := newFollowupFixture(t)
		ctx := context.Background()
		if err := f.db.Model(&model.Appointment{}).Where("id = ?", row.ID).Update("created_at", f.now.Add(-72*time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
		at := input.TreatedAt.Add(-time.Hour)
		arrived, err := f.repo.RegisterArrival(ctx, row.ID, store.ArrivalInput{Version: row.Version, ArrivedAt: at}, f.now)
		if err != nil {
			t.Fatal(err)
		}
		input.Version = arrived.Version
		invalid := input
		invalid.TreatedAt = at.Add(-time.Millisecond)
		if _, err := f.repo.RecordTreatment(ctx, row.ID, invalid, f.now); !errors.Is(err, store.ErrCareState) {
			t.Fatalf("treatment before arrival accepted: %v", err)
		}
		var count int64
		if err := f.db.Model(&model.Treatment{}).Where("appointment_id = ?", row.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("invalid treatment persisted: %d %v", count, err)
		}
		treatment, err := f.repo.RecordTreatment(ctx, row.ID, input, f.now)
		if err != nil {
			t.Fatal(err)
		}
		correction := store.TreatmentCorrectionInput{Version: treatment.Version, PlanVersionID: input.PlanVersionID, Summary: input.Summary, Notes: input.Notes, TreatedAt: at.Add(-time.Millisecond), CorrectionReason: "核对实际治疗时间"}
		if _, err := f.repo.CorrectTreatment(ctx, row.ID, correction, f.now); !errors.Is(err, store.ErrCareState) {
			t.Fatalf("treatment correction before arrival accepted: %v", err)
		}
		revisions, err := f.repo.TreatmentRevisions(ctx, row.ID, "")
		if err != nil || len(revisions) != 1 || !revisions[0].TreatedAt.Equal(input.TreatedAt) {
			t.Fatalf("invalid correction changed treatment history: %+v %v", revisions, err)
		}
		// Equality is valid, and the original arrival request still recovers its
		// saved result after treatment has incremented the appointment version.
		correction.TreatedAt = at
		if _, err := f.repo.CorrectTreatment(ctx, row.ID, correction, f.now); err != nil {
			t.Fatalf("treatment at arrival instant rejected: %v", err)
		}
		retried, err := f.repo.RegisterArrival(ctx, row.ID, store.ArrivalInput{Version: row.Version, ArrivedAt: at}, f.now)
		if err != nil || retried.Status != "completed" || retried.ArrivedAt == nil || !retried.ArrivedAt.Equal(at) || arrivalAuditCount(t, f, row.ID) != 1 {
			t.Fatalf("arrival retry after treatment: %+v %v", retried, err)
		}
	})

	t.Run("concurrent retries register one visit and arrival serializes with cancellation", func(t *testing.T) {
		f, patient, row := confirmedArrivalFixture(t)
		ctx := context.Background()
		input := store.ArrivalInput{Version: row.Version, ArrivedAt: f.now.Add(-time.Minute)}
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() {
				<-start
				_, err := f.repo.RegisterArrival(ctx, row.ID, input, f.now)
				results <- err
			})
		}
		close(start)
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatalf("identical concurrent arrival failed: %v", err)
			}
		}
		latest, err := f.repo.GetAppointment(ctx, row.ID, patient.Patient.ID, f.now)
		if err != nil || latest.Version != row.Version+1 || arrivalAuditCount(t, f, row.ID) != 1 {
			t.Fatalf("concurrent arrival not singular: %+v %v", latest, err)
		}
		f.assertReserved(1)

		otherPatient := f.patient()
		other := f.create(otherPatient, "arrival-cancel-race-01")
		if err := f.db.Model(&model.Appointment{}).Where("id = ?", other.ID).Update("created_at", f.now.Add(-time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
		other, err = f.repo.ReviewAppointment(ctx, other.ID, store.ReviewInput{Action: "confirm", Version: other.Version}, f.now)
		if err != nil {
			t.Fatal(err)
		}
		start = make(chan struct{})
		results = make(chan error, 2)
		wg.Go(func() {
			<-start
			_, err := f.repo.RegisterArrival(ctx, other.ID, store.ArrivalInput{Version: other.Version, ArrivedAt: f.now}, f.now)
			results <- err
		})
		wg.Go(func() {
			<-start
			_, err := f.repo.CancelAppointment(ctx, other.ID, otherPatient.Patient.ID, other.Version, "", "", f.now)
			results <- err
		})
		close(start)
		wg.Wait()
		close(results)
		success := 0
		for err := range results {
			if err == nil {
				success++
			} else if !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrTransition) {
				t.Fatal(err)
			}
		}
		if success != 1 {
			t.Fatalf("arrival/cancel race successes=%d", success)
		}
		latest, err = f.repo.GetAppointment(ctx, other.ID, otherPatient.Patient.ID, f.now)
		if err != nil {
			t.Fatal(err)
		}
		if latest.Status == "cancelled" {
			if latest.ArrivedAt != nil || arrivalAuditCount(t, f, other.ID) != 0 {
				t.Fatal("cancelled appointment acquired an arrival")
			}
			f.assertReserved(1)
		} else {
			if latest.Status != "confirmed" || latest.ArrivedAt == nil || arrivalAuditCount(t, f, other.ID) != 1 {
				t.Fatalf("arrival/cancel outcome: %+v", latest)
			}
			f.assertReserved(2)
		}
	})
}

func TestArrivalEmployeeScopeMySQLIntegration(t *testing.T) {
	for index, scenario := range []struct {
		name, role, staff string
		otherClinic       bool
		wantStatus        int
	}{
		{"manager", "manager", "", false, 200},
		{"customer service", "customer_service", "", false, 200},
		{"assigned doctor", "doctor", "assigned", false, 200},
		{"assigned clinician", "clinician", "assigned", false, 200},
		{"other doctor", "doctor", "other", false, 404},
		{"nurse", "nurse", "nurse-demo-1", false, 403},
		{"operations", "operations", "", false, 403},
		{"manager outside clinic", "manager", "", true, 404},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f, _, row := confirmedArrivalFixture(t)
			staff := scenario.staff
			if staff == "assigned" {
				staff = f.slot.DoctorID
			} else if staff == "other" {
				var doctor model.Doctor
				if err := f.db.Where("id <> ?", f.slot.DoctorID).First(&doctor).Error; err != nil {
					t.Fatal(err)
				}
				staff = doctor.ID
			}
			clinicID := f.slot.ClinicID
			if scenario.otherClinic {
				clinicID = "arrival-other-clinic"
				if err := f.db.Create(&model.Clinic{ID: clinicID, Name: "Other arrival clinic"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			ctx := store.WithClinicAssignment(context.Background(), []string{clinicID})
			username := fmt.Sprintf("arrival-%d-%d", index, time.Now().UnixNano())
			account, err := f.repo.CreateEmployee(ctx, username, "Arrival operator", "ActualArrivalTest123", scenario.role, staff, f.now)
			if err != nil {
				t.Fatal(err)
			}
			_, token, err := f.repo.LoginEmployee(context.Background(), username, "ActualArrivalTest123", f.now)
			if err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/admin/appointments/" + row.ID
			response := f.http("POST", path+"/arrival", arrivalRequestBody(t, row.Version, f.now.Add(-time.Minute)), token, "")
			requireHTTP(t, response, scenario.wantStatus, "")
			scope := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), scenario.role, staff, []string{clinicID}), account.ID)
			_, scopeErr := f.repo.RegisterArrival(scope, row.ID, store.ArrivalInput{Version: row.Version, ArrivedAt: f.now.Add(-time.Minute)}, f.now)
			if scenario.wantStatus == 403 && !errors.Is(scopeErr, store.ErrForbidden) || scenario.wantStatus == 404 && !errors.Is(scopeErr, store.ErrNotFound) || scenario.wantStatus == 200 && scopeErr != nil {
				t.Fatalf("store arrival scope status=%d err=%v", scenario.wantStatus, scopeErr)
			}
			if scenario.wantStatus == 200 {
				var saved model.Appointment
				if err := f.db.First(&saved, "id = ?", row.ID).Error; err != nil || saved.ArrivedBy != account.ID || saved.ArrivedAt == nil {
					t.Fatalf("arrival operator attribution: %+v %v", saved, err)
				}
			} else if count := arrivalAuditCount(t, f, row.ID); count != 0 {
				t.Fatalf("unauthorized arrival persisted: %d", count)
			}
		})
	}
}
