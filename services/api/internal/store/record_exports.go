package store

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrRecordExportInput    = errors.New("invalid record export selection")
	ErrRecordExportQuota    = errors.New("too many active record exports")
	ErrRecordExportNotReady = errors.New("record export is not ready")
	ErrRecordExportExpired  = errors.New("record export has expired")
	errRecordExportTooLarge = errors.New("record export exceeds size limit")
)

type RecordExportInput struct {
	PatientIDs []string `json:"patientIds"`
	Sections   []string `json:"sections"`
	DateFrom   string   `json:"dateFrom"`
	DateTo     string   `json:"dateTo"`
	Masked     bool     `json:"masked"`
	Reason     string   `json:"reason"`
}

type RecordExportRepository interface {
	CreateRecordExport(context.Context, string, RecordExportInput, time.Time) (model.RecordExportJob, error)
	ListRecordExports(context.Context, string, int, int, time.Time) ([]model.RecordExportJob, int64, error)
	RecordExport(context.Context, string, string, time.Time) (model.RecordExportJob, error)
	DownloadRecordExport(context.Context, string, string, time.Time) (model.RecordExportJob, []byte, error)
	RecordExportEvents(context.Context, string, string) ([]model.RecordExportEvent, error)
}

type recordExportSnapshot struct {
	ReferenceKey           string        `json:"referenceKey"`
	PatientIDs             []string      `json:"patientIds"`
	Scope                  EmployeeScope `json:"scope"`
	AppointmentIDs         []string      `json:"appointmentIds"`
	SelectedAppointmentIDs []string      `json:"selectedAppointmentIds"`
	ConsultationIDs        []string      `json:"consultationIds"`
	PlanVersionIDs         []string      `json:"planVersionIds"`
	TreatmentIDs           []string      `json:"treatmentIds"`
	RevisionIDs            []string      `json:"revisionIds"`
	TaskIDs                []string      `json:"taskIds"`
}

const recordExportPublicColumns = "id, employee_id, patient_count, sections, date_from, date_to, masked, reason, status, created_at, completed_at, expires_at, size_bytes, error_code, download_count"
const recordExportMaxRecords = 20000

func recordExportDates(input RecordExportInput) (DateRange, error) {
	if input.DateFrom == "" && input.DateTo == "" {
		return DateRange{}, nil
	}
	from, err := time.ParseInLocation("2006-01-02", input.DateFrom, database.Shanghai)
	if err != nil {
		return DateRange{}, ErrRecordExportInput
	}
	to, err := time.ParseInLocation("2006-01-02", input.DateTo, database.Shanghai)
	if err != nil || from.Year() < 1000 || to.Year() > 9998 || to.Before(from) || !to.Before(from.AddDate(0, 0, 366)) {
		return DateRange{}, ErrRecordExportInput
	}
	return DateRange{From: from, Until: to.AddDate(0, 0, 1)}, nil
}

func validateRecordExport(input RecordExportInput) (RecordExportInput, DateRange, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if len(input.PatientIDs) < 1 || len(input.PatientIDs) > 20 || len(input.Sections) < 1 || len(input.Sections) > 5 || utf8.RuneCountInString(input.Reason) < 1 || utf8.RuneCountInString(input.Reason) > 200 {
		return input, DateRange{}, ErrRecordExportInput
	}
	seen := map[string]bool{}
	for _, id := range input.PatientIDs {
		if strings.TrimSpace(id) != id || id == "" || len(id) > 64 || seen[id] {
			return input, DateRange{}, ErrRecordExportInput
		}
		seen[id] = true
	}
	seen = map[string]bool{}
	for _, section := range input.Sections {
		if seen[section] || (section != "appointments" && section != "consultations" && section != "care_plans" && section != "treatments" && section != "followups") {
			return input, DateRange{}, ErrRecordExportInput
		}
		seen[section] = true
	}
	dates, err := recordExportDates(input)
	return input, dates, err
}

