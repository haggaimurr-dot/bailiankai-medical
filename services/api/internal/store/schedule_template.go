package store

import (
	"context"
	"time"

	"example.com/bailiankai/api/internal/model"
)

func (s *MySQL) CreateScheduleTemplate(ctx context.Context, tpl model.ScheduleTemplate) (model.ScheduleTemplate, error) {
	if tpl.ID == "" {
		tpl.ID = randomID("sched-tpl-")
	}
	tpl.CreatedAt = time.Now()
	if err := s.db.WithContext(ctx).Create(&tpl).Error; err != nil {
		return model.ScheduleTemplate{}, err
	}
	return tpl, nil
}

func (s *MySQL) ListScheduleTemplates(ctx context.Context, clinicID string) ([]model.ScheduleTemplate, error) {
	var rows []model.ScheduleTemplate
	q := s.db.WithContext(ctx).Order("created_at DESC")
	if clinicID != "" {
		q = q.Where("clinic_id = ?", clinicID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *MySQL) GetScheduleTemplate(ctx context.Context, id string) (model.ScheduleTemplate, error) {
	var tpl model.ScheduleTemplate
	if err := s.db.WithContext(ctx).First(&tpl, "id = ?", id).Error; err != nil {
		return model.ScheduleTemplate{}, err
	}
	return tpl, nil
}

func (s *MySQL) SetHoliday(ctx context.Context, date time.Time, name string) error {
	h := model.Holiday{HolidayDate: date.Truncate(24 * time.Hour), Name: name, CreatedAt: time.Now()}
	return s.db.WithContext(ctx).Save(&h).Error
}

func (s *MySQL) RemoveHoliday(ctx context.Context, date time.Time) error {
	return s.db.WithContext(ctx).Delete(&model.Holiday{}, "holiday_date = ?", date.Truncate(24*time.Hour)).Error
}

func (s *MySQL) ListHolidays(ctx context.Context, from, to time.Time) ([]model.Holiday, error) {
	var rows []model.Holiday
	if err := s.db.WithContext(ctx).Where("holiday_date BETWEEN ? AND ?", from.Truncate(24*time.Hour), to.Truncate(24*time.Hour)).Order("holiday_date ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *MySQL) IsHoliday(ctx context.Context, date time.Time) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.Holiday{}).Where("holiday_date = ?", date.Truncate(24*time.Hour)).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
