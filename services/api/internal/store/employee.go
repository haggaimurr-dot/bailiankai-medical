package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInvalidCredentials = errors.New("invalid employee credentials")
var ErrAccountExists = errors.New("employee username or staff already linked")

type EmployeeRepository interface {
	LoginEmployee(context.Context, string, string, time.Time) (model.EmployeeAccount, string, error)
	AuthenticateEmployee(context.Context, string, time.Time) (model.EmployeeAccount, error)
	LogoutEmployee(context.Context, string, time.Time) error
	Employees(context.Context) ([]model.EmployeeAccount, error)
	CreateEmployee(context.Context, string, string, string, string, string, time.Time) (model.EmployeeAccount, error)
	SetEmployeeActive(context.Context, string, int, bool, time.Time) (model.EmployeeAccount, error)
	ResetEmployeePassword(context.Context, string, int, string, time.Time) (model.EmployeeAccount, error)
	EmployeeEvents(context.Context, string) ([]model.EmployeeAccountEvent, error)
	CanAccessAppointment(context.Context, string, bool) (bool, error)
	CanAccessTask(context.Context, string) (bool, error)
	CanAccessCarePhoto(context.Context, string) (bool, error)
	CanAccessFollowupPhoto(context.Context, string) (bool, error)
}

func accountEvent(tx *gorm.DB, ctx context.Context, employeeID, action string, now time.Time) error {
	actorID := "system"
	if value, ok := ctx.Value(employeeActorKey{}).(string); ok && value != "" {
		actorID = value
	}
	return tx.Create(&model.EmployeeAccountEvent{ID: randomID("account-event-"), EmployeeID: employeeID, ActorID: actorID, Action: action, CreatedAt: now}).Error
}

func employeeToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}
func employeeTokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func (s *MySQL) LoginEmployee(ctx context.Context, username, password string, now time.Time) (model.EmployeeAccount, string, error) {
	var account model.EmployeeAccount
	err := s.db.WithContext(ctx).Where("username = ?", strings.ToLower(username)).First(&account).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return account, "", err
	}
	// Check a fixed hash for unknown users to limit username timing disclosure.
	hash := "$2a$10$CwTycUXWue0Thq9StjUM0uJ8iGZJp8uO8KYPQFtQKtJMXxGv8F2Le"
	if err == nil {
		hash = account.PasswordHash
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil || err != nil || !account.Active || (ClinicalRole(account.Role) && account.StaffID == nil) {
		return model.EmployeeAccount{}, "", ErrInvalidCredentials
	}
	if account.StaffID != nil {
		var staff model.FollowupStaff
		if err := s.db.WithContext(ctx).Where("id = ?", *account.StaffID).First(&staff).Error; err != nil {
			return account, "", err
		}
		if staff.Status != "on_duty" {
			return model.EmployeeAccount{}, "", ErrInvalidCredentials
		}
	}
	token, err := employeeToken()
	if err != nil {
		return account, "", err
	}
	session := model.EmployeeSession{TokenHash: employeeTokenHash(token), EmployeeID: account.ID, ExpiresAt: now.Add(12 * time.Hour), CreatedAt: now}
	if err := s.db.WithContext(ctx).Create(&session).Error; err != nil {
		return account, "", err
	}
	return account, token, nil
}

func (s *MySQL) AuthenticateEmployee(ctx context.Context, token string, now time.Time) (model.EmployeeAccount, error) {
	var account model.EmployeeAccount
	if len(token) != 64 {
		return account, ErrInvalidCredentials
	}
	err := s.db.WithContext(ctx).Model(&model.EmployeeAccount{}).
		Joins("JOIN employee_sessions ON employee_sessions.employee_id = employee_accounts.id").
		Joins("LEFT JOIN followup_staff ON followup_staff.id = employee_accounts.staff_id").
		Where("employee_sessions.token_hash = ? AND employee_sessions.revoked_at IS NULL AND employee_sessions.expires_at > ? AND employee_accounts.active = ? AND (employee_accounts.staff_id IS NULL OR followup_staff.status = ?)", employeeTokenHash(token), now, true, "on_duty").
		First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return account, ErrInvalidCredentials
	}
	if err == nil && ClinicalRole(account.Role) && account.StaffID == nil {
		return model.EmployeeAccount{}, ErrInvalidCredentials
	}
	return account, err
}

