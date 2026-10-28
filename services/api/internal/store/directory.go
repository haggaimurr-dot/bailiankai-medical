package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DirectoryRepository interface {
	DirectoryList(context.Context, string) (any, error)
	SaveDirectory(context.Context, string, string, int, string, model.Clinic, model.Doctor, bool, time.Time) (any, error)
	DirectoryHistory(context.Context, string, string) ([]model.DirectoryRevision, error)
	LinkDoctorEmployee(context.Context, string, string, int, int, time.Time) (model.EmployeeAccount, error)
}

func (s *MySQL) DirectoryList(ctx context.Context, kind string) (any, error) {
	if kind == "clinics" {
		rows := make([]model.Clinic, 0)
		err := clinicScope(ctx, s.db.WithContext(ctx), "id").Order("id").Find(&rows).Error
		return rows, err
	}
	rows := make([]model.Doctor, 0)
	err := clinicScope(ctx, s.db.WithContext(ctx), "clinic_id").Order("id").Find(&rows).Error
	return rows, err
}

func directoryEvent(tx *gorm.DB, ctx context.Context, kind, id, action string, version int, row any, now time.Time) error {
	content, err := json.Marshal(row)
	if err != nil {
		return err
	}
	actor, _ := ctx.Value(employeeActorKey{}).(string)
	if actor == "" {
		actor = "system"
	}
	return tx.Create(&model.DirectoryRevision{ID: randomID("directory-"), Kind: kind, ResourceID: id, Version: version, Action: action, ContentJSON: string(content), ActorID: actor, CreatedAt: now}).Error
}

func (s *MySQL) SaveDirectory(ctx context.Context, kind, id string, version int, action string, clinic model.Clinic, doctor model.Doctor, active bool, now time.Time) (any, error) {
	var result any
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row any
		var state *model.DirectoryState
		if kind == "clinics" {
			row = &clinic
			state = &clinic.DirectoryState
		} else {
			row = &doctor
			state = &doctor.DirectoryState
		}
		if action == "created" {
			id = randomID("clinic-")
			if kind == "doctors" {
				id = randomID("doctor-")
			}
			if kind == "clinics" {
				clinic.ID = id
			} else {
				doctor.ID = id
				var parent model.Clinic
				if err := clinicScope(ctx, tx, "id").First(&parent, "id = ?", doctor.ClinicID).Error; err != nil {
					return followupDBError(err)
				}
			}
			*state = model.DirectoryState{Active: true, Version: 1}
			if err := tx.Create(row).Error; err != nil {
				return err
			}
			if kind == "doctors" {
				if err := tx.Create(&model.FollowupStaff{ID: id, DisplayName: doctor.Name, Role: "doctor", Status: "on_duty", Version: 1}).Error; err != nil {
					return err
				}
			} else if actor, ok := ctx.Value(employeeActorKey{}).(string); ok {
				var account model.EmployeeAccount
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, "id = ?", actor).Error; err != nil {
					return err
				}
				account.ClinicIDs = append(account.ClinicIDs, id)
				encoded, _ := json.Marshal(account.ClinicIDs)
				if err := tx.Model(&account).Updates(map[string]any{"clinic_ids": string(encoded), "version": account.Version + 1, "updated_at": now}).Error; err != nil {
					return err
				}
				if err := accountEvent(tx, ctx, actor, "clinic-created", "", now); err != nil {
					return err
				}
			}
		} else {
			newClinic, newDoctor := clinic, doctor
			column := "clinic_id"
			if kind == "clinics" {
				column = "id"
			}
			if err := clinicScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"}), column).First(row, "id = ?", id).Error; err != nil {
				return followupDBError(err)
			}
			if state.Version != version {
				return ErrConflict
			}
			switch action {
			case "updated":
				if kind == "clinics" {
					clinic.Name, clinic.Address, clinic.Phone = newClinic.Name, newClinic.Address, newClinic.Phone
					if newClinic.Presentation != nil {
						clinic.Presentation = newClinic.Presentation
					}
				} else {
					doctor.QualificationReview = nil
					// The clinic identity is immutable because historical slots reference it.
					if doctor.ClinicID != newDoctor.ClinicID {
						return ErrCareState
					}
					doctor.Name, doctor.Title, doctor.Summary, doctor.Focus, doctor.Initials, doctor.AvatarURL, doctor.ServiceIntroduction = newDoctor.Name, newDoctor.Title, newDoctor.Summary, newDoctor.Focus, newDoctor.Initials, newDoctor.AvatarURL, newDoctor.ServiceIntroduction
					if newDoctor.Presentation != nil {
						doctor.Presentation = newDoctor.Presentation
					}
				}
			case "status":
				state.Active = active
			case "published":
				if !state.Active {
					return ErrCareState
				}
				if kind == "doctors" {
					if !doctor.QualificationsPublishable() {
						return ErrDoctorQualifications
					}
					var parent model.Clinic
					if err := tx.First(&parent, "id = ?", doctor.ClinicID).Error; err != nil {
						return err
					}
					if !parent.Active || parent.PublishedJSON == nil {
						return ErrCareState
					}
				}
				content, err := json.Marshal(row)
				if err != nil {
					return err
				}
				snapshot := string(content)
				state.PublishedJSON = &snapshot
				state.PublishedAt = &now
			default:
				return ErrCareState
			}
			state.Version++
			if err := tx.Save(row).Error; err != nil {
				return err
			}
		}
		if err := directoryEvent(tx, ctx, kind, id, action, state.Version, row, now); err != nil {
			return err
		}
		result = row
		return nil
	})
	return result, err
}

