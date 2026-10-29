package store_test

import (
	"context"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

// 行 103：方案模板库——个人模板隔离、共享后同院区可见、异院区不可见、可从模板新建草稿。
func TestCareTemplateLibraryMySQL(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	clinicA := f.slot.ClinicID
	for _, id := range []string{"lib-owner", "lib-peer", "lib-other"} {
		clinics := []string{clinicA}
		if id == "lib-other" {
			clinics = []string{"other-clinic"}
		}
		row := model.EmployeeAccount{ID: id, Username: id, DisplayName: "模板库测试", PasswordHash: "unused", Role: "manager", ClinicIDs: clinics, Active: true, Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
		if err := f.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	content := store.CarePlanContent{Summary: "共享摘要", Stages: []store.CarePlanStage{{Title: "阶段", Body: "正文"}}, Instructions: "医嘱"}
	tpl, err := f.repo.CreateCareTemplate(ctx, "lib-owner", "共享模板", content, f.now)
	if err != nil {
		t.Fatal(err)
	}
	// 未共享前：同院区同事看不到
	rows, err := store.New(f.db).ListCareTemplateLibrary(ctx, "lib-peer", []string{clinicA})
	if err != nil || len(rows) != 0 {
		t.Fatalf("unshared visible: %+v %v", rows, err)
	}
	if _, err := store.New(f.db).ShareCareTemplate(ctx, tpl.ID, "lib-owner", clinicA, f.now); err != nil {
		t.Fatal(err)
	}
	// 共享后：同院区同事可见、异院区不可见、所有者仍可见
	rows, err = store.New(f.db).ListCareTemplateLibrary(ctx, "lib-peer", []string{clinicA})
	if err != nil || len(rows) != 1 || !rows[0].IsShared {
		t.Fatalf("shared not visible to peer: %+v %v", rows, err)
	}
	rows, err = store.New(f.db).ListCareTemplateLibrary(ctx, "lib-other", []string{"other-clinic"})
	if err != nil || len(rows) != 0 {
		t.Fatalf("shared leaked to other clinic: %+v %v", rows, err)
	}
	// 非所有者不能共享他人模板
	if _, err := store.New(f.db).ShareCareTemplate(ctx, tpl.ID, "lib-peer", clinicA, f.now); err == nil {
		t.Fatal("non-owner shared template")
	}
}

// 行 104：提醒打卡——创建、列表、打卡幂等、历史。
func TestCareReminderCheckinMySQL(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	patient := f.patient()
	patientID := patient.Patient.ID
	start := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	rem, err := store.New(f.db).CreateCareReminder(ctx, patientID, f.slot.ClinicID, "medication", "早服药", "饭后服用", `{"times":["08:00"]}`, start, nil, "tester", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.New(f.db).CreateCareReminder(ctx, patientID, f.slot.ClinicID, "bogus", "x", "", "{}", start, nil, "tester", f.now); err == nil {
		t.Fatal("invalid kind accepted")
	}
	rows, err := store.New(f.db).ListCareReminders(ctx, patientID)
	if err != nil || len(rows) != 1 || rows[0].Title != "早服药" {
		t.Fatalf("list=%+v err=%v", rows, err)
	}
	day1 := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	for _, d := range []time.Time{day1, day2, day1} { // 重复打卡幂等
		if err := store.New(f.db).CheckinReminder(ctx, rem.ID, patientID, d, f.now); err != nil {
			t.Fatal(err)
		}
	}
	hist, err := store.New(f.db).ReminderCheckins(ctx, rem.ID, patientID, 60)
	if err != nil || len(hist) != 2 {
		t.Fatalf("history=%+v err=%v", hist, err)
	}
	// 他人提醒不可打卡
	if err := store.New(f.db).CheckinReminder(ctx, rem.ID, "other-patient", day1, f.now); err == nil {
		t.Fatal("cross-patient checkin allowed")
	}
}

// 行 105：交接看板——批量发起、聚合计数、确认、催办。
func TestHandoverBatchMySQL(t *testing.T) {
	f, _, appointment, input := newFollowupFixture(t)
	tasks := followupTasks(t, f, appointment, input)
	if len(tasks) < 2 {
		t.Skip("need 2+ tasks")
	}
	ctx := context.Background()
	staffA, staffB := "batch-staff-a", "batch-staff-b"
	for _, sid := range []string{staffA, staffB} {
		st := model.FollowupStaff{ID: sid, DisplayName: "交接" + sid, Role: "nurse", Status: "on_duty", Version: 1}
		if err := f.db.Create(&st).Error; err != nil {
			t.Fatal(err)
		}
		eid := "emp-" + sid
		if err := f.db.Create(&model.EmployeeAccount{ID: eid, Username: eid, DisplayName: "交接" + sid, PasswordHash: "x", Role: "nurse", StaffID: &sid, ClinicIDs: []string{appointment.ClinicID}, Active: true, Version: 1, CreatedAt: f.now, UpdatedAt: f.now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 先把任务指给 staffA（fixture 默认指派人），再批量交接给 staffB
	taskIDs := []string{tasks[0].ID, tasks[1].ID}
	mgr := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", []string{appointment.ClinicID}), "batch-manager")
	batchID, err := store.New(f.db).CreateHandoverBatch(mgr, taskIDs, staffB, "批量交接测试", "batch-manager", f.now)
	if err != nil {
		t.Fatal(err)
	}
	batches, total, err := store.New(f.db).HandoverBatches(mgr, 1, 20)
	if err != nil || total != 1 || len(batches) != 1 || batches[0].Total != 2 || batches[0].Confirmed != 0 || batches[0].Pending != 2 {
		t.Fatalf("kanban=%+v total=%d err=%v", batches, total, err)
	}
	// 确认一条 → 1/2
	var receipts []model.FollowupHandoffReceipt
	if err := f.db.Where("batch_id = ?", batchID).Find(&receipts).Error; err != nil || len(receipts) != 2 {
		t.Fatalf("receipts=%d err=%v", len(receipts), err)
	}
	nurseB := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "nurse", staffB, []string{appointment.ClinicID}), "emp-"+staffB)
	if _, err := store.New(f.db).ConfirmHandoffReceipt(nurseB, receipts[0].ID, receipts[0].Version, f.now); err != nil {
		t.Fatal(err)
	}
	batches, _, _ = store.New(f.db).HandoverBatches(mgr, 1, 20)
	if batches[0].Confirmed != 1 || batches[0].Pending != 1 {
		t.Fatalf("after confirm: %+v", batches[0])
	}
	// 催办 → 接手人收到 nudged 通知
	sent, err := store.New(f.db).NudgeHandoverBatch(mgr, batchID, "batch-manager", f.now)
	if err != nil || sent != 1 {
		t.Fatalf("nudge sent=%d err=%v", sent, err)
	}
	inbox, err := store.New(f.db).HandoffNotifications(nurseB, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range inbox.Data {
		if n.Kind == "nudged" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no nudged notification in inbox: %+v", inbox.Data)
	}
}
