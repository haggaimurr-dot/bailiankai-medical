package store

import (
	"context"
	"encoding/json"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type clinicAssignmentKey struct{}

func WithClinicAssignment(ctx context.Context, ids []string) context.Context {
	return context.WithValue(ctx, clinicAssignmentKey{}, append([]string{}, ids...))
}
func requireManagedStaff(ctx context.Context, tx *gorm.DB, staffID string) error {
	if !employeeScoped(ctx) {
		return nil
	}
	var count int64
	if err := accountScope(ctx, tx.Model(&model.EmployeeAccount{})).Where("staff_id = ?", staffID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if err := tx.Model(&model.EmployeeAccount{}).Where("staff_id = ?", staffID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrForbidden
	}
	if err := clinicScope(ctx, tx.Model(&model.Doctor{}), "clinic_id").Where("id = ?", staffID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrForbidden
	}
	return nil
}
func requireClinicRecipient(tx *gorm.DB, appointmentID, staffID string) error {
	var clinic struct{ ClinicID string }
	if err := tx.Table("appointments").Joins("JOIN slots ON slots.id = appointments.slot_id").Where("appointments.id = ?", appointmentID).Select("slots.clinic_id").Take(&clinic).Error; err != nil {
		return err
	}
	var count int64
	if err := tx.Model(&model.EmployeeAccount{}).Where("staff_id = ? AND active = ? AND role IN ? AND JSON_CONTAINS(clinic_ids, JSON_QUOTE(?))", staffID, true, []string{"doctor", "nurse", "clinician"}, clinic.ClinicID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrForbidden
	}
	return nil
}
func accountScope(ctx context.Context, query *gorm.DB) *gorm.DB {
	if scope, ok := scopeFrom(ctx); ok && scope.EnforceClinics {
		if len(scope.ClinicIDs) == 0 {
			return query.Where("1 = 0")
		}
		encoded, _ := json.Marshal(scope.ClinicIDs)
		return query.Where("JSON_CONTAINS(?, employee_accounts.clinic_ids)", string(encoded))
	}
	return query
}
func ValidEmployeeRole(role string) bool {
	return role == "manager" || role == "clinician" || role == "doctor" || role == "nurse" || role == "customer_service" || role == "operations"
}
func ClinicalRole(role string) bool {
	return role == "clinician" || role == "doctor" || role == "nurse"
}
func assignAccountClinics(tx *gorm.DB, ctx context.Context, account *model.EmployeeAccount) error {
	ids, explicit := ctx.Value(clinicAssignmentKey{}).([]string)
	if !explicit && !employeeScoped(ctx) && account.Role == "manager" {
		if err := tx.Model(&model.Clinic{}).Pluck("id", &ids).Error; err != nil {
			return err
		}
	}
	if len(ids) == 0 && !explicit && account.StaffID != nil {
		if err := tx.Model(&model.Doctor{}).Where("id = ?", *account.StaffID).Pluck("clinic_id", &ids).Error; err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		return ErrCareState
	}
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			return ErrCareState
		}
		seen[id] = true
		if scope, ok := scopeFrom(ctx); ok && scope.EnforceClinics {
			allowed := false
			for _, clinic := range scope.ClinicIDs {
				if clinic == id {
					allowed = true
				}
			}
			if !allowed {
				return ErrForbidden
			}
		}
	}
	var count int64
	if err := tx.Model(&model.Clinic{}).Where("id IN ?", ids).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(ids)) {
		return ErrCareState
	}
	account.ClinicIDs = ids
	return nil
}
func (s *MySQL) SetEmployeeClinics(ctx context.Context, id string, version int, ids []string, now time.Time) (model.EmployeeAccount, error) {
	var account model.EmployeeAccount
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := accountScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"})).First(&account, "id = ?", id).Error; err != nil {
			return followupDBError(err)
		}
		if account.Version != version {
			return ErrConflict
		}
		if err := assignAccountClinics(tx, WithClinicAssignment(ctx, ids), &account); err != nil {
			return err
		}
		encoded, err := json.Marshal(account.ClinicIDs)
		if err != nil {
			return err
		}
		account.Version++
		account.UpdatedAt = now
		if err := tx.Model(&model.EmployeeAccount{}).Where("id = ?", id).Updates(map[string]any{"clinic_ids": string(encoded), "version": account.Version, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.EmployeeSession{}).Where("employee_id = ? AND revoked_at IS NULL", id).Update("revoked_at", now).Error; err != nil {
			return err
		}
		return accountEvent(tx, ctx, id, "clinics-updated", now)
	})
	return account, err
}
