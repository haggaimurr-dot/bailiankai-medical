package store_test

import (
	"context"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"testing"
)

func TestKnowledgeDiscoveryMySQL(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	manager := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"}), "manager-test")
	doctor := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "doctor", "doctor-demo-1", []string{"clinic-demo"}), "doctor-test")
	value, err := f.repo.ChangeKnowledge(manager, "categories", "", "create", store.KnowledgeChange{ClinicID: "clinic-demo", Name: "准备", Active: true}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	category := value.(model.KnowledgeCategory)
	create := func(title string) model.KnowledgeArticle {
		t.Helper()
		value, err := f.repo.ChangeKnowledge(manager, "articles", "", "create", store.KnowledgeChange{CategoryID: category.ID, Title: title, Summary: "准备材料", Body: "公开正文", SourceText: "来源第一版"}, f.now)
		if err != nil {
			t.Fatal(err)
		}
		row := value.(model.KnowledgeArticle)
		value, err = f.repo.ChangeKnowledge(doctor, "articles", row.ID, "review", store.KnowledgeChange{Version: row.Version, Decision: "approved"}, f.now)
		if err != nil {
			t.Fatal(err)
		}
		row = value.(model.KnowledgeArticle)
		value, err = f.repo.ChangeKnowledge(manager, "articles", row.ID, "publish", store.KnowledgeChange{Version: row.Version}, f.now)
		if err != nil {
			t.Fatal(err)
		}
		return value.(model.KnowledgeArticle)
	}
	first, second := create("面诊100%准备"), create("面诊流程")
	patient, other := f.patient(), f.patient()
	for i := 0; i < 2; i++ {
		if err := f.repo.SetKnowledgeFavorite(ctx, patient.Patient.ID, first.ID, true, f.now); err != nil {
			t.Fatal(err)
		}
	}
	check := func(filter store.KnowledgeFilter, expected int64) []model.KnowledgePublicArticle {
		t.Helper()
		filter.Public, filter.Page, filter.PageSize = true, 1, 20
		value, total, err := f.repo.KnowledgeArticles(ctx, filter)
		if err != nil || total != expected {
			t.Fatalf("filter %+v: %d %v", filter, total, err)
		}
		return value.([]model.KnowledgePublicArticle)
	}
	check(store.KnowledgeFilter{FavoritePatientID: patient.Patient.ID}, 1)
	check(store.KnowledgeFilter{FavoritePatientID: other.Patient.ID}, 0)
	check(store.KnowledgeFilter{Query: "100%"}, 1)
	check(store.KnowledgeFilter{Query: "_"}, 0)
	related := check(store.KnowledgeFilter{CategoryID: category.ID, ExcludeID: first.ID}, 1)
	if related[0].ID != second.ID || related[0].Body != "" {
		t.Fatal("invalid related summary", related)
	}
	value, err = f.repo.ChangeKnowledge(manager, "articles", first.ID, "update", store.KnowledgeChange{Version: first.Version, Title: "秘密草稿", Body: "不公开"}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	first = value.(model.KnowledgeArticle)
	check(store.KnowledgeFilter{Query: "秘密草稿"}, 0)
	check(store.KnowledgeFilter{Query: "100%"}, 1)
	_, err = f.repo.ChangeKnowledge(manager, "articles", first.ID, "unpublish", store.KnowledgeChange{Version: first.Version}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	check(store.KnowledgeFilter{FavoritePatientID: patient.Patient.ID}, 0)
	if err := f.repo.SetKnowledgeFavorite(ctx, patient.Patient.ID, first.ID, false, f.now); err != nil {
		t.Fatal("cannot remove unavailable favorite", err)
	}
}
