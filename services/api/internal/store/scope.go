package store

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

var ErrForbidden = errors.New("employee access denied")

type employeeScopeKey struct{}
type EmployeeScope struct {
	Role           string
	StaffID        string
	ClinicIDs      []string
	EnforceClinics bool
}

func WithEmployeeScope(ctx context.Context, role, staffID string, clinics ...[]string) context.Context {
	scope := EmployeeScope{Role: role, StaffID: staffID, EnforceClinics: true}
	if len(clinics) > 0 {
		scope.ClinicIDs, scope.EnforceClinics = clinics[0], true
	}
	return context.WithValue(ctx, employeeScopeKey{}, scope)
}
func employeeScoped(ctx context.Context) bool {
	_, ok := scopeFrom(ctx)
	return ok
}
func clinicScope(ctx context.Context, query *gorm.DB, column string) *gorm.DB {
	if scope, ok := scopeFrom(ctx); ok && scope.EnforceClinics {
		if len(scope.ClinicIDs) == 0 {
			return query.Where("1 = 0")
		}
		return query.Where(column+" IN ?", scope.ClinicIDs)
	}
	return query
}
func (s *MySQL) taskClinicScope(ctx context.Context, query *gorm.DB, column string) *gorm.DB {
	appointments := clinicScope(ctx, s.db.WithContext(ctx).Table("appointments").Joins("JOIN slots ON slots.id = appointments.slot_id"), "slots.clinic_id").Select("appointments.id")
	tasks := s.db.WithContext(ctx).Table("followup_tasks").Select("id").Where("appointment_id IN (?)", appointments)
	return query.Where(column+" IN (?)", tasks)
}
func scopeFrom(ctx context.Context) (EmployeeScope, bool) {
	scope, ok := ctx.Value(employeeScopeKey{}).(EmployeeScope)
	return scope, ok
}
func clinicianStaff(ctx context.Context) string {
	scope, ok := scopeFrom(ctx)
	if ok && (scope.Role == "clinician" || scope.Role == "doctor" || scope.Role == "nurse") {
		if scope.StaffID == "" {
			return "__unlinked_clinician__"
		}
		return scope.StaffID
	}
	return ""
}

func (s *MySQL) CanAccessAppointment(ctx context.Context, appointmentID string, treatingDoctorOnly bool) (bool, error) {
	staffID := clinicianStaff(ctx)
	q := s.db.WithContext(ctx).Table("appointments").Joins("JOIN slots ON slots.id = appointments.slot_id").Where("appointments.id = ?", appointmentID)
	q = clinicScope(ctx, q, "slots.clinic_id")
	if treatingDoctorOnly && staffID != "" {
		q = q.Where("slots.doctor_id = ?", staffID)
	} else if staffID != "" {
		q = q.Where("(slots.doctor_id = ? OR EXISTS (SELECT 1 FROM followup_tasks access_task WHERE access_task.appointment_id = appointments.id AND access_task.assignee_id = ?))", staffID, staffID)
	}
	var count int64
	err := q.Count(&count).Error
	return count > 0, err
}

func (s *MySQL) CanAccessTask(ctx context.Context, taskID string) (bool, error) {
	staffID := clinicianStaff(ctx)
	var count int64
	query := s.db.WithContext(ctx).Table("followup_tasks").Where("id = ?", taskID)
	query = s.taskClinicScope(ctx, query, "followup_tasks.id")
	if staffID != "" {
		query = query.Where("assignee_id = ?", staffID)
	}
	err := query.Count(&count).Error
	return count > 0, err
}

func (s *MySQL) CanAccessCarePhoto(ctx context.Context, photoID string) (bool, error) {
	var row struct{ AppointmentID string }
	result := s.db.WithContext(ctx).Table("care_photos").Select("appointment_id").Where("id = ?", photoID).Scan(&row)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	return s.CanAccessAppointment(ctx, row.AppointmentID, false)
}

func (s *MySQL) CanAccessFollowupPhoto(ctx context.Context, photoID string) (bool, error) {
	var row struct{ TaskID string }
	result := s.db.WithContext(ctx).Table("followup_photos").Select("task_id").Where("id = ?", photoID).Scan(&row)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	return s.CanAccessTask(ctx, row.TaskID)
}
