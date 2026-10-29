package store

import (
	"strings"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
)

// 新增-P1：停诊自动触发改期推荐消息。时段停诊时，给该时段每位待处理预约
// （待确认、已确认、候补中）的患者发一条站内消息，附同医生最近 3 个有空位
// 的未来时段；无可推荐时段时明说暂无推荐，仍由待联系清单走人工联系。
// 消息写入失败只记日志、不阻断停诊本身（待联系清单已同步建任务兜底）。
func notifySuspensionReschedule(tx *gorm.DB, slot model.Slot, reason string, now time.Time) error {
	var appointments []model.Appointment
	if err := tx.Where("slot_id = ? AND status IN ?", slot.ID, []string{"pending", "confirmed", "waiting"}).Find(&appointments).Error; err != nil {
		return err
	}
	if len(appointments) == 0 {
		return nil
	}
	var alternatives []model.Slot
	if err := tx.Where("doctor_id = ? AND clinic_id = ? AND id <> ? AND status = ? AND starts_at > ? AND reserved < capacity", slot.DoctorID, slot.ClinicID, slot.ID, "open", now).Order("starts_at, id").Limit(3).Find(&alternatives).Error; err != nil {
		return err
	}
	subject := appointmentSubject(tx, slot.ID)
	var recs []string
	for _, alt := range alternatives {
		recs = append(recs, alt.StartsAt.In(database.Shanghai).Format("2006-01-02 15:04"))
	}
	for _, appointment := range appointments {
		var body strings.Builder
		body.WriteString("您预约的（" + subject + "）因" + reason + "，原预约暂无法按期进行。")
		if len(recs) > 0 {
			body.WriteString("推荐以下同医生可约时段：" + strings.Join(recs, "、") + "。请在患者端改期到推荐时段，或联系客服协助改期。")
		} else {
			body.WriteString("目前暂无同医生可推荐时段，请联系客服协助改期，工作人员会尽快与您联系。")
		}
		if err := writePatientMessage(tx, appointment.PatientID, "suspension_reschedule", "原预约时段停诊，可改期", body.String(), appointment.ID, "", now); err != nil {
			return err
		}
	}
	return nil
}
