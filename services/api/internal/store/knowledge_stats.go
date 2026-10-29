package store

import (
	"context"
	"time"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
)

// RecordKnowledgeOpen stores one article-open beacon hit. Only opens of
// published snapshots count: drafts and unpublished articles are ignored so
// the stats口径 stays "已发布快照". patientID is empty for anonymous opens.
func (s *MySQL) RecordKnowledgeOpen(ctx context.Context, articleID, clinicID, patientID string, now time.Time) error {
	var published struct {
		ID string
	}
	if err := s.db.WithContext(ctx).Model(&model.KnowledgeArticle{}).
		Select("id").Where("id = ? AND published_json IS NOT NULL", articleID).
		First(&published).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return followupDBError(err)
	}
	var pid *string
	if patientID != "" {
		pid = &patientID
	}
	return s.db.WithContext(ctx).Create(&model.KnowledgeOpen{
		ID:        randomID("knowledge-open-"),
		ArticleID: articleID,
		ClinicID:  clinicID,
		PatientID: pid,
		OpenedAt:  now,
	}).Error
}

type knowledgeOpenStatRow struct {
	ArticleID      string
	Title          string
	Opens          int64
	UniquePatients int64
}

func knowledgeOpenScope(db *gorm.DB, clinicID string, since time.Time) *gorm.DB {
	q := db.Where("opened_at >= ?", since)
	if clinicID != "" {
		q = q.Where("clinic_id = ?", clinicID)
	}
	return q
}

// KnowledgeOpenStats aggregates open counts since the cutoff, optionally
// scoped to one clinic. Per-article rows drill down to a single article.
func (s *MySQL) KnowledgeOpenStats(ctx context.Context, clinicID string, since time.Time) (model.KnowledgeOpenStats, error) {
	stats := model.KnowledgeOpenStats{Articles: []model.KnowledgeOpenArticleStat{}}
	db := s.db.WithContext(ctx)
	if err := knowledgeOpenScope(db.Model(&model.KnowledgeOpen{}), clinicID, since).Count(&stats.Total).Error; err != nil {
		return stats, err
	}
	sub := knowledgeOpenScope(db.Model(&model.KnowledgeOpen{}), clinicID, since).
		Select("DISTINCT patient_id").Where("patient_id IS NOT NULL")
	if err := db.Table("(?) AS u", sub).Count(&stats.UniquePatients).Error; err != nil {
		return stats, err
	}
	rows := []knowledgeOpenStatRow{}
	q := db.Table("knowledge_article_opens o").
		Select("o.article_id AS article_id, COALESCE(a.title, '') AS title, COUNT(*) AS opens, COUNT(DISTINCT o.patient_id) AS unique_patients").
		Joins("LEFT JOIN knowledge_articles a ON a.id = o.article_id").
		Where("o.opened_at >= ?", since).
		Group("o.article_id, a.title").
		Order("opens DESC").
		Limit(100)
	if clinicID != "" {
		q = q.Where("o.clinic_id = ?", clinicID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return stats, err
	}
	for _, r := range rows {
		stats.Articles = append(stats.Articles, model.KnowledgeOpenArticleStat{
			ArticleID:      r.ArticleID,
			Title:          r.Title,
			Opens:          r.Opens,
			UniquePatients: r.UniquePatients,
		})
	}
	return stats, nil
}
