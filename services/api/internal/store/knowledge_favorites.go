package store

import (
	"context"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm/clause"
	"time"
)

type KnowledgeFavoriteRepository interface {
	KnowledgeFavorite(context.Context, string, string) (bool, error)
	SetKnowledgeFavorite(context.Context, string, string, bool, time.Time) error
}

func (s *MySQL) KnowledgeFavorite(ctx context.Context, patient, article string) (bool, error) {
	if _, err := s.KnowledgeArticle(ctx, article, true); err != nil {
		return false, err
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&model.KnowledgeFavorite{}).Where("patient_id = ? AND article_id = ?", patient, article).Count(&count).Error
	return count > 0, err
}
func (s *MySQL) SetKnowledgeFavorite(ctx context.Context, patient, article string, saved bool, now time.Time) error {
	if !saved {
		return s.db.WithContext(ctx).Where("patient_id = ? AND article_id = ?", patient, article).Delete(&model.KnowledgeFavorite{}).Error
	}
	if _, err := s.KnowledgeArticle(ctx, article, true); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model.KnowledgeFavorite{PatientID: patient, ArticleID: article, CreatedAt: now}).Error
}
