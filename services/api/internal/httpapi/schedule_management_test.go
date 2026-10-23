package httpapi

import (
	"context"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"net/http"
	"testing"
	"time"
)

type scheduleManagementFake struct{ scheduleFake }

func (f *scheduleManagementFake) GetScheduleSetting(_ context.Context, id string) (store.ScheduleSetting, error) {
	return store.ScheduleSetting{ClinicID: id, Templates: []store.ScheduleTemplate{}, Holidays: []store.HolidayRule{}}, f.err
}
func (f *scheduleManagementFake) SaveScheduleSetting(_ context.Context, in store.ScheduleSetting, _ time.Time) (store.ScheduleSetting, error) {
	f.calls++
	return in, f.err
}
func (f *scheduleManagementFake) SchedulePatientTasks(context.Context, string, time.Time) ([]store.SchedulePatientTask, error) {
	return []store.SchedulePatientTask{}, f.err
}
func (f *scheduleManagementFake) UpdateScheduleContact(context.Context, string, store.ScheduleContactInput, time.Time) error {
	f.calls++
	return f.err
}
func (f *scheduleManagementFake) RescheduleAffectedPatient(context.Context, string, string, store.RescheduleInput, time.Time) (model.Appointment, error) {
	f.calls++
	return model.Appointment{}, f.err
}
func TestScheduleManagementRequestValidation(t *testing.T) {
	f := &scheduleManagementFake{}
	router := New(f, "")
	for _, body := range []string{`{}`, `{"templates":null,"holidays":[],"version":0}`, `{"templates":[{"id":"t"}],"holidays":[],"version":0}`, `{"templates":[],"holidays":[{"date":"2099-01-01","name":"休诊"}],"version":0}`} {
		w := serve(router, http.MethodPut, "/api/v1/admin/schedule/settings/clinic-demo", body, testToken, "")
		if w.Code != 400 {
			t.Fatalf("body=%s status=%d", body, w.Code)
		}
	}
	if f.calls != 0 {
		t.Fatalf("invalid request reached store: %d", f.calls)
	}
	w := serve(router, http.MethodPut, "/api/v1/admin/schedule/settings/clinic-demo", `{"templates":[],"holidays":[],"version":0}`, testToken, "")
	if w.Code != 200 || f.calls != 1 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	w = serve(router, http.MethodGet, "/api/v1/admin/schedule/patients?clinicId=clinic-demo", "", "", "")
	if w.Code != 401 {
		t.Fatalf("unauthenticated: %d", w.Code)
	}
	w = serve(router, http.MethodPost, "/api/v1/admin/schedule/patients/task-test/reschedule", `{"slotId":"slot-target","version":1}`, testToken, "")
	if w.Code != 400 {
		t.Fatalf("missing request key: %d", w.Code)
	}
	f.err = store.ErrConflict
	w = serve(router, http.MethodPut, "/api/v1/admin/schedule/patients/task-test/contact", `{"version":1,"contactStatus":"contacted","note":"已联系"}`, testToken, "")
	if w.Code != 409 {
		t.Fatalf("conflict: %d %s", w.Code, w.Body.String())
	}
}
