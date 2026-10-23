package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func TestHandoffNotificationsMySQLIntegration(t *testing.T) {
	f, _, appointment, input := newFollowupFixture(t)
	tasks := followupTasks(t, f, appointment, input)
	ctx := context.Background()
	nurseID := "nurse-demo-1"
	accounts := []model.EmployeeAccount{
		{ID: "notice-nurse", Username: "notice-nurse", DisplayName: "接手人", PasswordHash: "test", Role: "nurse", StaffID: &nurseID, ClinicIDs: []string{appointment.ClinicID}, Active: true, Version: 1, CreatedAt: f.now, UpdatedAt: f.now},
		{ID: "notice-manager", Username: "notice-manager", DisplayName: "授权主管", PasswordHash: "test", Role: "manager", ClinicIDs: []string{appointment.ClinicID}, Active: true, Version: 1, CreatedAt: f.now, UpdatedAt: f.now},
		{ID: "notice-other", Username: "notice-other", DisplayName: "其他院区主管", PasswordHash: "test", Role: "manager", ClinicIDs: []string{"other-clinic"}, Active: true, Version: 1, CreatedAt: f.now, UpdatedAt: f.now},
		{ID: "notice-disabled", Username: "notice-disabled", DisplayName: "停用主管", PasswordHash: "test", Role: "manager", ClinicIDs: []string{appointment.ClinicID}, Active: false, Version: 1, CreatedAt: f.now, UpdatedAt: f.now},
	}
	if err := f.db.Create(&accounts).Error; err != nil {
		t.Fatal(err)
	}
	assignment := store.FollowupAssignmentInput{Version: tasks[0].Version, AssigneeID: nurseID, Reason: "通知测试"}
	if _, err := f.repo.AssignFollowup(ctx, tasks[0].ID, assignment, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.AssignFollowup(ctx, tasks[0].ID, assignment, f.now); err != nil {
		t.Fatal(err)
	}
	nurse := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "nurse", nurseID, []string{appointment.ClinicID}), "notice-nurse")
	manager := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", []string{appointment.ClinicID}), "notice-manager")
	inbox, err := f.repo.HandoffNotifications(nurse, 1, 20)
	if err != nil || inbox.Total != 1 || inbox.UnreadTotal != 1 || inbox.Data[0].Kind != "assigned" {
		t.Fatalf("assignment inbox=%+v err=%v", inbox, err)
	}
	if err := f.repo.EscalateHandoffs(ctx, f.now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	before, err := f.repo.HandoffNotifications(manager, 1, 20)
	if err != nil || before.Total != 0 {
		t.Fatalf("escalated at boundary: %+v %v", before, err)
	}
	for i := 0; i < 2; i++ {
		if err := f.repo.EscalateHandoffs(ctx, f.now.Add(25*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	escalated, err := f.repo.HandoffNotifications(manager, 1, 20)
	if err != nil || escalated.Total != 1 || escalated.Data[0].Kind != "escalated" {
		t.Fatalf("escalation inbox=%+v err=%v", escalated, err)
	}
	var forbiddenCount int64
	if err := f.db.Model(&model.HandoffNotification{}).Where("employee_id IN ?", []string{"notice-other", "notice-disabled"}).Count(&forbiddenCount).Error; err != nil || forbiddenCount != 0 {
		t.Fatalf("unauthorized recipients=%d err=%v", forbiddenCount, err)
	}
	if err := f.repo.ReadHandoffNotification(manager, inbox.Data[0].ID, f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("read another employee's notice: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := f.repo.ReadHandoffNotification(nurse, inbox.Data[0].ID, f.now); err != nil {
			t.Fatal(err)
		}
	}
	inbox, err = f.repo.HandoffNotifications(nurse, 1, 20)
	if err != nil || inbox.UnreadTotal != 0 {
		t.Fatalf("read inbox=%+v err=%v", inbox, err)
	}
	receipts, err := f.repo.HandoffReceipts(nurse, store.HandoffReceiptFilter{ReceiptID: inbox.Data[0].ReceiptID, Status: "pending", Page: 1, PageSize: 20, Now: f.now})
	if err != nil || receipts.Total != 1 {
		t.Fatalf("reading must not confirm receipt: %+v %v", receipts, err)
	}
	if _, err := f.repo.ConfirmHandoffReceipt(nurse, inbox.Data[0].ReceiptID, receipts.Data[0].Version, f.now.Add(26*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// A new authorized manager must not receive an escalation for an already confirmed receipt.
	account := accounts[1]
	account.ID = "notice-late"
	account.Username = "notice-late"
	if err := f.db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.repo.EscalateHandoffs(ctx, f.now.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	late := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", []string{appointment.ClinicID}), "notice-late")
	after, err := f.repo.HandoffNotifications(late, 1, 20)
	if err != nil || after.Total != 0 {
		t.Fatalf("confirmed receipt escalated: %+v %v", after, err)
	}
}
