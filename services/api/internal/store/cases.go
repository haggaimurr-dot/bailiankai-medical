package store

import (
	"bytes"
	"context"
	"encoding/json"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
	"unicode/utf8"
)

const CasePolicyVersion = "case-display-v1"
const CasePolicyContent = "我已查看本版本案例的全部文字和治疗前后图片，同意将其用于百莲凯小程序案例展示，向所有访问者公开。不包含姓名、手机号或患者编号；图片可能使我被识别。我可随时在我的资料中撤回案例展示授权，撤回后停止本应用后续展示，已被他人保存或截图的内容无法追回。本授权不包含社交媒体、广告或其他渠道，不替代诊疗照片授权，拒绝或撤回不影响诊疗服务。案例内容或图片变更须重新获得我的明确授权。"

type CasePair struct {
	BeforeSourceID string    `json:"beforeSourceId"`
	AfterSourceID  string    `json:"afterSourceId"`
	BeforeImageID  string    `json:"beforeImageId"`
	AfterImageID   string    `json:"afterImageId"`
	Angle          string    `json:"angle"`
	BeforeAt       time.Time `json:"beforeAt"`
	AfterAt        time.Time `json:"afterAt"`
}
type CaseContent struct {
	Category string     `json:"category"`
	Tags     []string   `json:"tags"`
	Course   string     `json:"course,omitempty"`
	Title    string     `json:"title"`
	Summary  string     `json:"summary"`
	Body     string     `json:"body"`
	Pairs    []CasePair `json:"pairs"`
}
type CaseView struct {
	model.CaseRecord
	Content       CaseContent `json:"content"`
	PolicyVersion string      `json:"policyVersion"`
	PolicyContent string      `json:"policyContent"`
}
type CaseChange struct {
	Category      string     `json:"category"`
	Tags          []string   `json:"tags"`
	Course        string     `json:"course"`
	AppointmentID string     `json:"appointmentId"`
	Version       int        `json:"version"`
	Title         string     `json:"title"`
	Summary       string     `json:"summary"`
	Body          string     `json:"body"`
	Pairs         []CasePair `json:"pairs"`
	Decision      string     `json:"decision"`
	Note          string     `json:"note"`
	Granted       bool       `json:"granted"`
	PolicyVersion string     `json:"policyVersion"`
}

func (in *CaseChange) UnmarshalJSON(data []byte) error {
	type plain CaseChange
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode((*plain)(in))
}

type CaseFilter struct {
	ClinicID, Status, Category, Tag, Course string
	Page, PageSize                          int
}
type CaseRepository interface {
	Cases(context.Context, string, string, CaseFilter) ([]CaseView, int64, error)
	Case(context.Context, string, string, string) (CaseView, error)
	ChangeCase(context.Context, string, string, string, CaseChange, Media, time.Time) (CaseView, error)
	CaseHistory(context.Context, string) ([]model.CaseEvent, error)
	CaseImage(context.Context, string, string, string, string) (model.CaseImage, error)
}

