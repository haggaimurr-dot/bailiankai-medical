package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func TestEmployeeLoginAnomalyAlert(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	account, err := f.repo.CreateEmployee(ctx, "login-anomaly-test", "登录异常测试", "LoginAnomalyTest123!", "manager", "", f.now)
	if err != nil {
		t.Fatal(err)
	}
	// Use incrementing timestamps so ORDER BY created_at DESC is deterministic.
	// (All attempts with the same created_at would tie-break by random ID.)
	now := f.now
	tick := func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	fail := func() {
		t.Helper()
		if _, _, err := f.repo.LoginEmployee(store.WithLoginClientIP(ctx, "203.0.113.9"), account.Username, "wrong-password", tick()); !errors.Is(err, store.ErrInvalidCredentials) {
			t.Fatalf("expected invalid credentials, got %v", err)
		}
	}
	// Four failures: logged, no anomaly yet.
	for i := 0; i < 4; i++ {
		fail()
	}
	rows, total, err := f.repo.LoginAttempts(ctx, account.Username, 1, 20)
	if err != nil || total != 4 || len(rows) != 4 {
		t.Fatalf("attempts after 4 failures: total=%d rows=%d err=%v", total, len(rows), err)
	}
	for _, row := range rows {
		if row.Success || row.ClientIP != "203.0.113.9" || row.EmployeeID != nil {
			t.Fatalf("bad attempt row: %+v", row)
		}
	}
	if count, err := f.repo.LoginAnomalyCount(ctx, f.now); err != nil || count != 0 {
		t.Fatalf("anomaly count after 4 failures: %d %v", count, err)
	}
	// Fifth consecutive failure: exactly one anomaly event fires.
	fail()
	if count, err := f.repo.LoginAnomalyCount(ctx, f.now); err != nil || count != 1 {
		t.Fatalf("anomaly count after 5 failures: %d %v", count, err)
	}
	var events []model.EmployeeAccountEvent
	if err := f.db.Where("employee_id = ? AND action = ?", account.ID, "login-anomaly").Find(&events).Error; err != nil || len(events) != 1 {
		t.Fatalf("anomaly events: %d %v", len(events), err)
	}
	if events[0].Details == "" {
		t.Fatal("anomaly event has no details")
	}
	// Sixth and seventh failures do not refire.
	fail()
	fail()
	if count, err := f.repo.LoginAnomalyCount(ctx, f.now); err != nil || count != 1 {
		t.Fatalf("anomaly count after 7 failures: %d %v", count, err)
	}
	// A success resets the run: five more failures fire a second event.
	if _, _, err := f.repo.LoginEmployee(store.WithLoginClientIP(ctx, "203.0.113.9"), account.Username, "LoginAnomalyTest123!", tick()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		fail()
	}
	if count, err := f.repo.LoginAnomalyCount(ctx, f.now); err != nil || count != 2 {
		t.Fatalf("anomaly count after second episode: %d %v", count, err)
	}
	// Attempts list now holds 12 failures + 1 success, newest first.
	rows, total, err = f.repo.LoginAttempts(ctx, account.Username, 1, 20)
	if err != nil || total != 13 || len(rows) != 13 {
		t.Fatalf("attempts total: %d rows=%d err=%v", total, len(rows), err)
	}
	successes := 0
	for _, row := range rows {
		if row.Success {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly 1 successful attempt, got %d", successes)
	}
	// Unknown username failures are logged too (no anomaly event: no account).
	if _, _, err := f.repo.LoginEmployee(ctx, "no-such-user", "whatever", f.now); !errors.Is(err, store.ErrInvalidCredentials) {
		t.Fatalf("unknown user: %v", err)
	}
	rows, total, err = f.repo.LoginAttempts(ctx, "no-such-user", 1, 20)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].Success || rows[0].EmployeeID != nil {
		t.Fatalf("unknown-user attempt: total=%d rows=%+v err=%v", total, rows, err)
	}
	// Anomaly window: events older than 24h do not count.
	if err := f.db.Model(&model.EmployeeAccountEvent{}).Where("action = ?", "login-anomaly").Update("created_at", f.now.Add(-25*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := f.repo.LoginAnomalyCount(ctx, f.now); err != nil || count != 0 {
		t.Fatalf("anomaly count after aging: %d %v", count, err)
	}
}
