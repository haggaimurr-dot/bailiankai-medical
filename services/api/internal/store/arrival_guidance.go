package store

import (
	"context"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
)

// 新增-P2 行 98：到诊后自动推送面诊流程指引给患者。
// 指引文案按院区配置（arrival_guidance_settings），到诊登记成功后自动发一条
// 站内消息（kind=arrival_guidance）。消息发送失败只记日志，不阻断到诊登记。

type ArrivalGuidanceSetting struct {
	ClinicID  string `gorm:"primaryKey;column:clinic_id"`
	Content   string `gorm:"column:content"`
	Enabled   bool   `gorm:"column:enabled"`
	UpdatedAt time.Time
	UpdatedBy string
}

func (ArrivalGuidanceSetting) TableName() string { return "arrival_guidance_settings" }

func (s *MySQL) GetArrivalGuidance(ctx context.Context, clinicID string) (ArrivalGuidanceSetting, error) {
	var row ArrivalGuidanceSetting
	if err := s.db.WithContext(ctx).First(&row, "clinic_id = ?", clinicID).Error; err != nil {
		return row, err
	}
	return row, nil
}

func (s *MySQL) SaveArrivalGuidance(ctx context.Context, clinicID, content string, enabled bool, now time.Time) (ArrivalGuidanceSetting, error) {
	if scope, ok := scopeFrom(ctx); ok {
		if scope.Role != "manager" {
			return ArrivalGuidanceSetting{}, ErrForbidden
		}
		if len(scope.ClinicIDs) > 0 && !containsString(scope.ClinicIDs, clinicID) {
			return ArrivalGuidanceSetting{}, ErrNotFound
		}
	}
	if len(content) == 0 || len(content) > 2000 {
		return ArrivalGuidanceSetting{}, ErrScheduleInput
	}
	actor := employeeActor(ctx)
	row := ArrivalGuidanceSetting{
		ClinicID: clinicID, Content: content, Enabled: enabled,
		UpdatedAt: now.In(database.Shanghai).Truncate(time.Millisecond), UpdatedBy: actor,
	}
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return ArrivalGuidanceSetting{}, err
	}
	return row, nil
}

// notifyArrivalGuidance 在到诊登记成功后发送面诊流程指引。best-effort：
// 配置不存在或未启用时静默跳过；写入失败只记日志，不阻断到诊。
func (s *MySQL) notifyArrivalGuidance(appointment model.Appointment, now time.Time) {
	var setting ArrivalGuidanceSetting
	if err := s.db.First(&setting, "clinic_id = ?", appointment.ClinicID).Error; err != nil {
		return
	}
	if !setting.Enabled || setting.Content == "" {
		return
	}
	// 预约确认与实际到诊独立统计：指引消息不改变任何统计口径，只做通知。
	_ = s.db.Transaction(func(tx *gorm.DB) error {
		return writePatientMessage(tx, appointment.PatientID, "arrival_guidance", "到诊指引", setting.Content, appointment.ID, "", now)
	})
}

func containsString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