// Authorization comes from the current database account, never a caller-provided scope.
// Lock the account for the duration of generation/download so revocation is ordered
// against the release of an archive. Clinical staff must still be on duty.
func recordExportAccount(ctx context.Context, tx *gorm.DB, id string, masked bool) (context.Context, model.EmployeeAccount, error) {
	var account model.EmployeeAccount
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ctx, account, ErrForbidden
	}
	if err != nil {
		return ctx, account, err
	}
	if !account.Active || (account.Role != "manager" && !ClinicalRole(account.Role)) || (!masked && account.Role != "manager") || len(account.ClinicIDs) == 0 {
		return ctx, account, ErrForbidden
	}
	staffID := ""
	if account.StaffID != nil {
		staffID = *account.StaffID
		var staff model.FollowupStaff
		err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND status = ?", staffID, "on_duty").First(&staff).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ctx, account, ErrForbidden
		}
		if err != nil {
			return ctx, account, err
		}
	}
	if ClinicalRole(account.Role) && staffID == "" {
		return ctx, account, ErrForbidden
	}
	return WithEmployeeActor(WithEmployeeScope(ctx, account.Role, staffID, account.ClinicIDs), account.ID), account, nil
}

func recordExportAudit(tx *gorm.DB, jobID, actor, action, code string, now time.Time) error {
	return tx.Create(&model.RecordExportEvent{ID: randomID("export-event-"), JobID: jobID, ActorID: actor, Action: action, DetailCode: code, CreatedAt: now}).Error
}

func exportSection(sections []string, want string) bool {
	for _, section := range sections {
		if section == want {
			return true
		}
	}
	return false
}

func exportDateQuery(query *gorm.DB, column string, dates DateRange) *gorm.DB {
	if dates.Active() {
		return query.Where(column+" >= ? AND "+column+" < ?", dates.From, dates.Until)
	}
	return query
}

// Capture IDs for every selected section. Later clinic/role expansion or newly
// created records cannot silently expand a queued request.
func (s *MySQL) captureRecordExport(ctx context.Context, input RecordExportInput, dates DateRange) (recordExportSnapshot, error) {
	scope, _ := scopeFrom(ctx)
	snapshot := recordExportSnapshot{PatientIDs: append([]string{}, input.PatientIDs...), Scope: scope}
	key, err := employeeToken()
	if err != nil {
		return snapshot, err
	}
	snapshot.ReferenceKey = key
	for _, id := range input.PatientIDs {
		var count int64
		if err := s.patientDirectoryQuery(ctx).Where("patients.id = ?", id).Count(&count).Error; err != nil {
			return snapshot, err
		}
		if count != 1 {
			return snapshot, ErrForbidden
		}
	}
	appts := s.patientAppointmentsQuery(ctx).Where("appointments.patient_id IN ?", input.PatientIDs)
	if err := appts.Order("appointments.id").Pluck("appointments.id", &snapshot.AppointmentIDs).Error; err != nil {
		return snapshot, err
	}
	if exportSection(input.Sections, "appointments") {
		if err := exportDateQuery(s.patientAppointmentsQuery(ctx).Where("appointments.id IN ?", snapshot.AppointmentIDs), "slots.starts_at", dates).Order("appointments.id").Pluck("appointments.id", &snapshot.SelectedAppointmentIDs).Error; err != nil {
			return snapshot, err
		}
	}
	if exportSection(input.Sections, "consultations") {
		query := clinicScope(ctx, s.db.Model(&model.Consultation{}).Where("patient_id IN ?", input.PatientIDs), "clinic_id")
		if staff := clinicianStaff(ctx); staff != "" {
			query = query.Where("recorded_by IN (SELECT id FROM employee_accounts WHERE staff_id = ?)", staff)
		}
		if err := exportDateQuery(query, "visited_at", dates).Order("id").Pluck("id", &snapshot.ConsultationIDs).Error; err != nil {
			return snapshot, err
		}
	}
	if exportSection(input.Sections, "care_plans") {
		query := s.db.Model(&model.CarePlanVersion{}).Where("care_plan_id IN (SELECT id FROM care_plans WHERE appointment_id IN ?)", snapshot.AppointmentIDs)
		if err := exportDateQuery(query, "published_at", dates).Order("id").Pluck("id", &snapshot.PlanVersionIDs).Error; err != nil {
			return snapshot, err
		}
	}
	if exportSection(input.Sections, "treatments") {
		query := s.db.Model(&model.Treatment{}).Where("appointment_id IN ?", snapshot.AppointmentIDs)
		if err := exportDateQuery(query, "treated_at", dates).Order("id").Pluck("id", &snapshot.TreatmentIDs).Error; err != nil {
			return snapshot, err
		}
		if len(snapshot.TreatmentIDs) > 0 {
			if err := s.db.Model(&model.TreatmentRevision{}).Where("treatment_id IN ?", snapshot.TreatmentIDs).Order("id").Pluck("id", &snapshot.RevisionIDs).Error; err != nil {
				return snapshot, err
			}
		}
	}
	if exportSection(input.Sections, "followups") {
		query := s.db.Model(&model.FollowupTask{}).Where("appointment_id IN ?", snapshot.AppointmentIDs)
		if staff := clinicianStaff(ctx); staff != "" {
			query = query.Where("assignee_id = ?", staff)
		}
		query = exportDateQuery(query, "due_at", dates)
		if err := query.Order("id").Pluck("id", &snapshot.TaskIDs).Error; err != nil {
			return snapshot, err
		}
	}
	count := len(snapshot.AppointmentIDs) + len(snapshot.ConsultationIDs) + len(snapshot.PlanVersionIDs) + len(snapshot.TreatmentIDs) + len(snapshot.RevisionIDs) + len(snapshot.TaskIDs)
	if count > recordExportMaxRecords {
		return snapshot, errRecordExportTooLarge
	}
	return snapshot, nil
}

