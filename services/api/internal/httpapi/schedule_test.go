package httpapi

import (
	"context"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"net/http"
	"strings"
	"testing"
	"time"
)

type scheduleFake struct{ fakeRepository }

func (f *scheduleFake) CreateSlots(_ context.Context, rows []model.Slot, _ time.Time) ([]model.Slot, error) {
	f.calls++
	if len(rows) == 0 || len(rows) > 500 {
		return nil, store.ErrScheduleInput
	}
	return rows, f.err
}
func (f *scheduleFake) PreviewBatchSlots(_ context.Context, grid store.SlotBatchGrid, _ time.Time) ([]store.SlotDraft, []store.SlotConflict, error) {
	f.calls++
	if grid.ClinicID == "" || grid.DoctorID == "" || len(grid.StartTimes) == 0 {
		return nil, nil, store.ErrScheduleInput
	}
	return []store.SlotDraft{}, []store.SlotConflict{}, f.err
}
func (f *scheduleFake) CreateBatchSlots(_ context.Context, grid store.SlotBatchGrid, _ time.Time) ([]model.Slot, []store.SlotConflict, error) {
	f.calls++
	if grid.ClinicID == "" || grid.DoctorID == "" || len(grid.StartTimes) == 0 {
		return nil, nil, store.ErrScheduleInput
	}
	return []model.Slot{}, []store.SlotConflict{}, f.err
}
func (f *scheduleFake) SuspendSlots(_ context.Context, refs []store.SlotReference, reason string, _ time.Time) ([]model.Slot, error) {
	f.calls++
	if len(refs) == 0 || strings.TrimSpace(reason) == "" {
		return nil, store.ErrScheduleInput
	}
	return []model.Slot{}, f.err
}

func TestScheduleBatchPreviewAndCreate(t *testing.T) {
	valid := `{"clinicId":"clinic-demo","doctorId":"doctor-demo-1","startDate":"2099-01-01","endDate":"2099-01-02","startTimes":["09:00"],"capacity":3}`
	f := &scheduleFake{}
	if w := serve(New(f, ""), http.MethodPost, "/api/v1/admin/slots/batch-preview", valid, testToken, ""); w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	if w := serve(New(f, ""), http.MethodPost, "/api/v1/admin/slots/batch-create", valid, testToken, ""); w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	for _, body := range []string{`{}`, `{"clinicId":"clinic-demo","doctorId":"doctor-demo-1","startDate":"2099-01-01","endDate":"2099-01-02","startTimes":[],"capacity":3}`, `{"clinicId":"clinic-demo","doctorId":"doctor-demo-1","startDate":"2099-01-01","endDate":"2099-01-02","startTimes":["09:00"],"capacity":3,"extra":1}`} {
		if w := serve(New(f, ""), http.MethodPost, "/api/v1/admin/slots/batch-preview", body, testToken, ""); w.Code != 400 {
			t.Fatalf("preview body=%s code=%d response=%s", body, w.Code, w.Body.String())
		}
	}
}

func TestScheduleRequestValidation(t *testing.T) {
	valid := `{"clinicId":"clinic-demo","doctorId":"doctor-demo-1","startsAt":"2099-01-01T09:00:00+08:00","endsAt":"2099-01-01T10:00:00+08:00","capacity":3,"status":"open"}`
	for _, body := range []string{`{}`, `{"slots":null}`, `{"slots":[]}`, `{"slots":[null]}`, `{"slots":[` + strings.Replace(valid, `,"capacity":3`, "", 1) + `]}`, `{"slots":[` + strings.Replace(valid, `"capacity":3`, `"capacity":null`, 1) + `]}`, `{"slots":[` + strings.Replace(valid, `"capacity":3`, `"capacity":3,"reserved":1`, 1) + `]}`} {
		f := &scheduleFake{}
		w := serve(New(f, ""), http.MethodPost, "/api/v1/admin/slots", body, testToken, "")
		if w.Code != 400 {
			t.Fatalf("body=%s code=%d response=%s", body, w.Code, w.Body.String())
		}
	}
	f := &scheduleFake{}
	if w := serve(New(f, ""), http.MethodPost, "/api/v1/admin/slots", `{"slots":[`+valid+`]}`, testToken, ""); w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if w := serve(New(f, ""), http.MethodPost, "/api/v1/admin/slots", `{"slots":[`+valid+`]}`, "", ""); w.Code != 401 {
		t.Fatalf("unauthenticated: %d", w.Code)
	}
	for _, body := range []string{`{"slots":[{"id":"slot-1"}],"reason":"停诊"}`, `{"slots":[{"id":"slot-1","version":1,"extra":1}],"reason":"停诊"}`, `{"slots":[{"id":"slot-1","version":1}],"reason":" "}`} {
		if w := serve(New(f, ""), http.MethodPost, "/api/v1/admin/slots/suspend", body, testToken, ""); w.Code != 400 {
			t.Fatalf("suspend: %d %s", w.Code, w.Body.String())
		}
	}
}
