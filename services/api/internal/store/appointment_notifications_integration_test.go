package store_test

import (
	"context"
	"time"
	"testing"

	"example.com/bailiankai/api/internal/store"
)

// 新增-P1：超时未确认预约的自动升级提醒。确认截止在 30 分钟内的待确认
// 预约给授权院区的主管与客服发临期提醒；经真实过期路径超时的预约只给
// 主管发升级提醒；重复扫表不重复发；列表按员工过滤。
func TestPendingAppointmentReminders(t *testing.T) {
	f := newBookingFixture(t)
	clinic := f.slot.ClinicID
	insertEmployee := func(id, username, role, clinics string) {
		if err := f.db.Exec(`INSERT INTO employee_accounts (id, username, display_name, password_hash, role, active, version, created_at, updated_at, clinic_ids)
		 VALUES (?, ?, ?, 'x', ?, TRUE, 1, ?, ?, ?)`, id, username, username, role, f.now, f.now, clinics).Error; err != nil {
			t.Fatal(err)
		}
	}
	insertEmployee("appt-test-manager", "appt-manager", "manager", `["`+clinic+`"]`)
	insertEmployee("appt-test-cs", "appt-cs", "customer_service", `["`+clinic+`"]`)
	insertEmployee("appt-test-cs-other", "appt-cs-other", "customer_service", `["clinic-other"]`)
	mgrCtx := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "manager", "", []string{clinic}), "appt-test-manager")

	soon := f.create(f.patient(), "appt-soon")
	if err := f.db.Exec("UPDATE appointments SET confirmation_deadline = ? WHERE id = ?", f.now.Add(20*time.Minute), soon.ID).Error; err != nil {
		t.Fatal(err)
	}
	if soonN, escN, err := f.repo.SweepPendingAppointmentReminders(context.Background(), f.now); err != nil || soonN != 2 || escN != 0 {
		t.Fatalf("soon sweep: soon=%d escalated=%d err=%v", soonN, escN, err)
	}
	if soonN, escN, err := f.repo.SweepPendingAppointmentReminders(context.Background(), f.now); err != nil || soonN != 0 || escN != 0 {
		t.Fatalf("resweep must be no-op: soon=%d escalated=%d err=%v", soonN, escN, err)
	}
	var counts []struct {
		EmployeeID string
		Kind       string
		Count      int64
	}
	if err := f.db.Table("appointment_notifications").Select("employee_id, kind, COUNT(*) AS count").Group("employee_id, kind").Find(&counts).Error; err != nil {
		t.Fatal(err)
	}
	if len(counts) != 2 {
		t.Fatalf("soon notices = %v, want manager + cs only", counts)
	}

	expired := f.create(f.patient(), "appt-expire")
	if err := f.db.Exec("UPDATE appointments SET confirmation_deadline = ? WHERE id = ?", f.now.Add(-time.Minute), expired.ID).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := f.repo.ExpirePending(context.Background(), f.now); err != nil || n != 1 {
		t.Fatalf("expire: n=%d err=%v", n, err)
	}
	if soonN, escN, err := f.repo.SweepPendingAppointmentReminders(context.Background(), f.now); err != nil || soonN != 0 || escN != 1 {
		t.Fatalf("escalated sweep: soon=%d escalated=%d err=%v", soonN, escN, err)
	}
	var escCount int64
	if err := f.db.Table("appointment_notifications").Where("appointment_id = ? AND kind = 'pending_escalated'", expired.ID).Count(&escCount).Error; err != nil || escCount != 1 {
		t.Fatalf("escalated notices = %d err=%v", escCount, err)
	}

	page, err := f.repo.AppointmentNotifications(mgrCtx, 1, 20)
	if err != nil || page.Total != 2 || page.UnreadTotal != 2 {
		t.Fatalf("manager list: total=%d unread=%d err=%v", page.Total, page.UnreadTotal, err)
	}
	soonID := ""
	for _, n := range page.Data {
		if n.Kind == "pending_soon" {
			soonID = n.ID
		}
		if n.DoctorName == "" || n.ClinicName == "" || n.ContactName == "" {
			t.Fatalf("notice display fields missing: %+v", n)
		}
	}
	if soonID == "" {
		t.Fatal("manager list missing pending_soon notice")
	}
	if err := f.repo.ReadAppointmentNotification(mgrCtx, soonID, f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.ReadAppointmentNotification(mgrCtx, soonID, f.now); err != nil {
		t.Fatalf("re-read must be idempotent: %v", err)
	}
	page, err = f.repo.AppointmentNotifications(mgrCtx, 1, 20)
	if err != nil || page.UnreadTotal != 1 {
		t.Fatalf("after read: unread=%d err=%v", page.UnreadTotal, err)
	}
	otherCtx := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "customer_service", "", []string{"clinic-other"}), "appt-test-cs-other")
	otherPage, err := f.repo.AppointmentNotifications(otherCtx, 1, 20)
	if err != nil || otherPage.Total != 0 {
		t.Fatalf("other-clinic list: total=%d err=%v", otherPage.Total, err)
	}
}
