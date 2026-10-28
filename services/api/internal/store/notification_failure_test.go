package store

import "testing"

func TestNotificationFailureCategory(t *testing.T) {
	cases := []struct {
		code     string
		category string
		retry    bool
	}{
		{"", "", false},
		{"0", "", false},
		{"authorization_required", "authorization", false},
		{"withdrawn", "authorization", false},
		{"43101", "authorization", false},
		{"40003", "authorization", false},
		{"expired_or_disabled", "template", false},
		{"40037", "template", false},
		{"41030", "template", false},
		{"40165", "template", false},
		{"45009", "rate_limit", true},
		{"-1", "transient", true},
		{"token_unavailable", "transient", true},
		{"worker_interrupted", "transient", true},
		{"unknown", "transient", true},
		{"40001", "transient", true},
		{"42001", "transient", true},
		{"source_changed", "source", false},
		{"49999", "other", false},
	}
	for _, tc := range cases {
		if got := notificationFailureCategory(tc.code); got != tc.category {
			t.Errorf("notificationFailureCategory(%q) = %q, want %q", tc.code, got, tc.category)
		}
		if got := notificationFailureRetryable(tc.category); got != tc.retry {
			t.Errorf("notificationFailureRetryable(%q) = %v, want %v", tc.category, got, tc.retry)
		}
	}
	// Every classified code must be findable through its category filter.
	for _, category := range []string{"authorization", "template", "rate_limit", "transient", "source"} {
		for _, code := range notificationFailureCodes(category) {
			if notificationFailureCategory(code) != category {
				t.Errorf("code %q listed under %q but classifies as %q", code, category, notificationFailureCategory(code))
			}
		}
	}
}
