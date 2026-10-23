package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

type MediaReference struct {
	ID        string `json:"id"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"sizeBytes"`
}

type MediaCatalog interface {
	MediaReferences(context.Context) ([]MediaReference, error)
	MediaReferenced(context.Context, string) (bool, error)
}

func (s *MySQL) MediaReferences(ctx context.Context) ([]MediaReference, error) {
	rows := make([]MediaReference, 0)
	err := s.db.WithContext(ctx).Raw("SELECT id, sha256, size_bytes FROM care_photos UNION ALL SELECT id, sha256, size_bytes FROM followup_photos UNION ALL SELECT id, sha256, size_bytes FROM case_images UNION ALL SELECT id, sha256, size_bytes FROM assessment_photos WHERE is_current = TRUE OR is_frozen = TRUE ORDER BY id").Scan(&rows).Error
	return rows, err
}

func (s *MySQL) MediaReferenced(ctx context.Context, id string) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Raw("SELECT (SELECT COUNT(*) FROM care_photos WHERE id = ?) + (SELECT COUNT(*) FROM followup_photos WHERE id = ?) + (SELECT COUNT(*) FROM case_images WHERE id = ?) + (SELECT COUNT(*) FROM assessment_photos WHERE id = ? AND (is_current = TRUE OR is_frozen = TRUE)) AS count", id, id, id, id).Scan(&count).Error
	return count != 0, err
}

type CleanupResult struct{ Candidates, Deleted, Failures int }

// Never delete a just-uploaded object: DB publication can still be in flight.
// Re-query references for each old candidate; a catalog or listing failure
// aborts the run. Failed deletes remain discoverable on the next run.
func CleanupMedia(ctx context.Context, media Media, catalog MediaCatalog, now time.Time, grace time.Duration, apply bool) (CleanupResult, error) {
	var result CleanupResult
	if grace < 24*time.Hour {
		return result, errors.New("orphan grace must be at least 24 hours")
	}
	if err := media.Ready(ctx); err != nil {
		return result, err
	}
	err := media.List(ctx, func(object MediaObject) error {
		if !strings.HasPrefix(object.ID, "photo-") || object.ModifiedAt.IsZero() || !object.ModifiedAt.Before(now.Add(-grace)) {
			return nil
		}
		referenced, err := catalog.MediaReferenced(ctx, object.ID)
		if err != nil {
			return err
		}
		if referenced {
			return nil
		}
		result.Candidates++
		if !apply {
			return nil
		}
		if err := media.Delete(ctx, object.ID); err != nil {
			result.Failures++
			return nil
		}
		result.Deleted++
		return nil
	})
	if err != nil {
		return result, err
	}
	if temporary, ok := media.(interface {
		cleanupTemporary(context.Context, time.Time, bool) (CleanupResult, error)
	}); ok {
		cleaned, err := temporary.cleanupTemporary(ctx, now.Add(-grace), apply)
		result.Candidates += cleaned.Candidates
		result.Deleted += cleaned.Deleted
		result.Failures += cleaned.Failures
		if err != nil {
			return result, err
		}
	}
	if result.Failures != 0 {
		return result, fmt.Errorf("%d orphan deletes failed; will retry", result.Failures)
	}
	return result, nil
}

