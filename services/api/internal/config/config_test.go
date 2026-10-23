package config

import (
	"testing"
	"time"
)

func TestDemoGuard(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("ENABLE_DEMO", "true")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DB_DSN", "test@tcp(127.0.0.1:3307)/demo")
	t.Setenv("DEMO_ADMIN_TOKEN", "test-only-token-at-least-16")
	if c, err := Load(); err != nil || c.Address != "127.0.0.1:8080" {
		t.Fatalf("valid local config: %+v %v", c, err)
	}
	for _, tc := range []struct{ key, value string }{{"APP_ENV", "production"}, {"ENABLE_DEMO", "false"}, {"HTTP_ADDR", "0.0.0.0:8080"}, {"DB_DSN", ""}, {"MEDIA_DRIVER", "invalid"}} {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("unsafe config accepted")
			}
		})
	}
}

func TestPendingConfirmationConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("ENABLE_DEMO", "true")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DB_DSN", "test@tcp(127.0.0.1:3307)/demo")
	t.Setenv("DEMO_ADMIN_TOKEN", "test-only-token-at-least-16")
	t.Setenv("PENDING_CONFIRMATION_MINUTES", "")
	if c, err := Load(); err != nil || c.PendingConfirmation != 120*time.Minute {
		t.Fatalf("default expiry: %v %v", c.PendingConfirmation, err)
	}
	t.Setenv("PENDING_CONFIRMATION_MINUTES", "45")
	if c, err := Load(); err != nil || c.PendingConfirmation != 45*time.Minute {
		t.Fatalf("configured expiry: %v %v", c.PendingConfirmation, err)
	}
	for _, value := range []string{"0", "-1", "1441", "abc"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("PENDING_CONFIRMATION_MINUTES", value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid expiry accepted")
			}
		})
	}
}
