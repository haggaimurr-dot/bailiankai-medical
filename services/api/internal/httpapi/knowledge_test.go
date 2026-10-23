package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

type knowledgeHTTPRepo struct {
	fakeRepository
	store.KnowledgeRepository
	store.BookingRepository
	role    string
	calls   int
	filter  store.KnowledgeFilter
	patient string
	saved   bool
}

func (r *knowledgeHTTPRepo) AuthenticatePatient(context.Context, string, time.Time) (model.Patient, error) {
	return model.Patient{ID: "patient-self"}, nil
}
func (r *knowledgeHTTPRepo) KnowledgeFavorite(_ context.Context, patient, id string) (bool, error) {
	r.patient = patient
	return r.saved, nil
}
func (r *knowledgeHTTPRepo) SetKnowledgeFavorite(_ context.Context, patient, id string, saved bool, _ time.Time) error {
	r.patient = patient
	r.saved = saved
	return nil
}

func TestKnowledgeFavoritesAuthenticationAndOwnership(t *testing.T) {
	repo := &knowledgeHTTPRepo{}
	router := New(repo, "")
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		w := serve(router, method, "/api/v1/me/knowledge/favorites/article-1", "", "", "")
		if w.Code != 401 {
			t.Fatalf("anonymous %s: %d", method, w.Code)
		}
		w = serve(router, method, "/api/v1/me/knowledge/favorites/article-1?patientId=other", "", "patient-token", "")
		if w.Code != 400 {
			t.Fatalf("forged owner %s: %d", method, w.Code)
		}
		w = serve(router, method, "/api/v1/me/knowledge/favorites/article-1", "", "patient-token", "")
		if w.Code != 200 || repo.patient != "patient-self" {
			t.Fatalf("owner %s: %d %s", method, w.Code, w.Body)
		}
	}
	w := serve(router, "GET", "/api/v1/me/knowledge/favorites", "", "patient-token", "")
	if w.Code != 200 || repo.filter.FavoritePatientID != "patient-self" || !repo.filter.Public {
		t.Fatalf("favorites filter: %d %+v", w.Code, repo.filter)
	}
	w = serve(router, "PUT", "/api/v1/me/knowledge/favorites/article-1", `{"patientId":"other"}`, "patient-token", "")
	if w.Code != 400 {
		t.Fatalf("forged body accepted: %d", w.Code)
	}
}

func (r *knowledgeHTTPRepo) AuthenticateEmployee(context.Context, string, time.Time) (model.EmployeeAccount, error) {
	return model.EmployeeAccount{ID: "employee-test", Role: r.role, StaffID: strPtr("doctor-demo-1"), ClinicIDs: []string{"clinic-demo"}}, nil
}
func strPtr(s string) *string { return &s }

