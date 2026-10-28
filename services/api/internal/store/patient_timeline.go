package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TimelineEvent struct {
	ID                   string     `json:"id"`
	Kind                 string     `json:"kind"`
	ResourceID           string     `json:"resourceId"`
	AppointmentID        string     `json:"appointmentId"`
	OccurredAt           time.Time  `json:"occurredAt"`
	Title                string     `json:"title"`
	Summary              string     `json:"summary"`
	History              string     `json:"history"`
	Advice               string     `json:"advice"`
	ParentConsultationID string     `json:"parentConsultationId"`
	NextVisitAt          *time.Time `json:"nextVisitAt"`
	StageIndex           int        `json:"stageIndex"`
	StageStatus          string     `json:"stageStatus"`
	AdviceVersion        string     `json:"adviceVersion"`
	ReadAt               *time.Time `json:"readAt"`
}

type AdviceReceipt struct {
	PatientID     string    `gorm:"primaryKey" json:"-"`
	Kind          string    `gorm:"primaryKey" json:"-"`
	ResourceID    string    `gorm:"primaryKey" json:"-"`
	AdviceVersion string    `gorm:"primaryKey" json:"adviceVersion"`
	ReadAt        time.Time `json:"readAt"`
	// TodosCreated is the number of follow-up todos created by this
	// confirmation (0 on a repeated confirmation). Not a column.
	TodosCreated int `gorm:"-" json:"todosCreated,omitempty"`
}

type PatientTimelineRepository interface {
	PatientAdvice(context.Context, string, string, string) (TimelineEvent, error)
	PatientTimeline(context.Context, string, int, int) ([]TimelineEvent, int64, error)
	ConfirmAdviceRead(context.Context, string, string, string, string, time.Time) (AdviceReceipt, error)
}

// Each branch enforces patient ownership before the union is sorted and paginated.
const patientTimelineSQL = `
SELECT c.id AS resource_id, c.visit_type AS kind, COALESCE(c.appointment_id, '') AS appointment_id,
 c.visited_at AS occurred_at, c.chief_complaint AS title, c.assessment AS summary, c.history, c.advice,
 COALESCE(c.parent_consultation_id, '') AS parent_consultation_id, c.next_visit_at, c.stage_index, c.stage_status
 FROM consultations c WHERE c.patient_id = ?
UNION ALL
SELECT v.id, 'plan', a.id, v.published_at, CONCAT('治疗方案 V', v.version),
 JSON_UNQUOTE(JSON_EXTRACT(v.content_json, '$.summary')), '',
 COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(v.content_json, '$.instructions')), 'null'), ''), '', NULL, 0, ''
 FROM care_plan_versions v JOIN care_plans p ON p.id = v.care_plan_id JOIN appointments a ON a.id = p.appointment_id WHERE a.patient_id = ?
UNION ALL
SELECT t.id, 'treatment', t.appointment_id, t.treated_at, '实际治疗', t.summary, '', '', '', NULL, t.stage_index, ''
 FROM treatments t WHERE t.patient_id = ?
UNION ALL
SELECT f.id, 'followup', f.appointment_id, f.due_at, f.title, f.status, '', '', '', NULL, 0, '' FROM followup_tasks f WHERE f.patient_id = ?
UNION ALL
SELECT f.id, 'submitted', f.appointment_id, f.submitted_at, '随访反馈', f.symptoms, '', '', '', NULL, 0, '' FROM followup_tasks f WHERE f.patient_id = ? AND f.submitted_at IS NOT NULL
UNION ALL
SELECT f.id, 'reviewed', f.appointment_id, f.reviewed_at, '医护回评', CONCAT_WS('；', NULLIF(f.response, ''), NULLIF(f.disposition, '')), '', '', '', NULL, 0, '' FROM followup_tasks f WHERE f.patient_id = ? AND f.reviewed_at IS NOT NULL
UNION ALL
SELECT a.id, 'appointment', a.id, s.starts_at, '预约', a.status, '', '', '', NULL, 0, '' FROM appointments a JOIN slots s ON s.id = a.slot_id WHERE a.patient_id = ?
`

