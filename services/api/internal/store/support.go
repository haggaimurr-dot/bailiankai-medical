package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"example.com/bailiankai/api/internal/model"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/http"
	"time"
)

type SupportUpload struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}
type SupportInput struct {
	Rating      int             `json:"rating,omitempty"`
	Attachments []SupportUpload `json:"attachments,omitempty"`
	ClinicID    string          `json:"clinicId"`
	Subject     string          `json:"subject"`
	Category    string          `json:"category"`
	Content     string          `json:"content"`
	RequestKey  string          `json:"requestKey"`
	Action      string          `json:"action"`
	AssigneeID  string          `json:"assigneeId"`
	Status      string          `json:"status"`
	DueAt       *time.Time      `json:"dueAt"`
	Version     int             `json:"version"`
}
type SupportFilter struct {
	PatientID, Status, Assignment string
	Overdue                       bool
	Page, Size                    int
	Now                           time.Time
}
type SupportAgent struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	ClinicIDs   []string `json:"clinicIds"`
}
type SupportRepository interface {
	SupportTickets(context.Context, SupportFilter) ([]model.SupportTicket, int64, error)
	SupportDetail(context.Context, string, string, int, int) (model.SupportTicket, []model.SupportEvent, int64, error)
	CreateSupport(context.Context, string, SupportInput, time.Time) (model.SupportTicket, error)
	ChangeSupport(context.Context, string, string, SupportInput, time.Time) (model.SupportTicket, error)
	SupportAgents(context.Context) ([]SupportAgent, error)
}

