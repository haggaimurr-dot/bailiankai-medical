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

// P2 咨询-患者端：客服回复写站内消息并亮未读角标；患者进工单详情后只清
// 当前工单的回复；内部记录不产生站内消息。
func TestSupportReplyNotifiesPatient(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	patient := f.patient()
	t.Cleanup(func() {
		f.db.Exec("DELETE FROM patient_messages WHERE patient_id = ?", patient.Patient.ID)
		f.db.Exec("DELETE FROM support_events WHERE ticket_id IN (SELECT id FROM support_tickets WHERE patient_id = ?)", patient.Patient.ID)
		f.db.Exec("DELETE FROM support_tickets WHERE patient_id = ?", patient.Patient.ID)
	})
	account := model.EmployeeAccount{ID: "support-reply-agent", Username: "support-reply-agent", DisplayName: "客服测试", Role: "customer_service", ClinicIDs: []string{"clinic-demo"}, Active: true, Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	if err := f.db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	agent := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "customer_service", "", []string{"clinic-demo"}), account.ID)
	mkTicket := func(key, subject string) model.SupportTicket {
		t.Helper()
		row, err := f.repo.CreateSupport(ctx, patient.Patient.ID, store.SupportInput{ClinicID: "clinic-demo", Subject: subject, Category: "booking", Content: "咨询内容", RequestKey: key}, f.now)
		if err != nil {
			t.Fatal(err)
		}
		// CreateSupport auto-assigns to the least-busy CS agent; only claim
		// explicitly when it wasn't already assigned to our test agent.
		// (Assigning an already-assigned ticket is forbidden for non-managers.)
		if row.AssigneeID == "" {
			due := f.now.Add(time.Hour)
			row, err = f.repo.ChangeSupport(agent, row.ID, "", store.SupportInput{Action: "assign", AssigneeID: account.ID, DueAt: &due, Content: "接手", Version: row.Version, RequestKey: key + "-assign"}, f.now)
			if err != nil {
				t.Fatal(err)
			}
		} else if row.AssigneeID != account.ID {
			t.Fatalf("ticket auto-assigned to unexpected agent %q", row.AssigneeID)
		}
		return row
	}
	unread := func() int64 {
		t.Helper()
		n, err := f.repo.UnreadPatientMessageCount(ctx, patient.Patient.ID)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	ticket := mkTicket("support-reply-t1", "咨询一")
	if unread() != 0 {
		t.Fatal("unexpected unread before reply")
	}
	ticket, err := f.repo.ChangeSupport(agent, ticket.ID, "", store.SupportInput{Action: "message", Content: "客服回复一", Version: ticket.Version, RequestKey: "support-reply-t1-msg"}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	msgs, total, err := f.repo.PatientMessages(ctx, patient.Patient.ID, 1, 20)
	if err != nil || total != 1 || len(msgs) != 1 || msgs[0].Kind != "support_reply" || msgs[0].TicketID != ticket.ID || msgs[0].ReadAt != nil {
		t.Fatalf("reply message: msgs=%+v total=%d err=%v", msgs, total, err)
	}
	if unread() != 1 {
		t.Fatal("unread should be 1")
	}
	ticket, err = f.repo.ChangeSupport(agent, ticket.ID, "", store.SupportInput{Action: "note", Content: "内部记录", Version: ticket.Version, RequestKey: "support-reply-t1-note"}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if unread() != 1 {
		t.Fatal("note created a message")
	}
	other := mkTicket("support-reply-t2", "咨询二")
	other, err = f.repo.ChangeSupport(agent, other.ID, "", store.SupportInput{Action: "message", Content: "客服回复二", Version: other.Version, RequestKey: "support-reply-t2-msg"}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if unread() != 2 {
		t.Fatal("unread should be 2")
	}
	if err := f.repo.MarkSupportRepliesRead(ctx, ticket.ID, patient.Patient.ID, f.now); err != nil {
		t.Fatal(err)
	}
	if unread() != 1 {
		t.Fatal("only the opened ticket's replies should clear")
	}
	if err := f.repo.MarkSupportRepliesRead(ctx, other.ID, patient.Patient.ID, f.now); err != nil {
		t.Fatal(err)
	}
	if unread() != 0 {
		t.Fatal("unread should be 0")
	}
}
