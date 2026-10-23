package store

import "testing"

func TestSupportTransitions(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		allowed  bool
	}{
		{"open", "in_progress", true}, {"open", "resolved", false}, {"in_progress", "waiting_patient", true},
		{"waiting_patient", "resolved", true}, {"resolved", "in_progress", true}, {"resolved", "closed", true},
		{"closed", "in_progress", false}, {"in_progress", "in_progress", false}, {"open", "invented", false},
	} {
		if supportTransition(tc.from, tc.to) != tc.allowed {
			t.Fatalf("%s -> %s", tc.from, tc.to)
		}
	}
}
