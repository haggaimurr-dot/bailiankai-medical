package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Seed only inserts missing demo rows; rerunning it preserves admin edits.
// It creates today's future slots plus the following six Shanghai dates.
func Seed(ctx context.Context, db *gorm.DB, now time.Time) error {
	now = now.In(Shanghai)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, Shanghai)
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		clinic := model.Clinic{ID: "clinic-demo", Name: "百莲凯 · 演示院区", Address: "演示地址，待院方核验", Phone: "待配置"}
		clinic.DirectoryState = model.DirectoryState{Active: true, Version: 1, PublishedAt: &now}
		content, _ := json.Marshal(clinic)
		snapshot := string(content)
		clinic.PublishedJSON = &snapshot
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&clinic).Error; err != nil {
			return err
		}
		doctors := []model.Doctor{
			{ID: "doctor-demo-1", ClinicID: clinic.ID, Name: "演示医生一", Title: "演示资料", Summary: "虚构演示人物，用于验证医生列表与排班联动。", Focus: []string{"面诊咨询", "方案沟通"}, Initials: "一"},
			{ID: "doctor-demo-2", ClinicID: clinic.ID, Name: "演示医生二", Title: "演示资料", Summary: "虚构演示人物，用于验证多医生排班筛选。", Focus: []string{"面诊咨询", "复诊沟通"}, Initials: "二"},
		}
		for i := range doctors {
			doctors[i].DirectoryState = model.DirectoryState{Active: true, Version: 1, PublishedAt: &now}
			content, _ := json.Marshal(doctors[i])
			snapshot := string(content)
			doctors[i].PublishedJSON = &snapshot
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&doctors).Error; err != nil {
			return err
		}
		for _, kind := range []string{"clinics", "doctors"} {
			// Only add a baseline when none exists; preserve edited demo history.
			if err := tx.Exec("INSERT INTO directory_revisions (id,kind,resource_id,version,action,content_json,actor_id,created_at) SELECT UUID(), ?, id, version, 'seeded', JSON_OBJECT('profile',published_json,'active',active), 'system', ? FROM "+kind+" WHERE NOT EXISTS (SELECT 1 FROM directory_revisions r WHERE r.kind = ? AND r.resource_id = "+kind+".id)", kind, now, kind).Error; err != nil {
				return err
			}
		}
		staff := []model.FollowupStaff{
			{ID: doctors[0].ID, DisplayName: doctors[0].Name, Role: "doctor", Status: "on_duty", Version: 1},
			{ID: doctors[1].ID, DisplayName: doctors[1].Name, Role: "doctor", Status: "on_duty", Version: 1},
			{ID: "nurse-demo-1", DisplayName: "演示随访护士", Role: "nurse", Status: "on_duty", Version: 1},
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&staff).Error; err != nil {
			return err
		}
		for day := range 7 {
			date := today.AddDate(0, 0, day)
			for _, doctor := range doctors {
				for _, hour := range []int{9, 10, 11, 14, 15, 16} {
					start := date.Add(time.Duration(hour) * time.Hour)
					if !start.After(now) {
						continue
					}
					slot := model.Slot{ID: fmt.Sprintf("%s-%s-%02d", doctor.ID, date.Format("20060102"), hour), ClinicID: clinic.ID, DoctorID: doctor.ID, StartsAt: start, EndsAt: start.Add(30 * time.Minute), Capacity: 3, Reserved: 0, Status: "open", Version: 1}
					if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&slot).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}
