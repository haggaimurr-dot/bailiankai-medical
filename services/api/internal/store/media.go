package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const MaxMediaBytes = 9 << 20

// Media never exposes object URLs. All reads go through the authenticated API.
type Media interface {
	Save(context.Context, string, []byte) error
	Read(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
	List(context.Context, func(MediaObject) error) error
	Ready(context.Context) error
}

type MediaObject struct {
	ID         string
	ModifiedAt time.Time
}

func validMediaID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// PrivateMedia writes files outside the web server's static directory. IDs are
// random and never derive from a patient-provided filename.
type PrivateMedia struct{ root string }

func NewPrivateMedia(root string) (*PrivateMedia, error) {
	if root == "" {
		return nil, errors.New("private media directory is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	return &PrivateMedia{root: abs}, nil
}

func (m *PrivateMedia) path(id string) string { return filepath.Join(m.root, id+".jpg") }

func (m *PrivateMedia) Save(ctx context.Context, id string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validMediaID(id) || len(data) == 0 || len(data) > MaxMediaBytes {
		return errors.New("invalid media id")
	}
	tmp, err := os.CreateTemp(m.root, ".upload-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, m.path(id))
}

func (m *PrivateMedia) Read(ctx context.Context, id string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validMediaID(id) {
		return nil, errors.New("invalid media id")
	}
	info, err := os.Lstat(m.path(id))
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxMediaBytes {
		return nil, errors.New("invalid media file")
	}
	return os.ReadFile(m.path(id))
}

func (m *PrivateMedia) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validMediaID(id) {
		return errors.New("invalid media id")
	}
	err := os.Remove(m.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (m *PrivateMedia) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.CreateTemp(m.root, ".probe-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		return err
	}
	return os.Remove(name)
}

func (m *PrivateMedia) List(ctx context.Context, visit func(MediaObject) error) error {
	f, err := os.Open(m.root)
	if err != nil {
		return err
	}
	defer f.Close()
	for {
		entries, err := f.ReadDir(500)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !strings.HasSuffix(entry.Name(), ".jpg") {
				continue
			}
			id := strings.TrimSuffix(entry.Name(), ".jpg")
			if !validMediaID(id) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				continue
			}
			if err := visit(MediaObject{ID: id, ModifiedAt: info.ModTime()}); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// Interrupted atomic uploads/probes have no database reference. Only old
// regular files with our generated temporary prefixes are eligible.
func (m *PrivateMedia) cleanupTemporary(ctx context.Context, before time.Time, apply bool) (CleanupResult, error) {
	var result CleanupResult
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !strings.HasPrefix(entry.Name(), ".upload-") && !strings.HasPrefix(entry.Name(), ".probe-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return result, err
		}
		if !info.Mode().IsRegular() || !info.ModTime().Before(before) {
			continue
		}
		result.Candidates++
		if !apply {
			continue
		}
		if err := os.Remove(filepath.Join(m.root, entry.Name())); err != nil {
			result.Failures++
		} else {
			result.Deleted++
		}
	}
	return result, nil
}

func newCareID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Errorf("generate media id: %w", err))
	}
	return prefix + hex.EncodeToString(b[:])
}

func NewMediaSnapshotID() string { return newCareID("snapshot-") }