func supportHash(in SupportInput) string {
	b, _ := json.Marshal(in)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func supportRole(ctx context.Context) bool {
	scope, ok := scopeFrom(ctx)
	return ok && (scope.Role == "manager" || scope.Role == "customer_service")
}
func (s *MySQL) supportQuery(ctx context.Context, db *gorm.DB, patientID string) *gorm.DB {
	if patientID != "" {
		return db.Where("patient_id = ?", patientID)
	}
	if !supportRole(ctx) {
		return db.Where("1=0")
	}
	q := clinicScope(ctx, db, "clinic_id")
	scope, _ := scopeFrom(ctx)
	if scope.Role == "customer_service" {
		q = q.Where("assignee_id = ? OR assignee_id = ''", employeeActor(ctx))
	}
	return q
}
func (s *MySQL) SupportTickets(ctx context.Context, f SupportFilter) ([]model.SupportTicket, int64, error) {
	rows := make([]model.SupportTicket, 0)
	var total int64
	q := s.supportQuery(ctx, s.db.WithContext(ctx).Model(&model.SupportTicket{}), f.PatientID)
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Assignment == "mine" {
		q = q.Where("assignee_id = ?", employeeActor(ctx))
	}
	if f.Assignment == "unassigned" {
		q = q.Where("assignee_id = ''")
	}
	if f.Overdue {
		q = q.Where("due_at < ? AND status NOT IN ?", f.Now, []string{"resolved", "closed"})
	}
	if err := q.Count(&total).Error; err != nil {
		return rows, 0, err
	}
	// Queue order: overdue unresolved tickets first, then longest waiting
	// (oldest created), with id as the deterministic tie-break so the order
	// is stable across refreshes. The cutoff is inlined as a literal because
	// GORM's Order() silently drops clause.Expr values.
	err := q.Order("CASE WHEN due_at < '" + f.Now.Format("2006-01-02 15:04:05") + "' AND status NOT IN ('resolved', 'closed') THEN 0 ELSE 1 END, created_at ASC, id ASC").Offset((f.Page - 1) * f.Size).Limit(f.Size).Find(&rows).Error
	return rows, total, err
}
func (s *MySQL) SupportDetail(ctx context.Context, id, patientID string, page, size int) (model.SupportTicket, []model.SupportEvent, int64, error) {
	var row model.SupportTicket
	events := make([]model.SupportEvent, 0)
	var total int64
	err := s.supportQuery(ctx, s.db.WithContext(ctx), patientID).Where("id = ?", id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrNotFound
	}
	if err != nil {
		return row, events, 0, err
	}
	q := s.db.WithContext(ctx).Model(&model.SupportEvent{}).Where("ticket_id = ?", id)
	if patientID != "" {
		q = q.Where("internal = false")
	}
	if err = q.Count(&total).Error; err != nil {
		return row, events, 0, err
	}
	err = q.Order("created_at DESC,id DESC").Offset((page - 1) * size).Limit(size).Find(&events).Error
	if err != nil {
		return row, events, total, err
	}
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	var attachments []model.SupportAttachment
	if len(ids) > 0 {
		err = s.db.WithContext(ctx).Select("id,ticket_id,event_id,name,mime_type,size").Where("event_id IN ?", ids).Order("id").Find(&attachments).Error
	}
	for i := range events {
		events[i].Attachments = []model.SupportAttachment{}
		for _, attachment := range attachments {
			if attachment.EventID == events[i].ID {
				events[i].Attachments = append(events[i].Attachments, attachment)
			}
		}
	}
	return row, events, total, err
}
func (s *MySQL) CreateSupport(ctx context.Context, patientID string, in SupportInput, now time.Time) (model.SupportTicket, error) {
	var row model.SupportTicket
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialise patient submissions, including concurrent retries with the same key.
		var patient model.Patient
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", patientID).First(&patient).Error; err != nil {
			return err
		}
		err := tx.Where("patient_id = ? AND request_key = ?", patientID, in.RequestKey).First(&row).Error
		if err == nil {
			if row.RequestHash != supportHash(in) {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var count int64
		if err := tx.Table("clinics").Where("id = ? AND active = true AND published_at IS NOT NULL", in.ClinicID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return ErrNotFound
		}
		row = model.SupportTicket{ID: randomID("support-"), PatientID: patientID, ClinicID: in.ClinicID, Subject: in.Subject, Category: in.Category, Status: "open", Version: 1, RequestKey: in.RequestKey, RequestHash: supportHash(in), CreatedAt: now, UpdatedAt: now}
		target, err := supportAutoAssignee(tx, in.ClinicID)
		if err != nil {
			return err
		}
		if target != "" {
			due := now.Add(24 * time.Hour)
			row.AssigneeID = target
			row.Status = "in_progress"
			row.DueAt = &due
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if target != "" {
			if err := tx.Create(&model.SupportEvent{ID: randomID("support-event-"), TicketID: row.ID, Kind: "auto_assign", ActorType: "system", ActorID: "system", Content: fmt.Sprintf("按未完成工单量自动分派：%s，24小时内跟进", target), Internal: true, RequestKey: in.RequestKey, RequestHash: supportHash(in), CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		return tx.Create(&model.SupportEvent{ID: randomID("support-event-"), TicketID: row.ID, Kind: "message", ActorType: "patient", ActorID: patientID, Content: in.Content, RequestKey: in.RequestKey, RequestHash: supportHash(in), CreatedAt: now}).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return row, err
}
func supportTransition(from, to string) bool {
	switch from {
	case "open":
		return to == "in_progress" || to == "closed"
	case "in_progress":
		return to == "waiting_patient" || to == "resolved" || to == "closed"
	case "waiting_patient":
		return to == "in_progress" || to == "resolved" || to == "closed"
	case "resolved":
		return to == "in_progress" || to == "closed"
	}
	return false
}
func (s *MySQL) ChangeSupport(ctx context.Context, id, patientID string, in SupportInput, now time.Time) (model.SupportTicket, error) {
	var row model.SupportTicket
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := s.supportQuery(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"}), patientID).Where("id = ?", id).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		actor, actorType := employeeActor(ctx), "employee"
		if patientID != "" {
			actor, actorType = patientID, "patient"
		}
		var prior model.SupportEvent
		err = tx.Where("ticket_id = ? AND actor_id = ? AND request_key = ?", id, actor, in.RequestKey).First(&prior).Error
		if err == nil {
			if prior.RequestHash != supportHash(in) {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if (in.Action == "assign" || in.Action == "followup") && (in.DueAt == nil || !in.DueAt.After(now) || !in.DueAt.Before(now.Add(365*24*time.Hour))) {
			return ErrCareState
		}
		if row.Version != in.Version {
			return ErrConflict
		}
		if row.Status == "closed" && in.Action != "rate" {
			return ErrCareState
		}
		scope, _ := scopeFrom(ctx)
		if patientID == "" && scope.Role != "manager" && !(in.Action == "assign" && row.AssigneeID == "" && in.AssigneeID == actor) && row.AssigneeID != actor {
			return ErrForbidden
		}
		switch in.Action {
		case "rate":
			if patientID == "" || (row.Status != "resolved" && row.Status != "closed") || row.Rating != 0 || in.Rating < 1 || in.Rating > 5 {
				return ErrCareState
			}
			row.Rating = in.Rating
			row.RatingComment = in.Content
		case "auto_assign":
			if patientID != "" || scope.Role != "manager" {
				return ErrForbidden
			}
			if row.AssigneeID != "" || row.Status != "open" {
				return ErrCareState
			}
			target, err := supportAutoAssignee(tx, row.ClinicID)
			if err != nil {
				return err
			}
			if target == "" {
				return ErrCareState
			}
			due := now.Add(24 * time.Hour)
			row.AssigneeID = target
			row.DueAt = &due
			row.Status = "in_progress"
		case "message":
			if patientID != "" {
				if row.AssigneeID == "" {
					row.Status = "open"
				} else {
					row.Status = "in_progress"
				}
			} else {
				if row.AssigneeID == "" {
					return ErrCareState
				}
				row.Status = "waiting_patient"
			}
		case "note":
			if patientID != "" {
				return ErrForbidden
			}
		case "assign":
			if patientID != "" || (scope.Role != "manager" && (row.AssigneeID != "" || in.AssigneeID != actor)) {
				return ErrForbidden
			}
			var target model.EmployeeAccount
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND active = true AND role IN ?", in.AssigneeID, []string{"manager", "customer_service"}).First(&target).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrNotFound
				}
				return err
			}
			allowed := false
			for _, clinic := range target.ClinicIDs {
				if clinic == row.ClinicID {
					allowed = true
				}
			}
			if !allowed {
				return ErrForbidden
			}
			row.AssigneeID = in.AssigneeID
			row.DueAt = in.DueAt
			if row.Status == "open" {
				row.Status = "in_progress"
			}
		case "status":
			if patientID != "" || !supportTransition(row.Status, in.Status) || (in.Status != "closed" && row.AssigneeID == "") {
				return ErrCareState
			}
			row.Status = in.Status
		case "followup":
			if patientID != "" || row.AssigneeID == "" || row.Status == "resolved" {
				return ErrCareState
			}
			row.DueAt = in.DueAt
		default:
			return ErrCareState
		}
		row.Version++
		row.UpdatedAt = now
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		content := in.Content
		if in.Action == "assign" {
			content = fmt.Sprintf("负责人：%s；%s", in.AssigneeID, in.Content)
		}
		if in.Action == "status" {
			content = fmt.Sprintf("状态：%s；%s", in.Status, in.Content)
		}
		if in.Action == "followup" {
			content = fmt.Sprintf("跟进期限：%s；%s", in.DueAt.Format(time.RFC3339), in.Content)
		}
		if in.Action == "auto_assign" {
			content = fmt.Sprintf("自动分派负责人：%s；%s", row.AssigneeID, in.Content)
		}
		event := model.SupportEvent{ID: randomID("support-event-"), TicketID: id, Kind: in.Action, ActorType: actorType, ActorID: actor, Content: content, Internal: in.Action != "message" && in.Action != "rate", RequestKey: in.RequestKey, RequestHash: supportHash(in), CreatedAt: now}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		for _, upload := range in.Attachments {
			if err := tx.Create(&model.SupportAttachment{ID: randomID("support-file-"), TicketID: id, EventID: event.ID, Name: upload.Name, MimeType: http.DetectContentType(upload.Data), Size: len(upload.Data), Data: upload.Data}).Error; err != nil {
				return err
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return row, err
}
func (s *MySQL) SupportAgents(ctx context.Context) ([]SupportAgent, error) {
	result := make([]SupportAgent, 0)
	if !supportRole(ctx) {
		return result, ErrForbidden
	}
	var rows []model.EmployeeAccount
	if err := s.db.WithContext(ctx).Where("active = true AND role IN ?", []string{"manager", "customer_service"}).Order("display_name,id").Find(&rows).Error; err != nil {
		return result, err
	}
	scope, _ := scopeFrom(ctx)
	for _, row := range rows {
		if scope.Role == "customer_service" && row.ID != employeeActor(ctx) {
			continue
		}
		clinics := []string{}
		for _, a := range row.ClinicIDs {
			for _, b := range scope.ClinicIDs {
				if a == b {
					clinics = append(clinics, a)
					break
				}
			}
		}
		if len(clinics) > 0 {
			result = append(result, SupportAgent{row.ID, row.DisplayName, clinics})
		}
	}
	return result, nil
}

// All automatic dispatches in a clinic share one lock. Read committed prevents
// a transaction that waited for this lock from selecting using stale workloads.
func supportAutoAssignee(tx *gorm.DB, clinicID string) (string, error) {
	var clinic model.Clinic
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", clinicID).First(&clinic).Error; err != nil {
		return "", err
	}
	var agents []model.EmployeeAccount
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("active = true AND role = 'customer_service' AND JSON_CONTAINS(clinic_ids, JSON_QUOTE(?))", clinicID).Order("id").Find(&agents).Error; err != nil {
		return "", err
	}
	selected := ""
	var lowest int64 = 1 << 62
	for _, agent := range agents {
		var load int64
		if err := tx.Model(&model.SupportTicket{}).Where("assignee_id = ? AND status NOT IN ?", agent.ID, []string{"resolved", "closed"}).Count(&load).Error; err != nil {
			return "", err
		}
		if load < lowest {
			selected = agent.ID
			lowest = load
		}
	}
	return selected, nil
}

type SupportAttachmentRepository interface {
	SupportAttachment(context.Context, string, string, string) (model.SupportAttachment, error)
}

func (s *MySQL) SupportAttachment(ctx context.Context, ticketID, attachmentID, patientID string) (model.SupportAttachment, error) {
	var ticket model.SupportTicket
	if err := s.supportQuery(ctx, s.db.WithContext(ctx), patientID).Where("id = ?", ticketID).First(&ticket).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.SupportAttachment{}, ErrNotFound
		}
		return model.SupportAttachment{}, err
	}
	var attachment model.SupportAttachment
	q := s.db.WithContext(ctx).Model(&model.SupportAttachment{}).Joins("JOIN support_events ON support_events.id = support_attachments.event_id").Where("support_attachments.id = ? AND support_attachments.ticket_id = ?", attachmentID, ticketID)
	if patientID != "" {
		q = q.Where("support_events.internal = false")
	}
	err := q.Select("support_attachments.*").First(&attachment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrNotFound
	}
	return attachment, err
}