func (s *MySQL) CreateRecordExport(ctx context.Context, employeeID string, input RecordExportInput, now time.Time) (model.RecordExportJob, error) {
	var job model.RecordExportJob
	input, dates, err := validateRecordExport(input)
	if err != nil {
		return job, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scoped, _, err := recordExportAccount(ctx, tx, employeeID, input.Masked)
		if err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.RecordExportJob{}).Where("employee_id = ? AND status IN ?", employeeID, []string{"queued", "processing"}).Count(&count).Error; err != nil {
			return err
		}
		if count >= 3 {
			return ErrRecordExportQuota
		}
		snapshot, err := (&MySQL{db: tx}).captureRecordExport(scoped, input, dates)
		if err != nil {
			if errors.Is(err, errRecordExportTooLarge) {
				return ErrRecordExportInput
			}
			return err
		}
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		job = model.RecordExportJob{ID: randomID("export-"), EmployeeID: employeeID, PatientCount: len(input.PatientIDs), Sections: input.Sections, DateFrom: input.DateFrom, DateTo: input.DateTo, Masked: input.Masked, Reason: input.Reason, Status: "queued", CreatedAt: now, SnapshotJSON: string(encoded)}
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
		return recordExportAudit(tx, job.ID, employeeID, "requested", "", now)
	})
	return job, err
}

func expireRecordExport(tx *gorm.DB, job *model.RecordExportJob, now time.Time) error {
	if job.Status != "ready" || job.ExpiresAt == nil || job.ExpiresAt.After(now) {
		return nil
	}
	if err := tx.Model(&model.RecordExportJob{}).Where("id = ?", job.ID).Updates(map[string]any{"status": "expired", "payload": nil, "lease_until": nil, "lease_token": ""}).Error; err != nil {
		return err
	}
	job.Status, job.Payload = "expired", nil
	return recordExportAudit(tx, job.ID, "system", "expired", "DOWNLOAD_EXPIRED", now)
}

