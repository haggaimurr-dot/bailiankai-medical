package store

import (
	"context"
	"encoding/json"
	"time"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
)

type PatientChartRepository interface {
	PatientDirectoryWithTag(context.Context, string, int, int, string, string) ([]PatientRecordSummary, int64, error)
	CreatePatientChart(context.Context, string, model.PatientChart, time.Time) (PatientRecordSummary, error)
	SavePatientChart(context.Context, string, model.PatientChart) (model.PatientChart, error)
	ConsultationTemplates(context.Context) ([]model.ConsultationTemplate, error)
	CreateConsultationTemplate(context.Context, model.ConsultationTemplate) (model.ConsultationTemplate, error)
	DeleteConsultationTemplate(context.Context, string, string) error
}

func chartWriter(ctx context.Context) bool {
	scope, ok := scopeFrom(ctx)
	return ok && (scope.Role == "manager" || scope.Role == "doctor" || scope.Role == "clinician")
}
func (s *MySQL) CreatePatientChart(ctx context.Context, name string, chart model.PatientChart, now time.Time) (PatientRecordSummary, error) {
	if !chartWriter(ctx) {
		return PatientRecordSummary{}, ErrForbidden
	}
	var count int64
	if err := clinicScope(ctx, s.db.WithContext(ctx).Table("clinics"), "id").Where("id = ?", chart.ClinicID).Count(&count).Error; err != nil {
		return PatientRecordSummary{}, err
	}
	if count != 1 {
		return PatientRecordSummary{}, ErrForbidden
	}
	chart.PatientID, chart.StaffID, chart.Version = randomID("patient-"), clinicianStaff(ctx), 1
	patient := model.Patient{ID: chart.PatientID, DisplayName: name, CreatedAt: now, Version: 1, PhoneAuthorization: "not_granted"}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&patient).Error; err != nil {
			return err
		}
		return tx.Create(&chart).Error
	})
	return PatientRecordSummary{ID: patient.ID, DisplayName: name, CreatedAt: now, Profile: &chart}, err
}
func (s *MySQL) SavePatientChart(ctx context.Context, name string, chart model.PatientChart) (model.PatientChart, error) {
	if !chartWriter(ctx) {
		return chart, ErrForbidden
	}
	if _, _, err := s.PatientRecordAppointments(ctx, chart.PatientID); err != nil {
		return chart, err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Version 0 creates structured information for a pre-existing patient.
		if chart.Version == 0 {
			var count int64
			q := s.patientAppointmentsQuery(ctx).Where("appointments.patient_id = ? AND slots.clinic_id = ?", chart.PatientID, chart.ClinicID)
			if staff := clinicianStaff(ctx); staff != "" {
				q = q.Where("slots.doctor_id = ?", staff)
			}
			if err := q.Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return ErrForbidden
			}
			chart.StaffID, chart.Version = clinicianStaff(ctx), 1
			result := tx.Where("patient_id = ?", chart.PatientID).First(&model.PatientChart{})
			if result.Error == nil {
				return ErrConflict
			}
			if result.Error != gorm.ErrRecordNotFound {
				return result.Error
			}
			if err := tx.Create(&chart).Error; err != nil {
				return err
			}
			return tx.Model(&model.Patient{}).Where("id = ?", chart.PatientID).Updates(map[string]any{"display_name": name, "version": gorm.Expr("version + 1")}).Error
		}
		result := clinicScope(ctx, tx.Model(&model.PatientChart{}), "clinic_id").Where("patient_id = ? AND version = ? AND clinic_id = ?", chart.PatientID, chart.Version, chart.ClinicID).Updates(map[string]any{"gender": chart.Gender, "birth_date": chart.BirthDate, "contact_phone": chart.ContactPhone, "tags": jsonTags(chart.Tags), "version": chart.Version + 1})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		if err := tx.Model(&model.Patient{}).Where("id = ?", chart.PatientID).Updates(map[string]any{"display_name": name, "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		return tx.First(&chart, "patient_id = ?", chart.PatientID).Error
	})
	return chart, err
}
func (s *MySQL) templateQuery(ctx context.Context) *gorm.DB {
	q := clinicScope(ctx, s.db.WithContext(ctx).Model(&model.ConsultationTemplate{}), "clinic_id")
	if scope, ok := scopeFrom(ctx); !ok || scope.Role != "manager" {
		q = q.Where("recorded_by IN (SELECT id FROM employee_accounts WHERE staff_id = ?)", clinicianStaff(ctx))
	}
	return q
}
func (s *MySQL) ConsultationTemplates(ctx context.Context) ([]model.ConsultationTemplate, error) {
	if !chartWriter(ctx) {
		return nil, ErrForbidden
	}
	rows := make([]model.ConsultationTemplate, 0)
	err := s.templateQuery(ctx).Order("created_at DESC, id").Find(&rows).Error
	return rows, err
}
func (s *MySQL) CreateConsultationTemplate(ctx context.Context, row model.ConsultationTemplate) (model.ConsultationTemplate, error) {
	if !chartWriter(ctx) {
		return row, ErrForbidden
	}
	var count int64
	if err := clinicScope(ctx, s.db.WithContext(ctx).Table("clinics"), "id").Where("id = ?", row.ClinicID).Count(&count).Error; err != nil {
		return row, err
	}
	if count != 1 {
		return row, ErrForbidden
	}
	row.ID = randomID("template-")
	return row, s.db.WithContext(ctx).Create(&row).Error
}
func (s *MySQL) DeleteConsultationTemplate(ctx context.Context, id, actor string) error {
	if !chartWriter(ctx) {
		return ErrForbidden
	}
	q := s.templateQuery(ctx).Where("id = ?", id)
	if scope, _ := scopeFrom(ctx); scope.Role != "manager" {
		q = q.Where("recorded_by = ?", actor)
	}
	result := q.Delete(&model.ConsultationTemplate{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func jsonTags(tags []string) string { b, _ := json.Marshal(tags); return string(b) }
