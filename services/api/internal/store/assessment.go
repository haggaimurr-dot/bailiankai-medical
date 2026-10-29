package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func assessmentText(value string, maximum int) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= maximum
}
func assessmentID(value string) bool { return value != "" && analyticsIDValid(value) }
func (s *AssessmentService) QualityAvailable() bool {
	return s.provider != nil && assessmentText(s.configuration.CheckerVersion, 200) && assessmentText(s.configuration.ThresholdProfileVersion, 200)
}
func (s *AssessmentService) ModelAvailable() bool {
	m := s.configuration.Model
	return s.QualityAvailable() && assessmentText(m.Provider, 200) && assessmentText(m.ModelID, 200) && assessmentText(m.ModelVersion, 200) && assessmentText(m.DeploymentVersion, 200) && assessmentText(m.PromptVersion, 200) && assessmentText(m.AdapterVersion, 200)
}

func assessmentPrivacy(tx *gorm.DB, patientID string) error {
	var consent model.PrivacyConsentEvent
	if err := tx.Where("patient_id = ?", patientID).Order("recorded_at DESC, id DESC").First(&consent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrAssessmentPrivacy
		}
		return err
	}
	if consent.PolicyVersion != PrivacyPolicyVersion || consent.Action != "accepted" {
		return ErrAssessmentPrivacy
	}
	return nil
}
func assessmentAdmin(ctx context.Context, tx *gorm.DB) (model.EmployeeAccount, error) {
	var account model.EmployeeAccount
	scope, ok := scopeFrom(ctx)
	if !ok || !scope.EnforceClinics || (scope.Role != "doctor" && scope.Role != "manager") {
		return account, ErrNotFound
	}
	if err := tx.First(&account, "id = ? AND active = TRUE", employeeActor(ctx)).Error; err != nil {
		return account, followupDBError(err)
	}
	if account.Role != scope.Role {
		return account, ErrNotFound
	}
	if account.Role == "doctor" {
		if account.StaffID == nil || scope.StaffID != *account.StaffID {
			return account, ErrNotFound
		}
		var staff model.FollowupStaff
		if err := tx.First(&staff, "id = ? AND role = 'doctor' AND status = 'on_duty'", *account.StaffID).Error; err != nil {
			return account, followupDBError(err)
		}
	}
	ids := []string{}
	for _, grant := range account.ClinicIDs {
		for _, id := range scope.ClinicIDs {
			if id == grant {
				ids = append(ids, id)
				break
			}
		}
	}
	account.ClinicIDs = ids
	return account, nil
}
func assessmentAccess(ctx context.Context, tx *gorm.DB, patientID string) (*gorm.DB, error) {
	q := tx.Model(&Assessment{})
	if patientID != "" {
		return q.Where("patient_id = ?", patientID), nil
	}
	account, err := assessmentAdmin(ctx, tx)
	if err != nil {
		return nil, err
	}
	q = q.Where("clinic_id IN ?", account.ClinicIDs)
	if account.Role == "doctor" {
		q = q.Where("reviewer_employee_id = ?", account.ID)
	}
	return q, nil
}

func assessmentAdminLock(ctx context.Context, tx *gorm.DB) (model.EmployeeAccount, error) {
	account, err := assessmentAdmin(ctx, tx)
	if err != nil {
		return account, err
	}
	// Staff transitions acquire staff before employee rows. Use the same order
	// so leaving duty cannot race a review or assignment eligibility check.
	if account.Role == "doctor" {
		var staff model.FollowupStaff
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&staff, "id = ? AND role = 'doctor' AND status = 'on_duty'", *account.StaffID).Error; err != nil {
			return account, followupDBError(err)
		}
	}
	var locked model.EmployeeAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ? AND active = TRUE", account.ID).Error; err != nil {
		return account, followupDBError(err)
	}
	return assessmentAdmin(ctx, tx)
}
func assessmentEvent(tx *gorm.DB, task Assessment, action, actor, detail string, now time.Time) error {
	return tx.Create(&AssessmentEvent{ID: randomID("ai-event-"), AssessmentID: task.ID, Action: action, ActorID: actor, Version: task.Version, Detail: detail, CreatedAt: now}).Error
}
func assessmentView(tx *gorm.DB, task Assessment, admin bool) (AssessmentView, error) {
	granted, err := assessmentGranted(tx, task)
	if err != nil {
		return AssessmentView{}, err
	}
	policy, err := currentAssessmentPolicy(tx)
	if err != nil {
		return AssessmentView{}, err
	}
	consent := AssessmentConsent{Version: policy.Version, Content: policy.Content, Granted: granted}
	if gv, err := latestConsentEventVersion(tx, task.ID); err != nil {
		return AssessmentView{}, err
	} else if gv != "" {
		consent.GrantedVersion = &gv
	}
	view := AssessmentView{Task: task, Photos: []AssessmentPhoto{}, Consent: consent}
	if !admin {
		view.Task.ReviewerEmployeeID = ""
	}
	// Withdrawal preserves history for the patient, but stops staff content access.
	if admin && !granted {
		return view, nil
	}
	if err := tx.Where("assessment_id = ? AND is_current = TRUE", task.ID).Order("angle").Find(&view.Photos).Error; err != nil {
		return view, err
	}
	for i := range view.Photos {
		if err := json.Unmarshal([]byte(view.Photos[i].QualityJSON), &view.Photos[i].QualityCheck); err != nil {
			return view, err
		}
	}
	if task.PublishedReviewID != "" && task.Status != "cancelled" {
		var review AssessmentReview
		if err := tx.First(&review, "id = ?", task.PublishedReviewID).Error; err != nil {
			return view, err
		}
		if review.FinalJSON != nil {
			review.FinalContent = &AssessmentContent{}
			if err := json.Unmarshal([]byte(*review.FinalJSON), review.FinalContent); err != nil {
				return view, err
			}
		}
		review.ReviewedByEmployeeID = ""
		review.SupersedesReviewID = ""
		review.AssessmentID = ""
		review.ResultID = ""
		view.PublishedReview = &AssessmentPublicReview{ID: review.ID, Version: review.Version, Decision: review.Decision, Note: review.Note, DoctorName: review.DoctorName, ReviewedAt: review.ReviewedAt, FinalContent: review.FinalContent}
	}
	if admin {
		if task.ResultID != "" {
			var result AssessmentResult
			if err := tx.First(&result, "id = ?", task.ResultID).Error; err != nil {
				return view, err
			}
			if err := json.Unmarshal([]byte(result.ContentJSON), &result.Analysis); err != nil {
				return view, err
			}
			view.Result = &result
		}
		view.Reviews = []AssessmentReview{}
		view.Attempts = []AssessmentAttempt{}
		if err := tx.Where("assessment_id = ?", task.ID).Order("version DESC").Find(&view.Reviews).Error; err != nil {
			return view, err
		}
		for i := range view.Reviews {
			if view.Reviews[i].FinalJSON != nil {
				view.Reviews[i].FinalContent = &AssessmentContent{}
				if err := json.Unmarshal([]byte(*view.Reviews[i].FinalJSON), view.Reviews[i].FinalContent); err != nil {
					return view, err
				}
			}
		}
		if err := tx.Where("assessment_id = ?", task.ID).Order("attempt_no").Find(&view.Attempts).Error; err != nil {
			return view, err
		}
	}
	return view, nil
}

