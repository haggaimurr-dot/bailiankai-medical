package store

import (
	"context"
	"encoding/json"
	"time"
)

// Templates are private to an employee.
type CarePlanTemplate struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	OwnerID     string    `json:"-"`
	Name        string    `json:"name"`
	ContentJSON string    `json:"-"`
	CreatedAt   time.Time `json:"createdAt"`
}

type CareTemplateRepository interface {
	ListCareTemplates(context.Context, string) ([]CarePlanTemplate, error)
	CreateCareTemplate(context.Context, string, string, CarePlanContent, time.Time) (CarePlanTemplate, error)
	DeleteCareTemplate(context.Context, string, string) error
}

func (s *MySQL) ListCareTemplates(ctx context.Context, owner string) ([]CarePlanTemplate, error) {
	rows := []CarePlanTemplate{}
	err := s.db.WithContext(ctx).Where("owner_id = ?", owner).Order("created_at DESC, id DESC").Find(&rows).Error
	return rows, err
}

func (s *MySQL) CreateCareTemplate(ctx context.Context, owner, name string, content CarePlanContent, now time.Time) (CarePlanTemplate, error) {
	data, err := json.Marshal(content)
	if err != nil {
		return CarePlanTemplate{}, err
	}
	row := CarePlanTemplate{ID: randomID("template-"), OwnerID: owner, Name: name, ContentJSON: string(data), CreatedAt: now}
	err = s.db.WithContext(ctx).Create(&row).Error
	return row, err
}

func (s *MySQL) DeleteCareTemplate(ctx context.Context, owner, id string) error {
	result := s.db.WithContext(ctx).Where("owner_id = ? AND id = ?", owner, id).Delete(&CarePlanTemplate{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
