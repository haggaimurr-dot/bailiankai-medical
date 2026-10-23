package httpapi

import (
	"context"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

type handoffNoticeFake struct {
	fakeRepository
	role       string
	page, size int
	readID     string
}

func (f *handoffNoticeFake) AuthenticateEmployee(ctx context.Context, token string, now time.Time) (model.EmployeeAccount, error) {
	row, err := f.fakeRepository.AuthenticateEmployee(ctx, token, now)
	row.Role = f.role
	return row, err
}
func (f *handoffNoticeFake) HandoffNotifications(_ context.Context, page, size int) (store.HandoffNotificationPage, error) {
	f.calls++
	f.page = page
	f.size = size
	return store.HandoffNotificationPage{Data: []model.HandoffNotification{}, Total: 2, UnreadTotal: 1}, nil
}
func (f *handoffNoticeFake) ReadHandoffNotification(_ context.Context, id string, _ time.Time) error {
	f.calls++
	f.readID = id
	return f.err
}

func TestHandoffNotificationAuthorizationAndValidation(t *testing.T) {
	for _, role := range []string{"manager", "doctor", "nurse", "clinician", "customer_service", "operations"} {
		t.Run(role, func(t *testing.T) {
			repo := &handoffNoticeFake{role: role}
			r := New(repo, "")
			want := 200
			if role == "customer_service" || role == "operations" {
				want = 403
			}
			w := serve(r, "GET", "/api/v1/admin/handoff-notifications?page=2&pageSize=10", "", testToken, "")
			if w.Code != want {
				t.Fatalf("list status=%d body=%s", w.Code, w.Body)
			}
			w = serve(r, "POST", "/api/v1/admin/handoff-notifications/notice-test/read", "", testToken, "")
			if w.Code != want {
				t.Fatalf("read status=%d body=%s", w.Code, w.Body)
			}
			if want == 200 && (repo.page != 2 || repo.size != 10 || repo.readID != "notice-test") {
				t.Fatalf("arguments=%+v", repo)
			}
			if want == 403 && repo.calls != 0 {
				t.Fatal("forbidden request reached store")
			}
		})
	}
	repo := &handoffNoticeFake{role: "manager"}
	r := New(repo, "")
	for _, query := range []string{"page=0", "page=abc", "pageSize=101", "page=1&page=2", "employeeId=other"} {
		w := serve(r, "GET", "/api/v1/admin/handoff-notifications?"+query, "", testToken, "")
		if w.Code != 400 || repo.calls != 0 {
			t.Fatalf("query=%s status=%d calls=%d", query, w.Code, repo.calls)
		}
	}
	if w := serve(r, "GET", "/api/v1/admin/handoff-notifications", "", "", ""); w.Code != 401 {
		t.Fatalf("unauthenticated status=%d", w.Code)
	}
	repo.err = store.ErrNotFound
	if w := serve(r, "POST", "/api/v1/admin/handoff-notifications/other/read", "", testToken, ""); w.Code != 404 {
		t.Fatalf("other recipient status=%d body=%s", w.Code, w.Body)
	}
}