func (s *MySQL) expireRecordExports(ctx context.Context, now time.Time, employeeID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status = ? AND expires_at <= ?", "ready", now).Select(recordExportPublicColumns).Order("expires_at, id").Limit(100)
		if employeeID != "" {
			query = query.Where("employee_id = ?", employeeID)
		}
		var jobs []model.RecordExportJob
		if err := query.Find(&jobs).Error; err != nil {
			return err
		}
		for i := range jobs {
			if err := expireRecordExport(tx, &jobs[i], now); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *MySQL) ListRecordExports(ctx context.Context, employeeID string, page, size int, now time.Time) ([]model.RecordExportJob, int64, error) {
	rows := make([]model.RecordExportJob, 0)
	var total int64
	if page < 1 || size < 1 || size > 100 {
		return rows, total, ErrRecordExportInput
	}
	if err := s.expireRecordExports(ctx, now, employeeID); err != nil {
		return rows, total, err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := recordExportAccount(ctx, tx, employeeID, true); err != nil {
			return err
		}
		query := tx.Model(&model.RecordExportJob{}).Where("employee_id = ?", employeeID)
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		return query.Select(recordExportPublicColumns).Order("created_at DESC,id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	})
	return rows, total, err
}

func (s *MySQL) RecordExport(ctx context.Context, employeeID, id string, now time.Time) (model.RecordExportJob, error) {
	var job model.RecordExportJob
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select(recordExportPublicColumns).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND employee_id = ?", id, employeeID).First(&job).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if _, _, err := recordExportAccount(ctx, tx, employeeID, true); err != nil {
			return err
		}
		return expireRecordExport(tx, &job, now)
	})
	return job, err
}

// Intersect the captured scope with current clinics and retain the original role
// and staff binding. A promotion or reassignment cannot expand this archive.
func (s *MySQL) authorizeRecordExport(ctx context.Context, tx *gorm.DB, job model.RecordExportJob) (context.Context, recordExportSnapshot, error) {
	var snapshot recordExportSnapshot
	if err := json.Unmarshal([]byte(job.SnapshotJSON), &snapshot); err != nil {
		return ctx, snapshot, err
	}
	scoped, account, err := recordExportAccount(ctx, tx, job.EmployeeID, job.Masked)
	if err != nil {
		return ctx, snapshot, err
	}
	current, _ := scopeFrom(scoped)
	if account.Role != snapshot.Scope.Role || current.StaffID != snapshot.Scope.StaffID {
		return ctx, snapshot, ErrForbidden
	}
	clinics := make([]string, 0)
	for _, original := range snapshot.Scope.ClinicIDs {
		for _, present := range current.ClinicIDs {
			if original == present {
				clinics = append(clinics, original)
				break
			}
		}
	}
	scoped = WithEmployeeScope(ctx, snapshot.Scope.Role, snapshot.Scope.StaffID, clinics)
	bound := &MySQL{db: tx}
	// Lock appointments, slots and tasks to serialize changing resource access.
	var appointments []model.Appointment
	if len(snapshot.AppointmentIDs) > 0 {
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id IN ?", snapshot.AppointmentIDs).Find(&appointments).Error; err != nil {
			return ctx, snapshot, err
		}
		if len(appointments) != len(snapshot.AppointmentIDs) {
			return ctx, snapshot, ErrForbidden
		}
		slotIDs := make([]string, 0, len(appointments))
		for _, a := range appointments {
			slotIDs = append(slotIDs, a.SlotID)
		}
		var slots []model.Slot
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id IN ?", slotIDs).Find(&slots).Error; err != nil {
			return ctx, snapshot, err
		}
		// An appointment can be accessible through a task assignment as well.
		var accessTasks []model.FollowupTask
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("appointment_id IN ?", snapshot.AppointmentIDs).Find(&accessTasks).Error; err != nil {
			return ctx, snapshot, err
		}
		var count int64
		if err := bound.patientAppointmentsQuery(scoped).Where("appointments.id IN ?", snapshot.AppointmentIDs).Count(&count).Error; err != nil {
			return ctx, snapshot, err
		}
		if int(count) != len(snapshot.AppointmentIDs) {
			return ctx, snapshot, ErrForbidden
		}
	}
	if len(snapshot.ConsultationIDs) > 0 {
		var rows []model.Consultation
		q := clinicScope(scoped, tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id IN ? AND patient_id IN ?", snapshot.ConsultationIDs, snapshot.PatientIDs), "clinic_id")
		if staff := clinicianStaff(scoped); staff != "" {
			q = q.Where("recorded_by IN (SELECT id FROM employee_accounts WHERE staff_id = ?)", staff)
		}
		if err := q.Find(&rows).Error; err != nil {
			return ctx, snapshot, err
		}
		if len(rows) != len(snapshot.ConsultationIDs) {
			return ctx, snapshot, ErrForbidden
		}
	}
	// Recheck each captured section's parent relationship too. This prevents a
	// moved or deleted record from keeping an already generated file accessible.
	checks := []struct {
		query *gorm.DB
		ids   []string
	}{
		{tx.Model(&model.CarePlanVersion{}).Where("care_plan_id IN (SELECT id FROM care_plans WHERE appointment_id IN ?)", snapshot.AppointmentIDs), snapshot.PlanVersionIDs},
		{tx.Model(&model.Treatment{}).Where("appointment_id IN ? AND patient_id IN ?", snapshot.AppointmentIDs, snapshot.PatientIDs), snapshot.TreatmentIDs},
		{tx.Model(&model.TreatmentRevision{}).Where("treatment_id IN ?", snapshot.TreatmentIDs), snapshot.RevisionIDs},
	}
	for _, check := range checks {
		if len(check.ids) == 0 {
			continue
		}
		var count int64
		if err := check.query.Where("id IN ?", check.ids).Count(&count).Error; err != nil {
			return ctx, snapshot, err
		}
		if int(count) != len(check.ids) {
			return ctx, snapshot, ErrForbidden
		}
	}
	if len(snapshot.TaskIDs) > 0 {
		query := bound.taskClinicScope(scoped, tx.Model(&model.FollowupTask{}).Where("id IN ? AND appointment_id IN ? AND patient_id IN ?", snapshot.TaskIDs, snapshot.AppointmentIDs, snapshot.PatientIDs), "followup_tasks.id")
		if staff := clinicianStaff(scoped); staff != "" {
			query = query.Where("assignee_id = ?", staff)
		}
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return ctx, snapshot, err
		}
		if int(count) != len(snapshot.TaskIDs) {
			return ctx, snapshot, ErrForbidden
		}
	}
	return scoped, snapshot, nil
}

