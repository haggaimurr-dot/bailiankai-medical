package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func TestAssessmentMySQLQualityRejectsAndFrozenVersions(t *testing.T) {
	for _, failure := range []string{"face", "clarity", "exposure", "occlusion", "angle"} {
		t.Run(failure, func(t *testing.T) {
			a := newAIFixture(t)
			view := a.prepared(t)
			a.now = a.now.Add(6 * time.Second)
			if _, err := a.service.Change(context.Background(), view.Task.ID, a.patient, "recheck", "quality-check", store.AssessmentChange{Version: view.Task.Version}, a.now); err != nil {
				t.Fatal(err)
			}
			job, err := a.service.Claim(context.Background(), true, a.now)
			if err != nil || job == nil {
				t.Fatal(err)
			}
			qualities, _ := assessmentProvider{}.Check(context.Background(), job.Input, nil)
			for id, q := range qualities {
				q.Status = "rejected"
				q.Issues = []string{failure + "-unknown"}
				q.RetakeAdvice = []string{"Synthetic retake advice"}
				for i := range q.Checks {
					if q.Checks[i].Code == failure {
						q.Checks[i].Status = "unknown"
					}
				}
				qualities[id] = q
			}
			if err := a.service.FinishQuality(context.Background(), *job, qualities, nil, a.now); err != nil {
				t.Fatal(err)
			}
			view, err = a.service.Get(context.Background(), view.Task.ID, a.patient)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.service.Change(context.Background(), view.Task.ID, a.patient, "quality-submit", "submit", store.AssessmentChange{Version: view.Task.Version}, a.now); !errors.Is(err, store.ErrAssessmentQuality) {
				t.Fatal(err)
			}
			if view.Photos[0].QualityCheck.CheckerVersion != assessmentConfig().CheckerVersion || len(view.Photos[0].QualityCheck.RetakeAdvice) == 0 {
				t.Fatal(view)
			}
		})
	}
}
func TestAssessmentMySQLReplacementDuplicateAndUnreviewedContent(t *testing.T) {
	a := newAIFixture(t)
	view := a.prepared(t)
	photo, data := syntheticAssessmentPhoto(t, "front", 7)
	photo.ID = "photo-ai-replacement"
	view, err := a.service.Upload(context.Background(), view.Task.ID, a.patient, "replace-front", view.Task.Version, photo, data, a.now)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range view.Photos {
		if p.Angle == "front" && p.QualityCheck.Status != "pending" {
			t.Fatal("replacement reused quality")
		}
	}
	duplicate := photo
	duplicate.ID = "photo-ai-duplicate"
	duplicate.Angle = "left"
	if _, err := a.service.Upload(context.Background(), view.Task.ID, a.patient, "duplicate", view.Task.Version, duplicate, data, a.now); !errors.Is(err, store.ErrAssessmentQuality) {
		t.Fatal(err)
	}
	job, err := a.service.Claim(context.Background(), true, a.now)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	qualities, _ := assessmentProvider{}.Check(context.Background(), job.Input, nil)
	if err := a.service.FinishQuality(context.Background(), *job, qualities, nil, a.now); err != nil {
		t.Fatal(err)
	}
	view, err = a.service.Get(context.Background(), view.Task.ID, a.patient)
	if err != nil {
		t.Fatal(err)
	}
	view, err = a.service.Change(context.Background(), view.Task.ID, a.patient, "freeze", "submit", store.AssessmentChange{Version: view.Task.Version}, a.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.service.Upload(context.Background(), view.Task.ID, a.patient, "after-freeze", view.Task.Version, photo, data, a.now); !errors.Is(err, store.ErrAssessmentConsent) && !errors.Is(err, store.ErrCareState) {
		t.Fatal(err)
	}
	var snapshot store.AssessmentInputSnapshot
	if err := a.f.db.First(&snapshot, "assessment_id = ?", view.Task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(snapshot.ContentJSON), []byte(photo.SHA256)) {
		t.Fatal("snapshot not frozen")
	}
}
func TestAssessmentMySQLUpgradeInconclusiveAndReviewerIsolation(t *testing.T) {
	a := newAIFixture(t)
	view := a.queued(t)
	upgraded := assessmentConfig()
	upgraded.Model.ModelVersion = "2"
	upgraded.Model.DeploymentVersion = "test-2"
	serviceB := store.NewAssessmentService(a.f.repo, a.media, assessmentProvider{}, upgraded)
	job, err := serviceB.Claim(context.Background(), false, a.now)
	if err != nil || job == nil || job.Input.Configuration.Model.ModelVersion != "1" {
		t.Fatal(job, err)
	}
	analysis, requestID, _ := assessmentProvider{}.Analyze(context.Background(), job.Input, nil, "")
	analysis.Outcome = "inconclusive"
	analysis.Observations = []store.AssessmentObservation{}
	analysis.Summary = "Synthetic input cannot yield reliable observations"
	if err := serviceB.Finish(context.Background(), *job, analysis, requestID, nil, a.now); err != nil {
		t.Fatal(err)
	}
	restored := store.NewAssessmentService(a.f.repo, a.media, assessmentProvider{}, upgraded)
	view, err = restored.Get(a.manager, view.Task.ID, "")
	if err != nil || view.Result.Analysis.Outcome != "inconclusive" || len(view.Result.Analysis.Observations) != 0 {
		t.Fatal(view, err)
	}
	if _, err := a.service.Get(a.doctor, view.Task.ID, ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("unassigned doctor accessed", err)
	}
	reviewers, _ := a.service.Reviewers(a.manager, view.Task.ClinicID)
	first, second := "", ""
	for _, r := range reviewers {
		if r.Username == "ai-doctor-a" {
			first = r.ID
		} else {
			second = r.ID
		}
	}
	view, err = a.service.Change(a.manager, view.Task.ID, "", "assign-first", "assignment", store.AssessmentChange{Version: view.Task.Version, ReviewerEmployeeID: first}, a.now)
	if err != nil {
		t.Fatal(err)
	}
	view, err = a.service.Change(a.manager, view.Task.ID, "", "assign-second", "assignment", store.AssessmentChange{Version: view.Task.Version, ReviewerEmployeeID: second}, a.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.service.Get(a.doctor, view.Task.ID, ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("old doctor accessed", err)
	}
	denied := store.WithEmployeeActor(store.WithEmployeeScope(a.manager, "manager", "", []string{"other-clinic"}), "missing-employee")
	list, err := a.service.List(denied, "", store.AssessmentFilter{Page: 1, PageSize: 20})
	if !errors.Is(err, store.ErrNotFound) || list.Meta.Total != 0 {
		t.Fatal(list, err)
	}
	if err := a.f.db.Model(&model.FollowupStaff{}).Where("id = ?", "doctor-demo-2").Update("status", "off_duty").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := a.service.Get(a.secondDoctor, view.Task.ID, ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("off-duty doctor accessed", err)
	}
	a.service = serviceB
	newView := a.queued(t)
	newJob, err := serviceB.Claim(context.Background(), false, a.now)
	if err != nil || newJob == nil || newJob.Task.ID != newView.Task.ID || newJob.Input.Configuration.Model.ModelVersion != "2" {
		t.Fatal(newJob, err)
	}
}
func TestAssessmentMySQLMediaBackupRecoveryAndPrivacyWithdrawal(t *testing.T) {
	a := newAIFixture(t)
	view := a.queued(t)
	backup, err := store.NewPrivateMedia(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x33}, 32)
	snapshot := "snapshot-00000000000000000000000000000001"
	if count, err := store.BackupMedia(context.Background(), a.media, backup, a.f.repo, snapshot, key); err != nil || count != 3 {
		t.Fatal(count, err)
	}
	view, err = a.service.Change(context.Background(), view.Task.ID, a.patient, "withdraw-before-restore", "consent", store.AssessmentChange{Version: view.Task.Version, PolicyVersion: store.AssessmentConsentVersion, Granted: false}, a.now)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range view.Photos {
		if err := a.media.Delete(context.Background(), p.ID); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := store.RestoreMedia(context.Background(), backup, a.media, a.f.repo, snapshot, key, true); err != nil || count != 3 {
		t.Fatal(count, err)
	}
	if _, err := a.service.Photo(a.manager, view.Task.ID, view.Photos[0].ID, ""); !errors.Is(err, store.ErrAssessmentConsent) {
		t.Fatal("restore resurrected consent", err)
	}
	recovered, err := a.media.Read(context.Background(), view.Photos[0].ID)
	if err != nil || store.VerifyMedia(recovered, store.MediaReference{ID: view.Photos[0].ID, SHA256: view.Photos[0].SHA256, SizeBytes: view.Photos[0].SizeBytes}) != nil {
		t.Fatal(err)
	}
	other := a.queued(t)
	if _, err := a.f.repo.RecordPrivacyConsent(context.Background(), a.patient, store.PrivacyPolicyVersion, "revoked", a.now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	job, err := a.service.Claim(context.Background(), false, a.now.Add(2*time.Second))
	if err != nil || job != nil {
		t.Fatal(job, err)
	}
	other, err = a.service.Get(context.Background(), other.Task.ID, a.patient)
	if err != nil || other.Task.Status != "cancelled" || other.Task.AttemptCount != 0 {
		t.Fatal(other, err)
	}
}

func TestAssessmentMySQLPolicyVersionAndClinicIsolation(t *testing.T) {
	a := newAIFixture(t)
	view := a.prepared(t)
	if err := a.f.db.Model(&store.AssessmentConsentEvent{}).Where("assessment_id = ?", view.Task.ID).Update("policy_version", "old-ai-policy").Error; err != nil {
		t.Fatal(err)
	}
	view, err := a.service.Get(context.Background(), view.Task.ID, a.patient)
	if err != nil || view.Consent.Granted {
		t.Fatal("old consent remained current", view, err)
	}
	if _, err := a.service.Change(context.Background(), view.Task.ID, a.patient, "old-policy-submit", "submit", store.AssessmentChange{Version: view.Task.Version}, a.now); !errors.Is(err, store.ErrAssessmentConsent) {
		t.Fatal(err)
	}
	view, err = a.service.Change(context.Background(), view.Task.ID, a.patient, "new-policy-grant", "consent", store.AssessmentChange{Version: view.Task.Version, PolicyVersion: store.AssessmentConsentVersion, Granted: true}, a.now)
	if err != nil {
		t.Fatal(err)
	}
	other := model.Clinic{ID: "assessment-hidden-clinic", Name: "工程测试其他院区", DirectoryState: model.DirectoryState{Active: true, Version: 1}}
	data, _ := json.Marshal(other)
	published := string(data)
	other.PublishedJSON = &published
	if err := a.f.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	account, err := a.f.repo.CreateEmployee(store.WithClinicAssignment(context.Background(), []string{other.ID}), "ai-outside-manager", "工程测试其他主管", "AssessmentTest123!", "manager", "", a.now)
	if err != nil {
		t.Fatal(err)
	}
	outside := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "manager", "", []string{other.ID}), account.ID)
	list, err := a.service.List(outside, "", store.AssessmentFilter{Page: 1, PageSize: 20})
	if err != nil || list.Meta.Total != 0 || len(list.Data) != 0 {
		t.Fatal("scope count leaked", list, err)
	}
	if _, err := a.service.Get(outside, view.Task.ID, ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	appointment := a.f.create(a.session, "assessment-does-not-block-booking")
	if _, _, err := a.service.Create(context.Background(), a.patient, store.AssessmentCreate{ClinicID: other.ID, AppointmentID: appointment.ID, RequestKey: "cross-clinic-association"}, a.now.Add(6*time.Second)); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, err := a.service.Create(context.Background(), a.patient, store.AssessmentCreate{ClinicID: other.ID, SupersedesAssessmentID: view.Task.ID, RequestKey: "cross-clinic-retake"}, a.now.Add(6*time.Second)); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	// Withdrawal remains available to an old client without accepting new terms.
	if _, err := a.service.Change(context.Background(), view.Task.ID, a.patient, "old-client-revoke", "consent", store.AssessmentChange{Version: view.Task.Version, PolicyVersion: "old-ai-policy", Granted: false}, a.now); err != nil {
		t.Fatal(err)
	}
}
