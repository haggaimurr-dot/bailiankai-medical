package mediaconfig

import (
	"path/filepath"
	"testing"
)

func TestMediaConfigurationFailsClosed(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	for _, key := range []string{"MEDIA_DRIVER", "COS_BUCKET", "COS_REGION", "COS_SECRET_ID", "COS_SECRET_KEY", "COS_PREFIX"} {
		t.Setenv(key, "")
	}
	if c, err := Load(); err != nil || c.Driver != "local" {
		t.Fatalf("development default %v %v", c, err)
	}
	t.Setenv("COS_BUCKET", "medical-123456")
	if _, err := Load(); err == nil {
		t.Fatal("COS configuration silently fell back to local")
	}
	t.Setenv("MEDIA_DRIVER", "cos")
	if _, err := Load(); err == nil {
		t.Fatal("missing credentials accepted")
	}
	t.Setenv("COS_REGION", "ap-shanghai")
	t.Setenv("COS_SECRET_ID", "test-id")
	t.Setenv("COS_SECRET_KEY", "test-key")
	if c, err := Load(); err != nil || c.Driver != "cos" {
		t.Fatalf("COS config %v %v", c, err)
	}
	t.Setenv("APP_ENV", "production")
	t.Setenv("MEDIA_DRIVER", "local")
	t.Setenv("COS_BUCKET", "")
	t.Setenv("COS_SECRET_ID", "")
	t.Setenv("COS_SECRET_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("production local backend accepted")
	}
}

func TestBackupSeparationAndSnapshotValidation(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("MEDIA_BACKUP_DRIVER", "local")
	primary := Config{Driver: "local", Root: filepath.Join(t.TempDir(), "primary")}
	snapshot := "snapshot-0123456789abcdef0123456789abcdef"
	for _, root := range []string{primary.Root, filepath.Join(primary.Root, "snapshots")} {
		t.Setenv("MEDIA_BACKUP_ROOT", root)
		if _, err := Backup(primary, snapshot); err == nil {
			t.Fatal("backup under source root accepted")
		}
	}
	t.Setenv("MEDIA_BACKUP_ROOT", filepath.Join(filepath.Dir(primary.Root), "backup"))
	if _, err := Backup(primary, snapshot); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIA_BACKUP_ROOT", filepath.Dir(primary.Root))
	if _, err := Backup(Config{Driver: "local", Root: filepath.Join(filepath.Dir(primary.Root), snapshot)}, snapshot); err == nil {
		t.Fatal("snapshot resolves to primary root")
	}
	if _, err := Backup(primary, "snapshot-../../secret"); err == nil {
		t.Fatal("snapshot traversal accepted")
	}
	t.Setenv("MEDIA_BACKUP_DRIVER", "cos")
	t.Setenv("COS_BACKUP_BUCKET", "medical-123456")
	if _, err := Backup(Config{Driver: "cos", COS: primary.COS}, snapshot); err == nil {
		t.Fatal("missing backup credentials accepted")
	}
}
