package store_test

import (
	"context"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/database"
)

// 新增-P1：取消原因收集。带原因取消与不带原因取消都成功；原因、补充说明与
// 取消时间同事务落库；统计按原因分组，未填原因归入 unspecified；院区与日期
// 筛选正确过滤。
func TestCancelReasonAndStats(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	if err := f.db.Exec("UPDATE slots SET capacity = 10 WHERE id = ?", f.slot.ID).Error; err != nil {
		t.Fatal(err)
	}
	a := f.patient()
	b := f.patient()
	c := f.patient()
	ra := f.create(a, "cancel-reason-a")
	rb := f.create(b, "cancel-reason-b")
	rc := f.create(c, "cancel-reason-c")

	cancelled, err := f.repo.CancelAppointment(ctx, ra.ID, a.Patient.ID, ra.Version, "schedule_conflict", "临时有事", f.now)
	if err != nil || cancelled.Status != "cancelled" || cancelled.CancelReason != "schedule_conflict" || cancelled.CancelReasonDetail != "临时有事" || cancelled.CancelledAt == nil {
		t.Fatalf("cancel with reason failed: %+v err=%v", cancelled, err)
	}
	cancelledB, err := f.repo.CancelAppointment(ctx, rb.ID, b.Patient.ID, rb.Version, "", "", f.now)
	if err != nil || cancelledB.Status != "cancelled" || cancelledB.CancelReason != "" || cancelledB.CancelledAt == nil {
		t.Fatalf("cancel without reason failed: %+v err=%v", cancelledB, err)
	}
	if _, err := f.repo.CancelAppointment(ctx, rc.ID, c.Patient.ID, rc.Version, "price", "", f.now); err != nil {
		t.Fatal(err)
	}

	day := time.Date(f.now.Year(), f.now.Month(), f.now.Day(), 0, 0, 0, 0, database.Shanghai)
	stats, err := f.repo.CancellationStats(ctx, day, day.AddDate(0, 0, 1), "", "")
	if err != nil || stats.Total != 3 {
		t.Fatalf("stats total wrong: %+v err=%v", stats, err)
	}
	counts := map[string]int64{}
	for _, r := range stats.ByReason {
		counts[r.Reason] = r.Count
	}
	if counts["schedule_conflict"] != 1 || counts["price"] != 1 || counts["unspecified"] != 1 {
		t.Fatalf("stats breakdown wrong: %v", counts)
	}
	scoped, err := f.repo.CancellationStats(ctx, day, day.AddDate(0, 0, 1), f.slot.ClinicID, "")
	if err != nil || scoped.Total != 3 {
		t.Fatalf("clinic-scoped stats wrong: %+v err=%v", scoped, err)
	}
	other, err := f.repo.CancellationStats(ctx, day, day.AddDate(0, 0, 1), "clinic-nonexistent", "")
	if err != nil || other.Total != 0 {
		t.Fatalf("other-clinic stats wrong: %+v err=%v", other, err)
	}
	empty, err := f.repo.CancellationStats(ctx, day.AddDate(0, 0, 30), day.AddDate(0, 0, 31), "", "")
	if err != nil || empty.Total != 0 {
		t.Fatalf("out-of-range stats wrong: %+v err=%v", empty, err)
	}
}