// All task mutations lock the patient first, including worker mutations. This
// serializes queue admission across tasks and avoids task/patient lock inversion.
func (s *AssessmentService) locked(ctx context.Context, id, patientID string, fn func(*gorm.DB, *Assessment) error) error {
	return s.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q, err := assessmentAccess(ctx, tx, patientID)
		if err != nil {
			return err
		}
		var initial Assessment
		if err = q.First(&initial, "id = ?", id).Error; err != nil {
			return followupDBError(err)
		}
		var patient model.Patient
		if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&patient, "id = ?", initial.PatientID).Error; err != nil {
			return followupDBError(err)
		}
		q, err = assessmentAccess(ctx, tx, patientID)
		if err != nil {
			return err
		}
		var task Assessment
		if err = q.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", id).Error; err != nil {
			return followupDBError(err)
		}
		return fn(tx, &task)
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}
func (s *AssessmentService) Get(ctx context.Context, id, patientID string) (AssessmentView, error) {
	var view AssessmentView
	err := s.locked(ctx, id, patientID, func(tx *gorm.DB, task *Assessment) error {
		var err error
		view, err = assessmentView(tx, *task, patientID == "")
		if err != nil {
			return err
		}
		if patientID == "" {
			return assessmentEvent(tx, *task, "read", employeeActor(ctx), "", time.Now())
		}
		return nil
	})
	return view, err
}
func (s *AssessmentService) List(ctx context.Context, patientID string, filter AssessmentFilter) (AssessmentPage, error) {
	out := AssessmentPage{Data: []Assessment{}}
	if filter.Page < 1 || filter.Page > 100000 || filter.PageSize < 1 || filter.PageSize > 100 || (filter.Status != "" && !assessmentStatus(filter.Status)) || len(filter.Query) > 100 {
		return out, ErrAssessmentInput
	}
	q, err := assessmentAccess(ctx, s.repo.db.WithContext(ctx), patientID)
	if err != nil {
		return out, err
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.ClinicID != "" {
		q = q.Where("clinic_id = ?", filter.ClinicID)
	}
	if filter.Query != "" {
		q = q.Where("id = ?", filter.Query)
	}
	if err = q.Count(&out.Meta.Total).Error; err != nil {
		return out, err
	}
	out.Meta.Page = filter.Page
	out.Meta.PageSize = filter.PageSize
	err = q.Order("created_at DESC, id DESC").Limit(filter.PageSize).Offset((filter.Page - 1) * filter.PageSize).Find(&out.Data).Error
	if patientID != "" {
		for i := range out.Data {
			out.Data[i].ReviewerEmployeeID = ""
		}
	}
	return out, err
}
// ReviewStats 返回与任务队列同一口径的复核时限统计：待复核、已超时、
// 已复核数与平均复核时长（分析完成→复核完成）。口径与 List 共用 assessmentAccess，
// 因此主管看全部授权院区、医生只看自己被指派的任务；超时以数据库时间计算，避免时区解析偏差。
func (s *AssessmentService) ReviewStats(ctx context.Context) (AssessmentReviewStats, error) {
	out := AssessmentReviewStats{ReviewSLAHours: AssessmentReviewSLAHours}
	q, err := assessmentAccess(ctx, s.repo.db.WithContext(ctx), "")
	if err != nil {
		return out, err
	}
	var row struct {
		AwaitingReview int64
		Overdue        int64
		ReviewedCount  int64
		AvgReviewHours *float64
	}
	err = q.Select(`COUNT(CASE WHEN status = 'awaiting_review' THEN 1 END) AS awaiting_review,
		COUNT(CASE WHEN status = 'awaiting_review' AND analysis_completed_at IS NOT NULL AND analysis_completed_at < DATE_SUB(NOW(), INTERVAL ? HOUR) THEN 1 END) AS overdue,
		COUNT(CASE WHEN status IN ('completed', 'rejected') AND reviewed_at IS NOT NULL AND analysis_completed_at IS NOT NULL THEN 1 END) AS reviewed_count,
		AVG(CASE WHEN status IN ('completed', 'rejected') AND reviewed_at IS NOT NULL AND analysis_completed_at IS NOT NULL THEN TIMESTAMPDIFF(SECOND, analysis_completed_at, reviewed_at) / 3600.0 END) AS avg_review_hours`,
		AssessmentReviewSLAHours).Scan(&row).Error
	if err != nil {
		return out, err
	}
	out.AwaitingReview, out.Overdue, out.ReviewedCount, out.AvgReviewHours = row.AwaitingReview, row.Overdue, row.ReviewedCount, row.AvgReviewHours
	return out, nil
}

func assessmentStatus(status string) bool {
	switch status {
	case "draft", "queued", "analyzing", "retry_wait", "failed", "awaiting_review", "completed", "rejected", "cancelled":
		return true
	}
	return false
}
func (s *AssessmentService) Create(ctx context.Context, patientID string, in AssessmentCreate, now time.Time) (AssessmentView, bool, error) {
	var task Assessment
	var out AssessmentView
	replayed := false
	if !assessmentID(in.ClinicID) || !assessmentText(in.RequestKey, 64) || (in.AppointmentID != "" && !assessmentID(in.AppointmentID)) || (in.SupersedesAssessmentID != "" && !assessmentID(in.SupersedesAssessmentID)) {
		return out, false, ErrAssessmentInput
	}
	hash := assessmentHash(in)
	err := s.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var patient model.Patient
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&patient, "id = ?", patientID).Error; err != nil {
			return followupDBError(err)
		}
		err := tx.Where("patient_id = ? AND request_key = ?", patientID, in.RequestKey).First(&task).Error
		if err == nil {
			if task.RequestHash != hash {
				return ErrIdempotency
			}
			replayed = true
			var e error
			out, e = assessmentView(tx, task, false)
			return e
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := assessmentPrivacy(tx, patientID); err != nil {
			return err
		}
		var clinic model.Clinic
		if err := tx.First(&clinic, "id = ? AND active = TRUE AND published_json IS NOT NULL", in.ClinicID).Error; err != nil {
			return followupDBError(err)
		}
		if in.AppointmentID != "" {
			var count int64
			err := tx.Table("appointments").Joins("JOIN slots ON slots.id = appointments.slot_id").Where("appointments.id = ? AND appointments.patient_id = ? AND slots.clinic_id = ?", in.AppointmentID, patientID, in.ClinicID).Count(&count).Error
			if err != nil {
				return err
			}
			if count != 1 {
				return ErrNotFound
			}
		}
		if in.SupersedesAssessmentID != "" {
			var old Assessment
			if err := tx.First(&old, "id = ? AND patient_id = ? AND clinic_id = ?", in.SupersedesAssessmentID, patientID, in.ClinicID).Error; err != nil {
				return followupDBError(err)
			}
		}
		var count int64
		if err := tx.Model(&Assessment{}).Where("patient_id = ? AND created_at > ?", patientID, now.Add(-time.Hour)).Count(&count).Error; err != nil {
			return err
		}
		if count >= 10 {
			return ErrAssessmentRateLimit
		}
		if err := tx.Model(&Assessment{}).Where("patient_id = ? AND created_at > ?", patientID, now.Add(-5*time.Second)).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrAssessmentRateLimit
		}
		task = Assessment{ID: randomID("assessment-"), PatientID: patientID, ClinicID: in.ClinicID, AppointmentID: in.AppointmentID, SupersedesAssessmentID: in.SupersedesAssessmentID, RequestKey: in.RequestKey, RequestHash: hash, Status: "draft", Version: 1, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		if err := assessmentEvent(tx, task, "created", patientID, "", now); err != nil {
			return err
		}
		out, err = assessmentView(tx, task, false)
		return err
	})
	return out, replayed, err
}
// assessmentObservationEqual compares the reviewable fields of one observation
// (evidence is compared as the set of photo ids).
func assessmentObservationEqual(a, b AssessmentObservation) bool {
	if a.Code != b.Code || a.Description != b.Description || a.Region != b.Region || a.Certainty != b.Certainty || len(a.Evidence) != len(b.Evidence) {
		return false
	}
	photos := make(map[string]bool, len(a.Evidence))
	for _, e := range a.Evidence {
		photos[e.PhotoID] = true
	}
	for _, e := range b.Evidence {
		if !photos[e.PhotoID] {
			return false
		}
	}
	return true
}

