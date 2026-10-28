package store

import (
	"context"
	"errors"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// PrivacyPolicyVersion 是当前隐私政策版本；发布新版即改此值并部署。
// 用 var 而非 const，以便集成测试覆盖“发布新版”后通知的生成。
var PrivacyPolicyVersion = "2026-10-02.v1"
const PrivacyPolicyContent = "百莲凯隐私说明（试点草案）：微信身份用于账号与档案归属；姓名和联系手机号用于预约及联系；诊疗方案、随访与照片仅用于本次诊疗和恢复管理，不用于营销公开展示。账号手机号和诊疗照片另行授权，可分别撤回。医生和护理仅访问本人负责且所属授权院区的资料；客服仅处理预约及联系资料，不访问诊疗方案或照片；运营不得访问患者明细。您可在个人资料中查看同意记录或撤回同意；撤回不自动删除既有诊疗记录。正式服务主体、保存期限、隐私联系渠道及删除申请流程须由院方审核补充后上线。"

var ErrPrivacyVersion = errors.New("privacy policy version has changed")

type PrivacyRepository interface {
	PrivacyConsentEvents(context.Context, string) ([]model.PrivacyConsentEvent, error)
	RecordPrivacyConsent(context.Context, string, string, string, time.Time) (model.PrivacyConsentEvent, error)
}

func (s *MySQL) PrivacyConsentEvents(ctx context.Context, patientID string) ([]model.PrivacyConsentEvent, error) {
	rows := make([]model.PrivacyConsentEvent, 0)
	err := s.db.WithContext(ctx).Where("patient_id = ?", patientID).Order("recorded_at DESC, id DESC").Find(&rows).Error
	return rows, err
}
func (s *MySQL) RecordPrivacyConsent(ctx context.Context, patientID, version, action string, now time.Time) (model.PrivacyConsentEvent, error) {
	row := model.PrivacyConsentEvent{}
	if version != PrivacyPolicyVersion {
		return row, ErrPrivacyVersion
	}
	if action != "accepted" && action != "revoked" {
		return row, ErrCareState
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var patient model.Patient
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&patient, "id = ?", patientID).Error; err != nil {
			return followupDBError(err)
		}
		var latest model.PrivacyConsentEvent
		err := tx.Where("patient_id = ?", patientID).Order("recorded_at DESC, id DESC").First(&latest).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && latest.PolicyVersion == version && latest.Action == action {
			row = latest
			return nil
		}
		if action == "revoked" && (err != nil || latest.Action != "accepted") {
			return ErrCareState
		}
		recorded := now.Truncate(time.Millisecond)
		if !latest.RecordedAt.IsZero() && !recorded.After(latest.RecordedAt) {
			recorded = latest.RecordedAt.Add(time.Millisecond)
		}
		row = model.PrivacyConsentEvent{ID: randomID("privacy-"), PatientID: patientID, PolicyVersion: version, PolicyContent: PrivacyPolicyContent, Action: action, Source: "miniprogram", RecordedAt: recorded}
		return tx.Create(&row).Error
	})
	return row, err
}
