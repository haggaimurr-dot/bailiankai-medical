package httpapi

import (
	"context"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"testing"
	"time"
)

type supportHTTPRepo struct {
	fakeRepository
	store.SupportRepository
	role  string
	calls int
}

func (r *supportHTTPRepo) AuthenticateEmployee(context.Context, string, time.Time) (model.EmployeeAccount, error) {
	return model.EmployeeAccount{ID: "support-agent", Role: r.role, ClinicIDs: []string{"clinic-demo"}}, nil
}
func (r *supportHTTPRepo) SupportTickets(context.Context, store.SupportFilter) ([]model.SupportTicket, int64, error) {
	r.calls++
	return []model.SupportTicket{}, 0, nil
}
func (r *supportHTTPRepo) SupportAgents(context.Context) ([]store.SupportAgent, error) {
	r.calls++
	return []store.SupportAgent{}, nil
}
func (r *supportHTTPRepo) ChangeSupport(context.Context, string, string, store.SupportInput, time.Time) (model.SupportTicket, error) {
	r.calls++
	return model.SupportTicket{}, nil
}
func TestSupportHTTPPermissionsAndValidation(t *testing.T) {
	for _, tc := range []struct {
		role, method, path, body string
		code                     int
	}{
		{"customer_service", "GET", "", "", 200}, {"manager", "GET", "/agents", "", 200},
		{"doctor", "GET", "", "", 403}, {"nurse", "GET", "", "", 403}, {"operations", "GET", "", "", 403},
		{"customer_service", "GET", "?pageSize=101", "", 400}, {"manager", "GET", "?overdue=invalid", "", 400},
		{"manager", "GET", "?status=invalid", "", 400}, {"manager", "GET", "?assignment=other", "", 400},
		{"customer_service", "POST", "/t-1/actions", `{"action":"message","content":"你好","requestKey":"support-request-0001","version":1}`, 200},
		{"customer_service", "POST", "/t-1/actions", `{"action":"note","content":" ","requestKey":"support-request-0001","version":1}`, 400},
		{"manager", "POST", "/t-1/actions", `{"action":"assign","content":"分派","requestKey":"support-request-0001","version":1,"assigneeId":"a"}`, 400},
		{"manager", "POST", "/t-1/actions", `{"action":"message","content":"你好","requestKey":"support-request-0001","version":1,"patientId":"forged"}`, 400},
	} {
		t.Run(tc.role+tc.method+tc.path+tc.body, func(t *testing.T) {
			r := &supportHTTPRepo{role: tc.role}
			w := serve(New(r, ""), tc.method, "/api/v1/admin/support"+tc.path, tc.body, testToken, "")
			if w.Code != tc.code {
				t.Fatalf("%d want %d: %s", w.Code, tc.code, w.Body)
			}
			if tc.code >= 400 && r.calls != 0 {
				t.Fatal("invalid request reached repository")
			}
		})
	}
}
func TestSupportInputDatesAndPatientActions(t *testing.T) {
	now := time.Now()
	past := now.Add(366 * 24 * time.Hour)
	future := now.Add(time.Hour)
	in := store.SupportInput{Action: "followup", Content: "实际跟进", RequestKey: "support-request-0001", Version: 1, DueAt: &past}
	if validSupportInput(in, false, false, now) {
		t.Fatal("accepted deadline beyond one year")
	}
	in.DueAt = &future
	if !validSupportInput(in, false, false, now) {
		t.Fatal("rejected future deadline")
	}
	if validSupportInput(in, false, true, now) {
		t.Fatal("patient performed internal action")
	}
}

func TestSupportAttachmentsAndRatingValidation(t *testing.T) {
	in := store.SupportInput{Action: "message", RequestKey: "support-request-0001", Version: 1, Attachments: []store.SupportUpload{{Name: "预约说明.pdf", Data: []byte("%PDF-1.4\nexample")}}}
	if !validSupportInput(in, false, true, time.Now()) {
		t.Fatal("attachment-only message rejected")
	}
	in.Attachments[0].Name = "../file.pdf"
	if validSupportInput(in, false, true, time.Now()) {
		t.Fatal("unsafe filename accepted")
	}
	in.Attachments[0].Name = "file.pdf"
	in.Attachments[0].Data = []byte("<script>alert(1)</script>")
	if validSupportInput(in, false, true, time.Now()) {
		t.Fatal("disguised active content accepted")
	}
	in.Attachments[0].Data = make([]byte, (2<<20)+1)
	if validSupportInput(in, false, true, time.Now()) {
		t.Fatal("oversized upload accepted")
	}
	in.Attachments = nil
	in.Content = "服务满意"
	in.Action = "rate"
	in.Rating = 5
	if !validSupportInput(in, false, true, time.Now()) {
		t.Fatal("valid patient rating rejected")
	}
	if validSupportInput(in, false, false, time.Now()) {
		t.Fatal("employee rating accepted")
	}
	in.Rating = 6
	if validSupportInput(in, false, true, time.Now()) {
		t.Fatal("invalid rating accepted")
	}
}
