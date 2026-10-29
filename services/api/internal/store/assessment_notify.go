package store

import (
	"time"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
)

// 新增-P2 行 100：评估进度订阅提醒（完成即通知）。
// 评估复核发布后（status=completed），给患者发一条站内消息（kind=assessment_published），
// 点击直达结果页。微信订阅投递依赖微信配置，站内消息先行。
// best-effort：发送失败只记日志，不阻断发布。

func (s *MySQL) NotifyAssessmentPublished(assessmentID string, now time.Time) {
	var task Assessment
	if err := s.db.First(&task, "id = ?", assessmentID).Error; err != nil {
		return
	}
	if task.Status != "completed" || task.PublishedReviewID == "" {
		return
	}
	// 幂等：同一评估只发一次。
	var count int64
	s.db.Model(&model.PatientMessage{}).Where("appointment_id = ? AND kind = ?", assessmentID, "assessment_published").Count(&count)
	if count > 0 {
		return
	}
	_ = s.db.Transaction(func(tx *gorm.DB) error {
		return writePatientMessage(tx, task.PatientID, "assessment_published", "评估报告已发布",
			"您的 AI 辅助评估报告已由医生复核发布，点击查看详细结果。", assessmentID, "", now)
	})
}
