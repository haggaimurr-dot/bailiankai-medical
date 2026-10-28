package store_test

import (
	"context"
	"errors"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"fmt"
	"testing"
	"time"
)

// fakeReminderSender records WeChat sends without touching WeChat.
type fakeReminderSender struct {
	reminders []string
	digests   []int
}

func (f *fakeReminderSender) SendReminder(ctx context.Context, openID string, t model.MessageTemplate, due time.Time) (string, string) {
	f.reminders = append(f.reminders, t.Kind)
	return "0", ""
}

func (f *fakeReminderSender) SendDigest(ctx context.Context, openID string, t model.MessageTemplate, due time.Time, count int) (string, string) {
	f.digests = append(f.digests, count)
	return "0", ""
}

func TestNotificationMergeDigestAndUrgent(t *testing.T) {
	fixture := newBookingFixture(t)
	ctx := context.Background()
	patient := fixture.patient()
	appointment := fixture.create(patient, "merge-appt")
	// WeChat identity + an accepted subscription consent for the template.
	if err := fixture.db.Model(&model.Patient{}).Where("id = ?", patient.Patient.ID).Updates(map[string]any{"wechat_app_id": "wx-test", "wechat_open_id": "test-openid"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Create(&model.MessageTemplate{ID: "tmpl-merge-test", ClinicID: "clinic-demo", Kind: "followup", TemplateID: "test-wx-followup", TitleKey: "thing1", TimeKey: "time2", Page: "pages/followups/index", State: "developer", LeadMinutes: 120, Enabled: true, Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Create(&model.MessageConsent{PatientID: patient.Patient.ID, TemplateID: "test-wx-followup", Status: "accepted", UpdatedAt: fixture.now}).Error; err != nil {
		t.Fatal(err)
	}
	// 3 ordinary reminders: due in 60/80/100 min, so their send times
	// (due - 120 min lead) fall within one 60-minute window.
	// 1 urgent reminder: already overdue at scheduling time.
	mkTask := func(suffix string, due time.Time) {
		if err := fixture.db.Create(&model.FollowupTask{ID: "merge-task-" + suffix, AppointmentID: appointment.ID, PatientID: patient.Patient.ID, AssigneeID: "doctor-demo-1", AssignedAt: fixture.now, Title: "合并测试" + suffix, DueAt: due, Status: "pending", Version: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	mkTask("a", fixture.now.Add(60*time.Minute))
	mkTask("b", fixture.now.Add(80*time.Minute))
	mkTask("c", fixture.now.Add(100*time.Minute))
	mkTask("urgent", fixture.now.Add(-30*time.Minute))

	if err := fixture.repo.ScheduleReminders(ctx, fixture.now); err != nil {
		t.Fatal(err)
	}
	var rows []model.Notification
	if err := fixture.db.Where("patient_id = ?", patient.Patient.ID).Order("send_at, id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("expected 4 notification rows, got %d: %+v", len(rows), rows)
	}
	var queued, merged, urgent int
	var digestID string
	for _, row := range rows {
		switch {
		case row.Urgent:
			urgent++
			if row.Status != "queued" || !row.SendAt.Equal(fixture.now) {
				t.Fatalf("urgent row must stay queued for immediate send: %+v", row)
			}
		case row.Status == "merged":
			merged++
			if row.MergedInto == nil {
				t.Fatalf("merged row without digest pointer: %+v", row)
			}
			digestID = *row.MergedInto
		case row.Status == "queued":
			queued++
			digestID = row.ID
		default:
			t.Fatalf("unexpected row: %+v", row)
		}
	}
	if queued != 1 || merged != 2 || urgent != 1 {
		t.Fatalf("want 1 digest + 2 merged + 1 urgent, got queued=%d merged=%d urgent=%d", queued, merged, urgent)
	}
	var digest model.Notification
	if err := fixture.db.First(&digest, "id = ?", digestID).Error; err != nil {
		t.Fatal(err)
	}
	if digest.Urgent || digest.Status != "queued" {
		t.Fatalf("digest row wrong: %+v", digest)
	}

	// Dispatch the digest: one digest send for 3 reminders, all marked sent.
	sender := &fakeReminderSender{}
	if err := fixture.repo.DispatchReminder(ctx, digest.ID, sender, fixture.now); err != nil {
		t.Fatal(err)
	}
	if len(sender.digests) != 1 || sender.digests[0] != 3 {
		t.Fatalf("want 1 digest send for 3, got %+v", sender.digests)
	}
	if len(sender.reminders) != 0 {
		t.Fatalf("digest must not send singles: %+v", sender.reminders)
	}
	var sent int64
	if err := fixture.db.Model(&model.Notification{}).Where("patient_id = ? AND status = ?", patient.Patient.ID, "sent").Count(&sent).Error; err != nil {
		t.Fatal(err)
	}
	if sent != 3 {
		t.Fatalf("want digest + 2 children sent, got %d", sent)
	}
	var attempts int64
	if err := fixture.db.Model(&model.NotificationAttempt{}).Where("notification_id IN (SELECT id FROM notifications WHERE patient_id = ?)", patient.Patient.ID).Count(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("want 3 attempt records, got %d", attempts)
	}

	// The digest consumed the one consent; re-accept, then the urgent
	// reminder goes out alone and immediately.
	if err := fixture.db.Model(&model.MessageConsent{}).Where("patient_id = ? AND template_id = ?", patient.Patient.ID, "test-wx-followup").Update("status", "accepted").Error; err != nil {
		t.Fatal(err)
	}
	var urgentRow model.Notification
	if err := fixture.db.Where("patient_id = ? AND urgent = ?", patient.Patient.ID, true).First(&urgentRow).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.repo.DispatchReminder(ctx, urgentRow.ID, sender, fixture.now); err != nil {
		t.Fatal(err)
	}
	if len(sender.reminders) != 1 || len(sender.digests) != 1 {
		t.Fatalf("urgent must send alone: reminders=%v digests=%v", sender.reminders, sender.digests)
	}
}

func TestNotificationMergeDisabledSendsSeparately(t *testing.T) {
	fixture := newBookingFixture(t)
	ctx := context.Background()
	patient := fixture.patient()
	appointment := fixture.create(patient, "merge-off-appt")
	if err := fixture.db.Create(&model.MessageTemplate{ID: "tmpl-merge-off", ClinicID: "clinic-demo", Kind: "followup", TemplateID: "test-wx-followup-off", TitleKey: "thing1", TimeKey: "time2", Page: "pages/followups/index", State: "developer", LeadMinutes: 120, Enabled: true, Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	setting, err := fixture.repo.NotificationMergeSetting(ctx)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := fixture.repo.UpdateNotificationMergeSetting(ctx, setting.MergeWindowMinutes, false, setting.Version, "manager-test", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := fixture.repo.UpdateNotificationMergeSetting(ctx, 60, true, updated.Version, "manager-test", fixture.now); err != nil {
			t.Errorf("restore merge setting: %v", err)
		}
	})
	for i, due := range []time.Duration{60 * time.Minute, 80 * time.Minute} {
		if err := fixture.db.Create(&model.FollowupTask{ID: fmt.Sprintf("merge-off-task-%d", i), AppointmentID: appointment.ID, PatientID: patient.Patient.ID, AssigneeID: "doctor-demo-1", AssignedAt: fixture.now, Title: "合并关闭测试", DueAt: fixture.now.Add(due), Status: "pending", Version: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.repo.ScheduleReminders(ctx, fixture.now); err != nil {
		t.Fatal(err)
	}
	var merged int64
	if err := fixture.db.Model(&model.Notification{}).Where("patient_id = ? AND status = ?", patient.Patient.ID, "merged").Count(&merged).Error; err != nil {
		t.Fatal(err)
	}
	if merged != 0 {
		t.Fatalf("merge disabled must not fold rows, got %d merged", merged)
	}
	var queued int64
	if err := fixture.db.Model(&model.Notification{}).Where("patient_id = ? AND status = ?", patient.Patient.ID, "queued").Count(&queued).Error; err != nil {
		t.Fatal(err)
	}
	if queued != 2 {
		t.Fatalf("want 2 separate queued rows, got %d", queued)
	}
	if _, err := fixture.repo.UpdateNotificationMergeSetting(ctx, 60, true, updated.Version+1, "manager-test", fixture.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale version must conflict: %v", err)
	}
}
