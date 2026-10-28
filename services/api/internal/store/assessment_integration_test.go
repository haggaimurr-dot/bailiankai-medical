package store_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/store"
)

type assessmentProvider struct{}

func assessmentConfig() store.AssessmentPipelineConfig {
	return store.AssessmentPipelineConfig{CheckerVersion: "synthetic-test-checker-v1", ThresholdProfileVersion: "synthetic-test-profile-v1", Model: store.AssessmentModel{Provider: "controlled-test", ModelID: "synthetic-observer", ModelVersion: "1", DeploymentVersion: "test-1", PromptVersion: "test-prompt-1", AdapterVersion: "gateway-v1"}}
}
func (assessmentProvider) Check(_ context.Context, input store.AssessmentFrozenInput, _ map[string][]byte) (map[string]store.AssessmentQuality, error) {
	out := map[string]store.AssessmentQuality{}
	checkedAt := time.Now()
	for _, p := range input.Photos {
		checks := []store.AssessmentQualityItem{}
		for _, code := range []string{"encoding", "dimensions", "face", "clarity", "exposure", "occlusion", "angle"} {
			checks = append(checks, store.AssessmentQualityItem{Code: code, Status: "pass", Message: "Synthetic test fixture only"})
		}
		out[p.ID] = store.AssessmentQuality{Status: "passed", CheckerVersion: input.Configuration.CheckerVersion, ThresholdProfileVersion: input.Configuration.ThresholdProfileVersion, CheckedAt: &checkedAt, Checks: checks, Issues: []string{}, RetakeAdvice: []string{}}
	}
	return out, nil
}
func (assessmentProvider) Analyze(_ context.Context, input store.AssessmentFrozenInput, _ map[string][]byte, _ string) (store.AssessmentAnalysis, string, error) {
	return store.AssessmentAnalysis{AssessmentContent: store.AssessmentContent{Summary: "Synthetic engineering observation, no clinical meaning", Observations: []store.AssessmentObservation{{ID: "observation-1", Code: "surface_irregularity", Description: "Synthetic visible texture", Region: "unknown", Certainty: "unknown", Evidence: []store.AssessmentEvidence{{PhotoID: input.Photos[0].ID, Angle: input.Photos[0].Angle}}}}, Limitations: []string{"Synthetic fixtures cannot evaluate clinical performance"}, ConsultationAdvice: store.AssessmentAdvice{Recommended: true, Reason: "Engineering test only", Preparation: []string{}}, PlainSummary: "Synthetic plain-language summary, no clinical meaning", CareAdvice: []string{"Synthetic care step, no clinical meaning"}}, SchemaVersion: "assessment-result-v1", Model: input.Configuration.Model, Outcome: "observations_available", RequiresHumanReview: true}, "controlled-request-1", nil
}

type aiFixture struct {
	session      store.SessionResult
	f            *bookingFixture
	service      *store.AssessmentService
	media        store.Media
	patient      string
	now          time.Time
	manager      context.Context
	doctor       context.Context
	secondDoctor context.Context
}

