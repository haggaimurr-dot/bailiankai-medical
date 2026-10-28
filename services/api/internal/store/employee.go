package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInvalidCredentials = errors.New("invalid employee credentials")
var ErrAccountExists = errors.New("employee username or staff already linked")

// loginAnomalyThreshold is how many consecutive failed sign-ins for one
// username raise a login-anomaly event for managers (P2 后台-账号）.
const loginAnomalyThreshold = 5

type loginClientIPKey struct{}

// WithLoginClientIP carries the client IP the server saw into LoginEmployee
// so the attempt log records it. Empty when unset (tests, old callers).
func WithLoginClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, loginClientIPKey{}, ip)
}

func loginClientIP(ctx context.Context) string {
	if ip, ok := ctx.Value(loginClientIPKey{}).(string); ok {
		return ip
	}
	return ""
}

type EmployeeRepository interface {
	LoginEmployee(context.Context, string, string, time.Time) (model.EmployeeAccount, string, error)
	AuthenticateEmployee(context.Context, string, time.Time) (model.EmployeeAccount, error)
	LogoutEmployee(context.Context, string, time.Time) error
	Employees(context.Context) ([]model.EmployeeAccount, error)
	CreateEmployee(context.Context, string, string, string, string, string, time.Time) (model.EmployeeAccount, error)
	SetEmployeeActive(context.Context, string, int, bool, time.Time) (model.EmployeeAccount, error)
	ResetEmployeePassword(context.Context, string, int, string, time.Time) (model.EmployeeAccount, error)
	EmployeeEvents(context.Context, string) ([]model.EmployeeAccountEvent, error)
	LoginAttempts(context.Context, string, int, int) ([]model.EmployeeLoginAttempt, int64, error)
	LoginAnomalyCount(context.Context, time.Time) (int64, error)
	CanAccessAppointment(context.Context, string, bool) (bool, error)
	CanAccessTask(context.Context, string) (bool, error)
	CanAccessCarePhoto(context.Context, string) (bool, error)
	CanAccessFollowupPhoto(context.Context, string) (bool, error)
}

func accountEvent(tx *gorm.DB, ctx context.Context, employeeID, action string, details string, now time.Time) error {
	actorID := "system"
	if value, ok := ctx.Value(employeeActorKey{}).(string); ok && value != "" {
		actorID = value
	}
	return tx.Create(&model.EmployeeAccountEvent{ID: randomID("account-event-"), EmployeeID: employeeID, ActorID: actorID, Action: action, Details: details, CreatedAt: now}).Error
}

// eventDetails marshals a before/after diff for a permission change event.
// Either side may be nil; both nil yields "" (no diff recorded).
func eventDetails(before, after any) string {
	if before == nil && after == nil {
		return ""
	}
	encoded, err := json.Marshal(map[string]any{"before": before, "after": after})
	if err != nil {
		return ""
	}
	return string(encoded)
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
	failed := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil || err != nil || !account.Active || (ClinicalRole(account.Role) && account.StaffID == nil)
	if failed {
		s.recordLoginAttempt(ctx, username, nil, false, now)
		if err == nil {
			s.maybeLoginAnomaly(ctx, account, now)
		}
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
	s.recordLoginAttempt(ctx, username, &account.ID, true, now)
	return account, token, nil
}

// recordLoginAttempt writes one audit row for a sign-in attempt. The audit
// write is best-effort: a logging failure must not block the sign-in itself.
func (s *MySQL) recordLoginAttempt(ctx context.Context, username string, employeeID *string, success bool, now time.Time) {
	row := model.EmployeeLoginAttempt{ID: randomID("login-attempt-"), Username: username, EmployeeID: employeeID, Success: success, ClientIP: loginClientIP(ctx), CreatedAt: now}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		slog.Warn("login attempt audit failed", "error", err)
	}
}

// maybeLoginAnomaly fires one login-anomaly event when a known account hits
// loginAnomalyThreshold consecutive failures. It fires exactly on the
// threshold-th failure, so a longer run does not spam events; a success
// resets the run and a later episode can fire again.
func (s *MySQL) maybeLoginAnomaly(ctx context.Context, account model.EmployeeAccount, now time.Time) {
	// Read more than the threshold so a run longer than the threshold is
	// counted as longer (and does not refire); only a run of exactly
	// loginAnomalyThreshold fires.
	var attempts []model.EmployeeLoginAttempt
	if err := s.db.WithContext(ctx).Where("username = ?", account.Username).Order("created_at DESC, id DESC").Limit(loginAnomalyThreshold * 2).Find(&attempts).Error; err != nil {
		slog.Warn("login anomaly check failed", "error", err)
		return
	}
	consecutive := 0
	for _, attempt := range attempts {
		if !attempt.Success {
			consecutive++
			continue
		}
		break
	}
	if consecutive != loginAnomalyThreshold {
		return
	}
	ip := loginClientIP(ctx)
	newIP := false
	if ip != "" {
		var seen int64
		if err := s.db.WithContext(ctx).Model(&model.EmployeeLoginAttempt{}).Where("employee_id = ? AND success = ? AND client_ip = ?", account.ID, true, ip).Count(&seen).Error; err == nil && seen == 0 {
			newIP = true
		}
	}
	details, _ := json.Marshal(map[string]any{"consecutiveFailures": consecutive, "clientIp": ip, "newIp": newIP})
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return accountEvent(tx, ctx, account.ID, "login-anomaly", string(details), now)
	}); err != nil {
		slog.Warn("login anomaly event failed", "error", err)
	}
}

// LoginAttempts lists sign-in attempts for managers (newest first); empty
// username lists all accounts.
func (s *MySQL) LoginAttempts(ctx context.Context, username string, page, size int) ([]model.EmployeeLoginAttempt, int64, error) {
	rows := make([]model.EmployeeLoginAttempt, 0)
	q := s.db.WithContext(ctx).Model(&model.EmployeeLoginAttempt{})
	if username != "" {
		q = q.Where("username = ?", strings.ToLower(username))
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return rows, 0, err
	}
	err := q.Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

// LoginAnomalyCount counts login-anomaly events in the last 24 hours for
// the workbench alert card.
func (s *MySQL) LoginAnomalyCount(ctx context.Context, now time.Time) (int64, error) {
	var total int64
	err := s.db.WithContext(ctx).Model(&model.EmployeeAccountEvent{}).Where("action = ? AND created_at >= ?", "login-anomaly", now.Add(-24*time.Hour)).Count(&total).Error
	return total, err
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
		return accountEvent(tx, ctx, account.ID, "created", "", now)
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
		beforeActive := account.Active
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
			return accountEvent(tx, ctx, id, "activated", eventDetails(map[string]any{"active": beforeActive}, map[string]any{"active": active}), now)
		}
		return accountEvent(tx, ctx, id, "deactivated", eventDetails(map[string]any{"active": beforeActive}, map[string]any{"active": active}), now)
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
		return accountEvent(tx, ctx, id, "password-reset", "", now)
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
		return accountEvent(tx, ctx, id, "password-changed", "", now)
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
		beforeRole := account.Role
		beforeStaffID := ""
		if account.StaffID != nil {
			beforeStaffID = *account.StaffID
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
		return accountEvent(tx, ctx, id, "access-updated", eventDetails(map[string]any{"role": beforeRole, "staffId": beforeStaffID}, map[string]any{"role": role, "staffId": staffID}), now)
	})
	return account, err
}