func (s *MySQL) DownloadRecordExport(ctx context.Context, employeeID, id string, now time.Time) (model.RecordExportJob, []byte, error) {
	started := time.Now()
	var job model.RecordExportJob
	var payload []byte
	var denial error
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Every rejected attempt for an owned job is persisted even if the account
		// has since been disabled. An unrelated job remains undisclosed.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND employee_id = ?", id, employeeID).First(&job).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		_, _, authErr := (&MySQL{db: tx}).authorizeRecordExport(ctx, tx, job)
		if authErr != nil {
			if !errors.Is(authErr, ErrForbidden) {
				return authErr
			}
			denial = ErrForbidden
			return recordExportAudit(tx, id, employeeID, "download_denied", "ACCESS_REVOKED", now)
		}
		checkedAt := now.Add(time.Since(started))
		if err := expireRecordExport(tx, &job, checkedAt); err != nil {
			return err
		}
		if job.Status == "expired" {
			denial = ErrRecordExportExpired
			return recordExportAudit(tx, id, employeeID, "download_denied", "DOWNLOAD_EXPIRED", checkedAt)
		}
		if job.Status != "ready" {
			denial = ErrRecordExportNotReady
			return recordExportAudit(tx, id, employeeID, "download_denied", "NOT_READY", now)
		}
		if len(job.Payload) == 0 {
			denial = ErrRecordExportNotReady
			return recordExportAudit(tx, id, employeeID, "download_denied", "PAYLOAD_UNAVAILABLE", now)
		}
		payload = append([]byte{}, job.Payload...)
		job.DownloadCount++
		if err := tx.Model(&model.RecordExportJob{}).Where("id = ?", id).Update("download_count", job.DownloadCount).Error; err != nil {
			return err
		}
		return recordExportAudit(tx, id, employeeID, "downloaded", "", now)
	})
	if err != nil {
		return job, nil, err
	}
	if denial != nil {
		return job, nil, denial
	}
	return job, payload, nil
}