func newAIFixture(t *testing.T) *aiFixture {
	f := newBookingFixture(t)
	patient := f.patient()
	media, err := store.NewPrivateMedia(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := store.WithClinicAssignment(context.Background(), []string{f.slot.ClinicID})
	manager, err := f.repo.CreateEmployee(ctx, "ai-manager", "AI验收主管", "AssessmentTest123!", "manager", "", f.now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.repo.CreateEmployee(ctx, "ai-doctor-a", "AI验收医生A", "AssessmentTest123!", "doctor", "doctor-demo-1", f.now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.repo.CreateEmployee(ctx, "ai-doctor-b", "AI验收医生B", "AssessmentTest123!", "doctor", "doctor-demo-2", f.now)
	if err != nil {
		t.Fatal(err)
	}
	return &aiFixture{session: patient, f: f, media: media, service: store.NewAssessmentService(f.repo, media, assessmentProvider{}, assessmentConfig()), patient: patient.Patient.ID, now: f.now, manager: store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "manager", "", []string{f.slot.ClinicID}), manager.ID), doctor: store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "doctor", *first.StaffID, []string{f.slot.ClinicID}), first.ID), secondDoctor: store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "doctor", *second.StaffID, []string{f.slot.ClinicID}), second.ID)}
}
func (a *aiFixture) create(t *testing.T) store.AssessmentView {
	t.Helper()
	a.now = a.now.Add(6 * time.Second)
	view, _, err := a.service.Create(context.Background(), a.patient, store.AssessmentCreate{ClinicID: a.f.slot.ClinicID, RequestKey: "create-" + a.now.Format("150405.000")}, a.now)
	// The public request key grammar deliberately excludes punctuation.
	if errors.Is(err, store.ErrAssessmentInput) {
		view, _, err = a.service.Create(context.Background(), a.patient, store.AssessmentCreate{ClinicID: a.f.slot.ClinicID, RequestKey: "create-" + a.now.Format("150405000")}, a.now)
	}
	if err != nil {
		t.Fatal(err)
	}
	return view
}
func syntheticAssessmentPhoto(t *testing.T, angle string, index int) (store.AssessmentPhoto, []byte) {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, 512, 512))
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			canvas.Set(x, y, color.RGBA{R: uint8((x + index*50) % 255), G: uint8((y + index*70) % 255), B: uint8(index * 60), A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, canvas, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	hash := sha256.Sum256(data)
	return store.AssessmentPhoto{ID: "photo-ai-" + angle + "-" + hex.EncodeToString(hash[:8]), Angle: angle, Width: 512, Height: 512, MediaType: "image/jpeg", SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}, data
}
func (a *aiFixture) prepared(t *testing.T) store.AssessmentView {
	t.Helper()
	view := a.create(t)
	var err error
	view, err = a.service.Change(context.Background(), view.Task.ID, a.patient, "ai-grant", "consent", store.AssessmentChange{Version: view.Task.Version, PolicyVersion: store.AssessmentConsentVersion, Granted: true}, a.now)
	if err != nil {
		t.Fatal(err)
	}
	for i, angle := range []string{"front", "left", "right"} {
		photo, data := syntheticAssessmentPhoto(t, angle, i+1)
		photo.ID = "photo-ai-" + strings.TrimPrefix(view.Task.ID, "assessment-") + "-" + angle
		view, err = a.service.Upload(context.Background(), view.Task.ID, a.patient, "upload-"+angle, view.Task.Version, photo, data, a.now)
		if err != nil {
			t.Fatal(err)
		}
	}
	job, err := a.service.Claim(context.Background(), true, a.now)
	if err != nil || job == nil {
		t.Fatal(job, err)
	}
	qualities, _ := assessmentProvider{}.Check(context.Background(), job.Input, nil)
	if err := a.service.FinishQuality(context.Background(), *job, qualities, nil, a.now); err != nil {
		t.Fatal(err)
	}
	view, err = a.service.Get(context.Background(), view.Task.ID, a.patient)
	if err != nil {
		t.Fatal(err)
	}
	return view
}
func (a *aiFixture) queued(t *testing.T) store.AssessmentView {
	t.Helper()
	view := a.prepared(t)
	view, err := a.service.Change(context.Background(), view.Task.ID, a.patient, "submit", "submit", store.AssessmentChange{Version: view.Task.Version}, a.now)
	if err != nil {
		t.Fatal(err)
	}
	return view
}
func (a *aiFixture) analyzed(t *testing.T) (store.AssessmentView, store.AssessmentJob) {
	t.Helper()
	view := a.queued(t)
	job, err := a.service.Claim(context.Background(), false, a.now)
	if err != nil || job == nil {
		t.Fatal(job, err)
	}
	result, requestID, _ := assessmentProvider{}.Analyze(context.Background(), job.Input, nil, job.Attempt.ID)
	if err := a.service.Finish(context.Background(), *job, result, requestID, nil, a.now); err != nil {
		t.Fatal(err)
	}
	view, err = a.service.Get(a.manager, view.Task.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	return view, *job
}

func TestAssessmentMySQLLifecycleAndProjection(t *testing.T) {
	a := newAIFixture(t)
	view, job := a.analyzed(t)
	patientView, err := a.service.Get(context.Background(), view.Task.ID, a.patient)
	if err != nil || patientView.Result != nil || patientView.PublishedReview != nil || len(patientView.Attempts) != 0 || patientView.Task.ReviewerEmployeeID != "" {
		t.Fatalf("unreviewed projection leaked: %+v %v", patientView, err)
	}
	input := store.AssessmentChange{Version: view.Task.Version, ReviewerEmployeeID: ""}
	// Resolve the actual account id without exposing credentials.
	reviewers, err := a.service.Reviewers(a.manager, view.Task.ClinicID)
	if err != nil || len(reviewers) != 2 {
		t.Fatal(reviewers, err)
	}
	for _, r := range reviewers {
		if r.Username == "ai-doctor-a" {
			input.ReviewerEmployeeID = r.ID
		}
	}
	view, err = a.service.Change(a.manager, view.Task.ID, "", "assign", "assignment", input, a.now)
	if err != nil {
		t.Fatal(err)
	}
	review := store.AssessmentChange{Version: view.Task.Version, ResultID: view.Result.ID, Decision: "approved", Note: "Synthetic physician review"}
	if _, err := a.service.Change(a.manager, view.Task.ID, "", "manager-review", "review", review, a.now); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("manager reviewed: %v", err)
	}
	view, err = a.service.Change(a.doctor, view.Task.ID, "", "review-1", "review", review, a.now)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := a.service.Change(a.doctor, view.Task.ID, "", "review-1", "review", review, a.now)
	if err != nil || len(replay.Reviews) != 1 {
		t.Fatal(replay, err)
	}
	patientView, err = a.service.Get(context.Background(), view.Task.ID, a.patient)
	if err != nil || patientView.PublishedReview == nil || patientView.PublishedReview.FinalContent == nil || patientView.Result != nil || len(patientView.Reviews) != 0 {
		t.Fatal(patientView, err)
	}
	encoded, _ := json.Marshal(patientView)
	for _, forbidden := range []string{"reviewedByEmployeeId", "providerRequestId", "modelVersion", "inputSnapshotId", "reviewerEmployeeId"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("patient received %s", forbidden)
		}
	}
	original := view.Result.Analysis.Summary
	content := view.Result.Analysis.AssessmentContent
	content.Summary = "Synthetic corrected summary"
	view, err = a.service.Change(a.doctor, view.Task.ID, "", "review-2", "review", store.AssessmentChange{Version: view.Task.Version, ResultID: view.Result.ID, Decision: "amended", Note: "Synthetic correction reason", FinalContent: &content}, a.now)
	if err != nil || len(view.Reviews) != 2 || view.Result.Analysis.Summary != original || view.PublishedReview.Version != 2 {
		t.Fatal(view, err)
	}
	if err := a.service.Finish(context.Background(), job, view.Result.Analysis, "controlled-request-1", nil, a.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("late response after review: %v", err)
	}
}
func TestAssessmentMySQLAdmissionAndIdempotency(t *testing.T) {
	a := newAIFixture(t)
	a.now = a.now.Add(6 * time.Second)
	in := store.AssessmentCreate{ClinicID: a.f.slot.ClinicID, RequestKey: "concurrent-create"}
	var wg sync.WaitGroup
	ids := make(chan string, 10)
	failures := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			view, _, err := a.service.Create(context.Background(), a.patient, in, a.now)
			ids <- view.Task.ID
			failures <- err
		}()
	}
	wg.Wait()
	close(ids)
	close(failures)
	id := ""
	for value := range ids {
		if id != "" && id != value {
			t.Fatal("duplicate task")
		}
		id = value
	}
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	in.AppointmentID = "other"
	if _, _, err := a.service.Create(context.Background(), a.patient, in, a.now); !errors.Is(err, store.ErrIdempotency) {
		t.Fatal(err)
	}
	view := a.prepared(t)
	version := view.Task.Version
	errorsOut := make(chan error, 10)
	wg = sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.service.Change(context.Background(), view.Task.ID, a.patient, "concurrent-submit", "submit", store.AssessmentChange{Version: version}, a.now)
			errorsOut <- err
		}()
	}
	wg.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := a.f.db.Model(&store.AssessmentInputSnapshot{}).Where("assessment_id = ?", view.Task.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("snapshots=%d %v", count, err)
	}
	second := a.prepared(t)
	if _, err := a.service.Change(context.Background(), second.Task.ID, a.patient, "second-submit", "submit", store.AssessmentChange{Version: second.Task.Version}, a.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("parallel patient queue: %v", err)
	}
}
func TestAssessmentMySQLLeaseRecoveryRetriesAndLateResults(t *testing.T) {
	a := newAIFixture(t)
	view := a.queued(t)
	job, err := a.service.Claim(context.Background(), false, a.now)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	restarted := store.NewAssessmentService(a.f.repo, a.media, assessmentProvider{}, assessmentConfig())
	if err := restarted.Recover(context.Background(), a.now.Add(91*time.Second)); err != nil {
		t.Fatal(err)
	}
	next, err := restarted.Claim(context.Background(), false, a.now.Add(102*time.Second))
	if err != nil || next == nil || next.Attempt.AttemptNo != 2 {
		t.Fatal(next, err)
	}
	result, requestID, _ := assessmentProvider{}.Analyze(context.Background(), job.Input, nil, job.Attempt.ID)
	if err := a.service.Finish(context.Background(), *job, result, requestID, nil, a.now.Add(103*time.Second)); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("late old lease accepted: %v", err)
	}
	if err := restarted.Finish(context.Background(), *next, store.AssessmentAnalysis{}, "", &store.AssessmentProviderError{Code: "MODEL_RATE_LIMITED", Retryable: true, RetryAfter: 60 * time.Second}, a.now.Add(103*time.Second)); err != nil {
		t.Fatal(err)
	}
	if early, err := restarted.Claim(context.Background(), false, a.now.Add(150*time.Second)); err != nil || early != nil {
		t.Fatal(early, err)
	}
	last, err := restarted.Claim(context.Background(), false, a.now.Add(164*time.Second))
	if err != nil || last == nil || last.Attempt.AttemptNo != 3 {
		t.Fatal(last, err)
	}
	if err := restarted.Finish(context.Background(), *last, store.AssessmentAnalysis{}, "", &store.AssessmentProviderError{Code: "MODEL_TIMEOUT", Retryable: true}, a.now.Add(165*time.Second)); err != nil {
		t.Fatal(err)
	}
	view, err = restarted.Get(context.Background(), view.Task.ID, a.patient)
	if err != nil || view.Task.Status != "failed" || view.Task.AttemptCount != 3 || view.Task.Retryable {
		t.Fatal(view, err)
	}
	if _, err := restarted.Change(context.Background(), view.Task.ID, a.patient, "bypass-limit", "retry", store.AssessmentChange{Version: view.Task.Version}, a.now.Add(166*time.Second)); !errors.Is(err, store.ErrCareState) {
		t.Fatal(err)
	}
}
func TestAssessmentMySQLWithdrawalIsolationAndMediaReferences(t *testing.T) {
	a := newAIFixture(t)
	view := a.queued(t)
	other := a.f.patient()
	if _, err := a.service.Get(context.Background(), view.Task.ID, other.Patient.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	job, err := a.service.Claim(context.Background(), false, a.now)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	view, err = a.service.Get(context.Background(), view.Task.ID, a.patient)
	if err != nil {
		t.Fatal(err)
	}
	view, err = a.service.Change(context.Background(), view.Task.ID, a.patient, "withdraw", "consent", store.AssessmentChange{Version: view.Task.Version, PolicyVersion: store.AssessmentConsentVersion, Granted: false}, a.now)
	if err != nil {
		t.Fatal(err)
	}
	result, requestID, _ := assessmentProvider{}.Analyze(context.Background(), job.Input, nil, job.Attempt.ID)
	if err := a.service.Finish(context.Background(), *job, result, requestID, nil, a.now); !errors.Is(err, store.ErrAssessmentConsent) {
		t.Fatal(err)
	}
	staffView, err := a.service.Get(a.manager, view.Task.ID, "")
	if err != nil || staffView.Result != nil || len(staffView.Photos) != 0 || len(staffView.Reviews) != 0 {
		t.Fatal(staffView, err)
	}
	if _, err := a.service.Photo(a.manager, view.Task.ID, view.Photos[0].ID, ""); !errors.Is(err, store.ErrAssessmentConsent) {
		t.Fatal(err)
	}
	if _, err := a.service.Photo(context.Background(), view.Task.ID, view.Photos[0].ID, a.patient); err != nil {
		t.Fatal(err)
	}
	refs, err := a.f.repo.MediaReferences(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, photo := range view.Photos {
		found := false
		for _, r := range refs {
			found = found || r.ID == photo.ID
		}
		if !found {
			t.Fatal("frozen input lost reference")
		}
	}
	if _, err := a.service.Change(context.Background(), view.Task.ID, a.patient, "regrant", "consent", store.AssessmentChange{Version: view.Task.Version, PolicyVersion: store.AssessmentConsentVersion, Granted: true}, a.now); !errors.Is(err, store.ErrCareState) {
		t.Fatal(err)
	}
}
func TestAssessmentMySQLUnavailableQualityAndInvalidResult(t *testing.T) {
	a := newAIFixture(t)
	view := a.create(t)
	unconfigured := store.NewAssessmentService(a.f.repo, a.media, nil, store.AssessmentPipelineConfig{})
	view, err := unconfigured.Change(context.Background(), view.Task.ID, a.patient, "grant-disabled", "consent", store.AssessmentChange{Version: view.Task.Version, PolicyVersion: store.AssessmentConsentVersion, Granted: true}, a.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unconfigured.Change(context.Background(), view.Task.ID, a.patient, "no-model", "submit", store.AssessmentChange{Version: view.Task.Version}, a.now); !errors.Is(err, store.ErrAssessmentConfiguration) {
		t.Fatal(err)
	}
	prepared := a.prepared(t)
	if _, err := a.service.Change(context.Background(), prepared.Task.ID, a.patient, "no-quality", "quality-check", store.AssessmentChange{Version: prepared.Task.Version}, a.now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	qualityJob, err := a.service.Claim(context.Background(), true, a.now.Add(6*time.Second))
	if err != nil || qualityJob == nil {
		t.Fatal(err)
	}
	if err := a.service.FinishQuality(context.Background(), *qualityJob, nil, errors.New("unavailable"), a.now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	prepared, err = a.service.Get(context.Background(), prepared.Task.ID, a.patient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.service.Change(context.Background(), prepared.Task.ID, a.patient, "reject-quality", "submit", store.AssessmentChange{Version: prepared.Task.Version}, a.now); !errors.Is(err, store.ErrAssessmentQuality) {
		t.Fatal(err)
	}
	queued := a.queued(t)
	job, err := a.service.Claim(context.Background(), false, a.now)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	analysis, requestID, _ := assessmentProvider{}.Analyze(context.Background(), job.Input, nil, job.Attempt.ID)
	analysis.Observations[0].Evidence[0].PhotoID = "another-patient-photo"
	if err := a.service.Finish(context.Background(), *job, analysis, requestID, nil, a.now); err != nil {
		t.Fatal(err)
	}
	queued, err = a.service.Get(context.Background(), queued.Task.ID, a.patient)
	if err != nil || queued.Task.Status != "failed" || queued.Task.FailureCode != "MODEL_OUTPUT_INVALID" || queued.Task.Retryable {
		t.Fatal(queued, err)
	}
}