// assessmentContentDiff summarises what a doctor changed against the AI
// original, for the review audit event. Empty means nothing changed.
func assessmentContentDiff(original, amended AssessmentContent) string {
	var parts []string
	if original.Summary != amended.Summary {
		parts = append(parts, "观察概要已改")
	}
	origObs := make(map[string]AssessmentObservation, len(original.Observations))
	for _, o := range original.Observations {
		origObs[o.ID] = o
	}
	added, removed, changed := 0, 0, 0
	seen := make(map[string]bool, len(amended.Observations))
	for _, o := range amended.Observations {
		seen[o.ID] = true
		if old, ok := origObs[o.ID]; !ok {
			added++
		} else if !assessmentObservationEqual(old, o) {
			changed++
		}
	}
	for _, o := range original.Observations {
		if !seen[o.ID] {
			removed++
		}
	}
	if added+removed+changed > 0 {
		parts = append(parts, fmt.Sprintf("观察新增%d条、删除%d条、修改%d条", added, removed, changed))
	}
	origLim, newLim := make(map[string]bool), make(map[string]bool)
	for _, l := range original.Limitations {
		origLim[l] = true
	}
	for _, l := range amended.Limitations {
		newLim[l] = true
	}
	limAdded, limRemoved := 0, 0
	for l := range newLim {
		if !origLim[l] {
			limAdded++
		}
	}
	for l := range origLim {
		if !newLim[l] {
			limRemoved++
		}
	}
	if limAdded+limRemoved > 0 {
		parts = append(parts, fmt.Sprintf("局限新增%d条、删除%d条", limAdded, limRemoved))
	}
	origAdvice, newAdvice := original.ConsultationAdvice, amended.ConsultationAdvice
	adviceChanged := origAdvice.Reason != newAdvice.Reason || origAdvice.Recommended != newAdvice.Recommended
	prepAdded, prepRemoved := 0, 0
	origPrep, newPrep := make(map[string]bool), make(map[string]bool)
	for _, p := range origAdvice.Preparation {
		origPrep[p] = true
	}
	for _, p := range newAdvice.Preparation {
		newPrep[p] = true
	}
	for p := range newPrep {
		if !origPrep[p] {
			prepAdded++
		}
	}
	for p := range origPrep {
		if !newPrep[p] {
			prepRemoved++
		}
	}
	if adviceChanged || prepAdded+prepRemoved > 0 {
		parts = append(parts, "面诊建议已改")
	}
	if original.PlainSummary != amended.PlainSummary {
		parts = append(parts, "通俗版解读已改")
	}
	origCare, newCare := make(map[string]bool), make(map[string]bool)
	for _, item := range original.CareAdvice {
		origCare[item] = true
	}
	for _, item := range amended.CareAdvice {
		newCare[item] = true
	}
	careAdded, careRemoved := 0, 0
	for item := range newCare {
		if !origCare[item] {
			careAdded++
		}
	}
	for item := range origCare {
		if !newCare[item] {
			careRemoved++
		}
	}
	if careAdded+careRemoved > 0 {
		parts = append(parts, fmt.Sprintf("护理建议新增%d条、删除%d条", careAdded, careRemoved))
	}
	if len(parts) == 0 {
		return ""
	}
	return "修订：" + strings.Join(parts, "；")
}

