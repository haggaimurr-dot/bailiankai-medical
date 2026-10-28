package store

import (
	"context"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
)

// writePatientMessage inserts one inbox message inside the caller's transaction.
func writePatientMessage(tx *gorm.DB, patientID, kind, title, body, appointmentID string, now time.Time) error {
	return tx.Create(&model.PatientMessage{
		ID: randomID("pmsg-"), PatientID: patientID, Kind: kind,
		Title: title, Body: body, AppointmentID: appointmentID, CreatedAt: now,
	}).Error
}

// appointmentStatusMessage builds the inbox message for an appointment status
// change. `to` is the new status; `detail` carries the reschedule "old → new"
// text for reschedules, or the literal "claim" for a waitlist claim.
func appointmentStatusMessage(tx *gorm.DB, a *model.Appointment, to, reason, detail string, now time.Time) error {
	var slot model.Slot
	doctorName, clinicName, when := "", "", ""
	if err := tx.First(&slot, "id = ?", a.SlotID).Error; err == nil {
		when = slot.StartsAt.In(database.Shanghai).Format("2006-01-02 15:04")
		var doctor model.Doctor
		if err := tx.First(&doctor, "id = ?", slot.DoctorID).Error; err == nil {
			doctorName = doctor.Name
		}
		var clinic model.Clinic
		if err := tx.First(&clinic, "id = ?", slot.ClinicID).Error; err == nil {
			clinicName = clinic.Name
		}
	}
	if doctorName == "" {
		doctorName = "医生"
	}
	subject := doctorName + " · " + when
	if clinicName != "" {
		subject += "（" + clinicName + "）"
	}
	var title, body string
	switch to {
	case "confirmed":
		title, body = "预约已确认", "您的预约（"+subject+"）已确认，请按时到院。"
	case "rejected":
		suffix := ""
		if strings.TrimSpace(reason) != "" {
			suffix = "，原因："+strings.TrimSpace(reason)
		}
		title, body = "预约未通过", "您的预约（"+subject+"）未通过"+suffix+"，您可以重新选择时间预约。"
	case "cancelled":
		title, body = "预约已取消", "您的预约（"+subject+"）已取消，名额已释放。如需再次到院，请重新预约。"
	case "expired":
		title, body = "预约申请已过期", "您的预约（"+subject+"）确认已超时，申请已过期，请重新提交申请。"
	case "completed":
		title, body = "治疗已记录", "您的就诊（"+subject+"）治疗已记录，可在预约详情查看随访任务。"
	case "pending":
		if detail == "claim" {
			title, body = "候补转为待确认", "您的候补（"+subject+"）有了空位，已转为待确认，请等待工作人员确认。"
		} else if detail != "" {
			title, body = "预约时间已更改", "您的预约已改期："+detail+"。请按新时间到院。"
		} else {
			return nil
		}
	default:
		return nil
	}
	return writePatientMessage(tx, a.PatientID, "appointment_status", title, body, a.ID, now)
}

func (s *MySQL) PatientMessages(ctx context.Context, patientID string, page, size int) ([]model.PatientMessage, int64, error) {
	rows := make([]model.PatientMessage, 0)
	q := s.db.WithContext(ctx).Model(&model.PatientMessage{}).Where("patient_id = ?", patientID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return rows, 0, err
	}
	err := q.Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

func (s *MySQL) UnreadPatientMessageCount(ctx context.Context, patientID string) (int64, error) {
	var total int64
	err := s.db.WithContext(ctx).Model(&model.PatientMessage{}).Where("patient_id = ? AND read_at IS NULL", patientID).Count(&total).Error
	return total, err
}

func (s *MySQL) MarkPatientMessageRead(ctx context.Context, patientID, id string, now time.Time) error {
	result := s.db.WithContext(ctx).Model(&model.PatientMessage{}).Where("patient_id = ? AND id = ? AND read_at IS NULL", patientID, id).Update("read_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.PatientMessage{}).Where("patient_id = ? AND id = ?", patientID, id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
