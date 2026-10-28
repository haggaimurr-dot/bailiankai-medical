package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"example.com/bailiankai/api/internal/model"
)

// These DTOs are an allowlist; database model serialization is never used in an
// archive. In particular, drafts, session/request hashes, photo IDs/media and
// internal audit details have no representation here.
type archiveRecordIdentity struct {
	Reference string `json:"reference"`
	ID        string `json:"id,omitempty"`
}
type recordArchive struct {
	FormatVersion int              `json:"formatVersion"`
	JobID         string           `json:"jobId"`
	GeneratedAt   time.Time        `json:"generatedAt"`
	Masked        bool             `json:"masked"`
	Sections      []string         `json:"sections"`
	DateFrom      string           `json:"dateFrom"`
	DateTo        string           `json:"dateTo"`
	Patients      []archivePatient `json:"patients"`
}
type archivePatient struct {
	archiveRecordIdentity
	DisplayName   string                `json:"displayName,omitempty"`
	Phone         string                `json:"phone,omitempty"`
	CreatedAt     time.Time             `json:"createdAt"`
	Appointments  []archiveAppointment  `json:"appointments,omitempty"`
	Consultations []archiveConsultation `json:"consultations,omitempty"`
	CarePlans     []archivePlan         `json:"carePlans,omitempty"`
	Treatments    []archiveTreatment    `json:"treatments,omitempty"`
	Followups     []archiveFollowup     `json:"followups,omitempty"`
}
type archiveAppointment struct {
	archiveRecordIdentity
	Status          string    `json:"status"`
	StartsAt        time.Time `json:"startsAt"`
	EndsAt          time.Time `json:"endsAt"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	ContactName     string    `json:"contactName,omitempty"`
	ContactPhone    string    `json:"contactPhone,omitempty"`
	ClinicName      string    `json:"clinicName,omitempty"`
	DoctorName      string    `json:"doctorName,omitempty"`
	Note            string    `json:"note,omitempty"`
	RejectionReason string    `json:"rejectionReason,omitempty"`
	HasFreeText     bool      `json:"hasFreeText"`
}
type archiveConsultation struct {
	archiveRecordIdentity
	AppointmentReference string     `json:"appointmentReference,omitempty"`
	ParentReference      string     `json:"parentReference,omitempty"`
	VisitType            string     `json:"visitType"`
	VisitedAt            time.Time  `json:"visitedAt"`
	NextVisitAt          *time.Time `json:"nextVisitAt"`
	StageIndex           int        `json:"stageIndex"`
	StageStatus          string     `json:"stageStatus"`
	RecordedAt           time.Time  `json:"recordedAt"`
	ChiefComplaint       string     `json:"chiefComplaint,omitempty"`
	History              string     `json:"history,omitempty"`
	Assessment           string     `json:"assessment,omitempty"`
	Advice               string     `json:"advice,omitempty"`
	HasFreeText          bool       `json:"hasFreeText"`
}
type archivePlan struct {
	archiveRecordIdentity
	AppointmentReference string           `json:"appointmentReference"`
	Version              int              `json:"version"`
	PublishedAt          time.Time        `json:"publishedAt"`
	Content              *CarePlanContent `json:"content,omitempty"`
	ChangeReason         string           `json:"changeReason,omitempty"`
	StageCount           int              `json:"stageCount"`
	HasFreeText          bool             `json:"hasFreeText"`
}
type archiveTreatment struct {
	archiveRecordIdentity
	AppointmentReference string                     `json:"appointmentReference"`
	PlanReference        string                     `json:"planReference"`
	Version              int                        `json:"version"`
	StageIndex           int                        `json:"stageIndex"`
	TreatedAt            time.Time                  `json:"treatedAt"`
	RecordedAt           time.Time                  `json:"recordedAt"`
	CorrectedAt          *time.Time                 `json:"correctedAt"`
	Procedure            string                     `json:"procedure,omitempty"`
	BodyArea             string                     `json:"bodyArea,omitempty"`
	Device               string                     `json:"device,omitempty"`
	Parameters           string                     `json:"parameters,omitempty"`
	Summary              string                     `json:"summary,omitempty"`
	Notes                string                     `json:"notes,omitempty"`
	HasFreeText          bool                       `json:"hasFreeText"`
	Revisions            []archiveTreatmentRevision `json:"revisions"`
}
type archiveTreatmentRevision struct {
	archiveRecordIdentity
	PlanReference    string    `json:"planReference"`
	Version          int       `json:"version"`
	StageIndex       int       `json:"stageIndex"`
	TreatedAt        time.Time `json:"treatedAt"`
	ChangedAt        time.Time `json:"changedAt"`
	Procedure        string    `json:"procedure,omitempty"`
	BodyArea         string    `json:"bodyArea,omitempty"`
	Device           string    `json:"device,omitempty"`
	Parameters       string    `json:"parameters,omitempty"`
	Summary          string    `json:"summary,omitempty"`
	Notes            string    `json:"notes,omitempty"`
	CorrectionReason string    `json:"correctionReason,omitempty"`
	HasFreeText      bool      `json:"hasFreeText"`
}
type archiveFollowup struct {
	archiveRecordIdentity
	AppointmentReference string     `json:"appointmentReference"`
	TreatmentReference   string     `json:"treatmentReference"`
	Status               string     `json:"status"`
	DueAt                time.Time  `json:"dueAt"`
	AssignedAt           time.Time  `json:"assignedAt"`
	SubmittedAt          *time.Time `json:"submittedAt"`
	ReviewedAt           *time.Time `json:"reviewedAt"`
	Severity             int        `json:"severity"`
	Concern              bool       `json:"concern"`
	Title                string     `json:"title,omitempty"`
	Symptoms             string     `json:"symptoms,omitempty"`
	Response             string     `json:"response,omitempty"`
	Disposition          string     `json:"disposition,omitempty"`
	HasFreeText          bool       `json:"hasFreeText"`
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func archiveReference(key, id string) string {
	if id == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(id))
	return "ref-" + hex.EncodeToString(mac.Sum(nil)[:12])
}
func archiveIdentity(job model.RecordExportJob, id string) archiveRecordIdentity {
	result := archiveRecordIdentity{Reference: archiveReference(job.ReferenceKey, id)}
	if !job.Masked {
		result.ID = id
	}
	return result
}
func archiveHasText(fields ...string) bool {
	for _, field := range fields {
		if field != "" {
			return true
		}
	}
	return false
}
func archiveAppointmentDTO(job model.RecordExportJob, row model.Appointment) archiveAppointment {
	dto := archiveAppointment{archiveRecordIdentity: archiveIdentity(job, row.ID), Status: row.Status, StartsAt: row.StartsAt, EndsAt: row.EndsAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, HasFreeText: archiveHasText(row.Note, row.RejectionReason)}
	if !job.Masked {
		dto.ContactName = row.ContactName
		dto.ContactPhone = row.ContactPhone
		dto.ClinicName = row.ClinicName
		dto.DoctorName = row.DoctorName
		dto.Note = row.Note
		dto.RejectionReason = row.RejectionReason
	}
	return dto
}
func archiveConsultationDTO(job model.RecordExportJob, row model.Consultation) archiveConsultation {
	dto := archiveConsultation{archiveRecordIdentity: archiveIdentity(job, row.ID), VisitType: row.VisitType, VisitedAt: row.VisitedAt, NextVisitAt: row.NextVisitAt, StageIndex: row.StageIndex, StageStatus: row.StageStatus, RecordedAt: row.RecordedAt, HasFreeText: archiveHasText(row.ChiefComplaint, row.History, row.Assessment, row.Advice)}
	if row.AppointmentID != nil {
		dto.AppointmentReference = archiveReference(job.ReferenceKey, *row.AppointmentID)
	}
	if row.ParentConsultationID != nil {
		dto.ParentReference = archiveReference(job.ReferenceKey, *row.ParentConsultationID)
	}
	if !job.Masked {
		dto.ChiefComplaint = row.ChiefComplaint
		dto.History = row.History
		dto.Assessment = row.Assessment
		dto.Advice = row.Advice
	}
	return dto
}
func archivePlanDTO(job model.RecordExportJob, row model.CarePlanVersion, appointmentID string) (archivePlan, error) {
	var content CarePlanContent
	if err := json.Unmarshal([]byte(row.ContentJSON), &content); err != nil {
		return archivePlan{}, err
	}
	dto := archivePlan{archiveRecordIdentity: archiveIdentity(job, row.ID), AppointmentReference: archiveReference(job.ReferenceKey, appointmentID), Version: row.Version, PublishedAt: row.PublishedAt, StageCount: len(content.Stages), HasFreeText: archiveHasText(content.Summary, content.Instructions, row.ChangeReason) || len(content.Stages) > 0}
	if !job.Masked {
		dto.Content = &content
		dto.ChangeReason = row.ChangeReason
	}
	return dto, nil
}
func archiveRevisionDTO(job model.RecordExportJob, row model.TreatmentRevision) archiveTreatmentRevision {
	dto := archiveTreatmentRevision{archiveRecordIdentity: archiveIdentity(job, row.ID), PlanReference: archiveReference(job.ReferenceKey, row.PlanVersionID), Version: row.Version, StageIndex: row.StageIndex, TreatedAt: row.TreatedAt, ChangedAt: row.ChangedAt, HasFreeText: archiveHasText(row.Procedure, row.BodyArea, row.Device, row.Parameters, row.Summary, row.Notes, row.CorrectionReason)}
	if !job.Masked {
		dto.Procedure = row.Procedure
		dto.BodyArea = row.BodyArea
		dto.Device = row.Device
		dto.Parameters = row.Parameters
		dto.Summary = row.Summary
		dto.Notes = row.Notes
		dto.CorrectionReason = row.CorrectionReason
	}
	return dto
}
func archiveTreatmentDTO(job model.RecordExportJob, row model.Treatment, revisions []model.TreatmentRevision) archiveTreatment {
	dto := archiveTreatment{archiveRecordIdentity: archiveIdentity(job, row.ID), AppointmentReference: archiveReference(job.ReferenceKey, row.AppointmentID), PlanReference: archiveReference(job.ReferenceKey, row.PlanVersionID), Version: row.Version, StageIndex: row.StageIndex, TreatedAt: row.TreatedAt, RecordedAt: row.RecordedAt, CorrectedAt: row.CorrectedAt, HasFreeText: archiveHasText(row.Procedure, row.BodyArea, row.Device, row.Parameters, row.Summary, row.Notes), Revisions: make([]archiveTreatmentRevision, 0)}
	if !job.Masked {
		dto.Procedure = row.Procedure
		dto.BodyArea = row.BodyArea
		dto.Device = row.Device
		dto.Parameters = row.Parameters
		dto.Summary = row.Summary
		dto.Notes = row.Notes
	}
	for _, revision := range revisions {
		dto.Revisions = append(dto.Revisions, archiveRevisionDTO(job, revision))
	}
	return dto
}
func archiveFollowupDTO(job model.RecordExportJob, row model.FollowupTask) archiveFollowup {
	dto := archiveFollowup{archiveRecordIdentity: archiveIdentity(job, row.ID), AppointmentReference: archiveReference(job.ReferenceKey, row.AppointmentID), TreatmentReference: archiveReference(job.ReferenceKey, strVal(row.TreatmentID)), Status: row.Status, DueAt: row.DueAt, AssignedAt: row.AssignedAt, SubmittedAt: row.SubmittedAt, ReviewedAt: row.ReviewedAt, Severity: row.Severity, Concern: row.Concern, HasFreeText: archiveHasText(row.Title, row.Symptoms, row.Response, row.Disposition)}
	if !job.Masked {
		dto.Title = row.Title
		dto.Symptoms = row.Symptoms
		dto.Response = row.Response
		dto.Disposition = row.Disposition
	}
	return dto
}

func (s *MySQL) recordExportPayload(ctx context.Context, job model.RecordExportJob, snapshot recordExportSnapshot, now time.Time) ([]byte, error) {
	if len(snapshot.ReferenceKey) != 64 {
		return nil, ErrRecordExportInput
	}
	job.ReferenceKey = snapshot.ReferenceKey
	result := recordArchive{FormatVersion: 1, JobID: job.ID, GeneratedAt: now, Masked: job.Masked, Sections: job.Sections, DateFrom: job.DateFrom, DateTo: job.DateTo, Patients: make([]archivePatient, 0, len(snapshot.PatientIDs))}
	var patients []model.Patient
	if err := s.db.WithContext(ctx).Where("id IN ?", snapshot.PatientIDs).Order("id").Find(&patients).Error; err != nil {
		return nil, err
	}
	if len(patients) != len(snapshot.PatientIDs) {
		return nil, ErrForbidden
	}
	indexes := map[string]int{}
	for _, patient := range patients {
		dto := archivePatient{archiveRecordIdentity: archiveIdentity(job, patient.ID), CreatedAt: patient.CreatedAt}
		if !job.Masked {
			dto.DisplayName = patient.DisplayName
			dto.Phone = patient.Phone
		}
		indexes[patient.ID] = len(result.Patients)
		result.Patients = append(result.Patients, dto)
	}
	// Load only captured records. Published plans are immutable; no draft is read.
	appointmentPatient := map[string]string{}
	var appointments []model.Appointment
	if len(snapshot.AppointmentIDs) > 0 {
		if err := appointmentQuery(s.db.WithContext(ctx)).Where("appointments.id IN ?", snapshot.AppointmentIDs).Order("appointments.id").Find(&appointments).Error; err != nil {
			return nil, err
		}
		if len(appointments) != len(snapshot.AppointmentIDs) {
			return nil, ErrForbidden
		}
	}
	selectedAppointments := map[string]bool{}
	for _, id := range snapshot.SelectedAppointmentIDs {
		selectedAppointments[id] = true
	}
	for _, row := range appointments {
		index, ok := indexes[row.PatientID]
		if !ok {
			return nil, ErrForbidden
		}
		appointmentPatient[row.ID] = row.PatientID
		if selectedAppointments[row.ID] {
			result.Patients[index].Appointments = append(result.Patients[index].Appointments, archiveAppointmentDTO(job, row))
		}
	}
	if len(snapshot.ConsultationIDs) > 0 {
		var rows []model.Consultation
		if err := s.db.WithContext(ctx).Where("id IN ?", snapshot.ConsultationIDs).Order("visited_at,id").Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) != len(snapshot.ConsultationIDs) {
			return nil, ErrForbidden
		}
		for _, row := range rows {
			index, ok := indexes[row.PatientID]
			if !ok {
				return nil, ErrForbidden
			}
			result.Patients[index].Consultations = append(result.Patients[index].Consultations, archiveConsultationDTO(job, row))
		}
	}
	if len(snapshot.PlanVersionIDs) > 0 {
		var plans []model.CarePlan
		var versions []model.CarePlanVersion
		if err := s.db.WithContext(ctx).Where("appointment_id IN ?", snapshot.AppointmentIDs).Select("id,appointment_id").Find(&plans).Error; err != nil {
			return nil, err
		}
		planAppointment := map[string]string{}
		for _, plan := range plans {
			planAppointment[plan.ID] = plan.AppointmentID
		}
		if err := s.db.WithContext(ctx).Where("id IN ?", snapshot.PlanVersionIDs).Order("published_at,id").Find(&versions).Error; err != nil {
			return nil, err
		}
		if len(versions) != len(snapshot.PlanVersionIDs) {
			return nil, ErrForbidden
		}
		for _, row := range versions {
			appointmentID := planAppointment[row.CarePlanID]
			patientID, ok := appointmentPatient[appointmentID]
			if !ok {
				return nil, ErrForbidden
			}
			dto, err := archivePlanDTO(job, row, appointmentID)
			if err != nil {
				return nil, err
			}
			index := indexes[patientID]
			result.Patients[index].CarePlans = append(result.Patients[index].CarePlans, dto)
		}
	}
	if len(snapshot.TreatmentIDs) > 0 {
		var rows []model.Treatment
		var revisions []model.TreatmentRevision
		if err := s.db.WithContext(ctx).Where("id IN ?", snapshot.TreatmentIDs).Order("treated_at,id").Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) != len(snapshot.TreatmentIDs) {
			return nil, ErrForbidden
		}
		if len(snapshot.RevisionIDs) > 0 {
			if err := s.db.WithContext(ctx).Where("id IN ?", snapshot.RevisionIDs).Order("version,id").Find(&revisions).Error; err != nil {
				return nil, err
			}
			if len(revisions) != len(snapshot.RevisionIDs) {
				return nil, ErrForbidden
			}
		}
		byTreatment := map[string][]model.TreatmentRevision{}
		for _, row := range revisions {
			byTreatment[row.TreatmentID] = append(byTreatment[row.TreatmentID], row)
		}
		for _, row := range rows {
			patientID, ok := appointmentPatient[row.AppointmentID]
			if !ok || patientID != row.PatientID {
				return nil, ErrForbidden
			}
			index := indexes[patientID]
			result.Patients[index].Treatments = append(result.Patients[index].Treatments, archiveTreatmentDTO(job, row, byTreatment[row.ID]))
		}
	}
	if len(snapshot.TaskIDs) > 0 {
		var rows []model.FollowupTask
		if err := s.db.WithContext(ctx).Where("id IN ?", snapshot.TaskIDs).Order("due_at,id").Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) != len(snapshot.TaskIDs) {
			return nil, ErrForbidden
		}
		for _, row := range rows {
			patientID, ok := appointmentPatient[row.AppointmentID]
			if !ok || patientID != row.PatientID {
				return nil, ErrForbidden
			}
			index := indexes[patientID]
			result.Patients[index].Followups = append(result.Patients[index].Followups, archiveFollowupDTO(job, row))
		}
	}
	return json.MarshalIndent(result, "", "  ")
}
