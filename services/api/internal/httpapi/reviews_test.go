package httpapi

import "testing"

func TestMaskDisplayName(t *testing.T) {
	for name, want := range map[string]string{
		"":       "匿名患者",
		"王":      "*",
		"王小明":    "王**",
		"001":    "0**",
		"  李雷  ": "李*",
	} {
		if got := maskDisplayName(name); got != want {
			t.Fatalf("mask(%q)=%q want %q", name, got, want)
		}
	}
}
