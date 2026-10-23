package config

import "testing"

func TestCloudRunConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "cloudrun")
	t.Setenv("ENABLE_DEMO", "false")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DB_DSN", "test@tcp(10.0.0.2:3306)/cloud_test")
	t.Setenv("MEDIA_DRIVER", "cos")
	t.Setenv("COS_BUCKET", "test-private-123456")
	t.Setenv("COS_REGION", "ap-shanghai")
	t.Setenv("COS_SECRET_ID", "test-id")
	t.Setenv("COS_SECRET_KEY", "test-key")
	t.Setenv("ADMIN_ALLOWED_ORIGINS", "https://admin.example.com")
	c, err := Load()
	if err != nil || c.Address != "0.0.0.0:8080" || c.Environment != "cloudrun" {
		t.Fatalf("cloud config: %+v %v", c, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"ENABLE_DEMO", "true"}, {"ENABLE_DEMO", ""},
		{"HTTP_ADDR", "127.0.0.1:8080"}, {"HTTP_ADDR", "0.0.0.0:9000"},
		{"MEDIA_DRIVER", "local"}, {"DB_DSN", ""},
		{"COS_SECRET_KEY", ""}, {"ADMIN_ALLOWED_ORIGINS", "https://*.example.com"},
		{"ADMIN_ALLOWED_ORIGINS", "http://admin.example.com"},
		{"ADMIN_ALLOWED_ORIGINS", "https://admin.example.com/path"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("unsafe cloud configuration accepted")
			}
		})
	}
}