func timelineArgs(patientID string) []any {
	return []any{patientID, patientID, patientID, patientID, patientID, patientID, patientID}
}
func adviceVersion(advice string) string {
	if strings.TrimSpace(advice) == "" {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(advice)))
}
func (s *MySQL) PatientTimeline(ctx context.Context, patientID string, page, size int) ([]TimelineEvent, int64, error) {
	rows := make([]TimelineEvent, 0)
	var total int64
	if patientID == "" || page < 1 || size < 1 || size > 100 {
		return rows, 0, ErrCareState
	}
	args := timelineArgs(patientID)
	if err := s.db.WithContext(ctx).Raw("SELECT COUNT(*) FROM ("+patientTimelineSQL+") timeline", args...).Scan(&total).Error; err != nil {
		return rows, 0, err
	}
	args = append(args, size, (page-1)*size)
	if err := s.db.WithContext(ctx).Raw("SELECT * FROM ("+patientTimelineSQL+") timeline ORDER BY occurred_at DESC, kind, resource_id LIMIT ? OFFSET ?", args...).Scan(&rows).Error; err != nil {
		return rows, total, err
	}
	receipts := []AdviceReceipt{}
	ids := []string{}
	for i := range rows {
		rows[i].ID = rows[i].Kind + ":" + rows[i].ResourceID
		rows[i].AdviceVersion = adviceVersion(rows[i].Advice)
		if rows[i].AdviceVersion != "" {
			ids = append(ids, rows[i].ResourceID)
		}
	}
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Where("patient_id = ? AND resource_id IN ?", patientID, ids).Find(&receipts).Error; err != nil {
			return rows, total, err
		}
	}
	for i := range rows {
		for _, receipt := range receipts {
			if receipt.Kind == rows[i].Kind && receipt.ResourceID == rows[i].ResourceID && receipt.AdviceVersion == rows[i].AdviceVersion {
				at := receipt.ReadAt
				rows[i].ReadAt = &at
			}
		}
	}
	return rows, total, nil
}
func (s *MySQL) ConfirmAdviceRead(ctx context.Context, patientID, kind, id, version string, now time.Time) (AdviceReceipt, error) {
	receipt := AdviceReceipt{PatientID: patientID, Kind: kind, ResourceID: id, AdviceVersion: version, ReadAt: now}
	if kind != "initial" && kind != "revisit" && kind != "plan" {
		return receipt, ErrNotFound
	}
	event, err := s.PatientAdvice(ctx, patientID, kind, id)
	if err != nil {
		return receipt, err
	}
	if version == "" || adviceVersion(event.Advice) != version {
		return receipt, ErrConflict
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&receipt)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 1 && kind == "plan" && strings.TrimSpace(event.Advice) != "" {
			count, err := createAdviceTodos(tx, patientID, event, now)
			if err != nil {
				return err
			}
			receipt.TodosCreated = count
		}
		return nil
	})
	if err != nil {
		return receipt, err
	}
	err = s.db.WithContext(ctx).Where("patient_id = ? AND kind = ? AND resource_id = ? AND advice_version = ?", patientID, kind, id, version).First(&receipt).Error
	return receipt, err
}

// adviceTodoKind classifies one instruction line so the generated todo shows
// its kind in the title prefix (followup_tasks has no kind column).
func adviceTodoKind(line string) string {
	for _, word := range []string{"复诊", "复查", "到院", "面诊", "回院"} {
		if strings.Contains(line, word) {
			return "【复诊】"
		}
	}
	for _, word := range []string{"注意", "避免", "不要", "忌", "不可", "禁止", "慎用", "小心", "切勿"} {
		if strings.Contains(line, word) {
			return "【注意事项】"
		}
	}
	return "【护理】"
}

func adviceTodoTitle(prefix, line string) string {
	title := prefix + line
	if utf8.RuneCountInString(title) > 100 {
		title = string([]rune(title)[:100])
	}
	return title
}

// createAdviceTodos splits a published plan version's instructions into one
// follow-up task per non-empty line, so the patient sees them in 我的待办
// and on the recovery calendar and can mark each done. It runs in the same
// transaction as the read receipt, and only on the first confirmation, so a
// repeated confirmation never creates duplicates. The assignee is the
// appointment's doctor (a follow-up staff row); if the doctor is not on the
// staff roster the todos are skipped rather than failing the confirmation.
// Due at the end of the confirmation day: the items are today's care todos.
func createAdviceTodos(tx *gorm.DB, patientID string, event TimelineEvent, now time.Time) (int, error) {
	appointment, err := loadAppointment(tx, event.AppointmentID, patientID)
	if err != nil {
		return 0, err
	}
	var staff model.FollowupStaff
	if err := tx.First(&staff, "id = ?", appointment.DoctorID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	var treatmentID *string
	var treatment model.Treatment
	if err := tx.Select("id").Where("appointment_id = ?", event.AppointmentID).Order("treated_at DESC").First(&treatment).Error; err == nil {
		treatmentID = &treatment.ID
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	due := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
	lines := []string{}
	for _, line := range strings.Split(event.Advice, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) > 10 {
		lines = lines[:10]
	}
	for _, line := range lines {
		task := model.FollowupTask{ID: randomID("followup-"), AppointmentID: event.AppointmentID, TreatmentID: treatmentID, PatientID: patientID, AssigneeID: staff.ID, AssignedAt: now, Title: adviceTodoTitle(adviceTodoKind(line), line), DueAt: due, Status: "pending", Version: 1}
		if err := tx.Create(&task).Error; err != nil {
			return 0, err
		}
	}
	return len(lines), nil
}

func (s *MySQL) PatientAdvice(ctx context.Context, patientID, kind, id string) (TimelineEvent, error) {
	var event TimelineEvent
	if patientID == "" || (kind != "initial" && kind != "revisit" && kind != "plan") {
		return event, ErrNotFound
	}
	args := append(timelineArgs(patientID), kind, id)
	result := s.db.WithContext(ctx).Raw("SELECT * FROM ("+patientTimelineSQL+") timeline WHERE kind = ? AND resource_id = ?", args...).Scan(&event)
	if result.Error != nil {
		return event, result.Error
	}
	if result.RowsAffected != 1 {
		return event, ErrNotFound
	}
	event.ID = event.Kind + ":" + event.ResourceID
	event.AdviceVersion = adviceVersion(event.Advice)
	var receipts []AdviceReceipt
	if err := s.db.WithContext(ctx).Where("patient_id = ? AND kind = ? AND resource_id = ? AND advice_version = ?", patientID, kind, id, event.AdviceVersion).Find(&receipts).Error; err != nil {
		return event, err
	}
	if len(receipts) > 0 {
		event.ReadAt = &receipts[0].ReadAt
	}
	return event, nil
}
