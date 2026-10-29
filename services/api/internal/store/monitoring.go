package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
)

var ErrMonitoringInput = errors.New("invalid monitoring input")

// ============ 行117: 模板效果统计 ============

// RecordMessageClick records a patient clicking a link inside a message; the
// link carries the template marker so the click rolls up to the template.
func (s *MySQL) RecordMessageClick(ctx context.Context, templateID, notificationID, patientID string, now time.Time) error {
	if templateID == "" || notificationID == "" || patientID == "" {
		return ErrMonitoringInput
	}
	return s.db.WithContext(ctx).Create(&model.MessageClick{
		ID:             randomID("click-"),
		TemplateID:     templateID,
		NotificationID: notificationID,
		PatientID:      patientID,
		ClickedAt:      now,
	}).Error
}

// TemplateEffectStats aggregates send/deliver/click/fail counts for one
// template. Delivered = notifications in sent status; failed reuses the
// notification failure口径.
func (s *MySQL) TemplateEffectStats(ctx context.Context, templateID string) (model.TemplateEffectStats, error) {
	stats := model.TemplateEffectStats{TemplateID: templateID}
	q := s.db.WithContext(ctx)
	if err := q.Model(&model.Notification{}).Where("template_id = ?", templateID).Count(&stats.Sent).Error; err != nil {
		return stats, err
	}
	if err := q.Model(&model.Notification{}).Where("template_id = ? AND status = ?", templateID, "sent").Count(&stats.Delivered).Error; err != nil {
		return stats, err
	}
	if err := q.Model(&model.Notification{}).Where("template_id = ? AND status = ?", templateID, "failed").Count(&stats.Failed).Error; err != nil {
		return stats, err
	}
	if err := q.Model(&model.MessageClick{}).Where("template_id = ?", templateID).Count(&stats.Clicked).Error; err != nil {
		return stats, err
	}
	return stats, nil
}

// ============ 行118: 接口调用监控 ============

const defaultSlowThresholdMs = 2000

// SlowRequestThreshold returns the configured slow-request threshold in ms.
func (s *MySQL) SlowRequestThreshold(ctx context.Context) int {
	var row model.MonitoringSetting
	if err := s.db.WithContext(ctx).First(&row, "k = ?", "slow_request_threshold_ms").Error; err != nil {
		return defaultSlowThresholdMs
	}
	ms, err := strconv.Atoi(row.V)
	if err != nil || ms <= 0 {
		return defaultSlowThresholdMs
	}
	return ms
}

// SetSlowRequestThreshold updates the slow-request threshold (manager only;
// the httpapi layer enforces the role).
func (s *MySQL) SetSlowRequestThreshold(ctx context.Context, ms int, now time.Time) error {
	if ms <= 0 || ms > 600000 {
		return ErrMonitoringInput
	}
	row := model.MonitoringSetting{K: "slow_request_threshold_ms", V: strconv.Itoa(ms), UpdatedAt: now}
	return s.db.WithContext(ctx).Save(&row).Error
}

// RecordApiRequest upserts the per-day aggregate for one route pattern.
// Only the route pattern is stored, never query strings or bodies.
func (s *MySQL) RecordApiRequest(ctx context.Context, route, method string, status, durationMs int, day time.Time) error {
	if route == "" || method == "" {
		return nil
	}
	err4xx, err5xx := 0, 0
	switch {
	case status >= 500:
		err5xx = 1
	case status >= 400:
		err4xx = 1
	}
	dayStr := day.In(database.Shanghai).Format("2006-01-02")
	return s.db.WithContext(ctx).Exec(`
INSERT INTO api_request_stats (route, method, stat_date, total, err_4xx, err_5xx, sum_ms, max_ms)
VALUES (?, ?, ?, 1, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
 total = total + 1,
 err_4xx = err_4xx + ?,
 err_5xx = err_5xx + ?,
 sum_ms = sum_ms + ?,
 max_ms = GREATEST(max_ms, ?)`,
		route, method, dayStr, err4xx, err5xx, durationMs, durationMs,
		err4xx, err5xx, durationMs, durationMs).Error
}

// RecordSlowAlert stores one request that exceeded the slow threshold.
func (s *MySQL) RecordSlowAlert(ctx context.Context, route, method string, status, durationMs, thresholdMs int, now time.Time) error {
	return s.db.WithContext(ctx).Create(&model.ApiSlowAlert{
		ID:          randomID("slow-"),
		Route:       route,
		Method:      method,
		Status:      status,
		DurationMs:  durationMs,
		ThresholdMs: thresholdMs,
		CreatedAt:   now,
	}).Error
}

// RouteStats returns per-route aggregates for a date (YYYY-MM-DD).
func (s *MySQL) RouteStats(ctx context.Context, dateStr string) ([]model.ApiRequestStat, error) {
	rows := make([]model.ApiRequestStat, 0)
	err := s.db.WithContext(ctx).Where("stat_date = ?", dateStr).Order("total DESC").Find(&rows).Error
	return rows, err
}

