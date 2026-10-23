package store

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestSupportLegacyRequestHash(t *testing.T) {
	// Uncertain text requests from before attachments/ratings were introduced
	// must still match the hashes already committed to the database.
	legacy := `{"clinicId":"","subject":"","category":"","content":"回复","requestKey":"support-request-0001","action":"message","assigneeId":"","status":"","dueAt":null,"version":1}`
	in := SupportInput{Content: "回复", RequestKey: "support-request-0001", Action: "message", Version: 1}
	if got, want := supportHash(in), fmt.Sprintf("%x", sha256.Sum256([]byte(legacy))); got != want {
		t.Fatalf("legacy hash changed: %s != %s", got, want)
	}
}
