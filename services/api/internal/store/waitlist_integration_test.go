package store_test

import (
	"context"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

// 新增-P1：改期与候补——候补成功自动通知并限时确认，超时自动顺延。
//
// 语义锚点：offer 本身不占名额（reserved 只计真实预约），独占靠准入拦截
// （直接预约与非持有者 claim 都返回 SLOT_FULL）；原改期 claim 路径对持有
// 者仍可用，确认接口与之等价。
func TestWaitlistOfferConfirmAndTimeout(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	if err := f.db.Exec("UPDATE slots SET capacity = 1 WHERE id = ?", f.slot.ID).Error; err != nil {
		t.Fatal(err)
	}
	a := f.patient()
	b := f.patient()
	c := f.patient()
	d := f.patient()
	e := f.patient()

	booked := f.create(a, "waitlist-test-booked")
	f.assertReserved(1)

	waitInput := store.AppointmentInput{SlotID: f.slot.ID, ContactName: "演示访客", ContactPhone: "13800000000", Waitlist: true}
	// 候补登记时间必须严格递增：同毫秒创建时 created_at 打平，候补排序退化为
	// 随机 id 顺序，offer 会发给非最早登记人，测试变 flaky。
	waitB, _, err := f.repo.CreateAppointment(ctx, b.Patient.ID, "waitlist-test-b", waitInput, f.now.Add(time.Second))
	if err != nil || waitB.Status != "waiting" {
		t.Fatalf("waitlist B failed: status=%s err=%v", waitB.Status, err)
	}
	waitC, _, err := f.repo.CreateAppointment(ctx, c.Patient.ID, "waitlist-test-c", waitInput, f.now.Add(2*time.Second))
	if err != nil || waitC.Status != "waiting" {
		t.Fatalf("waitlist C failed: status=%s err=%v", waitC.Status, err)
	}
	waitE, _, err := f.repo.CreateAppointment(ctx, e.Patient.ID, "waitlist-test-e", waitInput, f.now.Add(3*time.Second))
	if err != nil || waitE.Status != "waiting" {
		t.Fatalf("waitlist E failed: status=%s err=%v", waitE.Status, err)
	}
	f.assertReserved(1)

	// A 取消 → 同一事务内 B 收到限时 offer；名额已释放（reserved 0），但
	// offer 有效期内他人无法直接预约。取消时间晚于候补登记时间。
	if _, err := f.repo.CancelAppointment(ctx, booked.ID, a.Patient.ID, booked.Version, "", "", f.now.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	f.assertReserved(0)
	entries, err := f.repo.WaitlistEntries(ctx, b.Patient.ID, f.now)
	if err != nil || len(entries) != 1 || entries[0].Offer == nil {
		t.Fatalf("B should hold an offer: entries=%d err=%v", len(entries), err)
	}
	if !entries[0].Offer.ExpiresAt.After(f.now) || entries[0].Offer.ExpiresAt.After(f.now.Add(2*time.Hour+time.Minute)) {
		t.Fatalf("offer deadline wrong: %v", entries[0].Offer.ExpiresAt)
	}
	var offerMsg int64
	if err := f.db.Model(&model.PatientMessage{}).Where("patient_id = ? AND kind = ?", b.Patient.ID, "waitlist_offer").Count(&offerMsg).Error; err != nil || offerMsg != 1 {
		t.Fatalf("B should have exactly one offer message: count=%d err=%v", offerMsg, err)
	}

	// 暂留期内：他人直接预约被 offer 拦截；非持有者的原改期 claim 同样被拦截。
	if _, _, err := f.repo.CreateAppointment(ctx, d.Patient.ID, "waitlist-test-d", f.input(), f.now); err != store.ErrSlotFull {
		t.Fatalf("direct booking during hold should fail slot-full, got %v", err)
	}
	if _, err := f.repo.RescheduleAppointment(ctx, waitE.ID, e.Patient.ID, "waitlist-test-e-claim", store.RescheduleInput{SlotID: f.slot.ID, Version: waitE.Version}, f.now); err != store.ErrSlotFull {
		t.Fatalf("non-holder claim during hold should fail slot-full, got %v", err)
	}

	// B 限时内确认 → 转待确认，offer 已接受，名额被占。
	confirmed, err := f.repo.ConfirmWaitlist(ctx, waitB.ID, b.Patient.ID, f.now)
	if err != nil || confirmed.Status != "pending" {
		t.Fatalf("B confirm failed: status=%s err=%v", confirmed.Status, err)
	}
	f.assertReserved(1)
	entries, err = f.repo.WaitlistEntries(ctx, b.Patient.ID, f.now)
	if err != nil || len(entries) != 0 {
		t.Fatalf("B should no longer be waiting: entries=%d err=%v", len(entries), err)
	}

	// B 再取消 → 顺延给 C。
	if _, err := f.repo.CancelAppointment(ctx, confirmed.ID, b.Patient.ID, confirmed.Version, "", "", f.now); err != nil {
		t.Fatal(err)
	}
	f.assertReserved(0)
	entries, err = f.repo.WaitlistEntries(ctx, c.Patient.ID, f.now)
	if err != nil || len(entries) != 1 || entries[0].Offer == nil {
		t.Fatalf("C should hold the forwarded offer: entries=%d err=%v", len(entries), err)
	}

	// C 超时未确认：确认时返回冲突，同一事务内 offer 过期并顺延给 E
	//（C 已有一次 lapse，排在未 lapse 的 E 之后）；名额仍空闲。
	if err := f.db.Exec("UPDATE waitlist_offers SET expires_at = ? WHERE appointment_id = ?", f.now.Add(-time.Hour), waitC.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ConfirmWaitlist(ctx, waitC.ID, c.Patient.ID, f.now); err != store.ErrTransition {
		t.Fatalf("expired confirm should fail transition, got %v", err)
	}
	f.assertReserved(0)
	entries, err = f.repo.WaitlistEntries(ctx, c.Patient.ID, f.now)
	if err != nil || len(entries) != 1 || entries[0].Offer != nil {
		t.Fatalf("C should have no active offer after lapse: entries=%d err=%v", len(entries), err)
	}
	var expiredMsg int64
	if err := f.db.Model(&model.PatientMessage{}).Where("patient_id = ? AND kind = ?", c.Patient.ID, "waitlist_offer_expired").Count(&expiredMsg).Error; err != nil || expiredMsg != 1 {
		t.Fatalf("C should have exactly one lapse message: count=%d err=%v", expiredMsg, err)
	}
	entries, err = f.repo.WaitlistEntries(ctx, e.Patient.ID, f.now)
	if err != nil || len(entries) != 1 || entries[0].Offer == nil {
		t.Fatalf("E should hold the forwarded offer: entries=%d err=%v", len(entries), err)
	}

	// E 确认后取消 → 只剩 C（已有一次 lapse），第二轮重新 offer 给 C，名额不闲置。
	confirmedE, err := f.repo.ConfirmWaitlist(ctx, waitE.ID, e.Patient.ID, f.now)
	if err != nil || confirmedE.Status != "pending" {
		t.Fatalf("E confirm failed: status=%s err=%v", confirmedE.Status, err)
	}
	if _, err := f.repo.CancelAppointment(ctx, confirmedE.ID, e.Patient.ID, confirmedE.Version, "", "", f.now); err != nil {
		t.Fatal(err)
	}
	f.assertReserved(0)
	entries, err = f.repo.WaitlistEntries(ctx, c.Patient.ID, f.now)
	if err != nil || len(entries) != 1 || entries[0].Offer == nil {
		t.Fatalf("C should hold the round-2 offer: entries=%d err=%v", len(entries), err)
	}

	// 持有者走原改期 claim 路径同样可用，且 offer 被接受。
	claimed, err := f.repo.RescheduleAppointment(ctx, waitC.ID, c.Patient.ID, "waitlist-test-c-claim", store.RescheduleInput{SlotID: f.slot.ID, Version: waitC.Version}, f.now)
	if err != nil || claimed.Status != "pending" {
		t.Fatalf("holder reschedule-claim failed: status=%s err=%v", claimed.Status, err)
	}
	f.assertReserved(1)
	var accepted int64
	if err := f.db.Model(&model.WaitlistOffer{}).Where("appointment_id = ? AND status = ?", waitC.ID, "accepted").Count(&accepted).Error; err != nil || accepted != 1 {
		t.Fatalf("C offer should be accepted after claim: count=%d err=%v", accepted, err)
	}
}

func TestWaitlistSweep(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	if err := f.db.Exec("UPDATE slots SET capacity = 1 WHERE id = ?", f.slot.ID).Error; err != nil {
		t.Fatal(err)
	}
	a := f.patient()
	b := f.patient()
	booked := f.create(a, "waitlist-sweep-booked")
	waitInput := store.AppointmentInput{SlotID: f.slot.ID, ContactName: "演示访客", ContactPhone: "13800000000", Waitlist: true}
	if _, _, err := f.repo.CreateAppointment(ctx, b.Patient.ID, "waitlist-sweep-b", waitInput, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.CancelAppointment(ctx, booked.ID, a.Patient.ID, booked.Version, "", "", f.now); err != nil {
		t.Fatal(err)
	}
	f.assertReserved(0)
	if err := f.db.Exec("UPDATE waitlist_offers SET expires_at = ? WHERE status = 'offered'", f.now.Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	result, err := f.repo.SweepWaitlist(ctx, f.now)
	if err != nil || result.Expired != 1 || result.Offered != 1 {
		t.Fatalf("sweep should expire 1 and re-offer 1: %+v err=%v", result, err)
	}
	f.assertReserved(0)
	result, err = f.repo.SweepWaitlist(ctx, f.now)
	if err != nil || result.Expired != 0 || result.Offered != 0 {
		t.Fatalf("second sweep should be a no-op: %+v err=%v", result, err)
	}
	f.assertReserved(0)
}