func (s *MySQL) RecordExportEvents(ctx context.Context, employeeID, id string) ([]model.RecordExportEvent, error) {
	rows := make([]model.RecordExportEvent, 0)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := recordExportAccount(ctx, tx, employeeID, true); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.RecordExportJob{}).Where("id = ? AND employee_id = ?", id, employeeID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return ErrNotFound
		}
		return tx.Where("job_id = ?", id).Order("created_at,id").Find(&rows).Error
	})
	return rows, err
}

// Durable, bounded polling replaces per-request goroutines. A crashed process
// leaves a short lease which another worker can recover after restart.
func (s *MySQL) RunRecordExportWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.ProcessRecordExports(ctx, time.Now().In(database.Shanghai)); err != nil && ctx.Err() == nil {
			slog.Error("Record export worker failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *MySQL) ProcessRecordExports(ctx context.Context, now time.Time) error {
	started := time.Now()
	if err := s.expireRecordExports(ctx, now, ""); err != nil {
		return err
	}
	for i := 0; i < 4; i++ {
		claimedAt := now.Add(time.Since(started))
		var job model.RecordExportJob
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status = ? OR (status = ? AND lease_until <= ?)", "queued", "processing", claimedAt).Order("created_at,id").First(&job).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			lease := claimedAt.Add(2 * time.Minute)
			job.LeaseToken = randomID("lease-")
			job.LeaseUntil = &lease
			job.Status = "processing"
			return tx.Model(&model.RecordExportJob{}).Where("id = ?", job.ID).Updates(map[string]any{"status": job.Status, "lease_until": lease, "lease_token": job.LeaseToken}).Error
		})
		if err != nil {
			return err
		}
		if job.ID == "" {
			return nil
		}
		buildCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		err = s.generateRecordExport(buildCtx, job, claimedAt)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			code := "GENERATION_FAILED"
			if errors.Is(err, ErrForbidden) {
				code = "ACCESS_REVOKED"
			}
			if errors.Is(err, errRecordExportTooLarge) {
				code = "EXPORT_TOO_LARGE"
			}
			failedAt := now.Add(time.Since(started))
			if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				result := tx.Model(&model.RecordExportJob{}).Where("id = ? AND status = ? AND lease_token = ?", job.ID, "processing", job.LeaseToken).Updates(map[string]any{"status": "failed", "error_code": code, "completed_at": failedAt, "payload": nil, "lease_until": nil, "lease_token": ""})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					return nil
				}
				return recordExportAudit(tx, job.ID, "system", "failed", code, failedAt)
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *MySQL) generateRecordExport(ctx context.Context, job model.RecordExportJob, now time.Time) error {
	started := time.Now()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var owned model.RecordExportJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ? AND lease_token = ?", job.ID, "processing", job.LeaseToken).First(&owned).Error; err != nil {
			return err
		}
		_, snapshot, err := (&MySQL{db: tx}).authorizeRecordExport(ctx, tx, owned)
		if err != nil {
			return err
		}
		payload, err := (&MySQL{db: tx}).recordExportPayload(ctx, owned, snapshot, now.Add(time.Since(started)))
		if err != nil {
			return err
		}
		if len(payload) > 12*1024*1024 {
			return errRecordExportTooLarge
		}
		completedAt := now.Add(time.Since(started))
		expires := completedAt.Add(24 * time.Hour)
		if err := tx.Model(&model.RecordExportJob{}).Where("id = ?", job.ID).Updates(map[string]any{"status": "ready", "completed_at": completedAt, "expires_at": expires, "size_bytes": len(payload), "payload": payload, "lease_until": nil, "lease_token": "", "error_code": ""}).Error; err != nil {
			return err
		}
		return recordExportAudit(tx, job.ID, "system", "generated", "", completedAt)
	})
}