func caseView(row model.CaseRecord, audience string) (CaseView, error) {
	v := CaseView{CaseRecord: row, PolicyVersion: CasePolicyVersion, PolicyContent: CasePolicyContent}
	err := json.Unmarshal([]byte(row.ContentJSON), &v.Content)
	if v.Content.Tags == nil {
		v.Content.Tags = []string{}
	}
	if audience != "admin" {
		v.PatientID, v.AppointmentID, v.ReviewerID, v.ReviewNote = "", "", "", ""
		for i := range v.Content.Pairs {
			v.Content.Pairs[i].BeforeSourceID, v.Content.Pairs[i].AfterSourceID = "", ""
		}
	}
	return v, err
}
func (s *MySQL) casesQuery(ctx context.Context, tx *gorm.DB, audience, patient string) *gorm.DB {
	q := tx.Model(&model.CaseRecord{})
	switch audience {
	case "admin":
		scope, ok := scopeFrom(ctx)
		if !ok || (scope.Role != "manager" && scope.Role != "doctor" && scope.Role != "clinician") {
			return q.Where("1 = 0")
		}
		q = clinicScope(ctx, q, "clinic_id")
	case "patient":
		if patient == "" {
			return q.Where("1 = 0")
		}
		q = q.Where("patient_id = ?", patient)
	case "public":
		q = q.Where("published_at IS NOT NULL AND consent_revision = revision AND review_status = ? AND clinic_id IN (SELECT id FROM clinics WHERE active = ? AND published_json IS NOT NULL)", "approved", true)
	default:
		q = q.Where("1 = 0")
	}
	return q
}
func (s *MySQL) Cases(ctx context.Context, audience, patient string, f CaseFilter) ([]CaseView, int64, error) {
	q := s.casesQuery(ctx, s.db.WithContext(ctx), audience, patient)
	if f.ClinicID != "" {
		q = q.Where("clinic_id = ?", f.ClinicID)
	}
	if f.Category != "" {
		q = q.Where("JSON_UNQUOTE(JSON_EXTRACT(content_json, '$.category')) = ?", f.Category)
	}
	if f.Tag != "" {
		tag, _ := json.Marshal(f.Tag)
		q = q.Where("JSON_CONTAINS(JSON_EXTRACT(content_json, '$.tags'), ?)", string(tag))
	}
	if f.Course != "" {
		q = q.Where("JSON_UNQUOTE(JSON_EXTRACT(content_json, '$.course')) = ?", f.Course)
	}
	if f.Status == "published" {
		q = q.Where("published_at IS NOT NULL")
	}
	if f.Status == "unpublished" {
		q = q.Where("published_at IS NULL")
	}
	if f.Page < 1 || f.PageSize < 1 || f.PageSize > 100 {
		return nil, 0, ErrCareState
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []model.CaseRecord{}
	if err := q.Order("id DESC").Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	result := []CaseView{}
	for _, row := range rows {
		v, err := caseView(row, audience)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, v)
	}
	return result, total, nil
}
func (s *MySQL) Case(ctx context.Context, id, audience, patient string) (CaseView, error) {
	var row model.CaseRecord
	if err := s.casesQuery(ctx, s.db.WithContext(ctx), audience, patient).First(&row, "id = ?", id).Error; err != nil {
		return CaseView{}, followupDBError(err)
	}
	return caseView(row, audience)
}
func (s *MySQL) ChangeCase(ctx context.Context, id, action, patient string, in CaseChange, media Media, now time.Time) (CaseView, error) {
	if action == "create" || action == "update" {
		if !NormalizeCaseMetadata(&in) {
			return CaseView{}, ErrCareState
		}
	}
	scope, scoped := scopeFrom(ctx)
	if action == "consent" {
		if patient == "" || scoped {
			return CaseView{}, ErrForbidden
		}
	} else if !scoped || (action == "review" && scope.Role != "doctor" && scope.Role != "clinician") || (action != "review" && scope.Role != "manager") {
		return CaseView{}, ErrForbidden
	}
	var row model.CaseRecord
	audience := "admin"
	if action == "consent" {
		audience = "patient"
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Appointment lock precedes case lock, matching clinical photo writes/withdrawals.
		appointmentID := in.AppointmentID
		if action != "create" {
			var existing model.CaseRecord
			if err := s.casesQuery(ctx, tx, audience, patient).First(&existing, "id = ?", id).Error; err != nil {
				return followupDBError(err)
			}
			appointmentID = existing.AppointmentID
		}
		var appt model.Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&appt, "id = ?", appointmentID).Error; err != nil {
			return followupDBError(err)
		}
		if action == "create" {
			var slot model.Slot
			if err := clinicScope(ctx, tx, "clinic_id").First(&slot, "id = ?", appt.SlotID).Error; err != nil {
				return followupDBError(err)
			}
			var treatment model.Treatment
			if appt.Status != "completed" {
				return ErrCareState
			}
			if err := tx.First(&treatment, "appointment_id = ? AND patient_id = ?", appt.ID, appt.PatientID).Error; err != nil {
				return followupDBError(err)
			}
			row = model.CaseRecord{ID: randomID("case-"), AppointmentID: appt.ID, PatientID: appt.PatientID, ClinicID: slot.ClinicID, Version: 1, Revision: 1, ReviewStatus: "pending", ContentJSON: "{}"}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else {
			if err := s.casesQuery(ctx, tx, audience, patient).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
				return followupDBError(err)
			}
			// Withdrawal is always effective, even from an older patient screen.
			if !(action == "consent" && !in.Granted) && row.Version != in.Version {
				return ErrConflict
			}
			row.Version++
		}
		switch action {
		case "create", "update":
			if media == nil || len(in.Pairs) < 1 || len(in.Pairs) > 3 {
				return ErrCareState
			}
			consent, err := latestPhotoConsent(tx, appt.ID, appt.PatientID)
			if err != nil {
				return err
			}
			if consent.Action != "granted" || consent.Version != "photo-care-v2" {
				return ErrPhotoConsent
			}
			content := CaseContent{Category: in.Category, Tags: in.Tags, Course: in.Course, Title: in.Title, Summary: in.Summary, Body: in.Body, Pairs: []CasePair{}}
			seen := map[string]bool{}
			for _, pair := range in.Pairs {
				var before model.CarePhoto
				var after model.FollowupPhoto
				if err := tx.First(&before, "id = ? AND appointment_id = ? AND patient_id = ?", pair.BeforeSourceID, appt.ID, appt.PatientID).Error; err != nil {
					return followupDBError(err)
				}
				if err := tx.First(&after, "id = ? AND appointment_id = ? AND patient_id = ?", pair.AfterSourceID, appt.ID, appt.PatientID).Error; err != nil {
					return followupDBError(err)
				}
				var task model.FollowupTask
				if err := tx.First(&task, "id = ? AND submitted_at IS NOT NULL", after.TaskID).Error; err != nil {
					return followupDBError(err)
				}
				var treatment model.Treatment
				if task.TreatmentID == nil {
					return ErrCareState
				}
				if err := tx.First(&treatment, "id = ? AND appointment_id = ?", *task.TreatmentID, appt.ID).Error; err != nil {
					return followupDBError(err)
				}
				if before.Angle != after.Angle || seen[before.Angle] || before.UploadedAt.After(treatment.TreatedAt) || after.UploadedAt.Before(treatment.TreatedAt) {
					return ErrCareState
				}
				seen[before.Angle] = true
				pair = CasePair{BeforeSourceID: before.ID, AfterSourceID: after.ID, Angle: before.Angle, BeforeAt: before.UploadedAt, AfterAt: after.UploadedAt}
				for _, source := range []struct {
					id, hash string
					size     int64
					target   *string
				}{{before.ID, before.SHA256, before.SizeBytes, &pair.BeforeImageID}, {after.ID, after.SHA256, after.SizeBytes, &pair.AfterImageID}} {
					data, err := media.Read(ctx, source.id)
					if err != nil {
						return err
					}
					if err := VerifyMedia(data, MediaReference{ID: source.id, SHA256: source.hash, SizeBytes: source.size}); err != nil {
						return err
					}
					imageID := randomID("photo-case-")
					if err := media.Save(ctx, imageID, data); err != nil {
						return err
					}
					if err := tx.Create(&model.CaseImage{ID: imageID, CaseID: row.ID, SHA256: source.hash, SizeBytes: source.size}).Error; err != nil {
						return err
					}
					*source.target = imageID
				}
				content.Pairs = append(content.Pairs, pair)
			}
			data, err := json.Marshal(content)
			if err != nil {
				return err
			}
			row.ContentJSON = string(data)
			if action == "update" {
				row.Revision++
			}
			row.ConsentRevision, row.ReviewStatus, row.ReviewNote, row.ReviewerID, row.ReviewerName, row.ReviewedAt, row.PublishedAt = 0, "pending", "", "", "", nil, nil
		case "consent":
			if in.Granted {
				if in.PolicyVersion != CasePolicyVersion {
					return ErrCareState
				}
				row.ConsentRevision = row.Revision
			} else {
				row.ConsentRevision, row.PublishedAt = 0, nil
			}
		case "review":
			if (in.Decision != "approved" && in.Decision != "rejected") || (in.Decision == "rejected" && in.Note == "") {
				return ErrCareState
			}
			var doctor model.FollowupStaff
			if scope.StaffID == "" {
				return ErrForbidden
			}
			if err := tx.First(&doctor, "id = ? AND role = ? AND status = ?", scope.StaffID, "doctor", "on_duty").Error; err != nil {
				return ErrForbidden
			}
			row.ReviewStatus, row.ReviewNote, row.ReviewerID, row.ReviewerName, row.ReviewedAt = in.Decision, in.Note, employeeActor(ctx), doctor.DisplayName, &now
			if in.Decision == "rejected" {
				row.PublishedAt = nil
			}
		case "publish":
			if row.ConsentRevision != row.Revision || row.ReviewStatus != "approved" || row.ReviewedAt == nil {
				return ErrCareState
			}
			var clinic model.Clinic
			if err := tx.First(&clinic, "id = ? AND active = ? AND published_json IS NOT NULL", row.ClinicID, true).Error; err != nil {
				return ErrCareState
			}
			row.PublishedAt = &now
		case "unpublish":
			row.PublishedAt = nil
		default:
			return ErrCareState
		}
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		snapshot, err := caseView(row, "admin")
		if err != nil {
			return err
		}
		data, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		actor := employeeActor(ctx)
		policyVersion, policy := "", ""
		if action == "consent" {
			actor, policyVersion, policy = patient, CasePolicyVersion, CasePolicyContent
		}
		return tx.Create(&model.CaseEvent{ID: randomID("case-event-"), CaseID: row.ID, Version: row.Version, Action: action, ActorID: actor, SnapshotJSON: string(data), PolicyVersion: policyVersion, PolicyContent: policy, CreatedAt: now}).Error
	})
	if err != nil {
		return CaseView{}, err
	}
	return caseView(row, audience)
}
func (s *MySQL) CaseHistory(ctx context.Context, id string) ([]model.CaseEvent, error) {
	if _, err := s.Case(ctx, id, "admin", ""); err != nil {
		return nil, err
	}
	rows := []model.CaseEvent{}
	err := s.db.WithContext(ctx).Where("case_id = ?", id).Order("version DESC").Find(&rows).Error
	return rows, err
}
func (s *MySQL) CaseImage(ctx context.Context, caseID, imageID, audience, patient string) (model.CaseImage, error) {
	v, err := s.Case(ctx, caseID, audience, patient)
	if err != nil {
		return model.CaseImage{}, err
	}
	found := false
	for _, pair := range v.Content.Pairs {
		if pair.BeforeImageID == imageID || pair.AfterImageID == imageID {
			found = true
		}
	}
	if !found {
		return model.CaseImage{}, ErrNotFound
	}
	var image model.CaseImage
	err = s.db.WithContext(ctx).First(&image, "id = ? AND case_id = ?", imageID, caseID).Error
	return image, followupDBError(err)
}

// Metadata is part of the authorized snapshot, including subsequent edits.
func NormalizeCaseMetadata(in *CaseChange) bool {
	in.Category = strings.TrimSpace(in.Category)
	in.Course = strings.TrimSpace(in.Course)
	if utf8.RuneCountInString(in.Category) > 40 || len(in.Tags) > 10 || utf8.RuneCountInString(in.Course) > 20 {
		return false
	}
	tags := []string{}
	seen := map[string]bool{}
	for _, value := range in.Tags {
		tag := strings.TrimSpace(value)
		if tag == "" || utf8.RuneCountInString(tag) > 30 {
			return false
		}
		if !seen[tag] {
			tags = append(tags, tag)
			seen[tag] = true
		}
	}
	in.Tags = tags
	return true
}
