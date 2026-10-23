package store

import (
	"context"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
)

type PatientRecordSummary struct {
	WechatBound      bool                `json:"wechatBound"`
	Profile          *model.PatientChart `gorm:"-" json:"profile,omitempty"`
	ID               string              `json:"id"`
	DisplayName      string              `json:"displayName"`
	CreatedAt        time.Time           `json:"createdAt"`
	AppointmentCount int64               `json:"appointmentCount"`
}

type PatientRecordRepository interface {
	PatientDirectory(context.Context, string, int, int) ([]PatientRecordSummary, int64, error)
	PatientRecordAppointments(context.Context, string) (PatientRecordSummary, []model.Appointment, error)
	PatientConsultations(context.Context, string, ...DateRange) ([]model.Consultation, error)
	CreateConsultation(context.Context, model.Consultation) (model.Consultation, error)
}

func (s *MySQL) patientAppointmentsQuery(ctx context.Context) *gorm.DB {
	query := clinicScope(ctx, s.db.WithContext(ctx).Table("appointments").Joins("JOIN slots ON slots.id = appointments.slot_id"), "slots.clinic_id")
	if staffID := clinicianStaff(ctx); staffID != "" {
		query = query.Where("(slots.doctor_id = ? OR EXISTS (SELECT 1 FROM followup_tasks task WHERE task.appointment_id = appointments.id AND task.assignee_id = ?))", staffID, staffID)
	}
	return query
}

func (s *MySQL) patientDirectoryQuery(ctx context.Context) *gorm.DB {
	query := s.db.WithContext(ctx).Table("patients")
	if employeeScoped(ctx) {
		charts := clinicScope(ctx, s.db.WithContext(ctx).Table("patient_charts"), "clinic_id")
		if staffID := clinicianStaff(ctx); staffID != "" {
			charts = charts.Where("staff_id = ?", staffID)
		}
		query = query.Where("(patients.id IN (?) OR patients.id IN (?))", s.patientAppointmentsQuery(ctx).Select("appointments.patient_id"), charts.Select("patient_id"))
	}
	return query
}

func (s *MySQL) PatientDirectory(ctx context.Context, search string, page, size int) ([]PatientRecordSummary, int64, error) {
	return s.patientDirectory(ctx, search, page, size, "")
}
func (s *MySQL) PatientDirectoryWithTag(ctx context.Context, search string, page, size int, tag string) ([]PatientRecordSummary, int64, error) {
	return s.patientDirectory(ctx, search, page, size, tag)
}
func (s *MySQL) patientDirectory(ctx context.Context, search string, page, size int, tag string) ([]PatientRecordSummary, int64, error) {
	query := s.patientDirectoryQuery(ctx)
	if tag != "" {
		charts := clinicScope(ctx, s.db.WithContext(ctx).Table("patient_charts"), "clinic_id").Where("JSON_CONTAINS(tags, ?) = 1", jsonTags([]string{tag}))
		query = query.Where("patients.id IN (?)", charts.Select("patient_id"))
	}
	if search != "" {
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(search)
		query = query.Where("(patients.display_name LIKE ? ESCAPE '!' OR patients.id = ?)", "%"+escaped+"%", search)
	}
	var total int64
	rows := make([]PatientRecordSummary, 0)
	if err := query.Count(&total).Error; err != nil {
		return rows, total, err
	}
	err := query.Select("patients.id, patients.display_name, patients.created_at, (patients.wechat_open_id IS NOT NULL) AS wechat_bound, (?) AS appointment_count", s.patientAppointmentsQuery(ctx).Select("COUNT(*)").Where("appointments.patient_id = patients.id")).Order("patients.created_at DESC, patients.id").Offset((page - 1) * size).Limit(size).Scan(&rows).Error
	if err == nil && len(rows) > 0 {
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		var profiles []model.PatientChart
		if err = clinicScope(ctx, s.db.WithContext(ctx), "clinic_id").Where("patient_id IN ?", ids).Find(&profiles).Error; err == nil {
			for i := range rows {
				for j := range profiles {
					if profiles[j].PatientID == rows[i].ID {
						rows[i].Profile = &profiles[j]
					}
				}
			}
		}
	}
	return rows, total, err
}

func (s *MySQL) PatientRecordAppointments(ctx context.Context, id string) (PatientRecordSummary, []model.Appointment, error) {
	var patient PatientRecordSummary
	rows := make([]model.Appointment, 0)
	result := s.patientDirectoryQuery(ctx).Select("patients.id, patients.display_name, patients.created_at, (patients.wechat_open_id IS NOT NULL) AS wechat_bound").Where("patients.id = ?", id).Scan(&patient)
	if result.Error != nil {
		return patient, rows, result.Error
	}
	if result.RowsAffected == 0 {
		return patient, rows, ErrNotFound
	}
	err := appointmentQuery(s.db.WithContext(ctx)).Where("appointments.id IN (?)", s.patientAppointmentsQuery(ctx).Select("appointments.id").Where("appointments.patient_id = ?", id)).Order("slots.starts_at DESC, appointments.id").Find(&rows).Error
	patient.AppointmentCount = int64(len(rows))
	if err == nil {
		var profile model.PatientChart
		result := clinicScope(ctx, s.db.WithContext(ctx), "clinic_id").Where("patient_id = ?", id).Find(&profile)
		if result.Error != nil {
			return patient, rows, result.Error
		}
		if result.RowsAffected > 0 {
			patient.Profile = &profile
		}
	}
	return patient, rows, err
}

