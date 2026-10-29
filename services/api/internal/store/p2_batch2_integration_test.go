package store_test

import (
	"context"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/store"
)

// 行 107：恢复阶段计划——绑定后 6 个里程碑生成，到期自动生成小结并推送患者站内信。
func TestRecoveryStagePlanMySQL(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	repo := store.New(f.db)
	patientID := f.patient().Patient.ID
	clinicID := f.slot.ClinicID
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, database.Shanghai)

	plan, err := repo.CreateRecoveryStagePlan(ctx, patientID, "appt-1", clinicID, start, "manager-1", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Milestones) != 6 {
		t.Fatalf("expected 6 milestones, got %d", len(plan.Milestones))
	}
	if plan.Milestones[0].DayOffset != 7 || plan.Milestones[5].DayOffset != 42 {
		t.Fatalf("unexpected offsets: %+v", plan.Milestones)
	}

	// 到期前检查：不应推送
	if n, err := repo.CheckRecoveryStageMilestones(ctx, start.AddDate(0, 0, 6)); err != nil || n != 0 {
		t.Fatalf("check before due: n=%d err=%v", n, err)
	}
	// 第 7 天到期：应推送 1 条
	checkAt := time.Date(2026, 10, 8, 10, 0, 0, 0, database.Shanghai)
	n, err := repo.CheckRecoveryStageMilestones(ctx, checkAt)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 notified, got %d", n)
	}
	// 幂等：再次检查不应重复推送
	if n, err := repo.CheckRecoveryStageMilestones(ctx, checkAt); err != nil || n != 0 {
		t.Fatalf("repeat check: n=%d err=%v", n, err)
	}
	// 患者站内信应有一条 recovery_stage
	msgs, _, err := repo.PatientMessages(ctx, patientID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range msgs {
		if m.Kind == "recovery_stage" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a recovery_stage patient message")
	}
	// 患者端可查看计划
	plans, err := repo.RecoveryStagePlans(ctx, patientID)
	if err != nil || len(plans) != 1 {
		t.Fatalf("plans: %v err=%v", len(plans), err)
	}
	notified := 0
	for _, m := range plans[0].Milestones {
		if m.Notified {
			notified++
		}
	}
	if notified != 1 {
		t.Fatalf("expected 1 notified milestone, got %d", notified)
	}

	// 输入校验
	if _, err := repo.CreateRecoveryStagePlan(ctx, "", "appt-1", clinicID, start, "manager-1", f.now); err == nil {
		t.Fatal("expected error for empty patient")
	}

	// 清理（FK 级联会自动清里程碑）
	if err := repo.DeleteRecoveryStagePlan(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
}

// 行 108：FAQ——后台录入/编辑/删除，患者端仅见启用项，关键词搜索。
func TestFaqMySQL(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	repo := store.New(f.db)
	clinicID := f.slot.ClinicID

	mk := func(q, a, cat string, enabled bool) store.Faq {
		faq, err := repo.CreateFaq(ctx, store.FaqInput{
			ClinicID: clinicID, Category: cat, Question: q, Answer: a, Enabled: enabled,
		}, "manager-1", f.now)
		if err != nil {
			t.Fatal(err)
		}
		return faq
	}
	f1 := mk("复诊需要带什么？", "带上次的检查报告。", "复诊", true)
	f2 := mk("可以刷医保吗？", "支持医保结算。", "费用", false)
	defer func() {
		f.db.Exec("DELETE FROM faqs WHERE id IN ?", []string{f1.ID, f2.ID})
	}()

	// 后台可见全部
	all, err := repo.AdminFaqs(ctx, clinicID, "", "")
	if err != nil || len(all) != 2 {
		t.Fatalf("admin faqs: %d err=%v", len(all), err)
	}
	// 患者端仅见启用
	pub, err := repo.PatientFaqs(ctx, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range pub {
		if !f.Enabled {
			t.Fatal("patient saw a disabled faq")
		}
	}
	if len(pub) != 1 || pub[0].ID != f1.ID {
		t.Fatalf("patient faqs: %+v", pub)
	}
	// 关键词搜索
	hit, err := repo.PatientFaqs(ctx, nil, "复诊")
	if err != nil || len(hit) != 1 || hit[0].ID != f1.ID {
		t.Fatalf("keyword search: %+v err=%v", hit, err)
	}
	// 编辑
	updated, err := repo.UpdateFaq(ctx, f1.ID, store.FaqInput{
		Category: "复诊", Question: "复诊需要带什么？", Answer: "带报告和身份证。", SortOrder: 1, Enabled: true,
	}, f.now)
	if err != nil || updated.Answer != "带报告和身份证。" {
		t.Fatalf("update: %+v err=%v", updated, err)
	}
	// 删除
	if err := repo.DeleteFaq(ctx, f2.ID); err != nil {
		t.Fatal(err)
	}
	rest, _ := repo.AdminFaqs(ctx, clinicID, "", "")
	if len(rest) != 1 {
		t.Fatalf("after delete: %d", len(rest))
	}
	// 输入校验
	if _, err := repo.CreateFaq(ctx, store.FaqInput{Question: "", Answer: "x"}, "m", f.now); err == nil {
		t.Fatal("expected error for empty question")
	}
}
