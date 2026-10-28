package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func TestNotificationBacklogAlert(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	patient := f.patient()
	if err := f.db.Create(&model.MessageTemplate{ID: "tmpl-backlog-test", ClinicID: "clinic-demo", Kind: "followup", TemplateID: "test-wx-backlog", TitleKey: "thing1", TimeKey: "time2", Page: "pages/followups/index", State: "developer", LeadMinutes: 120, Enabled: true, Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	mk := func(suffix string, sendAt time.Time, status string) {
		t.Helper()
		if err := f.db.Create(&model.Notification{ID: "backlog-" + suffix, ClinicID: "clinic-demo", PatientID: patient.Patient.ID, TemplateID: "tmpl-backlog-test", Kind: "followup", SourceID: "backlog-src-" + suffix, DueAt: f.now.Add(time.Hour), SendAt: sendAt, Status: status, UpdatedAt: f.now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 3 queued rows: oldest send time 40 min ago, one 10 min ago, one in 1h.
	mk("a", f.now.Add(-40*time.Minute), "queued")
	mk("b", f.now.Add(-10*time.Minute), "queued")
	mk("c", f.now.Add(time.Hour), "queued")
	// A sent row and a merged row are not backlog.
	mk("sent", f.now.Add(-2*time.Hour), "sent")
	mk("merged", f.now.Add(-2*time.Hour), "merged")

	// Default thresholds (20 pending / 30 min): count is only 3, but the
	// oldest row waited 40 min, so the age rule breaches.
	bl, err := f.repo.NotificationBacklog(ctx, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if bl.PendingCount != 3 || bl.OldestAgeMinutes == nil || *bl.OldestAgeMinutes != 40 || !bl.Breached {
		t.Fatalf("backlog with defaults: %+v", bl)
	}
	// Raise the age threshold: neither rule breaches any more.
	setting, err := f.repo.NotificationBacklogSetting(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if setting.MaxPending != 20 || setting.MaxOldestMinutes != 30 {
		t.Fatalf("default setting: %+v", setting)
	}
	setting, err = f.repo.UpdateNotificationBacklogSetting(ctx, 20, 1440, setting.Version, "manager-test", f.now)
	if err != nil || setting.MaxOldestMinutes != 1440 {
		t.Fatalf("update setting: %+v %v", setting, err)
	}
	bl, err = f.repo.NotificationBacklog(ctx, f.now)
	if err != nil || bl.Breached {
		t.Fatalf("backlog after raising age threshold: %+v %v", bl, err)
	}
	// Lower the count threshold to 3: the count rule breaches.
	setting, err = f.repo.UpdateNotificationBacklogSetting(ctx, 3, 1440, setting.Version, "manager-test", f.now)
	if err != nil {
		t.Fatal(err)
	}
	bl, err = f.repo.NotificationBacklog(ctx, f.now)
	if err != nil || !bl.Breached {
		t.Fatalf("backlog after lowering count threshold: %+v %v", bl, err)
	}
	// Stale version conflicts; invalid values are rejected.
	if _, err := f.repo.UpdateNotificationBacklogSetting(ctx, 3, 1440, setting.Version-1, "manager-test", f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale version: %v", err)
	}
	if _, err := f.repo.UpdateNotificationBacklogSetting(ctx, 0, 1440, setting.Version, "manager-test", f.now); !errors.Is(err, store.ErrCareState) {
		t.Fatalf("maxPending 0: %v", err)
	}
	// Drain the queue: the alert clears and the oldest age goes null.
	if err := f.db.Model(&model.Notification{}).Where("id IN ?", []string{"backlog-a", "backlog-b", "backlog-c"}).Update("status", "sent").Error; err != nil {
		t.Fatal(err)
	}
	bl, err = f.repo.NotificationBacklog(ctx, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if bl.PendingCount != 0 || bl.OldestAgeMinutes != nil || bl.Breached {
		t.Fatalf("backlog after drain: %+v", bl)
	}
}
