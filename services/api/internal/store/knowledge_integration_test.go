package store_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func TestKnowledgeLifecycleMySQL(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	manager := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"}), "manager-test")
	doctor := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "doctor", "doctor-demo-1", []string{"clinic-demo"}), "doctor-test")
	foreign := store.WithEmployeeScope(ctx, "manager", "", []string{"other-clinic"})
	catInput := store.KnowledgeChange{ClinicID: "clinic-demo", Name: "知识库", Active: true, SortOrder: 2}
	value, err := f.repo.ChangeKnowledge(manager, "categories", "", "create", catInput, f.now)
	if err != nil {
		t.Fatal(err)
	}
	category := value.(model.KnowledgeCategory)
	input := store.KnowledgeChange{CategoryID: category.ID, Title: "已审文章", Summary: "摘要", Body: "旧版正文", SourceText: "来源第一版", SortOrder: 5}
	value, err = f.repo.ChangeKnowledge(manager, "articles", "", "create", input, f.now)
	if err != nil {
		t.Fatal(err)
	}
	article := value.(model.KnowledgeArticle)
	change := func(actor context.Context, action string, in store.KnowledgeChange) {
		t.Helper()
		in.Version = article.Version
		value, err = f.repo.ChangeKnowledge(actor, "articles", article.ID, action, in, f.now)
		if err != nil {
			t.Fatal(action, err)
		}
		article = value.(model.KnowledgeArticle)
	}
	expect := func(actor context.Context, action string, in store.KnowledgeChange, want error) {
		t.Helper()
		_, err := f.repo.ChangeKnowledge(actor, "articles", article.ID, action, in, f.now)
		if !errors.Is(err, want) {
			t.Fatalf("%s: %v want %v", action, err, want)
		}
	}
	invisible := func() {
		t.Helper()
		if _, err := f.repo.KnowledgeArticle(ctx, article.ID, true); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("unpublished content exposed", err)
		}
	}
	invisible()
	// A draft without references can be saved and returned, but not approved.
	missingSource := input
	missingSource.SourceText = ""
	change(manager, "update", missingSource)
	expect(doctor, "review", store.KnowledgeChange{Version: article.Version, Decision: "approved"}, store.ErrCareState)
	change(manager, "update", input)
	expect(manager, "publish", store.KnowledgeChange{Version: article.Version}, store.ErrCareState)
	expect(manager, "review", store.KnowledgeChange{Version: article.Version, Decision: "approved"}, store.ErrForbidden)
	expect(foreign, "update", store.KnowledgeChange{Version: article.Version}, store.ErrNotFound)
	if _, err := f.repo.KnowledgeHistory(foreign, "articles", article.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("history scope", err)
	}
	change(doctor, "review", store.KnowledgeChange{Decision: "rejected", Note: "需修订"})
	expect(manager, "publish", store.KnowledgeChange{Version: article.Version}, store.ErrCareState)
	change(manager, "update", input)
	change(doctor, "review", store.KnowledgeChange{Decision: "approved"})
	expect(manager, "publish", store.KnowledgeChange{Version: article.Version - 1}, store.ErrConflict)
	change(manager, "publish", store.KnowledgeChange{})
	public, err := f.repo.KnowledgeArticle(ctx, article.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if public.(model.KnowledgePublicArticle).Body != "旧版正文" || public.(model.KnowledgePublicArticle).SourceText != "来源第一版" || public.(model.KnowledgePublicArticle).ReviewerName == "" {
		t.Fatal("snapshot missing reviewer", public)
	}
	sourceOnly := input
	sourceOnly.SourceText = "仅修改来源"
	change(manager, "update", sourceOnly)
	if article.ReviewStatus != "pending" || article.ReviewedAt != nil {
		t.Fatal("source-only edit retained approval", article)
	}
	public, err = f.repo.KnowledgeArticle(ctx, article.ID, true)
	if err != nil || public.(model.KnowledgePublicArticle).SourceText != "来源第一版" {
		t.Fatal("source-only draft leaked", public, err)
	}
	input.Title = "未审新版"
	input.Body = "未审正文"
	input.SourceText = "未审来源"
	change(manager, "update", input)
	if article.ReviewStatus != "pending" || article.ReviewedAt != nil || article.ReviewerName != "" {
		t.Fatal("editing did not clear review", article)
	}
	pending, count, err := f.repo.KnowledgeArticles(manager, store.KnowledgeFilter{ReviewStatus: "pending", Status: "published", Page: 1, PageSize: 20})
	if err != nil || count != 1 || pending.([]model.KnowledgeArticle)[0].ID != article.ID {
		t.Fatal("published draft review queue", pending, count, err)
	}
	public, err = f.repo.KnowledgeArticle(ctx, article.ID, true)
	if err != nil || public.(model.KnowledgePublicArticle).Body != "旧版正文" || public.(model.KnowledgePublicArticle).SourceText != "来源第一版" {
		t.Fatal("draft leaked", public, err)
	}
	expect(manager, "publish", store.KnowledgeChange{Version: article.Version}, store.ErrCareState)
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/articles/"+article.ID, nil))
	if response.Code != 200 || strings.Contains(response.Body.String(), "未审") || strings.Contains(response.Body.String(), "doctor-test") {
		t.Fatal("public HTTP leak", response.Body)
	}
	change(doctor, "review", store.KnowledgeChange{Decision: "approved"})
	change(manager, "publish", store.KnowledgeChange{})
	// Competing requests with the same version: exactly one wins.
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	version := article.Version
	for _, order := range []int{1, 9} {
		wg.Add(1)
		go func(order int) {
			defer wg.Done()
			_, err := f.repo.ChangeKnowledge(manager, "articles", article.ID, "sort", store.KnowledgeChange{Version: version, SortOrder: order}, f.now)
			outcomes <- err
		}(order)
	}
	wg.Wait()
	close(outcomes)
	successes, conflicts := 0, 0
	for err := range outcomes {
		if err == nil {
			successes++
		} else if errors.Is(err, store.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrency %d/%d", successes, conflicts)
	}
	value, err = f.repo.KnowledgeArticle(manager, article.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	article = value.(model.KnowledgeArticle)
	otherInput := input
	otherInput.SortOrder = 0
	otherValue, err := f.repo.ChangeKnowledge(manager, "articles", "", "create", otherInput, f.now)
	if err != nil {
		t.Fatal(err)
	}
	other := otherValue.(model.KnowledgeArticle)
	otherValue, err = f.repo.ChangeKnowledge(doctor, "articles", other.ID, "review", store.KnowledgeChange{Version: other.Version, Decision: "approved"}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	other = otherValue.(model.KnowledgeArticle)
	if _, err := f.repo.ChangeKnowledge(manager, "articles", other.ID, "publish", store.KnowledgeChange{Version: other.Version}, f.now); err != nil {
		t.Fatal(err)
	}
	rows, total, err := f.repo.KnowledgeArticles(ctx, store.KnowledgeFilter{Public: true, Page: 1, PageSize: 1, CategoryID: category.ID})
	if err != nil || total != 2 || len(rows.([]model.KnowledgePublicArticle)) != 1 || rows.([]model.KnowledgePublicArticle)[0].Body != "" || rows.([]model.KnowledgePublicArticle)[0].ID != other.ID {
		t.Fatal("list", rows, total, err)
	}
	rows, total, err = f.repo.KnowledgeArticles(ctx, store.KnowledgeFilter{Public: true, Page: 2, PageSize: 1, CategoryID: category.ID})
	if err != nil || total != 2 || rows.([]model.KnowledgePublicArticle)[0].ID != article.ID {
		t.Fatal("second page", rows, total, err)
	}
	if _, err := f.repo.KnowledgeArticle(foreign, article.ID, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("foreign detail", err)
	}
	rows, total, err = f.repo.KnowledgeArticles(foreign, store.KnowledgeFilter{Page: 1, PageSize: 20})
	if err != nil || total != 0 {
		t.Fatal("foreign list", rows, total, err)
	}
	otherCatInput := catInput
	otherCatInput.SortOrder = 0
	otherCategoryValue, err := f.repo.ChangeKnowledge(manager, "categories", "", "create", otherCatInput, f.now)
	if err != nil {
		t.Fatal(err)
	}
	categories, err := f.repo.KnowledgeCategories(ctx, store.KnowledgeFilter{Public: true, ClinicID: "clinic-demo"})
	if err != nil || len(categories) != 2 || categories[0].ID != otherCategoryValue.(model.KnowledgeCategory).ID {
		t.Fatal("category order", categories, err)
	}
	categories, err = f.repo.KnowledgeCategories(foreign, store.KnowledgeFilter{})
	if err != nil || len(categories) != 0 {
		t.Fatal("foreign categories", categories, err)
	}
	catInput.Version = category.Version
	catInput.Active = false
	value, err = f.repo.ChangeKnowledge(manager, "categories", category.ID, "update", catInput, f.now)
	if err != nil {
		t.Fatal(err)
	}
	category = value.(model.KnowledgeCategory)
	invisible()
	expect(manager, "publish", store.KnowledgeChange{Version: article.Version}, store.ErrCareState)
	catInput.Version = category.Version
	catInput.Active = true
	if _, err = f.repo.ChangeKnowledge(manager, "categories", category.ID, "update", catInput, f.now); err != nil {
		t.Fatal(err)
	}
	change(manager, "unpublish", store.KnowledgeChange{})
	invisible()
	history, err := f.repo.KnowledgeHistory(manager, "articles", article.ID)
	if err != nil || len(history) != article.Version {
		t.Fatal("history", len(history), article.Version, err)
	}
	found := false
	for _, event := range history {
		if event.Action == "publish" && strings.Contains(event.ContentJSON, "旧版正文") && event.ActorID == "manager-test" {
			found = true
		}
	}
	if !found {
		t.Fatal("published snapshot missing from audit")
	}
	change(manager, "publish", store.KnowledgeChange{})
	if err := f.db.Model(&model.FollowupStaff{}).Where("id = ?", "doctor-demo-1").Update("status", "off_duty").Error; err != nil {
		t.Fatal(err)
	}
	expect(doctor, "review", store.KnowledgeChange{Version: article.Version, Decision: "approved"}, store.ErrForbidden)
}