func (s *MySQL) DirectoryHistory(ctx context.Context, kind, id string) ([]model.DirectoryRevision, error) {
	var row any = &model.Doctor{}
	column := "clinic_id"
	if kind == "clinics" {
		row = &model.Clinic{}
		column = "id"
	}
	if err := clinicScope(ctx, s.db.WithContext(ctx), column).First(row, "id = ?", id).Error; err != nil {
		return nil, followupDBError(err)
	}
	rows := make([]model.DirectoryRevision, 0)
	err := s.db.WithContext(ctx).Where("kind = ? AND resource_id = ?", kind, id).Order("version DESC").Find(&rows).Error
	return rows, err
}

func (s *MySQL) LinkDoctorEmployee(ctx context.Context, doctorID, employeeID string, doctorVersion, employeeVersion int, now time.Time) (model.EmployeeAccount, error) {
	var account model.EmployeeAccount
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var doctor model.Doctor
		if err := clinicScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"}), "clinic_id").First(&doctor, "id = ?", doctorID).Error; err != nil {
			return followupDBError(err)
		}
		if doctor.Version != doctorVersion {
			return ErrConflict
		}
		if err := accountScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"})).First(&account, "id = ?", employeeID).Error; err != nil {
			return followupDBError(err)
		}
		if account.Version != employeeVersion {
			return ErrConflict
		}
		allowed := false
		for _, id := range account.ClinicIDs {
			if id == doctor.ClinicID {
				allowed = true
			}
		}
		if !allowed || account.Role != "doctor" || !account.Active || !doctor.Active {
			return ErrCareState
		}
		// Reassigning an existing identity would silently transfer historical access.
		if account.StaffID != nil && *account.StaffID != doctorID {
			return ErrCareState
		}
		account.StaffID = &doctorID
		account.Version++
		account.UpdatedAt = now
		if err := tx.Model(&account).Updates(map[string]any{"staff_id": doctorID, "version": account.Version, "updated_at": now}).Error; err != nil {
			if strings.Contains(err.Error(), "Duplicate entry") {
				return ErrAccountExists
			}
			return err
		}
		if err := tx.Model(&model.EmployeeSession{}).Where("employee_id = ? AND revoked_at IS NULL", employeeID).Update("revoked_at", now).Error; err != nil {
			return err
		}
		if err := accountEvent(tx, ctx, employeeID, "doctor-linked", "", now); err != nil {
			return err
		}
		doctor.Version++
		if err := tx.Model(&doctor).Update("version", doctor.Version).Error; err != nil {
			return err
		}
		return directoryEvent(tx, ctx, "doctors", doctorID, "employee-linked", doctor.Version, map[string]any{"doctor": doctor, "employeeId": employeeID}, now)
	})
	return account, err
}