// SlowAlerts returns recent slow-request alerts.
func (s *MySQL) SlowAlerts(ctx context.Context, limit int) ([]model.ApiSlowAlert, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows := make([]model.ApiSlowAlert, 0)
	err := s.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// ============ 行119: 数据变更日报 ============

// dataDailyTables are the key tables snapshotted by the daily report.
// Counts only, never patient details.
var dataDailyTables = []string{
	"patients",
	"appointments",
	"assessments",
	"followup_tasks",
	"notifications",
	"patient_messages",
	"knowledge_articles",
	"case_records",
}

// GenerateDataDailyReport snapshots row counts of the key tables for date.
// The job is read-only statistics; it never modifies business data.
func (s *MySQL) GenerateDataDailyReport(ctx context.Context, date, now time.Time) error {
	day := date.In(database.Shanghai)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, table := range dataDailyTables {
			var n int64
			if err := tx.Table(table).Count(&n).Error; err != nil {
				return err
			}
			row := model.DataDailyReport{
				ReportDate: day,
				TableName:  table,
				RowCount:   n,
				CreatedAt:  now,
			}
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// GetDataDailyReport returns the snapshot rows for a date (YYYY-MM-DD).
func (s *MySQL) GetDataDailyReport(ctx context.Context, dateStr string) ([]model.DataDailyReport, error) {
	rows := make([]model.DataDailyReport, 0)
	err := s.db.WithContext(ctx).Where("report_date = ?", dateStr).Order("table_name").Find(&rows).Error
	return rows, err
}

// ============ 行123: 异常访问行为告警 ============

const (
	securitySignalDeniedPhoto = "photo-access-denied"
	// 5 denied photo accesses by the same actor within 10 minutes raise an
	// alert; at most one alert per actor+kind per hour to avoid spam.
	securitySignalWindow  = 10 * time.Minute
	securitySignalLimit   = 5
	securityAlertCooldown = time.Hour
)

// RecordSecuritySignal logs one abnormal-access signal and raises an alert
// when the threshold is crossed. Only behavior counts and account ids are
// recorded, never photo content.
func (s *MySQL) RecordSecuritySignal(ctx context.Context, actorType, actorID, kind, resourceType, resourceID string, now time.Time) error {
	if actorType == "" || actorID == "" || kind == "" {
		return ErrMonitoringInput
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model.SecuritySignal{
			ID:           randomID("sig-"),
			ActorType:    actorType,
			ActorID:      actorID,
			Kind:         kind,
			ResourceType: resourceType,
			ResourceID:   resourceID,
			CreatedAt:    now,
		}).Error; err != nil {
			return err
		}
		if kind != securitySignalDeniedPhoto {
			return nil
		}
		var n int64
		if err := tx.Model(&model.SecuritySignal{}).
			Where("actor_type = ? AND actor_id = ? AND kind = ? AND created_at >= ?",
				actorType, actorID, kind, now.Add(-securitySignalWindow)).
			Count(&n).Error; err != nil {
			return err
		}
		if n < securitySignalLimit {
			return nil
		}
		var recent int64
		if err := tx.Model(&model.SecurityAlert{}).
			Where("actor_type = ? AND actor_id = ? AND kind = ? AND alerted_at >= ?",
				actorType, actorID, kind, now.Add(-securityAlertCooldown)).
			Count(&recent).Error; err != nil {
			return err
		}
		if recent > 0 {
			return nil
		}
		return tx.Create(&model.SecurityAlert{
			ID:        randomID("sec-"),
			ActorType: actorType,
			ActorID:   actorID,
			Kind:      kind,
			Detail:    "10 分钟内越权访问照片被拒 " + strconv.FormatInt(n, 10) + " 次",
			AlertedAt: now,
		}).Error
	})
}

// SecurityAlerts lists security alerts, newest first.
func (s *MySQL) SecurityAlerts(ctx context.Context, unreadOnly bool, limit int) ([]model.SecurityAlert, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows := make([]model.SecurityAlert, 0)
	q := s.db.WithContext(ctx).Order("alerted_at DESC").Limit(limit)
	if unreadOnly {
		q = q.Where("read_at IS NULL")
	}
	return rows, q.Find(&rows).Error
}

// MarkSecurityAlertRead marks an alert as reviewed by a manager.
func (s *MySQL) MarkSecurityAlertRead(ctx context.Context, id string, now time.Time) error {
	res := s.db.WithContext(ctx).Model(&model.SecurityAlert{}).Where("id = ?", id).Update("read_at", now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ============ 行117/123 支撑方法 ============

// FollowupPhotoExists reports whether a followup photo ID exists at all,
// regardless of who may access it. Used to distinguish "no such photo" from
// "photo exists but this actor may not access it" for the security signal.
func (s *MySQL) FollowupPhotoExists(ctx context.Context, id string) bool {
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.FollowupPhoto{}).Where("id = ?", id).Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}

// GetNotification returns one notification belonging to the patient.
func (s *MySQL) GetNotification(ctx context.Context, id, patientID string) (model.Notification, error) {
	var row model.Notification
	err := s.db.WithContext(ctx).Where("id = ? AND patient_id = ?", id, patientID).First(&row).Error
	if err != nil {
		return row, followupDBError(err)
	}
	return row, nil
}

// MessageTemplate returns one template visible in the caller's clinic scope.
func (s *MySQL) MessageTemplate(ctx context.Context, id string) (model.MessageTemplate, error) {
	var row model.MessageTemplate
	err := clinicScope(ctx, s.db.WithContext(ctx), "clinic_id").Where("id = ?", id).First(&row).Error
	if err != nil {
		return row, followupDBError(err)
	}
	return row, nil
}
