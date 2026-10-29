package store_test

import (
	"testing"

	"example.com/bailiankai/api/internal/store"
)

// 新增-P1–P2：复核时限统计与超时提醒。统计口径与任务队列共用 assessmentAccess，
// 超时以数据库时间计算（分析完成超过 24 小时未复核）。
func TestAssessmentReviewStats(t *testing.T) {
	a := newAIFixture(t)
	clinic := a.f.slot.ClinicID
	patient := a.patient
	rows := []struct {
		id, status, completed, reviewed string
	}{
		{"assessment-stats-overdue", "awaiting_review", "DATE_SUB(NOW(), INTERVAL 30 HOUR)", "NULL"},
		{"assessment-stats-waiting", "awaiting_review", "DATE_SUB(NOW(), INTERVAL 1 HOUR)", "NULL"},
		{"assessment-stats-reviewed", "completed", "DATE_SUB(NOW(), INTERVAL 50 HOUR)", "DATE_SUB(NOW(), INTERVAL 26 HOUR)"},
	}
	for _, row := range rows {
		if err := a.f.db.Exec(`INSERT INTO assessments (id, patient_id, clinic_id, request_key, request_hash, status, analysis_completed_at, reviewed_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'x', ?, `+row.completed+`, `+row.reviewed+`, NOW(), NOW())`,
			row.id, patient, clinic, "stats-"+row.id, row.status).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		a.f.db.Exec("DELETE FROM assessments WHERE id LIKE 'assessment-stats-%'")
	})
	stats, err := a.service.ReviewStats(a.manager)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ReviewSLAHours != store.AssessmentReviewSLAHours {
		t.Fatalf("sla=%d", stats.ReviewSLAHours)
	}
	if stats.AwaitingReview != 2 || stats.Overdue != 1 || stats.ReviewedCount != 1 {
		t.Fatalf("stats=%+v want awaiting=2 overdue=1 reviewed=1", stats)
	}
	if stats.AvgReviewHours == nil || *stats.AvgReviewHours < 23.9 || *stats.AvgReviewHours > 24.1 {
		t.Fatalf("avg=%v want ~24", stats.AvgReviewHours)
	}
	// 医生视角只看自己被指派的任务：未指派给该医生时统计为 0。
	doctorStats, err := a.service.ReviewStats(a.doctor)
	if err != nil {
		t.Fatal(err)
	}
	if doctorStats.AwaitingReview != 0 || doctorStats.Overdue != 0 {
		t.Fatalf("doctor stats=%+v want zeros", doctorStats)
	}
}
