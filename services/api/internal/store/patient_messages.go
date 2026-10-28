package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
)

// writePatientMessage inserts one inbox message inside the caller's transaction.
func writePatientMessage(tx *gorm.DB, patientID, kind, title, body, appointmentID, ticketID string, now time.Time) error {
	return tx.Create(&model.PatientMessage{
		ID: randomID("pmsg-"), PatientID: patientID, Kind: kind,
		Title: title, Body: body, AppointmentID: appointmentID, TicketID: ticketID, CreatedAt: now,
	}).Error
}

// MarkSupportRepliesRead marks a ticket's unread support-reply messages read
// when the patient opens the ticket detail (P2 咨询-患者端：进详情后角标清零).
func (s *MySQL) MarkSupportRepliesRead(ctx context.Context, ticketID, patientID string, now time.Time) error {
	return s.db.WithContext(ctx).Model(&model.PatientMessage{}).
		Where("patient_id = ? AND ticket_id = ? AND kind = ? AND read_at IS NULL", patientID, ticketID, "support_reply").
		Update("read_at", now).Error
}

// appointmentSubject renders "医生名 · YYYY-MM-DD HH:mm（院区名）" for an appointment.
func appointmentSubject(tx *gorm.DB, slotID string) string {
	var slot model.Slot
	doctorName, clinicName, when := "", "", ""
	if err := tx.First(&slot, "id = ?", slotID).Error; err == nil {
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
	return subject
}

// appointmentStatusMessage builds the inbox message for an appointment status
// change. `to` is the new status; `detail` carries the reschedule "old → new"
// text for reschedules, or the literal "claim" for a waitlist claim.
func appointmentStatusMessage(tx *gorm.DB, a *model.Appointment, to, reason, detail string, now time.Time) error {
	subject := appointmentSubject(tx, a.SlotID)
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
	return writePatientMessage(tx, a.PatientID, "appointment_status", title, body, a.ID, "", now)
}

// AppointmentRemindResult is the per-appointment outcome of a batch remind.
type AppointmentRemindResult struct {
	AppointmentID string
	Sent          bool
	Reason        string
}

// RemindAppointments sends a "就诊提醒" inbox message to the patient of each
// appointment. Each id is processed independently so one bad id cannot block
// the rest; failures are reported per item instead of aborting the batch.
func (s *MySQL) RemindAppointments(ctx context.Context, ids []string, now time.Time) ([]AppointmentRemindResult, error) {
	results := make([]AppointmentRemindResult, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		result := AppointmentRemindResult{AppointmentID: id}
		accessible, err := s.CanAccessAppointment(ctx, id, false)
		if err != nil {
			return results, err
		}
		if !accessible {
			result.Reason = "预约不存在或无权访问"
			results = append(results, result)
			continue
		}
		var row model.Appointment
		if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				result.Reason = "预约不存在或无权访问"
				results = append(results, result)
				continue
			}
			return results, err
		}
		switch row.Status {
		case "pending", "confirmed", "waiting":
		default:
			result.Reason = "当前状态无需提醒"
			results = append(results, result)
			continue
		}
		err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return writePatientMessage(tx, row.PatientID, "reminder", "就诊提醒", "您的预约（"+appointmentSubject(tx, row.SlotID)+"）即将到院，请按时到访。如需改期或取消，请提前联系门店。", row.ID, "", now)
		})
		if err != nil {
			return results, err
		}
		result.Sent = true
		results = append(results, result)
	}
	return results, nil
}

// ensurePrivacyPolicyMessage 隐私政策变更通知：患者尚未就当前版本表态
// （无同意记录，或最近一次同意记录的版本不是当前版）时，为当前版补一条
// 站内通知；每版只补一条，重复打开收件箱不重复发。发布新版即改
// PrivacyPolicyVersion 并部署，通知在患者下次打开收件箱或拉取未读数时生成。
func (s *MySQL) ensurePrivacyPolicyMessage(ctx context.Context, patientID string, now time.Time) error {
	var latest model.PrivacyConsentEvent
	err := s.db.WithContext(ctx).Where("patient_id = ?", patientID).Order("recorded_at DESC, id DESC").First(&latest).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil && latest.PolicyVersion == PrivacyPolicyVersion {
		return nil
	}
	title := "隐私政策已更新（" + PrivacyPolicyVersion + "）"
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.PatientMessage{}).Where("patient_id = ? AND kind = ? AND title = ?", patientID, "privacy_policy", title).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return writePatientMessage(s.db.WithContext(ctx), patientID, "privacy_policy", title, "隐私政策已更新至 "+PrivacyPolicyVersion+"，请阅读新版政策；如不同意，可在「我的资料」中撤回同意。", "", "", now)
}

func (s *MySQL) PatientMessages(ctx context.Context, patientID string, page, size int) ([]model.PatientMessage, int64, error) {
	if err := s.ensurePrivacyPolicyMessage(ctx, patientID, time.Now()); err != nil {
		return nil, 0, err
	}
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
	if err := s.ensurePrivacyPolicyMessage(ctx, patientID, time.Now()); err != nil {
		return 0, err
	}
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