// assessmentReviewDetail builds the audit detail for the latest review of a
// task: the decision plus a compact before/after summary. Best effort — any
// read failure yields "" and the event is still recorded.
func assessmentReviewDetail(tx *gorm.DB, assessmentID string) string {
	var review AssessmentReview
	if err := tx.Where("assessment_id = ?", assessmentID).Order("version DESC").First(&review).Error; err != nil {
		return ""
	}
	switch review.Decision {
	case "approved":
		return "通过，未改动"
	case "rejected":
		return "补拍/面诊"
	}
	if review.FinalJSON == nil {
		return ""
	}
	var result AssessmentResult
	if err := tx.First(&result, "id = ? AND assessment_id = ?", review.ResultID, assessmentID).Error; err != nil {
		return ""
	}
	var analysis AssessmentAnalysis
	if err := json.Unmarshal([]byte(result.ContentJSON), &analysis); err != nil {
		return ""
	}
	var final AssessmentContent
	if err := json.Unmarshal([]byte(*review.FinalJSON), &final); err != nil {
		return ""
	}
	return assessmentContentDiff(analysis.AssessmentContent, final)
}

func (s *AssessmentService) operate(ctx context.Context, id, patientID, key, action string, in any, version int, now time.Time, fn func(*gorm.DB, *Assessment) error) (AssessmentView, error) {
	var view AssessmentView
	if !assessmentText(key, 64) || version < 1 {
		return view, ErrAssessmentInput
	}
	hash := assessmentHash(struct {
		Action string
		Input  any
	}{action, in})
	err := s.locked(ctx, id, patientID, func(tx *gorm.DB, task *Assessment) error {
		if patientID == "" {
			granted, err := assessmentGranted(tx, *task)
			if err != nil {
				return err
			}
			if !granted {
				return ErrAssessmentConsent
			}
		}
		var operation AssessmentOperation
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&operation, "assessment_id = ? AND request_key = ?", id, key).Error
		if err == nil {
			if operation.RequestHash != hash || operation.Action != action {
				return ErrIdempotency
			}
			return json.Unmarshal([]byte(operation.ResponseJSON), &view)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if task.Version != version {
			return ErrConflict
		}
		if err := fn(tx, task); err != nil {
			return err
		}
		task.Version++
		task.UpdatedAt = now
		if err := tx.Save(task).Error; err != nil {
			return err
		}
		actor := patientID
		if actor == "" {
			actor = employeeActor(ctx)
		}
		detail := ""
		if action == "review" {
			detail = assessmentReviewDetail(tx, id)
		}
		if err := assessmentEvent(tx, *task, action, actor, detail, now); err != nil {
			return err
		}
		view, err = assessmentView(tx, *task, patientID == "")
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(view)
		if err != nil {
			return err
		}
		return tx.Create(&AssessmentOperation{AssessmentID: id, RequestKey: key, RequestHash: hash, Action: action, ResponseJSON: string(encoded), CreatedAt: now}).Error
	})
	return view, err
}
func (s *AssessmentService) Change(ctx context.Context, id, patientID, key, action string, in AssessmentChange, now time.Time) (AssessmentView, error) {
	return s.operate(ctx, id, patientID, key, action, in, in.Version, now, func(tx *gorm.DB, t *Assessment) error {
		switch action {
		case "consent":
			policy, err := currentAssessmentPolicy(tx)
			if err != nil {
				return err
			}
			if patientID == "" || (in.Granted && in.PolicyVersion != policy.Version) {
				return ErrAssessmentInput
			}
			if in.Granted {
				if t.Status != "draft" {
					return ErrCareState
				}
				if err := assessmentPrivacy(tx, t.PatientID); err != nil {
					return err
				}
			}
			if !in.Granted && !t.ConsentGranted {
				return ErrCareState
			}
			event := AssessmentConsentEvent{ID: randomID("ai-consent-"), AssessmentID: id, PatientID: t.PatientID, PolicyVersion: policy.Version, PolicyContent: policy.Content, Granted: in.Granted, CreatedAt: now}
			if err := tx.Create(&event).Error; err != nil {
				return err
			}
			t.ConsentGranted = in.Granted
			t.ConsentEventID = event.ID
			if !in.Granted {
				assessmentCancel(t, "AI_CONSENT_REVOKED")
				return assessmentEndAttempt(tx, *t, "AI_CONSENT_REVOKED", false, now)
			}
		case "cancel":
			if patientID == "" || t.Status == "cancelled" {
				return ErrCareState
			}
			assessmentCancel(t, "PATIENT_CANCELLED")
			return assessmentEndAttempt(tx, *t, "PATIENT_CANCELLED", false, now)
		case "quality-check":
			if t.Status != "draft" {
				return ErrCareState
			}
			if err := assessmentReady(tx, *t); err != nil {
				return err
			}
			if !s.QualityAvailable() {
				return ErrAssessmentConfiguration
			}
			if t.QualityRequestedAt != nil && now.Before(t.QualityRequestedAt.Add(5*time.Second)) {
				return ErrAssessmentRateLimit
			}
			t.QualityRequested = true
			t.QualityRequestedAt = &now
			t.QualityLeaseToken = ""
			t.QualityLeaseExpiresAt = nil
		case "submit":
			if t.Status != "draft" {
				return ErrCareState
			}
			if err := assessmentReady(tx, *t); err != nil {
				return err
			}
			if !s.ModelAvailable() {
				return ErrAssessmentConfiguration
			}
			var photos []AssessmentPhoto
			if err := tx.Where("assessment_id = ? AND is_current = TRUE", id).Order("angle").Find(&photos).Error; err != nil {
				return err
			}
			if len(photos) != 3 {
				return ErrAssessmentQuality
			}
			seen := map[string]bool{}
			for i := range photos {
				p := &photos[i]
				if err := json.Unmarshal([]byte(p.QualityJSON), &p.QualityCheck); err != nil {
					return err
				}
				if seen[p.SHA256] || p.QualityCheck.Status != "passed" || !validAssessmentQuality(p.QualityCheck, s.configuration) {
					return ErrAssessmentQuality
				}
				seen[p.SHA256] = true
			}
			var active int64
			if err := tx.Model(&Assessment{}).Where("patient_id = ? AND status IN ?", t.PatientID, []string{"queued", "analyzing", "retry_wait"}).Count(&active).Error; err != nil {
				return err
			}
			if active > 0 {
				return ErrConflict
			}
			frozen := AssessmentFrozenInput{Photos: photos, Configuration: s.configuration, ConsentEventID: t.ConsentEventID}
			encoded, err := json.Marshal(frozen)
			if err != nil {
				return err
			}
			snapshot := AssessmentInputSnapshot{ID: randomID("ai-input-"), AssessmentID: id, ContentJSON: string(encoded), CreatedAt: now}
			if err := tx.Create(&snapshot).Error; err != nil {
				return err
			}
			if err := tx.Model(&AssessmentPhoto{}).Where("assessment_id = ? AND is_current = TRUE", id).Update("is_frozen", true).Error; err != nil {
				return err
			}
			t.SnapshotID = snapshot.ID
			t.Status = "queued"
			t.SubmittedAt = &now
			t.NextAttemptAt = &now
			t.QualityRequested = false
			t.QualityLeaseToken = ""
			t.QualityLeaseExpiresAt = nil
		case "retry":
			if t.Status != "failed" || !t.Retryable || t.AttemptCount >= t.MaxAttempts {
				return ErrCareState
			}
			if err := assessmentReady(tx, *t); err != nil {
				return err
			}
			if !s.ModelAvailable() {
				return ErrAssessmentConfiguration
			}
			var count int64
			if err := tx.Model(&Assessment{}).Where("patient_id = ? AND id <> ? AND status IN ?", t.PatientID, t.ID, []string{"queued", "analyzing", "retry_wait"}).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrConflict
			}
			t.Status = "queued"
			t.NextAttemptAt = &now
			t.FailureCode = ""
			t.Retryable = false
		case "assignment":
			if patientID != "" || !assessmentID(in.ReviewerEmployeeID) || t.Status == "cancelled" {
				return ErrAssessmentInput
			}
			actor, err := assessmentAdminLock(ctx, tx)
			if err != nil {
				return err
			}
			if actor.Role != "manager" {
				return ErrForbidden
			}
			targetCtx := WithEmployeeActor(WithEmployeeScope(ctx, "doctor", "", []string{t.ClinicID}), in.ReviewerEmployeeID)
			var target model.EmployeeAccount
			if err := tx.First(&target, "id = ? AND active = TRUE AND role = 'doctor'", in.ReviewerEmployeeID).Error; err != nil {
				return followupDBError(err)
			}
			if target.StaffID == nil {
				return ErrForbidden
			}
			targetCtx = WithEmployeeActor(WithEmployeeScope(targetCtx, "doctor", *target.StaffID, []string{t.ClinicID}), target.ID)
			target, err = assessmentAdminLock(targetCtx, tx)
			if err != nil {
				return err
			}
			if len(target.ClinicIDs) != 1 {
				return ErrForbidden
			}
			t.ReviewerEmployeeID = target.ID
		case "review":
			if patientID != "" || in.ResultID != t.ResultID || t.ResultID == "" || !assessmentText(in.Note, 2000) || (t.Status != "awaiting_review" && t.Status != "completed" && t.Status != "rejected") {
				return ErrCareState
			}
			actor, err := assessmentAdminLock(ctx, tx)
			if err != nil {
				return err
			}
			if actor.Role != "doctor" || actor.ID != t.ReviewerEmployeeID {
				return ErrForbidden
			}
			var result AssessmentResult
			if err := tx.First(&result, "id = ? AND assessment_id = ?", in.ResultID, id).Error; err != nil {
				return err
			}
			var analysis AssessmentAnalysis
			if err := json.Unmarshal([]byte(result.ContentJSON), &analysis); err != nil {
				return err
			}
			var snapshot AssessmentInputSnapshot
			if err := tx.First(&snapshot, "id = ?", t.SnapshotID).Error; err != nil {
				return err
			}
			var frozen AssessmentFrozenInput
			if err := json.Unmarshal([]byte(snapshot.ContentJSON), &frozen); err != nil {
				return err
			}
			review := AssessmentReview{ID: randomID("ai-review-"), AssessmentID: id, ResultID: result.ID, Decision: in.Decision, Note: strings.TrimSpace(in.Note), ReviewedByEmployeeID: actor.ID, DoctorName: actor.DisplayName, ReviewedAt: now, SupersedesReviewID: t.PublishedReviewID}
			var latest int
			if err := tx.Model(&AssessmentReview{}).Where("assessment_id = ?", id).Select("COALESCE(MAX(version),0)").Scan(&latest).Error; err != nil {
				return err
			}
			review.Version = latest + 1
			var final *AssessmentContent
			switch in.Decision {
			case "approved":
				if in.FinalContent != nil {
					return ErrAssessmentInput
				}
				final = &analysis.AssessmentContent
				t.Status = "completed"
			case "amended":
				final = in.FinalContent
				if final == nil {
					return ErrAssessmentInput
				}
				t.Status = "completed"
			case "rejected":
				if in.FinalContent != nil {
					return ErrAssessmentInput
				}
				t.Status = "rejected"
			default:
				return ErrAssessmentInput
			}
			if final != nil {
				if !validAssessmentContent(*final, frozen.Photos) {
					return ErrAssessmentInput
				}
				data, err := json.Marshal(final)
				if err != nil {
					return err
				}
				encoded := string(data)
				review.FinalJSON = &encoded
			}
			if err := tx.Create(&review).Error; err != nil {
				return err
			}
			t.PublishedReviewID = review.ID
			t.ReviewedAt = &now
		default:
			return ErrAssessmentInput
		}
		return nil
	})
}
func assessmentReady(tx *gorm.DB, t Assessment) error {
	granted, err := assessmentGranted(tx, t)
	if err != nil {
		return err
	}
	if !granted || t.Status == "cancelled" {
		return ErrAssessmentConsent
	}
	return assessmentPrivacy(tx, t.PatientID)
}
func assessmentGranted(tx *gorm.DB, t Assessment) (bool, error) {
	if !t.ConsentGranted || t.ConsentEventID == "" {
		return false, nil
	}
	var event AssessmentConsentEvent
	err := tx.First(&event, "id = ? AND assessment_id = ? AND patient_id = ?", t.ConsentEventID, t.ID, t.PatientID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	policy, err := currentAssessmentPolicy(tx)
	if err != nil {
		return false, err
	}
	return event.Granted && event.PolicyVersion == policy.Version && event.PolicyContent == policy.Content, nil
}

// currentAssessmentPolicy returns the newest published consent text. When
// the table is empty (pre-migration data) the seed constants are current,
// so existing grants keep working.
func currentAssessmentPolicy(tx *gorm.DB) (AssessmentConsentPolicy, error) {
	var policy AssessmentConsentPolicy
	err := tx.Order("created_at DESC, id DESC").First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AssessmentConsentPolicy{Version: AssessmentConsentVersion, Content: AssessmentConsentContent}, nil
	}
	return policy, err
}

