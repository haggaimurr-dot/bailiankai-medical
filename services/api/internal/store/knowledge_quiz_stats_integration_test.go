package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func quizTestFixture(t *testing.T) (*bookingFixture, context.Context, context.Context) {
	t.Helper()
	f := newBookingFixture(t)
	ctx := context.Background()
	manager := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"}), "manager-quiz-test")
	return f, ctx, manager
}

func createQuizCategory(t *testing.T, f *bookingFixture, manager context.Context) model.KnowledgeCategory {
	t.Helper()
	value, err := f.repo.ChangeKnowledge(manager, "categories", "", "create", store.KnowledgeChange{
		ClinicID: "clinic-demo", Name: "知识库", Active: true, SortOrder: 1,
	}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	return value.(model.KnowledgeCategory)
}

func quizSample() []model.KnowledgeQuizQuestion {
	return []model.KnowledgeQuizQuestion{
		{Question: "痘坑能自己长平吗？", Options: []string{"能", "不能", "看情况"}, AnswerIndex: 2, Explanation: "真性痘坑需医学干预。"},
		{Question: "防晒对痘印重要吗？", Options: []string{"重要", "不重要"}, AnswerIndex: 0},
	}
}

// TestKnowledgeQuizVideoLifecycleMySQL covers row 91 （短视频科普与看完测一测）:
// video URL + quiz survive create/update, publish snapshots them, the public
// detail exposes both, and the public list strips the quiz (answers must not
// leak in list responses).
func TestKnowledgeQuizVideoLifecycleMySQL(t *testing.T) {
	f, ctx, manager := quizTestFixture(t)
	doctor := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "doctor", "doctor-demo-1", []string{"clinic-demo"}), "doctor-quiz-test")
	category := createQuizCategory(t, f, manager)

	articleInput := func(quiz []model.KnowledgeQuizQuestion) store.KnowledgeChange {
		return store.KnowledgeChange{
			CategoryID: category.ID, Title: "视频科普", Summary: "摘要", Body: "正文",
			SourceText: "来源", VideoURL: "https://example.com/video.mp4", Quiz: quiz, SortOrder: 1,
		}
	}
	value, err := f.repo.ChangeKnowledge(manager, "articles", "", "create", articleInput(quizSample()), f.now)
	if err != nil {
		t.Fatal(err)
	}
	article := value.(model.KnowledgeArticle)
	if article.VideoURL != "https://example.com/video.mp4" {
		t.Fatalf("video url not stored: %q", article.VideoURL)
	}

	// Update the quiz: change an answer and drop to one question.
	updated := quizSample()[:1]
	updated[0].AnswerIndex = 0
	in := articleInput(updated)
	in.Version = article.Version
	value, err = f.repo.ChangeKnowledge(manager, "articles", article.ID, "update", in, f.now)
	if err != nil {
		t.Fatal(err)
	}
	article = value.(model.KnowledgeArticle)

	// Review + publish.
	if _, err := f.repo.ChangeKnowledge(doctor, "articles", article.ID, "review", store.KnowledgeChange{Version: article.Version, Decision: "approved"}, f.now); err != nil {
		t.Fatal("review:", err)
	}
	article = mustKnowledgeArticle(t, f, manager, article.ID)
	if _, err := f.repo.ChangeKnowledge(manager, "articles", article.ID, "publish", store.KnowledgeChange{Version: article.Version}, f.now); err != nil {
		t.Fatal("publish:", err)
	}

	// Public detail exposes video + quiz.
	pub, err := f.repo.KnowledgeArticle(ctx, article.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	pa := pub.(model.KnowledgePublicArticle)
	if pa.VideoURL != "https://example.com/video.mp4" {
		t.Fatalf("detail video url: %q", pa.VideoURL)
	}
	if len(pa.Quiz) != 1 || pa.Quiz[0].Question != "痘坑能自己长平吗？" || pa.Quiz[0].AnswerIndex != 0 || pa.Quiz[0].Explanation != "真性痘坑需医学干预。" {
		t.Fatalf("detail quiz mismatch: %+v", pa.Quiz)
	}

	// Public list strips the quiz but keeps the video URL.
	rows, _, err := f.repo.KnowledgeArticles(ctx, store.KnowledgeFilter{Public: true, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows.([]model.KnowledgePublicArticle) {
		if row.ID == article.ID {
			found = true
			if row.Quiz != nil {
				t.Fatal("list must not expose quiz answers")
			}
			if row.VideoURL != "https://example.com/video.mp4" {
				t.Fatalf("list video url: %q", row.VideoURL)
			}
		}
	}
	if !found {
		t.Fatal("published article missing from public list")
	}
}

func mustKnowledgeArticle(t *testing.T, f *bookingFixture, manager context.Context, id string) model.KnowledgeArticle {
	t.Helper()
	value, err := f.repo.KnowledgeArticle(manager, id, false)
	if err != nil {
		t.Fatal(err)
	}
	return value.(model.KnowledgeArticle)
}

// TestKnowledgeOpenStatsMySQL covers row 116 （内容阅读数据统计）: the
// fire-and-forget beacon records only published snapshots, unknown articles
// are ignored, and stats aggregate totals, unique patients and per-article
// rows with clinic scoping.
func TestKnowledgeOpenStatsMySQL(t *testing.T) {
	f, ctx, manager := quizTestFixture(t)
	doctor := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "doctor", "doctor-demo-1", []string{"clinic-demo"}), "doctor-stats-test")
	category := createQuizCategory(t, f, manager)

	mkPublished := func(title string) string {
		t.Helper()
		value, err := f.repo.ChangeKnowledge(manager, "articles", "", "create", store.KnowledgeChange{
			CategoryID: category.ID, Title: title, Summary: "s", Body: "b", SourceText: "src", SortOrder: 1,
		}, f.now)
		if err != nil {
			t.Fatal(err)
		}
		article := value.(model.KnowledgeArticle)
		if _, err := f.repo.ChangeKnowledge(doctor, "articles", article.ID, "review", store.KnowledgeChange{Version: article.Version, Decision: "approved"}, f.now); err != nil {
			t.Fatal(err)
		}
		article = mustKnowledgeArticle(t, f, manager, article.ID)
		if _, err := f.repo.ChangeKnowledge(manager, "articles", article.ID, "publish", store.KnowledgeChange{Version: article.Version}, f.now); err != nil {
			t.Fatal(err)
		}
		return article.ID
	}
	publishedID := mkPublished("统计文章")

	// Draft article: opens must not count.
	draftValue, err := f.repo.ChangeKnowledge(manager, "articles", "", "create", store.KnowledgeChange{
		CategoryID: category.ID, Title: "草稿", Summary: "s", Body: "b", SourceText: "src", SortOrder: 2,
	}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	draftID := draftValue.(model.KnowledgeArticle).ID
	defer func() {
		f.db.Exec("DELETE FROM knowledge_article_opens")
	}()

	record := func(articleID, patientID string) {
		t.Helper()
		if err := f.repo.RecordKnowledgeOpen(ctx, articleID, "clinic-demo", patientID, f.now); err != nil {
			t.Fatal(err)
		}
	}
	record(publishedID, "patient-1")
	record(publishedID, "patient-1")
	record(publishedID, "patient-2")
	record(publishedID, "") // anonymous
	record(draftID, "patient-1")
	if err := f.repo.RecordKnowledgeOpen(ctx, "no-such-article", "clinic-demo", "patient-1", f.now); err != nil {
		t.Fatal("unknown article must be ignored without error:", err)
	}

	stats, err := f.repo.KnowledgeOpenStats(ctx, "", f.now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 4 {
		t.Fatalf("total opens: %d want 4 (draft excluded)", stats.Total)
	}
	if stats.UniquePatients != 2 {
		t.Fatalf("unique patients: %d want 2 (anonymous excluded)", stats.UniquePatients)
	}
	if len(stats.Articles) != 1 || stats.Articles[0].ArticleID != publishedID || stats.Articles[0].Opens != 4 || stats.Articles[0].UniquePatients != 2 {
		t.Fatalf("per-article stats: %+v", stats.Articles)
	}

	scoped, err := f.repo.KnowledgeOpenStats(ctx, "other-clinic", f.now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if scoped.Total != 0 || len(scoped.Articles) != 0 {
		t.Fatalf("clinic scope leak: %+v", scoped)
	}
}

// TestCaseCourseFilterMySQL covers row 92 （按疗程筛选）: the course query
// filter matches JSON content course exactly, and empty/non-matching values
// behave correctly.
func TestCaseCourseFilterMySQL(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	patient := f.patient()
	appt := f.create(patient, "case-course-booking-001")
	mk := func(id, course string) {
		t.Helper()
		content, _ := json.Marshal(map[string]any{"title": id, "course": course, "category": "c", "tags": []string{"痘坑"}})
		now := f.now
		if err := f.db.Create(&model.CaseRecord{
			ID: id, AppointmentID: appt.ID, ClinicID: "clinic-demo", PatientID: patient.Patient.ID,
			ContentJSON: string(content), Version: 1, Revision: 1, ConsentRevision: 1,
			ReviewStatus: "approved", PublishedAt: &now,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	mk("case-course-1", "第 2 疗程")
	mk("case-course-2", "第 1 疗程")
	defer f.db.Exec("DELETE FROM case_records WHERE id LIKE 'case-course-%'")

	rows, total, err := f.repo.Cases(ctx, "patient", patient.Patient.ID, store.CaseFilter{Course: "第 2 疗程", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 || rows[0].Content.Course != "第 2 疗程" {
		t.Fatalf("course filter: total=%d rows=%+v", total, rows)
	}
	rows, total, err = f.repo.Cases(ctx, "patient", patient.Patient.ID, store.CaseFilter{Page: 1, PageSize: 20})
	if err != nil || total != 2 {
		t.Fatalf("no filter: total=%d err=%v", total, err)
	}
	rows, total, err = f.repo.Cases(ctx, "patient", patient.Patient.ID, store.CaseFilter{Course: "第 9 疗程", Page: 1, PageSize: 20})
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatalf("non-matching course: total=%d err=%v", total, err)
	}
}