func (s *MySQL) PatientConsultations(ctx context.Context, id string, ranges ...DateRange) ([]model.Consultation, error) {
	if _, _, err := s.PatientRecordAppointments(ctx, id); err != nil {
		return nil, err
	}
	rows := make([]model.Consultation, 0)
	query := clinicScope(ctx, s.db.WithContext(ctx).Where("patient_id = ?", id), "clinic_id")
	if scope, ok := scopeFrom(ctx); ok && (scope.Role == "clinician" || scope.Role == "doctor" || scope.Role == "nurse") {
		query = query.Where("recorded_by IN (SELECT id FROM employee_accounts WHERE staff_id = ?)", scope.StaffID)
	}
	if len(ranges) > 0 && ranges[0].Active() {
		query = query.Where("visited_at >= ? AND visited_at < ?", ranges[0].From, ranges[0].Until)
	}
	err := query.Order("visited_at DESC, recorded_at DESC, id").Find(&rows).Error
	return rows, err
}

func (s *MySQL) CreateConsultation(ctx context.Context, row model.Consultation) (model.Consultation, error) {
	if _, _, err := s.PatientRecordAppointments(ctx, row.PatientID); err != nil {
		return row, err
	}
	if scope, ok := scopeFrom(ctx); !ok || (scope.Role != "manager" && scope.Role != "clinician" && scope.Role != "doctor") {
		return row, ErrForbidden
	}
	clinics := make([]string, 0)
	query := s.patientAppointmentsQuery(ctx).Where("appointments.patient_id = ?", row.PatientID)
	if row.ClinicID != "" {
		query = query.Where("slots.clinic_id = ?", row.ClinicID)
	}
	if scope, ok := scopeFrom(ctx); ok && ClinicalRole(scope.Role) {
		query = query.Where("slots.doctor_id = ?", scope.StaffID)
	}
	if err := query.Distinct().Pluck("slots.clinic_id", &clinics).Error; err != nil {
		return row, err
	}
	if len(clinics) == 0 {
		charts := clinicScope(ctx, s.db.WithContext(ctx).Table("patient_charts").Where("patient_id = ?", row.PatientID), "clinic_id")
		if row.ClinicID != "" {
			charts = charts.Where("clinic_id = ?", row.ClinicID)
		}
		if staffID := clinicianStaff(ctx); staffID != "" {
			charts = charts.Where("staff_id = ?", staffID)
		}
		if err := charts.Pluck("clinic_id", &clinics).Error; err != nil {
			return row, err
		}
	}
	if len(clinics) != 1 {
		return row, ErrForbidden
	}
	row.ClinicID = clinics[0]
	if row.VisitType == "" {
		row.VisitType = "initial"
	}
	if row.StageStatus == "" {
		row.StageStatus = "not_started"
	}
	if row.VisitedAt.IsZero() {
		row.VisitedAt = row.RecordedAt
	}
	if (row.VisitType != "initial" && row.VisitType != "revisit") || row.StageIndex < 0 || row.StageIndex > 8 || (row.StageStatus != "not_started" && row.StageStatus != "in_progress" && row.StageStatus != "completed") || row.VisitedAt.Year() < 1000 || row.VisitedAt.After(row.RecordedAt) || (row.NextVisitAt != nil && (row.NextVisitAt.Year() > 9999 || !row.NextVisitAt.After(row.VisitedAt))) {
		return row, ErrCareState
	}
	if (row.VisitType == "revisit") != (row.ParentConsultationID != nil) {
		return row, ErrCareState
	}
	if row.ParentConsultationID != nil {
		parents, err := s.PatientConsultations(ctx, row.PatientID)
		if err != nil {
			return row, err
		}
		found := false
		for _, parent := range parents {
			if parent.ID == *row.ParentConsultationID && parent.ClinicID == row.ClinicID && !parent.VisitedAt.After(row.VisitedAt) {
				found = true
			}
		}
		if !found {
			return row, ErrForbidden
		}
	}
	if row.AppointmentID != nil {
		var count int64
		if err := s.patientAppointmentsQuery(ctx).Where("appointments.id = ? AND appointments.patient_id = ? AND slots.clinic_id = ?", *row.AppointmentID, row.PatientID, row.ClinicID).Count(&count).Error; err != nil {
			return row, err
		}
		allowed, err := s.CanAccessAppointment(ctx, *row.AppointmentID, true)
		if err != nil {
			return row, err
		}
		if count != 1 || !allowed {
			return row, ErrForbidden
		}
	}
	row.ID = randomID("consultation-")
	err := s.db.WithContext(ctx).Create(&row).Error
	return row, err
}