func (s *MySQL) RunMediaCleanupWorker(ctx context.Context, media Media) {
	run := func() {
		workCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		result, err := CleanupMedia(workCtx, media, s, time.Now(), 24*time.Hour, true)
		if err != nil && ctx.Err() == nil {
			slog.Error("media cleanup failed", "deleted", result.Deleted, "failures", result.Failures)
		}
		if result.Deleted > 0 {
			slog.Info("media orphans removed", "count", result.Deleted)
		}
	}
	run()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

type mediaManifest struct {
	Version   int              `json:"version"`
	Snapshot  string           `json:"snapshot"`
	CreatedAt time.Time        `json:"createdAt"`
	Objects   []MediaReference `json:"objects"`
}

func backupCipher(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("backup encryption requires a 32-byte key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func encryptBackup(aead cipher.AEAD, data []byte, aad string) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, data, []byte(aad)), nil
}

func decryptBackup(aead cipher.AEAD, data []byte, aad string) ([]byte, error) {
	if len(data) < aead.NonceSize() {
		return nil, errors.New("truncated backup")
	}
	return aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte(aad))
}

func VerifyMedia(data []byte, reference MediaReference) error {
	hash := sha256.Sum256(data)
	if int64(len(data)) != reference.SizeBytes || hex.EncodeToString(hash[:]) != reference.SHA256 {
		return errors.New("media checksum or size mismatch")
	}
	return nil
}

// A snapshot becomes usable only when its encrypted manifest is written last.
// Every object is read back and authenticated before that commit marker.
func BackupMedia(ctx context.Context, source, destination Media, catalog MediaCatalog, snapshot string, key []byte) (int, error) {
	aead, err := backupCipher(key)
	if err != nil {
		return 0, err
	}
	if err := source.Ready(ctx); err != nil {
		return 0, err
	}
	if err := destination.Ready(ctx); err != nil {
		return 0, err
	}
	if _, err := destination.Read(ctx, "manifest"); !errors.Is(err, os.ErrNotExist) {
		return 0, errors.New("snapshot already exists or cannot be checked")
	}
	refs, err := catalog.MediaReferences(ctx)
	if err != nil {
		return 0, err
	}
	manifest := mediaManifest{Version: 1, Snapshot: snapshot, CreatedAt: time.Now().UTC(), Objects: refs}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return 0, err
	}
	if len(manifestBytes)+aead.NonceSize()+aead.Overhead() > MaxMediaBytes {
		return 0, errors.New("backup manifest exceeds size limit")
	}
	for i, ref := range refs {
		data, err := source.Read(ctx, ref.ID)
		if err != nil {
			return i, fmt.Errorf("backup source unavailable: %s", ref.ID)
		}
		if err := VerifyMedia(data, ref); err != nil {
			return i, err
		}
		aad := snapshot + "/" + ref.ID
		encrypted, err := encryptBackup(aead, data, aad)
		if err != nil {
			return i, err
		}
		if err := destination.Save(ctx, ref.ID, encrypted); err != nil {
			return i, err
		}
		check, err := destination.Read(ctx, ref.ID)
		if err != nil {
			return i, err
		}
		check, err = decryptBackup(aead, check, aad)
		if err != nil {
			return i, err
		}
		if err := VerifyMedia(check, ref); err != nil {
			return i, err
		}
	}
	encrypted, err := encryptBackup(aead, manifestBytes, snapshot+"/manifest")
	if err != nil {
		return len(refs), err
	}
	if err := destination.Save(ctx, "manifest", encrypted); err != nil {
		return len(refs), err
	}
	check, err := destination.Read(ctx, "manifest")
	if err != nil {
		return len(refs), err
	}
	_, err = decryptBackup(aead, check, snapshot+"/manifest")
	return len(refs), err
}

// Recovery consults the current database; it never restores consent, session or
// photo rows. Old snapshots cannot resurrect replaced photos or revoked access.
// apply=false verifies the entire recovery plan without writing objects.
func RestoreMedia(ctx context.Context, source, destination Media, catalog MediaCatalog, snapshot string, key []byte, apply bool) (int, error) {
	aead, err := backupCipher(key)
	if err != nil {
		return 0, err
	}
	if err := source.Ready(ctx); err != nil {
		return 0, err
	}
	if err := destination.Ready(ctx); err != nil {
		return 0, err
	}
	data, err := source.Read(ctx, "manifest")
	if err != nil {
		return 0, errors.New("snapshot has no readable completion manifest")
	}
	data, err = decryptBackup(aead, data, snapshot+"/manifest")
	if err != nil {
		return 0, errors.New("backup key or manifest authentication failed")
	}
	var manifest mediaManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return 0, err
	}
	if manifest.Version != 1 || manifest.Snapshot != snapshot {
		return 0, errors.New("unsupported snapshot manifest")
	}
	index := make(map[string]MediaReference, len(manifest.Objects))
	for _, ref := range manifest.Objects {
		if !validMediaID(ref.ID) || !strings.HasPrefix(ref.ID, "photo-") {
			return 0, errors.New("invalid snapshot object id")
		}
		if _, exists := index[ref.ID]; exists {
			return 0, errors.New("duplicate snapshot object")
		}
		index[ref.ID] = ref
	}
	refs, err := catalog.MediaReferences(ctx)
	if err != nil {
		return 0, err
	}
	// Validate all current references before any write, avoiding partial recovery
	// caused by selecting a snapshot that predates current database metadata.
	for _, ref := range refs {
		if saved, exists := index[ref.ID]; !exists || saved != ref {
			return 0, fmt.Errorf("snapshot does not match current database photo: %s", ref.ID)
		}
	}
	for i, ref := range refs {
		encrypted, err := source.Read(ctx, ref.ID)
		if err != nil {
			return i, err
		}
		plain, err := decryptBackup(aead, encrypted, snapshot+"/"+ref.ID)
		if err != nil {
			return i, err
		}
		if err := VerifyMedia(plain, ref); err != nil {
			return i, err
		}
		if !apply {
			continue
		}
		referenced, err := catalog.MediaReferenced(ctx, ref.ID)
		if err != nil {
			return i, err
		}
		if !referenced {
			continue
		}
		existing, err := destination.Read(ctx, ref.ID)
		if err == nil && VerifyMedia(existing, ref) == nil {
			continue
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return i, err
		}
		if err := destination.Save(ctx, ref.ID, plain); err != nil {
			return i, err
		}
		check, err := destination.Read(ctx, ref.ID)
		if err != nil {
			return i, err
		}
		if err := VerifyMedia(check, ref); err != nil {
			return i, err
		}
	}
	return len(refs), nil
}