// latestConsentEventVersion is the version the patient last acted on
// (granted or revoked), or "" when the patient never touched consent.
func latestConsentEventVersion(tx *gorm.DB, assessmentID string) (string, error) {
	var event AssessmentConsentEvent
	err := tx.Select("policy_version").Order("created_at DESC, id DESC").First(&event, "assessment_id = ?", assessmentID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return event.PolicyVersion, nil
}

var consentPolicyVersionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
var consentPolicyVersionBumpPattern = regexp.MustCompile(`^(.*-v)([0-9]+)$`)

// nextConsentPolicyVersion derives the next version from the current one
// ("photo-ai-v1" -> "photo-ai-v2"); versions without a -vN suffix get "-v2".
func nextConsentPolicyVersion(current string) string {
	if m := consentPolicyVersionBumpPattern.FindStringSubmatch(current); m != nil {
		if n, err := strconv.Atoi(m[2]); err == nil {
			return m[1] + strconv.Itoa(n+1)
		}
	}
	return current + "-v2"
}

// GetConsentPolicy returns the current consent text plus the version
// history, newest first. Any employee may read it; only a manager may
// publish a new version.
func (s *AssessmentService) GetConsentPolicy(ctx context.Context) (AssessmentConsentPolicyList, error) {
	tx := s.repo.db.WithContext(ctx)
	if _, err := assessmentAdmin(ctx, tx); err != nil {
		return AssessmentConsentPolicyList{}, err
	}
	policy, err := currentAssessmentPolicy(tx)
	if err != nil {
		return AssessmentConsentPolicyList{}, err
	}
	var history []AssessmentConsentPolicy
	if err := tx.Order("created_at DESC, id DESC").Limit(20).Find(&history).Error; err != nil {
		return AssessmentConsentPolicyList{}, err
	}
	return AssessmentConsentPolicyList{Policy: policy, History: history}, nil
}

// CreateConsentPolicy publishes a new version of the AI-use consent text
// (manager only). Grants given under older versions stop matching
// immediately: patients see the update prompt and must re-confirm before
// the new terms can be used. An empty version auto-bumps from the current
// one; a supplied version must be unique.
func (s *AssessmentService) CreateConsentPolicy(ctx context.Context, content, version string, now time.Time) (AssessmentConsentPolicy, error) {
	tx := s.repo.db.WithContext(ctx)
	actor, err := assessmentAdmin(ctx, tx)
	if err != nil {
		return AssessmentConsentPolicy{}, err
	}
	if actor.Role != "manager" {
		return AssessmentConsentPolicy{}, ErrForbidden
	}
	content = strings.TrimSpace(content)
	if utf8.RuneCountInString(content) < 10 || utf8.RuneCountInString(content) > 5000 {
		return AssessmentConsentPolicy{}, ErrAssessmentInput
	}
	current, err := currentAssessmentPolicy(tx)
	if err != nil {
		return AssessmentConsentPolicy{}, err
	}
	if version == "" {
		version = nextConsentPolicyVersion(current.Version)
	} else if !consentPolicyVersionPattern.MatchString(version) {
		return AssessmentConsentPolicy{}, ErrAssessmentInput
	}
	var existing AssessmentConsentPolicy
	if err := tx.First(&existing, "version = ?", version).Error; err == nil {
		return AssessmentConsentPolicy{}, ErrConflict
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return AssessmentConsentPolicy{}, err
	}
	policy := AssessmentConsentPolicy{ID: randomID("acp-"), Version: version, Content: content, CreatedBy: actor.ID, CreatedAt: now}
	if err := tx.Create(&policy).Error; err != nil {
		return AssessmentConsentPolicy{}, err
	}
	return policy, nil
}
func assessmentCancel(t *Assessment, code string) {
	t.Status = "cancelled"
	t.FailureCode = code
	t.Retryable = false
	t.NextAttemptAt = nil
	t.QualityRequested = false
	t.QualityLeaseToken = ""
	t.QualityLeaseExpiresAt = nil
	t.LeaseToken = ""
	t.LeaseExpiresAt = nil
}
func assessmentEndAttempt(tx *gorm.DB, t Assessment, code string, retryable bool, now time.Time) error {
	return tx.Model(&AssessmentAttempt{}).Where("assessment_id = ? AND finished_at IS NULL", t.ID).Updates(map[string]any{"finished_at": now, "error_code": code, "retryable": retryable}).Error
}

func (s *AssessmentService) Upload(ctx context.Context, id, patientID, key string, version int, photo AssessmentPhoto, data []byte, now time.Time) (AssessmentView, error) {
	if s.media == nil {
		return AssessmentView{}, ErrAssessmentConfiguration
	}
	if photo.Angle != "front" && photo.Angle != "left" && photo.Angle != "right" {
		return AssessmentView{}, ErrAssessmentInput
	}
	initial, err := s.Get(ctx, id, patientID)
	if err != nil {
		return initial, err
	}
	if !initial.Consent.Granted {
		return initial, ErrAssessmentConsent
	}
	if initial.Task.Status != "draft" {
		return initial, ErrCareState
	}
	if err = s.media.Save(ctx, photo.ID, data); err != nil {
		return initial, &AssessmentProviderError{Code: "MEDIA_UNAVAILABLE", Retryable: true}
	}
	input := struct {
		Version       int
		Angle, SHA256 string
	}{version, photo.Angle, photo.SHA256}
	return s.operate(ctx, id, patientID, key, "photo-upload", input, version, now, func(tx *gorm.DB, t *Assessment) error {
		if t.Status != "draft" {
			return ErrCareState
		}
		if err := assessmentReady(tx, *t); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&AssessmentPhoto{}).Where("assessment_id = ? AND is_current = TRUE AND angle <> ? AND sha256 = ?", id, photo.Angle, photo.SHA256).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			// Same photo reused for another angle: still a quality failure
			// (submit also rejects duplicate hashes), but wrapped so the
			// API can tell the patient why instead of the generic message.
			return fmt.Errorf("%w: %w", ErrAssessmentQuality, ErrAssessmentDuplicatePhoto)
		}
		if err := tx.Model(&AssessmentPhoto{}).Where("assessment_id = ? AND is_current = TRUE AND angle = ?", id, photo.Angle).Update("is_current", false).Error; err != nil {
			return err
		}
		quality := AssessmentQuality{Status: "pending", CheckerVersion: s.configuration.CheckerVersion, ThresholdProfileVersion: s.configuration.ThresholdProfileVersion, CheckedAt: nil, Checks: []AssessmentQualityItem{}, Issues: []string{}, RetakeAdvice: []string{}}
		if !s.QualityAvailable() {
			quality.Status = "unavailable"
			quality.CheckedAt = &now
			quality.Issues = []string{"CHECKER_NOT_CONFIGURED"}
		}
		encoded, _ := json.Marshal(quality)
		photo.AssessmentID = id
		photo.QualityJSON = string(encoded)
		photo.IsCurrent = true
		photo.UploadedAt = now
		if err := tx.Create(&photo).Error; err != nil {
			return err
		}
		t.QualityRequested = s.QualityAvailable()
		t.QualityRequestedAt = &now
		t.QualityLeaseToken = ""
		t.QualityLeaseExpiresAt = nil
		return nil
	})
}
func (s *AssessmentService) Photo(ctx context.Context, id, photoID, patientID string) (AssessmentPhoto, error) {
	var photo AssessmentPhoto
	err := s.locked(ctx, id, patientID, func(tx *gorm.DB, t *Assessment) error {
		if patientID == "" {
			granted, err := assessmentGranted(tx, *t)
			if err != nil {
				return err
			}
			if !granted {
				return ErrAssessmentConsent
			}
		}
		if err := tx.First(&photo, "id = ? AND assessment_id = ? AND (is_current = TRUE OR is_frozen = TRUE)", photoID, id).Error; err != nil {
			return followupDBError(err)
		}
		actor := patientID
		if actor == "" {
			actor = employeeActor(ctx)
		}
		return assessmentEvent(tx, *t, "photo-read", actor, photo.ID, time.Now())
	})
	if errors.Is(err, ErrAssessmentConsent) && patientID == "" {
		// The denial happens inside a transaction that rolls back, so the
		// attempt itself would leave no audit trail. Record it in a fresh
		// transaction: a withdrawn consent must be provably effective.
		s.recordDeniedPhotoRead(ctx, id, photoID)
	}
	return photo, err
}