func (s *MySQL) LogoutEmployee(ctx context.Context, token string, now time.Time) error {
	if len(token) != 64 {
		return ErrInvalidCredentials
	}
	return s.db.WithContext(ctx).Model(&model.EmployeeSession{}).Where("token_hash = ? AND revoked_at IS NULL", employeeTokenHash(token)).Update("revoked_at", now).Error
}

func (s *MySQL) Employees(ctx context.Context) ([]model.EmployeeAccount, error) {
	rows := make([]model.EmployeeAccount, 0)
	err := accountScope(ctx, s.db.WithContext(ctx)).Order("created_at, id").Find(&rows).Error
	return rows, err
}

func (s *MySQL) CreateEmployee(ctx context.Context, username, displayName, password, role, staffID string, now time.Time) (model.EmployeeAccount, error) {
	var account model.EmployeeAccount
	if len(password) < 12 || len(password) > 128 || !ValidEmployeeRole(role) || (ClinicalRole(role) && staffID == "") {
		return account, ErrCareState
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return account, err
	}
	account = model.EmployeeAccount{ID: randomID("employee-"), Username: strings.ToLower(username), DisplayName: displayName, PasswordHash: string(hash), Role: role, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	if staffID != "" {
		account.StaffID = &staffID
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := assignAccountClinics(tx, ctx, &account); err != nil {
			return err
		}
		if staffID != "" {
			var staff model.FollowupStaff
			if err := tx.Where("id = ?", staffID).First(&staff).Error; err != nil {
				return followupDBError(err)
			}
			if role == "doctor" && staff.Role != "doctor" || role == "nurse" && staff.Role != "nurse" {
				return ErrCareState
			}
		}
		if err := tx.Create(&account).Error; err != nil {
			if strings.Contains(err.Error(), "Duplicate entry") {
				return ErrAccountExists
			}
			return err
		}
		return accountEvent(tx, ctx, account.ID, "created", now)
	})
	return account, err
}

func (s *MySQL) SetEmployeeActive(ctx context.Context, id string, version int, active bool, now time.Time) (model.EmployeeAccount, error) {
	var account model.EmployeeAccount
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := accountScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"})).Where("id = ?", id).First(&account).Error; err != nil {
			return followupDBError(err)
		}
		if account.Version != version {
			return ErrConflict
		}
		if account.Active == active {
			return ErrCareState
		}
		if !active && account.Role == "manager" {
			var managers []model.EmployeeAccount
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("role = ? AND active = ?", "manager", true).Find(&managers).Error; err != nil {
				return err
			}
			if len(managers) <= 1 {
				return ErrCareState
			}
		}
		account.Active, account.Version, account.UpdatedAt = active, version+1, now
		if err := tx.Model(&model.EmployeeAccount{}).Where("id = ?", id).Updates(map[string]any{"active": active, "version": account.Version, "updated_at": now}).Error; err != nil {
			return err
		}
		if !active {
			if err := tx.Model(&model.EmployeeSession{}).Where("employee_id = ? AND revoked_at IS NULL", id).Update("revoked_at", now).Error; err != nil {
				return err
			}
		}
		if active {
			return accountEvent(tx, ctx, id, "activated", now)
		}
		return accountEvent(tx, ctx, id, "deactivated", now)
	})
	return account, err
}

func (s *MySQL) ResetEmployeePassword(ctx context.Context, id string, version int, password string, now time.Time) (model.EmployeeAccount, error) {
	var account model.EmployeeAccount
	if len(password) < 12 || len(password) > 128 {
		return account, ErrCareState
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return account, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := accountScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"})).Where("id = ?", id).First(&account).Error; err != nil {
			return followupDBError(err)
		}
		if account.Version != version {
			return ErrConflict
		}
		account.Version++
		account.UpdatedAt = now
		if err := tx.Model(&model.EmployeeAccount{}).Where("id = ?", id).Updates(map[string]any{"password_hash": string(hash), "version": account.Version, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.EmployeeSession{}).Where("employee_id = ? AND revoked_at IS NULL", id).Update("revoked_at", now).Error; err != nil {
			return err
		}
		return accountEvent(tx, ctx, id, "password-reset", now)
	})
	return account, err
}

