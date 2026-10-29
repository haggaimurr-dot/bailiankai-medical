package store

import (
	"context"
	"time"

	"example.com/bailiankai/api/internal/database"
)

// 新增-P2 行 102：模型效果抽检与人工评分回流。
// 抽检已复核发布（completed）的评估，人工打分（1–5），报表按模型版本汇总，可下钻到单条。
// 生产网关未配密钥前只在测试环境抽检（调用方控制）。

type AssessmentQualityScore struct {
	ID              string `gorm:"primaryKey"`
	AssessmentID    string
	ScorerEmployeeID string
	Score           int
	Comment         string
	ModelVersion    string
	CreatedAt       time.Time
}

func (AssessmentQualityScore) TableName() string { return "assessment_quality_scores" }

type QualitySampleItem struct {
	AssessmentID string
	PatientID    string
	ClinicID     string
	ReviewedAt   time.Time
	ModelVersion string
	HasScore     bool
}

// SampleQualityAssessments 随机抽检 N 条已复核发布的评估。
func (s *MySQL) SampleQualityAssessments(ctx context.Context, clinicID string, n int, now time.Time) ([]QualitySampleItem, error) {
	if scope, ok := scopeFrom(ctx); ok && scope.Role != "manager" {
		return nil, ErrForbidden
	}
	if n < 1 || n > 50 {
		return nil, ErrScheduleInput
	}
	var rows []struct {
		AssessmentID string
		PatientID    string
		ClinicID     string
		ReviewedAt   time.Time
	}
	q := s.db.WithContext(ctx).Table("assessments").
		Select("assessments.id AS assessment_id, assessments.patient_id, assessments.clinic_id, assessments.reviewed_at").
		Where("assessments.status = ? AND assessments.published_review_id <> ''", "completed")
	if clinicID != "" {
		q = q.Where("assessments.clinic_id = ?", clinicID)
	}
	if scope, ok := scopeFrom(ctx); ok && len(scope.ClinicIDs) > 0 {
		q = q.Where("assessments.clinic_id IN ?", scope.ClinicIDs)
	}
	if err := q.Order("RAND()").Limit(n).Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]QualitySampleItem, 0, len(rows))
	for _, r := range rows {
		var scored int64
		s.db.WithContext(ctx).Model(&AssessmentQualityScore{}).Where("assessment_id = ?", r.AssessmentID).Count(&scored)
		items = append(items, QualitySampleItem{
			AssessmentID: r.AssessmentID, PatientID: r.PatientID, ClinicID: r.ClinicID,
			ReviewedAt: r.ReviewedAt, HasScore: scored > 0,
		})
	}
	return items, nil
}

// SaveQualityScore 保存人工评分（同一评估同一评分人只保留一条，可更新）。
func (s *MySQL) SaveQualityScore(ctx context.Context, assessmentID string, score int, comment, modelVersion string, now time.Time) (AssessmentQualityScore, error) {
	if scope, ok := scopeFrom(ctx); ok && scope.Role != "manager" {
		return AssessmentQualityScore{}, ErrForbidden
	}
	if score < 1 || score > 5 {
		return AssessmentQualityScore{}, ErrScheduleInput
	}
	if len(comment) > 1000 || len(modelVersion) > 200 {
		return AssessmentQualityScore{}, ErrScheduleInput
	}
	// 确认评估存在且已发布。
	var task Assessment
	if err := s.db.WithContext(ctx).First(&task, "id = ? AND status = ? AND published_review_id <> ''", assessmentID, "completed").Error; err != nil {
		return AssessmentQualityScore{}, ErrNotFound
	}
	if scope, ok := scopeFrom(ctx); ok && len(scope.ClinicIDs) > 0 && !containsString(scope.ClinicIDs, task.ClinicID) {
		return AssessmentQualityScore{}, ErrNotFound
	}
	scorer := employeeActor(ctx)
	row := AssessmentQualityScore{
		ID: randomID("aqs-"), AssessmentID: assessmentID, ScorerEmployeeID: scorer,
		Score: score, Comment: comment, ModelVersion: modelVersion,
		CreatedAt: now.In(database.Shanghai).Truncate(time.Millisecond),
	}
	// upsert：同一评估同一人只一条。
	if err := s.db.WithContext(ctx).Where("assessment_id = ? AND scorer_employee_id = ?", assessmentID, scorer).
		Assign(AssessmentQualityScore{Score: score, Comment: comment, ModelVersion: modelVersion, CreatedAt: row.CreatedAt}).
		FirstOrCreate(&row).Error; err != nil {
		return AssessmentQualityScore{}, err
	}
	return row, nil
}

type QualityReportRow struct {
	ModelVersion string
	Count        int64 `gorm:"column:cnt"`
	AvgScore     float64
	MinScore     int
	MaxScore     int
}

type QualityReportItem struct {
	ScoreID      string
	AssessmentID string
	Score        int
	Comment      string
	Scorer       string
	CreatedAt    time.Time
}

// QualityReport 按模型版本汇总评分；传 modelVersion 可下钻到单条明细。
func (s *MySQL) QualityReport(ctx context.Context, modelVersion string) ([]QualityReportRow, []QualityReportItem, error) {
	if scope, ok := scopeFrom(ctx); ok && scope.Role != "manager" {
		return nil, nil, ErrForbidden
	}
	var rows []QualityReportRow
	q := s.db.WithContext(ctx).Table("assessment_quality_scores").
		Select("model_version, COUNT(*) AS cnt, AVG(score) AS avg_score, MIN(score) AS min_score, MAX(score) AS max_score").
		Group("model_version").Order("model_version")
	if scope, ok := scopeFrom(ctx); ok && len(scope.ClinicIDs) > 0 {
		q = q.Joins("JOIN assessments ON assessments.id = assessment_quality_scores.assessment_id").
			Where("assessments.clinic_id IN ?", scope.ClinicIDs)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	var items []QualityReportItem
	if modelVersion != "" {
		var scores []AssessmentQualityScore
		q2 := s.db.WithContext(ctx).Where("model_version = ?", modelVersion).Order("created_at DESC")
		if scope, ok := scopeFrom(ctx); ok && len(scope.ClinicIDs) > 0 {
			q2 = q2.Joins("JOIN assessments ON assessments.id = assessment_quality_scores.assessment_id").
				Where("assessments.clinic_id IN ?", scope.ClinicIDs)
		}
		if err := q2.Find(&scores).Error; err != nil {
			return nil, nil, err
		}
		for _, sc := range scores {
			items = append(items, QualityReportItem{
				ScoreID: sc.ID, AssessmentID: sc.AssessmentID, Score: sc.Score,
				Comment: sc.Comment, Scorer: sc.ScorerEmployeeID, CreatedAt: sc.CreatedAt,
			})
		}
	}
	return rows, items, nil
}
