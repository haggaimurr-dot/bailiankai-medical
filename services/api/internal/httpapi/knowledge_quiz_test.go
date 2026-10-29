package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

// TestKnowledgeQuizValidation covers row 91's input bounds: the quiz is
//科普互动 only, so the API clamps its shape before it ever reaches the repo.
func TestKnowledgeQuizValidation(t *testing.T) {
	repo := &knowledgeHTTPRepo{role: "manager"}
	router := New(repo, "")
	valid := `{"question":"Q","options":["A","B"],"answerIndex":0}`
	cases := []struct {
		name string
		quiz string
		want int
	}{
		{"answer index out of range", `[{"question":"Q","options":["A","B"],"answerIndex":5}]`, 400},
		{"negative answer index", `[{"question":"Q","options":["A","B"],"answerIndex":-1}]`, 400},
		{"single option", `[{"question":"Q","options":["A"],"answerIndex":0}]`, 400},
		{"empty question", `[{"question":"","options":["A","B"],"answerIndex":0}]`, 400},
		{"empty option", `[{"question":"Q","options":["A",""],"answerIndex":0}]`, 400},
		{"too many questions", `[` + strings.Repeat(valid+",", 20) + valid + `]`, 400},
		{"non-https video", ``, 400}, // videoUrl checked separately below
	}
	base := `{"categoryId":"cat-1","title":"标题","summary":"摘要","body":"正文","sortOrder":0,"sourceText":"来源"}`
	for _, tc := range cases {
		body := base[:len(base)-1] + `,"quiz":` + tc.quiz + `}`
		if tc.name == "non-https video" {
			body = base[:len(base)-1] + `,"videoUrl":"http://example.com/v.mp4"}`
		}
		before := repo.calls
		w := serve(router, "POST", "/api/v1/admin/knowledge/articles", body, "employee-token", "")
		if w.Code != tc.want || repo.calls != before {
			t.Fatalf("%s: code=%d calls=%d->%d body=%s", tc.name, w.Code, before, repo.calls, w.Body)
		}
	}
	// Valid quiz + HTTPS video passes validation and reaches the repo.
	okBody := base[:len(base)-1] + `,"videoUrl":"https://example.com/v.mp4","quiz":[` + valid + `]}`
	w := serve(router, "POST", "/api/v1/admin/knowledge/articles", okBody, "employee-token", "")
	if w.Code != 201 {
		t.Fatalf("valid quiz rejected: %d %s", w.Code, w.Body)
	}
}

// TestKnowledgeStatsAndBeaconRoutes covers row 116's HTTP surface: the admin
// stats endpoint is registered and permission-gated (not 403/404 for
// manager/doctor), and the public open beacon always answers 204.
func TestKnowledgeStatsAndBeaconRoutes(t *testing.T) {
	for _, role := range []string{"manager", "doctor", "clinician"} {
		repo := &knowledgeHTTPRepo{role: role}
		router := New(repo, "")
		w := serve(router, "GET", "/api/v1/admin/knowledge/stats?days=7", "", "employee-token", "")
		// The fake repo does not implement the stats reader, so 503 proves the
		// route is registered and the role passed permission (not 403/404).
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("stats %s: %d %s", role, w.Code, w.Body)
		}
	}
	w := serve(New(&knowledgeHTTPRepo{role: "manager"}, ""), "GET", "/api/v1/admin/knowledge/stats?days=0", "", "employee-token", "")
	if w.Code != 400 {
		t.Fatalf("bad days accepted: %d", w.Code)
	}
	// Public beacon: no auth required, never fails the reader.
	w = serve(New(&knowledgeHTTPRepo{}, ""), "POST", "/api/v1/knowledge/articles/article-1/open", `{"clinicId":"clinic-demo"}`, "", "")
	if w.Code != 204 {
		t.Fatalf("beacon: %d %s", w.Code, w.Body)
	}
}
