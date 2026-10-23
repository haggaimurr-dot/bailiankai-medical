package store

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

type AssessmentJob struct {
	Task    Assessment
	Attempt AssessmentAttempt
	Input   AssessmentFrozenInput
	Quality bool
	Token   string
}

func (s *AssessmentService) Recover(ctx context.Context, now time.Time) error {
	var tasks []Assessment
	if err := s.repo.db.WithContext(ctx).Where("status = 'analyzing' AND lease_expires_at <= ?", now).Limit(50).Find(&tasks).Error; err != nil {
		return err
	}
	for _, candidate := range tasks {
		err := s.locked(ctx, candidate.ID, candidate.PatientID, func(tx *gorm.DB, t *Assessment) error {
			if t.Status != "analyzing" || t.LeaseExpiresAt == nil || t.LeaseExpiresAt.After(now) {
				return nil
			}
			return s.failAttempt(tx, t, "WORKER_LEASE_EXPIRED", true, 0, now)
		})
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *AssessmentService) Claim(ctx context.Context, quality bool, now time.Time) (*AssessmentJob, error) {
	var candidates []Assessment
	q := s.repo.db.WithContext(ctx)
	if quality {
		q = q.Where("status = 'draft' AND consent_granted = TRUE AND quality_requested = TRUE AND (quality_lease_expires_at IS NULL OR quality_lease_expires_at <= ?)", now)
	} else {
		q = q.Where("status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)", []string{"queued", "retry_wait"}, now)
	}
	if err := q.Order("created_at, id").Limit(10).Find(&candidates).Error; err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		var job *AssessmentJob
		err := s.locked(ctx, candidate.ID, candidate.PatientID, func(tx *gorm.DB, t *Assessment) error {
			if err := assessmentReady(tx, *t); err != nil {
				if !errors.Is(err, ErrAssessmentConsent) && !errors.Is(err, ErrAssessmentPrivacy) {
					return err
				}
				assessmentCancel(t, "CONSENT_UNAVAILABLE")
				t.Version++
				t.UpdatedAt = now
				if err := tx.Save(t).Error; err != nil {
					return err
				}
				return assessmentEvent(tx, *t, "cancelled", "system", "CONSENT_UNAVAILABLE", now)
			}
			token := randomID("ai-lease-")
			expires := now.Add(90 * time.Second)
			if quality {
				if t.Status != "draft" || !t.QualityRequested || (t.QualityLeaseExpiresAt != nil && t.QualityLeaseExpiresAt.After(now)) {
					return nil
				}
				photos := []AssessmentPhoto{}
				if err := tx.Where("assessment_id = ? AND is_current = TRUE", t.ID).Order("angle").Find(&photos).Error; err != nil {
					return err
				}
				if len(photos) == 0 {
					t.QualityRequested = false
					return tx.Save(t).Error
				}
				t.QualityLeaseToken = token
				t.QualityLeaseExpiresAt = &expires
				job = &AssessmentJob{Task: *t, Quality: true, Token: token, Input: AssessmentFrozenInput{Photos: photos, Configuration: s.configuration, ConsentEventID: t.ConsentEventID}}
			} else {
				if !assessmentOneOf(t.Status, "queued", "retry_wait") || (t.NextAttemptAt != nil && t.NextAttemptAt.After(now)) {
					return nil
				}
				if t.AttemptCount >= t.MaxAttempts {
					t.Status = "failed"
					t.Retryable = false
					t.NextAttemptAt = nil
					return tx.Save(t).Error
				}
				var snapshot AssessmentInputSnapshot
				if err := tx.First(&snapshot, "id = ? AND assessment_id = ?", t.SnapshotID, t.ID).Error; err != nil {
					return err
				}
				var input AssessmentFrozenInput
				if err := json.Unmarshal([]byte(snapshot.ContentJSON), &input); err != nil {
					return err
				}
				t.Status = "analyzing"
				t.Version++
				t.AttemptCount++
				t.LeaseToken = token
				t.LeaseExpiresAt = &expires
				t.NextAttemptAt = nil
				t.FailureCode = ""
				t.Retryable = false
				attempt := AssessmentAttempt{ID: randomID("ai-attempt-"), AssessmentID: t.ID, AttemptNo: t.AttemptCount, StartedAt: now, LeaseExpiresAt: expires, LeaseToken: token}
				if err := tx.Create(&attempt).Error; err != nil {
					return err
				}
				job = &AssessmentJob{Task: *t, Attempt: attempt, Input: input, Token: token}
			}
			t.UpdatedAt = now
			if err := tx.Save(t).Error; err != nil {
				return err
			}
			return assessmentEvent(tx, *t, func() string {
				if quality {
					return "quality-claimed"
				}
				return "analysis-claimed"
			}(), "system", "", now)
		})
		if err != nil {
			return nil, err
		}
		if job != nil {
			return job, nil
		}
	}
	return nil, nil
}
func (s *AssessmentService) checkLease(tx *gorm.DB, t Assessment, job AssessmentJob, now time.Time) error {
	if err := assessmentReady(tx, t); err != nil {
		return err
	}
	if job.Quality {
		if t.Status != "draft" || t.Version != job.Task.Version || t.QualityLeaseToken != job.Token || t.QualityLeaseExpiresAt == nil || !t.QualityLeaseExpiresAt.After(now) {
			return ErrConflict
		}
	} else {
		if t.Status != "analyzing" || t.LeaseToken != job.Token || t.LeaseExpiresAt == nil || !t.LeaseExpiresAt.After(now) {
			return ErrConflict
		}
	}
	return nil
}
func (s *AssessmentService) Renew(ctx context.Context, job AssessmentJob, now time.Time) error {
	return s.locked(ctx, job.Task.ID, job.Task.PatientID, func(tx *gorm.DB, t *Assessment) error {
		if err := s.checkLease(tx, *t, job, now); err != nil {
			return err
		}
		expires := now.Add(90 * time.Second)
		if job.Quality {
			t.QualityLeaseExpiresAt = &expires
		} else {
			t.LeaseExpiresAt = &expires
			if err := tx.Model(&AssessmentAttempt{}).Where("id = ? AND lease_token = ? AND finished_at IS NULL", job.Attempt.ID, job.Token).Update("lease_expires_at", expires).Error; err != nil {
				return err
			}
		}
		return tx.Save(t).Error
	})
}
func (s *AssessmentService) ReadInput(ctx context.Context, job AssessmentJob, now time.Time) (map[string][]byte, error) {
	data := map[string][]byte{}
	if s.media == nil {
		return nil, &AssessmentProviderError{Code: "MEDIA_UNAVAILABLE", Retryable: true}
	}
	for _, photo := range job.Input.Photos {
		bytes, err := s.media.Read(ctx, photo.ID)
		if err != nil {
			return nil, &AssessmentProviderError{Code: "MEDIA_UNAVAILABLE", Retryable: true}
		}
		if err := VerifyMedia(bytes, MediaReference{ID: photo.ID, SHA256: photo.SHA256, SizeBytes: photo.SizeBytes}); err != nil {
			return nil, &AssessmentProviderError{Code: "MEDIA_UNAVAILABLE", Retryable: true}
		}
		data[photo.ID] = bytes
	}
	// Recheck withdrawal after potentially slow object reads, before provider I/O.
	err := s.locked(ctx, job.Task.ID, job.Task.PatientID, func(tx *gorm.DB, t *Assessment) error { return s.checkLease(tx, *t, job, now) })
	return data, err
}
func (s *AssessmentService) FinishQuality(ctx context.Context, job AssessmentJob, qualities map[string]AssessmentQuality, providerErr error, now time.Time) error {
	return s.locked(ctx, job.Task.ID, job.Task.PatientID, func(tx *gorm.DB, t *Assessment) error {
		if err := s.checkLease(tx, *t, job, now); err != nil {
			return err
		}
		for _, photo := range job.Input.Photos {
			q, ok := qualities[photo.ID]
			if providerErr != nil || !ok || len(qualities) != len(job.Input.Photos) || !validAssessmentQuality(q, job.Input.Configuration) {
				q = AssessmentQuality{Status: "unavailable", CheckerVersion: job.Input.Configuration.CheckerVersion, ThresholdProfileVersion: job.Input.Configuration.ThresholdProfileVersion, CheckedAt: &now, Checks: []AssessmentQualityItem{}, Issues: []string{"QUALITY_SERVICE_UNAVAILABLE"}, RetakeAdvice: []string{}}
			}
			q.CheckedAt = &now
			encoded, err := json.Marshal(q)
			if err != nil {
				return err
			}
			if err := tx.Model(&AssessmentPhoto{}).Where("id = ? AND assessment_id = ? AND is_current = TRUE", photo.ID, t.ID).Update("quality_json", string(encoded)).Error; err != nil {
				return err
			}
		}
		t.QualityRequested = false
		t.QualityLeaseToken = ""
		t.QualityLeaseExpiresAt = nil
		t.Version++
		t.UpdatedAt = now
		if err := tx.Save(t).Error; err != nil {
			return err
		}
		return assessmentEvent(tx, *t, "quality-checked", "system", "", now)
	})
}
func (s *AssessmentService) failAttempt(tx *gorm.DB, t *Assessment, code string, retryable bool, retryAfter time.Duration, now time.Time) error {
	if err := assessmentEndAttempt(tx, *t, code, retryable, now); err != nil {
		return err
	}
	t.FailureCode = code
	t.Retryable = retryable && t.AttemptCount < t.MaxAttempts
	t.LeaseToken = ""
	t.LeaseExpiresAt = nil
	t.Version++
	t.UpdatedAt = now
	if err := assessmentReady(tx, *t); err != nil {
		if !errors.Is(err, ErrAssessmentConsent) && !errors.Is(err, ErrAssessmentPrivacy) {
			return err
		}
		assessmentCancel(t, "CONSENT_UNAVAILABLE")
	} else if t.Retryable {
		t.Status = "retry_wait"
		delay := 10 * time.Second
		if t.AttemptCount > 1 {
			delay = 30 * time.Second
		}
		if retryAfter > delay {
			delay = retryAfter
		}
		next := now.Add(delay)
		t.NextAttemptAt = &next
	} else {
		t.Status = "failed"
		t.NextAttemptAt = nil
	}
	if err := tx.Save(t).Error; err != nil {
		return err
	}
	return assessmentEvent(tx, *t, "analysis-failed", "system", code, now)
}
func (s *AssessmentService) Finish(ctx context.Context, job AssessmentJob, analysis AssessmentAnalysis, providerRequestID string, providerErr error, now time.Time) error {
	return s.locked(ctx, job.Task.ID, job.Task.PatientID, func(tx *gorm.DB, t *Assessment) error {
		if t.Status == "awaiting_review" && t.ResultID != "" {
			var result AssessmentResult
			if err := tx.First(&result, "id = ? AND attempt_id = ?", t.ResultID, job.Attempt.ID).Error; err == nil {
				return nil
			}
		}
		if err := s.checkLease(tx, *t, job, now); err != nil {
			return err
		}
		if providerErr != nil {
			code := "MODEL_UNAVAILABLE"
			retryable := true
			delay := time.Duration(0)
			var failure *AssessmentProviderError
			if errors.As(providerErr, &failure) {
				code = failure.Code
				retryable = failure.Retryable
				delay = failure.RetryAfter
			}
			return s.failAttempt(tx, t, code, retryable, delay, now)
		}
		if !validAssessmentAnalysis(analysis, job.Input) || len(providerRequestID) > 200 {
			return s.failAttempt(tx, t, "MODEL_OUTPUT_INVALID", false, 0, now)
		}
		encoded, err := json.Marshal(analysis)
		if err != nil {
			return err
		}
		result := AssessmentResult{ID: randomID("ai-result-"), AssessmentID: t.ID, AttemptID: job.Attempt.ID, InputSnapshotID: t.SnapshotID, ContentJSON: string(encoded), GeneratedAt: now}
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		if err := tx.Model(&AssessmentAttempt{}).Where("id = ? AND lease_token = ? AND finished_at IS NULL", job.Attempt.ID, job.Token).Updates(map[string]any{"finished_at": now, "provider_request_id": providerRequestID}).Error; err != nil {
			return err
		}
		t.ResultID = result.ID
		t.Status = "awaiting_review"
		t.AnalysisCompletedAt = &now
		t.LeaseToken = ""
		t.LeaseExpiresAt = nil
		t.Version++
		t.UpdatedAt = now
		if err := tx.Save(t).Error; err != nil {
			return err
		}
		return assessmentEvent(tx, *t, "analysis-completed", "system", result.ID, now)
	})
}
func (s *AssessmentService) ProcessNext(ctx context.Context, now time.Time) (bool, error) {
	if err := s.Recover(ctx, now); err != nil {
		return false, err
	}
	job, err := s.Claim(ctx, true, now)
	if err != nil {
		return false, err
	}
	if job == nil {
		job, err = s.Claim(ctx, false, now)
	}
	if err != nil || job == nil {
		return false, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		timer := time.NewTicker(20 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-callCtx.Done():
				return
			case stamp := <-timer.C:
				if s.Renew(callCtx, *job, stamp) != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-heartbeatDone }()
	data, providerErr := s.ReadInput(callCtx, *job, time.Now())
	if errors.Is(providerErr, ErrConflict) || errors.Is(providerErr, ErrAssessmentConsent) || errors.Is(providerErr, ErrAssessmentPrivacy) {
		return true, nil
	}
	if job.Quality {
		qualities := map[string]AssessmentQuality{}
		if providerErr == nil {
			if s.provider == nil {
				providerErr = ErrAssessmentConfiguration
			} else {
				qualities, providerErr = s.provider.Check(callCtx, job.Input, data)
			}
		}
		err = s.FinishQuality(ctx, *job, qualities, providerErr, time.Now())
	} else {
		var result AssessmentAnalysis
		var requestID string
		if providerErr == nil {
			if s.provider == nil {
				providerErr = &AssessmentProviderError{Code: "MODEL_CONFIGURATION_ERROR"}
			} else {
				result, requestID, providerErr = s.provider.Analyze(callCtx, job.Input, data, job.Task.SnapshotID)
			}
		}
		if errors.Is(providerErr, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			providerErr = &AssessmentProviderError{Code: "MODEL_TIMEOUT", Retryable: true}
		}
		// Retry persistence without another model call; a lease loss/withdrawal is final.
		for n := 0; n < 3; n++ {
			err = s.Finish(ctx, *job, result, requestID, providerErr, time.Now())
			if err == nil || errors.Is(err, ErrConflict) || errors.Is(err, ErrAssessmentConsent) || errors.Is(err, ErrAssessmentPrivacy) {
				break
			}
			select {
			case <-ctx.Done():
				return true, ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if errors.Is(err, ErrConflict) || errors.Is(err, ErrAssessmentConsent) || errors.Is(err, ErrAssessmentPrivacy) {
		return true, nil
	}
	return true, err
}
func (s *AssessmentService) Run(ctx context.Context) {
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-timer.C:
			if _, err := s.ProcessNext(ctx, now); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("assessment worker operation failed")
			}
		}
	}
}
