package store_test

import (
	"context"
	"errors"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"sync"
	"testing"
)

func TestSupportEnhancementsIntegration(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	patient, other := f.patient(), f.patient()
	manager := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"}), "manager-support")
	create := func(patientID, key string) model.SupportTicket {
		t.Helper()
		row, err := f.repo.CreateSupport(ctx, patientID, store.SupportInput{ClinicID: "clinic-demo", Subject: "咨询", Category: "general", Content: "问题", RequestKey: key}, f.now)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	waiting := create(patient.Patient.ID, "support-waiting-001")
	if waiting.AssigneeID != "" {
		t.Fatal("assigned without eligible agent")
	}
	for _, id := range []string{"support-agent-a", "support-agent-b", "support-agent-off"} {
		row := model.EmployeeAccount{ID: id, Username: id, DisplayName: id, Role: "customer_service", ClinicIDs: []string{"clinic-demo"}, Active: id != "support-agent-off", Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
		if err := f.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	assigned, err := f.repo.ChangeSupport(manager, waiting.ID, "", store.SupportInput{Action: "auto_assign", Content: "自动分派", Version: waiting.Version, RequestKey: "support-auto-00001"}, f.now)
	if err != nil || assigned.AssigneeID != "support-agent-a" {
		t.Fatal("automatic dispatch", assigned, err)
	}
	second := create(patient.Patient.ID, "support-second-001")
	if second.AssigneeID != "support-agent-b" || second.Status != "in_progress" || second.DueAt == nil {
		t.Fatal("least-loaded agent not selected", second)
	}
	agent := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "customer_service", "", []string{"clinic-demo"}), assigned.AssigneeID)
	message := store.SupportInput{Action: "message", Content: "附件", Version: assigned.Version, RequestKey: "support-file-00001", Attachments: []store.SupportUpload{{Name: "说明.pdf", Data: []byte("%PDF-1.4\nexample")}}}
	assigned, err = f.repo.ChangeSupport(agent, assigned.ID, "", message, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.repo.ChangeSupport(agent, assigned.ID, "", message, f.now); err != nil {
		t.Fatal("file replay", err)
	}
	var count int64
	f.db.Model(&model.SupportAttachment{}).Count(&count)
	if count != 1 {
		t.Fatal("duplicate file", count)
	}
	_, events, _, err := f.repo.SupportDetail(ctx, assigned.ID, patient.Patient.ID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	var fileID string
	for _, event := range events {
		if len(event.Attachments) > 0 {
			fileID = event.Attachments[0].ID
		}
	}
	if fileID == "" {
		t.Fatal("missing attachment metadata")
	}
	file, err := f.repo.SupportAttachment(ctx, assigned.ID, fileID, patient.Patient.ID)
	if err != nil || string(file.Data) != "%PDF-1.4\nexample" {
		t.Fatal("file content", err)
	}
	if _, err = f.repo.SupportAttachment(ctx, assigned.ID, fileID, other.Patient.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("foreign patient read attachment", err)
	}
	if _, err = f.repo.SupportAttachment(ctx, second.ID, fileID, patient.Patient.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("file escaped its ticket", err)
	}
	note := store.SupportInput{Action: "note", Content: "内部附件", Version: assigned.Version, RequestKey: "support-note-file01", Attachments: message.Attachments}
	assigned, err = f.repo.ChangeSupport(agent, assigned.ID, "", note, f.now)
	if err != nil {
		t.Fatal(err)
	}
	_, internal, _, _ := f.repo.SupportDetail(manager, assigned.ID, "", 1, 20)
	for _, event := range internal {
		if event.Internal {
			for _, file := range event.Attachments {
				if _, err = f.repo.SupportAttachment(ctx, assigned.ID, file.ID, patient.Patient.ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatal("internal attachment leaked", err)
				}
			}
		}
	}
	rating := store.SupportInput{Action: "rate", Rating: 5, Content: "满意", Version: assigned.Version, RequestKey: "support-rating-001"}
	if _, err = f.repo.ChangeSupport(ctx, assigned.ID, patient.Patient.ID, rating, f.now); !errors.Is(err, store.ErrCareState) {
		t.Fatal("premature rating", err)
	}
	assigned, err = f.repo.ChangeSupport(agent, assigned.ID, "", store.SupportInput{Action: "status", Status: "closed", Content: "结单", Version: assigned.Version, RequestKey: "support-close-001"}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	rating.Version = assigned.Version
	assigned, err = f.repo.ChangeSupport(ctx, assigned.ID, patient.Patient.ID, rating, f.now)
	if err != nil || assigned.Rating != 5 {
		t.Fatal("closed rating", err)
	}
	if _, err = f.repo.ChangeSupport(ctx, assigned.ID, patient.Patient.ID, rating, f.now); err != nil {
		t.Fatal("rating replay", err)
	}
	rating.Version = assigned.Version
	rating.RequestKey = "support-rating-002"
	if _, err = f.repo.ChangeSupport(ctx, assigned.ID, patient.Patient.ID, rating, f.now); !errors.Is(err, store.ErrCareState) {
		t.Fatal("multiple ratings", err)
	}
}

func TestSupportConcurrentAutoAssignment(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	for _, id := range []string{"support-a", "support-b"} {
		if err := f.db.Create(&model.EmployeeAccount{ID: id, Username: id, DisplayName: id, Role: "customer_service", ClinicIDs: []string{"clinic-demo"}, Active: true, Version: 1, CreatedAt: f.now, UpdatedAt: f.now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	patients := []string{f.patient().Patient.ID, f.patient().Patient.ID}
	var wg sync.WaitGroup
	results := make(chan model.SupportTicket, 2)
	errs := make(chan error, 2)
	for _, id := range patients {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			row, err := f.repo.CreateSupport(ctx, id, store.SupportInput{ClinicID: "clinic-demo", Subject: "咨询", Category: "general", Content: "问题", RequestKey: "support-concurrent01"}, f.now)
			results <- row
			errs <- err
		}(id)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assigned := map[string]bool{}
	for row := range results {
		assigned[row.AssigneeID] = true
	}
	if len(assigned) != 2 || assigned[""] {
		t.Fatal("concurrent dispatch did not balance", assigned)
	}
}
