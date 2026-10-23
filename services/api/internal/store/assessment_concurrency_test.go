package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/store"
	"gorm.io/gorm"
)

func TestAssessmentMySQLWorkerCompetitionPersistenceAndReviewConcurrency(t *testing.T) {
	a := newAIFixture(t)
	queued := a.queued(t)
	jobs := make(chan *store.AssessmentJob, 10)
	failures := make(chan error, 10)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, err := a.service.Claim(context.Background(), false, a.now)
			jobs <- job
			failures <- err
		}()
	}
	wg.Wait()
	close(jobs)
	close(failures)
	var job *store.AssessmentJob
	count := 0
	for candidate := range jobs {
		if candidate != nil {
			job = candidate
			count++
		}
	}
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatal("multiple active leases", count)
	}
	if err := a.service.Renew(context.Background(), *job, a.now.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := a.service.Recover(context.Background(), a.now.Add(95*time.Second)); err != nil {
		t.Fatal(err)
	}
	view, err := a.service.Get(context.Background(), queued.Task.ID, a.patient)
	if err != nil || view.Task.Status != "analyzing" || view.Task.AttemptCount != 1 {
		t.Fatal(view, err)
	}
	analysis, requestID, _ := assessmentProvider{}.Analyze(context.Background(), job.Input, nil, "")
	failResultWrites := true
	if err := a.f.db.Callback().Create().Before("gorm:create").Register("assessment_test_result_failure", func(tx *gorm.DB) {
		if failResultWrites && tx.Statement.Table == "assessment_results" {
			tx.AddError(errors.New("controlled persistence failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.f.db.Callback().Create().Remove("assessment_test_result_failure") })
	if err := a.service.Finish(context.Background(), *job, analysis, requestID, nil, a.now.Add(40*time.Second)); err == nil {
		t.Fatal("persistence failure ignored")
	}
	view, err = a.service.Get(context.Background(), queued.Task.ID, a.patient)
	if err != nil || view.Task.Status != "analyzing" {
		t.Fatal(view, err)
	}
	failResultWrites = false
	if err := a.service.Finish(context.Background(), *job, analysis, requestID, nil, a.now.Add(41*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := a.service.Finish(context.Background(), *job, analysis, requestID, nil, a.now.Add(42*time.Second)); err != nil {
		t.Fatal("same result replay failed", err)
	}
	view, err = a.service.Get(a.manager, queued.Task.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewers, _ := a.service.Reviewers(a.manager, view.Task.ClinicID)
	doctorID := ""
	for _, r := range reviewers {
		if r.Username == "ai-doctor-a" {
			doctorID = r.ID
		}
	}
	view, err = a.service.Change(a.manager, view.Task.ID, "", "assign-reviewer", "assignment", store.AssessmentChange{Version: view.Task.Version, ReviewerEmployeeID: doctorID}, a.now.Add(43*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	input := store.AssessmentChange{Version: view.Task.Version, ResultID: view.Result.ID, Decision: "approved", Note: "Controlled concurrent review"}
	errs := make(chan error, 2)
	wg = sync.WaitGroup{}
	for _, key := range []string{"concurrent-review-a", "concurrent-review-b"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			_, err := a.service.Change(a.doctor, view.Task.ID, "", key, "review", input, a.now.Add(44*time.Second))
			errs <- err
		}(key)
	}
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, store.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
}
func TestAssessmentMySQLMissingConsentImagesAndManualRetry(t *testing.T) {
	a := newAIFixture(t)
	view := a.create(t)
	photo, data := syntheticAssessmentPhoto(t, "front", 1)
	if _, err := a.service.Upload(context.Background(), view.Task.ID, a.patient, "without-ai", view.Task.Version, photo, data, a.now); !errors.Is(err, store.ErrAssessmentConsent) {
		t.Fatal(err)
	}
	if _, err := a.service.Change(context.Background(), view.Task.ID, a.patient, "wrong-version", "consent", store.AssessmentChange{Version: view.Task.Version, PolicyVersion: "old-ai-policy", Granted: true}, a.now); !errors.Is(err, store.ErrAssessmentInput) {
		t.Fatal(err)
	}
	view, err := a.service.Change(context.Background(), view.Task.ID, a.patient, "grant", "consent", store.AssessmentChange{Version: view.Task.Version, PolicyVersion: store.AssessmentConsentVersion, Granted: true}, a.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.service.Change(context.Background(), view.Task.ID, a.patient, "missing-photos", "submit", store.AssessmentChange{Version: view.Task.Version}, a.now); !errors.Is(err, store.ErrAssessmentQuality) {
		t.Fatal(err)
	}
	queued := a.queued(t)
	job, err := a.service.Claim(context.Background(), false, a.now)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	if err := a.media.Delete(context.Background(), job.Input.Photos[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.service.ReadInput(context.Background(), *job, a.now); err == nil {
		t.Fatal("analyzed missing bytes")
	}
	// A controlled failed/retryable fixture covers explicit manual recovery; it
	// shares the attempt budget with the automatic queue.
	if err := a.f.db.Model(&store.Assessment{}).Where("id = ?", queued.Task.ID).Updates(map[string]any{"status": "failed", "retryable": true, "lease_token": "", "lease_expires_at": nil}).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.f.db.Model(&store.AssessmentAttempt{}).Where("id = ?", job.Attempt.ID).Update("finished_at", a.now).Error; err != nil {
		t.Fatal(err)
	}
	queued, err = a.service.Get(context.Background(), queued.Task.ID, a.patient)
	if err != nil {
		t.Fatal(err)
	}
	input := store.AssessmentChange{Version: queued.Task.Version}
	retried, err := a.service.Change(context.Background(), queued.Task.ID, a.patient, "manual-retry", "retry", input, a.now)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := a.service.Change(context.Background(), queued.Task.ID, a.patient, "manual-retry", "retry", input, a.now)
	if err != nil || replay.Task.Version != retried.Task.Version {
		t.Fatal(replay, err)
	}
	next, err := a.service.Claim(context.Background(), false, a.now)
	if err != nil || next == nil || next.Attempt.AttemptNo != 2 {
		t.Fatal(next, err)
	}
}
