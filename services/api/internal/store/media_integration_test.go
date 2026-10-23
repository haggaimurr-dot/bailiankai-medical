package store_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func TestMediaMySQLRecoveryPreservesRevokedConsent(t *testing.T) {
	ctx := context.Background()
	f, patient, appointment, treatment := newFollowupFixture(t)
	root := t.TempDir()
	primary, err := store.NewPrivateMedia(root)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := store.NewPrivateMedia(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.repo.SetPhotoConsent(ctx, appointment.ID, patient.Patient.ID, true, f.now); err != nil {
		t.Fatal(err)
	}
	data := []byte("synthetic media recovery fixture")
	hash := sha256.Sum256(data)
	care := model.CarePhoto{ID: "photo-care-recovery", AppointmentID: appointment.ID, PatientID: patient.Patient.ID, Angle: "front", MediaType: "image/jpeg", SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(hash[:]), UploadedAt: f.now}
	if err := primary.Save(ctx, care.ID, data); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.SaveCarePhoto(ctx, care, f.now); err != nil {
		t.Fatal(err)
	}
	tasks := followupTasks(t, f, appointment, treatment)
	photo := model.FollowupPhoto{ID: "photo-followup-recovery", Angle: "front", SizeBytes: care.SizeBytes, SHA256: care.SHA256, UploadedAt: f.now}
	if err := primary.Save(ctx, photo.ID, data); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.repo.SaveFollowupPhoto(ctx, tasks[0].ID, patient.Patient.ID, photo, f.now); err != nil {
		t.Fatal(err)
	}
	refs, err := f.repo.MediaReferences(ctx)
	if err != nil || len(refs) != 2 {
		t.Fatalf("catalog %v %v", refs, err)
	}
	for _, ref := range refs {
		if exists, err := f.repo.MediaReferenced(ctx, ref.ID); err != nil || !exists {
			t.Fatalf("reference %s %v %v", ref.ID, exists, err)
		}
	}
	snapshot, key := store.NewMediaSnapshotID(), make([]byte, 32)
	if count, err := store.BackupMedia(ctx, primary, backup, f.repo, snapshot, key); err != nil || count != 2 {
		t.Fatalf("backup %d %v", count, err)
	}
	if err := f.repo.SetPhotoConsent(ctx, appointment.ID, patient.Patient.ID, false, f.now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if err := primary.Delete(ctx, ref.ID); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := store.RestoreMedia(ctx, backup, primary, f.repo, snapshot, key, true); err != nil || count != 2 {
		t.Fatalf("restore %d %v", count, err)
	}
	if _, err := f.repo.CarePhotoByID(ctx, care.ID, "", true); !errors.Is(err, store.ErrPhotoConsent) {
		t.Fatal("recovery re-enabled clinician care access", err)
	}
	if _, err := f.repo.FollowupPhoto(ctx, photo.ID, "", true); !errors.Is(err, store.ErrPhotoConsent) {
		t.Fatal("recovery re-enabled clinician followup access", err)
	}
	if _, err := f.repo.CarePhotoByID(ctx, care.ID, patient.Patient.ID, false); err != nil {
		t.Fatal("patient cannot read own recovered photograph", err)
	}
	if _, err := f.repo.FollowupPhoto(ctx, photo.ID, patient.Patient.ID, false); err != nil {
		t.Fatal("patient cannot read own recovered followup photograph", err)
	}
	if err := primary.Save(ctx, "photo-orphan-recovery", data); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	for _, id := range []string{care.ID, photo.ID, "photo-orphan-recovery"} {
		if err := os.Chtimes(filepath.Join(root, id+".jpg"), old, old); err != nil {
			t.Fatal(err)
		}
	}
	result, err := store.CleanupMedia(ctx, primary, f.repo, time.Now(), 24*time.Hour, true)
	if err != nil || result.Deleted != 1 {
		t.Fatalf("cleanup %v %v", result, err)
	}
	for _, ref := range refs {
		if _, err := primary.Read(ctx, ref.ID); err != nil {
			t.Fatal("revoked but referenced photograph deleted", err)
		}
	}
}