func TestKnowledgeMedicalSourceValidationAndReviewFilter(t *testing.T) {
	repo := &knowledgeHTTPRepo{role: "manager"}
	router := New(repo, "")
	for _, source := range []string{`null`, `42`, `"` + strings.Repeat("医", 5001) + `"`} {
		body := `{"categoryId":"cat-1","title":"标题","summary":"摘要","body":"正文","sortOrder":0,"sourceText":` + source + `}`
		w := serve(router, "POST", "/api/v1/admin/knowledge/articles", body, "employee-token", "")
		if w.Code != 400 || repo.calls != 0 {
			t.Fatalf("invalid source accepted: %d %s", w.Code, w.Body)
		}
	}
	w := serve(router, "POST", "/api/v1/admin/knowledge/articles", `{"categoryId":"cat-1","title":"标题","summary":"摘要","body":"正文","sortOrder":0,"sourceText":"  机构 · 版本 · 链接  "}`, "employee-token", "")
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"sourceText":"机构 · 版本 · 链接"`) {
		t.Fatalf("source not preserved and trimmed: %d %s", w.Code, w.Body)
	}
	for _, query := range []string{"?reviewStatus=invalid", "?reviewStatus=pending&reviewStatus=approved"} {
		w = serve(router, "GET", "/api/v1/admin/knowledge/articles"+query, "", "employee-token", "")
		if w.Code != 400 {
			t.Fatalf("invalid review filter accepted: %d", w.Code)
		}
	}
	w = serve(router, "GET", "/api/v1/admin/knowledge/articles?reviewStatus=pending&status=published", "", "employee-token", "")
	if w.Code != 200 || repo.filter.ReviewStatus != "pending" || repo.filter.Status != "published" {
		t.Fatalf("review/publication filters lost: %d %+v", w.Code, repo.filter)
	}
	w = serve(router, "GET", "/api/v1/knowledge/articles?reviewStatus=pending", "", "", "")
	if w.Code != 400 {
		t.Fatal("public endpoint accepted internal review filter")
	}
}
func (r *knowledgeHTTPRepo) ChangeKnowledge(_ context.Context, _, _, _ string, in store.KnowledgeChange, _ time.Time) (any, error) {
	r.calls++
	return in, nil
}
func (r *knowledgeHTTPRepo) KnowledgeArticles(_ context.Context, f store.KnowledgeFilter) (any, int64, error) {
	r.calls++
	r.filter = f
	return []model.KnowledgePublicArticle{}, 0, nil
}

func (r *knowledgeHTTPRepo) KnowledgeArticle(_ context.Context, id string, public bool) (any, error) {
	if id == "missing" {
		return nil, store.ErrNotFound
	}
	return model.KnowledgePublicArticle{ID: id, CategoryID: "cat-1"}, nil
}

func TestKnowledgeSearchAndRelated(t *testing.T) {
	repo := &knowledgeHTTPRepo{role: "manager"}
	router := New(repo, "")
	for _, query := range []string{"?q=" + strings.Repeat("x", 101), "?q=a&q=b", "?q=abc&patientId=other"} {
		w := serve(router, "GET", "/api/v1/knowledge/articles"+query, "", "", "")
		if w.Code != 400 {
			t.Fatalf("invalid search accepted: %d %s", w.Code, w.Body)
		}
	}
	w := serve(router, "GET", "/api/v1/knowledge/articles?q=title&page=2", "", "", "")
	if w.Code != 200 || repo.filter.Query != "title" || repo.filter.Page != 2 || !repo.filter.Public {
		t.Fatalf("search filter lost: %d %+v", w.Code, repo.filter)
	}
	w = serve(router, "GET", "/api/v1/knowledge/articles/article-1/related", "", "", "")
	if w.Code != 200 || repo.filter.CategoryID != "cat-1" || repo.filter.ExcludeID != "article-1" || repo.filter.PageSize != 3 || !repo.filter.Public {
		t.Fatalf("related filter: %d %+v", w.Code, repo.filter)
	}
	w = serve(router, "GET", "/api/v1/knowledge/articles/missing/related", "", "", "")
	if w.Code != 404 {
		t.Fatalf("unpublished related: %d", w.Code)
	}
	for _, role := range []string{"manager", "doctor", "clinician", "nurse", "operations", "customer_service"} {
		found := false
		for _, key := range employeeCapabilities(role) {
			if key == "/knowledge" {
				found = true
			}
		}
		if found != (role == "manager" || role == "doctor" || role == "clinician") {
			t.Fatalf("knowledge capability: %s %v", role, found)
		}
	}
}
func TestKnowledgeHTTPValidationAndPermissions(t *testing.T) {
	tests := []struct {
		role, method, path, body string
		status                   int
	}{
		{"manager", "POST", "categories", `{"clinicId":"clinic-demo","name":"科普","sortOrder":0,"active":true}`, 201},
		{"manager", "POST", "categories", `{"clinicId":"clinic-demo","name":"科普","sortOrder":0}`, 400},
		{"manager", "POST", "articles", `{"categoryId":"cat-1","title":"文章","summary":"","body":"正文","sortOrder":0}`, 201},
		{"manager", "PUT", "articles/article-1", `{"version":1,"title":"文章","summary":"","body":"正文","reviewStatus":"approved"}`, 400},
		{"manager", "POST", "articles/article-1/review", `{"version":1,"decision":"approved","note":""}`, 403},
		{"doctor", "POST", "articles/article-1/review", `{"version":1,"decision":"approved","note":""}`, 200},
		{"doctor", "POST", "articles/article-1/review", `{"version":1,"decision":"rejected","note":""}`, 400},
		{"doctor", "POST", "articles/article-1/review", `{"version":0,"decision":"approved","note":""}`, 400},
		{"doctor", "POST", "articles/article-1/publish", `{"version":1}`, 403},
		{"nurse", "POST", "articles/article-1/review", `{"version":1,"decision":"approved","note":""}`, 403},
		{"operations", "POST", "articles", `{}`, 403},
		{"customer_service", "GET", "articles", "", 403},
		{"manager", "POST", "articles/article-1/sort", `{"version":1,"sortOrder":-1}`, 400},
		{"manager", "POST", "articles/article-1/unpublish", `{"version":1,"patientId":"other"}`, 400},
	}
	for _, tc := range tests {
		t.Run(tc.role+tc.method+tc.path+tc.body, func(t *testing.T) {
			repo := &knowledgeHTTPRepo{role: tc.role}
			router := New(repo, "")
			w := serve(router, tc.method, "/api/v1/admin/knowledge/"+tc.path, tc.body, testToken, "")
			if w.Code != tc.status {
				t.Fatalf("status %d want %d: %s", w.Code, tc.status, w.Body)
			}
			if tc.status >= 400 && repo.calls != 0 {
				t.Fatal("invalid request reached store")
			}
		})
	}
	repo := &knowledgeHTTPRepo{role: "manager"}
	router := New(repo, "")
	for _, query := range []string{"?page=0", "?pageSize=101", "?status=published", "?page=1&page=2", "?clinicId=", "?patientId=other"} {
		w := serve(router, http.MethodGet, "/api/v1/knowledge/articles"+query, "", "", "")
		if w.Code != 400 {
			t.Fatalf("public query %s: %d", query, w.Code)
		}
	}
	w := serve(router, http.MethodGet, "/api/v1/knowledge/articles?pageSize=100", "", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Fatalf("public read requires session: %d %s", w.Code, w.Body)
	}
}