// recordDeniedPhotoRead writes one "photo-read-denied" assessment event for
// a staff photo read refused after consent withdrawal. Best-effort: the
// denial itself must never fail because the audit write did, but the
// integration test asserts the event lands (an audit that silently drops
// entries is worse than no audit).
func (s *AssessmentService) recordDeniedPhotoRead(ctx context.Context, id, photoID string) {
	err := s.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task Assessment
		if err := tx.First(&task, "id = ?", id).Error; err != nil {
			return followupDBError(err)
		}
		return assessmentEvent(tx, task, "photo-read-denied", employeeActor(ctx), photoID, time.Now())
	})
	if err != nil {
		slog.Warn("assessment denied photo read not audited", "assessment", id, "photo", photoID, "error", err)
	}
}

// DeniedPhotoReads lists staff photo-read attempts denied after consent
// withdrawal, newest first, for the manager's periodic access audit.
func (s *AssessmentService) DeniedPhotoReads(ctx context.Context, page, size int) ([]AssessmentEvent, int64, error) {
	rows := make([]AssessmentEvent, 0)
	q := s.repo.db.WithContext(ctx).Model(&AssessmentEvent{}).Where("action = ?", "photo-read-denied")
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return rows, 0, err
	}
	err := q.Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}
func (s *AssessmentService) Events(ctx context.Context, id string) ([]AssessmentEvent, error) {
	rows := []AssessmentEvent{}
	err := s.locked(ctx, id, "", func(tx *gorm.DB, t *Assessment) error {
		return tx.Where("assessment_id = ?", id).Order("version, created_at, id").Find(&rows).Error
	})
	return rows, err
}
func (s *AssessmentService) Reviewers(ctx context.Context, clinicID string) ([]model.EmployeeAccount, error) {
	tx := s.repo.db.WithContext(ctx)
	actor, err := assessmentAdmin(ctx, tx)
	if err != nil {
		return nil, err
	}
	if actor.Role != "manager" {
		return nil, ErrForbidden
	}
	found := false
	for _, id := range actor.ClinicIDs {
		found = found || id == clinicID
	}
	if !found {
		return nil, ErrNotFound
	}
	rows := []model.EmployeeAccount{}
	err = tx.Joins("JOIN followup_staff ON followup_staff.id = employee_accounts.staff_id AND followup_staff.status = 'on_duty' AND followup_staff.role = 'doctor'").Where("employee_accounts.active = TRUE AND employee_accounts.role = 'doctor' AND JSON_CONTAINS(employee_accounts.clinic_ids, JSON_QUOTE(?))", clinicID).Find(&rows).Error
	return rows, err
}
