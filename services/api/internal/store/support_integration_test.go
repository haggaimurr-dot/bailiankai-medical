package store_test

import (
	"context"
	"errors"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"sync"
	"testing"
	"time"
)

func TestSupportIntegration(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	patient, other := f.patient(), f.patient()
	t.Cleanup(func() { f.db.Exec("DELETE FROM support_events"); f.db.Exec("DELETE FROM support_tickets") })
	in := store.SupportInput{ClinicID: "clinic-demo", Subject: "预约与服务咨询", Category: "booking", Content: "希望了解面诊预约", RequestKey: "support-create-0001"}
	var wg sync.WaitGroup
	results := make(chan model.SupportTicket, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			row, err := f.repo.CreateSupport(ctx, patient.Patient.ID, in, f.now)
			results <- row
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var ticket model.SupportTicket
	for row := range results {
		if ticket.ID != "" && row.ID != ticket.ID {
			t.Fatal("duplicate ticket")
		}
		ticket = row
	}
	if _, _, _, err := f.repo.SupportDetail(ctx, ticket.ID, other.Patient.ID, 1, 20); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("patient isolation", err)
	}
	in.Content = "different"
	if _, err := f.repo.CreateSupport(ctx, patient.Patient.ID, in, f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatal("key reuse", err)
	}
	account := model.EmployeeAccount{ID: "support-test-agent", Username: "support-test-agent", DisplayName: "客服测试", Role: "customer_service", ClinicIDs: []string{"clinic-demo"}, Active: true, Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	if err := f.db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	agent := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "customer_service", "", []string{"clinic-demo"}), account.ID)
	outsider := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "customer_service", "", []string{"clinic-demo"}), "other-agent")
	due := f.now.Add(time.Hour)
	change := store.SupportInput{Action: "assign", AssigneeID: account.ID, DueAt: &due, Content: "接手", Version: ticket.Version, RequestKey: "support-assign-0001"}
	ticket, err := f.repo.ChangeSupport(agent, ticket.ID, "", change, f.now)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.repo.ChangeSupport(agent, ticket.ID, "", change, f.now)
	if err != nil || replay.Version != ticket.Version {
		t.Fatal("assignment replay", err)
	}
	if _, err := f.repo.ChangeSupport(agent, ticket.ID, "", change, f.now.Add(2*time.Hour)); err != nil {
		t.Fatal("committed assignment retry after deadline", err)
	}
	badDeadline := change
	badDeadline.RequestKey = "support-past-due-001"
	badDeadline.Version = ticket.Version
	if _, err := f.repo.ChangeSupport(agent, ticket.ID, "", badDeadline, f.now.Add(2*time.Hour)); !errors.Is(err, store.ErrCareState) {
		t.Fatal("new expired deadline accepted", err)
	}
	wrongClinic := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-other"}), "manager-other")
	if _, _, _, err := f.repo.SupportDetail(wrongClinic, ticket.ID, "", 1, 20); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("cross clinic access", err)
	}
	if _, _, _, err := f.repo.SupportDetail(outsider, ticket.ID, "", 1, 20); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("agent isolation", err)
	}
	change = store.SupportInput{Action: "note", Content: "内部服务记录", Version: ticket.Version, RequestKey: "support-note-00001"}
	ticket, err = f.repo.ChangeSupport(agent, ticket.ID, "", change, f.now)
	if err != nil {
		t.Fatal(err)
	}
	_, events, total, err := f.repo.SupportDetail(ctx, ticket.ID, patient.Patient.ID, 1, 20)
	if err != nil || total != 1 {
		t.Fatal("internal event leaked", events, total, err)
	}
	if _, err := f.repo.ChangeSupport(agent, ticket.ID, "", store.SupportInput{Action: "message", Content: "过期回复", Version: 1, RequestKey: "support-stale-0001"}, f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatal("stale version", err)
	}
	change = store.SupportInput{Action: "message", Content: "客服回复", Version: ticket.Version, RequestKey: "support-reply-0001"}
	ticket, err = f.repo.ChangeSupport(agent, ticket.ID, "", change, f.now)
	if err != nil || ticket.Status != "waiting_patient" {
		t.Fatal("reply", err)
	}
	change = store.SupportInput{Action: "status", Status: "resolved", Content: "服务完成", Version: ticket.Version, RequestKey: "support-resolve-01"}
	ticket, err = f.repo.ChangeSupport(agent, ticket.ID, "", change, f.now)
	if err != nil {
		t.Fatal(err)
	}
	change = store.SupportInput{Action: "message", Content: "还有问题", Version: ticket.Version, RequestKey: "support-patient-01"}
	ticket, err = f.repo.ChangeSupport(ctx, ticket.ID, patient.Patient.ID, change, f.now)
	if err != nil || ticket.Status != "in_progress" {
		t.Fatal("patient reopen", err)
	}
	change = store.SupportInput{Action: "status", Status: "closed", Content: "按客户要求关闭", Version: ticket.Version, RequestKey: "support-close-001"}
	ticket, err = f.repo.ChangeSupport(agent, ticket.ID, "", change, f.now)
	if err != nil {
		t.Fatal(err)
	}
	change = store.SupportInput{Action: "message", Content: "关闭后消息", Version: ticket.Version, RequestKey: "support-closed-01"}
	if _, err := f.repo.ChangeSupport(ctx, ticket.ID, patient.Patient.ID, change, f.now); !errors.Is(err, store.ErrCareState) {
		t.Fatal("closed accepted message", err)
	}
}
