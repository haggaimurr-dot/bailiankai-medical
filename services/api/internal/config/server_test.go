package config

import (
	"path/filepath"
	"testing"
)

func TestPersistentServerConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "server")
	t.Setenv("ENABLE_DEMO", "false")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DB_DSN", "test@tcp(127.0.0.1:3306)/bailiankai")
	t.Setenv("MEDIA_DRIVER", "local")
	t.Setenv("MEDIA_ROOT", filepath.Join(t.TempDir(), "private-media"))
	t.Setenv("COS_BUCKET", "")
	t.Setenv("COS_SECRET_ID", "")
	t.Setenv("COS_SECRET_KEY", "")
	c, err := Load()
	if err != nil || c.Address != "127.0.0.1:8080" || c.Environment != "server" {
		t.Fatalf("server config: %+v %v", c, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"ENABLE_DEMO", "true"}, {"ENABLE_DEMO", ""},
		{"HTTP_ADDR", "0.0.0.0:8080"}, {"HTTP_ADDR", "bad"},
		{"MEDIA_ROOT", ""}, {"MEDIA_ROOT", "relative/media"},
		{"DB_DSN", ""},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("unsafe server config accepted")
			}
		})
	}
}
