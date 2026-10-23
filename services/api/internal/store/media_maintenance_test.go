package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type testMediaCatalog struct {
	refs []MediaReference
	err  error
}

func (c *testMediaCatalog) MediaReferences(context.Context) ([]MediaReference, error) {
	return c.refs, c.err
}
func (c *testMediaCatalog) MediaReferenced(_ context.Context, id string) (bool, error) {
	for _, ref := range c.refs {
		if ref.ID == id {
			return true, c.err
		}
	}
	return false, c.err
}
func mediaRef(id string, data []byte) MediaReference {
	hash := sha256.Sum256(data)
	return MediaReference{ID: id, SHA256: hex.EncodeToString(hash[:]), SizeBytes: int64(len(data))}
}
func localMedia(t *testing.T) *PrivateMedia {
	t.Helper()
	m, err := NewPrivateMedia(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMediaIDValidationAndCancellation(t *testing.T) {
	m := localMedia(t)
	ctx := context.Background()
	for _, id := range []string{"", "..", "../secret", `..\secret`, "/absolute", "photo:stream", "photo.jpg", "a b"} {
		if err := m.Save(ctx, id, []byte("test")); err == nil {
			t.Fatalf("save accepted %q", id)
		}
		if _, err := m.Read(ctx, id); err == nil {
			t.Fatalf("read accepted %q", id)
		}
		if err := m.Delete(ctx, id); err == nil {
			t.Fatalf("delete accepted %q", id)
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := m.Save(ctx, "photo-test", []byte("test")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type failingDeleteMedia struct{ Media }

func (m failingDeleteMedia) Delete(context.Context, string) error {
	return errors.New("temporary delete failure")
}

func TestOrphanCleanupGraceReferencesAndRetry(t *testing.T) {
	ctx := context.Background()
	m := localMedia(t)
	now := time.Now()
	for _, id := range []string{"photo-care", "photo-followup", "photo-orphan", "photo-inflight", "manifest"} {
		if err := m.Save(ctx, id, []byte(id)); err != nil {
			t.Fatal(err)
		}
		if id != "photo-inflight" {
			if err := os.Chtimes(m.path(id), now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
	}
	catalog := &testMediaCatalog{refs: []MediaReference{{ID: "photo-care"}, {ID: "photo-followup"}}}
	result, err := CleanupMedia(ctx, m, catalog, now, 24*time.Hour, false)
	if err != nil || result.Candidates != 1 || result.Deleted != 0 {
		t.Fatalf("dry-run %+v %v", result, err)
	}
	result, err = CleanupMedia(ctx, failingDeleteMedia{m}, catalog, now, 24*time.Hour, true)
	if err == nil || result.Failures != 1 {
		t.Fatalf("failed deletes must be reported %+v %v", result, err)
	}
	catalog.err = errors.New("catalog unavailable")
	if _, err := CleanupMedia(ctx, m, catalog, now, 24*time.Hour, true); err == nil {
		t.Fatal("database error did not stop cleanup")
	}
	if _, err := m.Read(ctx, "photo-orphan"); err != nil {
		t.Fatal("orphan removed while database unavailable", err)
	}
	catalog.err = nil
	result, err = CleanupMedia(ctx, m, catalog, now, 24*time.Hour, true)
	if err != nil || result.Deleted != 1 {
		t.Fatalf("retry %+v %v", result, err)
	}
	for _, id := range []string{"photo-care", "photo-followup", "photo-inflight", "manifest"} {
		if _, err := m.Read(ctx, id); err != nil {
			t.Fatalf("retained object %s: %v", id, err)
		}
	}
	if _, err := CleanupMedia(ctx, m, catalog, now, time.Minute, true); err == nil {
		t.Fatal("unsafe grace accepted")
	}
	for _, name := range []string{".upload-crashed", ".upload-active", "unrelated-file"} {
		path := filepath.Join(m.root, name)
		if err := os.WriteFile(path, []byte("temporary"), 0o600); err != nil {
			t.Fatal(err)
		}
		if name != ".upload-active" {
			if err := os.Chtimes(path, now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
	}
	result, err = CleanupMedia(ctx, m, catalog, now, 24*time.Hour, true)
	if err != nil || result.Deleted != 1 {
		t.Fatalf("temporary cleanup %+v %v", result, err)
	}
	for _, name := range []string{".upload-active", "unrelated-file"} {
		if _, err := os.Stat(filepath.Join(m.root, name)); err != nil {
			t.Fatal("unexpected temporary cleanup", name, err)
		}
	}
}

func TestEncryptedBackupAndRecoveryUsesCurrentReferences(t *testing.T) {
	ctx := context.Background()
	primary, backup := localMedia(t), localMedia(t)
	data := []byte("private photograph bytes")
	catalog := &testMediaCatalog{refs: []MediaReference{mediaRef("photo-original", data), mediaRef("photo-replaced", data)}}
	for _, ref := range catalog.refs {
		if err := primary.Save(ctx, ref.ID, data); err != nil {
			t.Fatal(err)
		}
	}
	key := make([]byte, 32)
	snapshot := NewMediaSnapshotID()
	if count, err := BackupMedia(ctx, primary, backup, catalog, snapshot, key); err != nil || count != 2 {
		t.Fatalf("backup %d %v", count, err)
	}
	ciphertext, err := backup.Read(ctx, "photo-original")
	if err != nil {
		t.Fatal(err)
	}
	if string(ciphertext) == string(data) {
		t.Fatal("backup contains plaintext")
	}
	// Current catalog keeps the original photo but has removed the other photo.
	catalog.refs = catalog.refs[:1]
	for _, id := range []string{"photo-original", "photo-replaced"} {
		if err := primary.Delete(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := RestoreMedia(ctx, backup, primary, catalog, snapshot, key, false); err != nil || count != 1 {
		t.Fatalf("verify %d %v", count, err)
	}
	if _, err := primary.Read(ctx, "photo-original"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("dry-run wrote an object")
	}
	if _, err := RestoreMedia(ctx, backup, primary, catalog, snapshot, key, true); err != nil {
		t.Fatal(err)
	}
	if _, err := primary.Read(ctx, "photo-replaced"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("recovery resurrected replaced photo")
	}
	if _, err := RestoreMedia(ctx, backup, primary, catalog, snapshot, key, true); err != nil {
		t.Fatal("recovery not resumable", err)
	}
	key[0] = 1
	if _, err := RestoreMedia(ctx, backup, primary, catalog, snapshot, key, true); err == nil {
		t.Fatal("wrong key accepted")
	}
	key[0] = 0
	ciphertext[len(ciphertext)-1] ^= 1
	if err := backup.Save(ctx, "photo-original", ciphertext); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreMedia(ctx, backup, primary, catalog, snapshot, key, false); err == nil {
		t.Fatal("tampered snapshot accepted")
	}
}

func TestBackupRejectsIncompleteOrIncompatibleSnapshot(t *testing.T) {
	ctx := context.Background()
	primary, backup := localMedia(t), localMedia(t)
	data := []byte("bytes")
	catalog := &testMediaCatalog{refs: []MediaReference{mediaRef("photo-a", data)}}
	snapshot, key := NewMediaSnapshotID(), make([]byte, 32)
	if _, err := BackupMedia(ctx, primary, backup, catalog, snapshot, key); err == nil {
		t.Fatal("backup succeeded with missing source")
	}
	if _, err := backup.Read(ctx, "manifest"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed backup was marked complete")
	}
	if err := primary.Save(ctx, "photo-a", data); err != nil {
		t.Fatal(err)
	}
	if _, err := BackupMedia(ctx, primary, backup, catalog, snapshot, key); err != nil {
		t.Fatal(err)
	}
	catalog.refs = append(catalog.refs, mediaRef("photo-new", data))
	if _, err := RestoreMedia(ctx, backup, primary, catalog, snapshot, key, true); err == nil {
		t.Fatal("old snapshot accepted for newer database")
	}
	if _, err := BackupMedia(ctx, primary, backup, catalog, snapshot, key); err == nil {
		t.Fatal("completed snapshot overwritten")
	}
	if err := os.WriteFile(filepath.Join(primary.root, "photo-oversize.jpg"), make([]byte, MaxMediaBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := primary.Read(ctx, "photo-oversize"); err == nil {
		t.Fatal("oversized object accepted")
	}
}