func (s *MySQL) EmployeeEvents(ctx context.Context, employeeID string) ([]model.EmployeeAccountEvent, error) {
	var account model.EmployeeAccount
	if err := accountScope(ctx, s.db.WithContext(ctx)).Where("id = ?", employeeID).First(&account).Error; err != nil {
		return nil, followupDBError(err)
	}
	rows := make([]model.EmployeeAccountEvent, 0)
	err := s.db.WithContext(ctx).Model(&model.EmployeeAccountEvent{}).
		Select("employee_account_events.*, COALESCE(actor.display_name, employee_account_events.actor_id) AS actor_name").
		Joins("LEFT JOIN employee_accounts actor ON actor.id = employee_account_events.actor_id").
		Where("employee_account_events.employee_id = ?", employeeID).
		Order("employee_account_events.created_at DESC, employee_account_events.id DESC").Find(&rows).Error
	return rows, err
}

// Verify and replace under one row lock to prevent concurrent resets racing a self change.
func (s *MySQL) ChangeEmployeePassword(ctx context.Context, id string, version int, currentPassword, password string, now time.Time) (model.EmployeeAccount, error) {
	var account model.EmployeeAccount
	if len(password) < 12 || len(password) > 72 || currentPassword == password {
		return account, ErrCareState
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return account, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		actor, _ := ctx.Value(employeeActorKey{}).(string)
		if actor != id {
			return ErrForbidden
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, "id = ?", id).Error; err != nil {
			return followupDBError(err)
		}
		if account.Version != version {
			return ErrConflict
		}
		if !account.Active || bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(currentPassword)) != nil {
			return ErrInvalidCredentials
		}
		account.Version++
		account.UpdatedAt = now
		if err := tx.Model(&model.EmployeeAccount{}).Where("id = ?", id).Updates(map[string]any{"password_hash": string(hash), "version": account.Version, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.EmployeeSession{}).Where("employee_id = ? AND revoked_at IS NULL", id).Update("revoked_at", now).Error; err != nil {
			return err
		}
		return accountEvent(tx, ctx, id, "password-changed", now)
	})
	return account, err
}
func (s *MySQL) SetEmployeeAccess(ctx context.Context, id string, version int, role, staffID string, now time.Time) (model.EmployeeAccount, error) {
	var account model.EmployeeAccount
	if !ValidEmployeeRole(role) || (ClinicalRole(role) && staffID == "") {
		return account, ErrCareState
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var managers []model.EmployeeAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("role = ? AND active = ?", "manager", true).Order("id").Find(&managers).Error; err != nil {
			return err
		}
		if err := accountScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"})).First(&account, "id = ?", id).Error; err != nil {
			return followupDBError(err)
		}
		if account.Version != version {
			return ErrConflict
		}
		actor, _ := ctx.Value(employeeActorKey{}).(string)
		if account.Role == "manager" && role != "manager" && (actor == id || account.Active && len(managers) <= 1) {
			return ErrCareState
		}
		if staffID != "" {
			var staff model.FollowupStaff
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&staff, "id = ?", staffID).Error; err != nil {
				return followupDBError(err)
			}
			if staff.Status != "on_duty" || role == "doctor" && staff.Role != "doctor" || role == "nurse" && staff.Role != "nurse" {
				return ErrCareState
			}
			if err := requireManagedStaff(ctx, tx, staffID); err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&model.EmployeeAccount{}).Where("staff_id = ? AND id <> ?", staffID, id).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrAccountExists
			}
		}
		account.Role = role
		account.StaffID = nil
		if staffID != "" {
			account.StaffID = &staffID
		}
		account.Version++
		account.UpdatedAt = now
		if err := tx.Model(&model.EmployeeAccount{}).Where("id = ?", id).Updates(map[string]any{"role": role, "staff_id": account.StaffID, "version": account.Version, "updated_at": now}).Error; err != nil {
			if strings.Contains(err.Error(), "Duplicate entry") {
				return ErrAccountExists
			}
			return err
		}
		if err := tx.Model(&model.EmployeeSession{}).Where("employee_id = ? AND revoked_at IS NULL", id).Update("revoked_at", now).Error; err != nil {
			return err
		}
		return accountEvent(tx, ctx, id, "access-updated", now)
	})
	return account, err
}
