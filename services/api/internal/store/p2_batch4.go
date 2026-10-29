package store

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"strings"
	"time"

	"gorm.io/gorm"

	"example.com/bailiankai/api/internal/model"
)

// ============ 行 114: 提醒偏好 ============

type NotificationPreference struct {
	PatientID           string `gorm:"primaryKey;size:64"`
	AppointmentReminder bool
	FollowupReminder    bool
	CareReminder        bool
	LeadHours           int
	UpdatedAt           time.Time
}

func (NotificationPreference) TableName() string { return "notification_preferences" }

func (s *MySQL) NotificationPreferences(ctx context.Context, patientID string) (NotificationPreference, error) {
	var pref NotificationPreference
	err := s.db.WithContext(ctx).First(&pref, "patient_id = ?", patientID).Error
	if err == gorm.ErrRecordNotFound {
		return NotificationPreference{
			PatientID: patientID, AppointmentReminder: true,
			FollowupReminder: true, CareReminder: true, LeadHours: 24,
		}, nil
	}
	return pref, err
}

func (s *MySQL) UpdateNotificationPreferences(ctx context.Context, patientID string, appointmentReminder, followupReminder, careReminder bool, leadHours int, now time.Time) (NotificationPreference, error) {
	if strings.TrimSpace(patientID) == "" || leadHours < 0 || leadHours > 168 {
		return NotificationPreference{}, ErrPreferenceInput
	}
	pref := NotificationPreference{
		PatientID: patientID, AppointmentReminder: appointmentReminder,
		FollowupReminder: followupReminder, CareReminder: careReminder,
		LeadHours: leadHours, UpdatedAt: now,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing NotificationPreference
		if err := tx.First(&existing, "patient_id = ?", patientID).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return err
			}
			return tx.Create(&pref).Error
		}
		return tx.Save(&pref).Error
	})
	return pref, err
}

var ErrPreferenceInput = &shareLinkError{"invalid notification preference input"}

// CreateEmployeeSession mints a session token for an already-authenticated
// account (used after 2FA verification).
func (s *MySQL) CreateEmployeeSession(ctx context.Context, accountID string, now time.Time) (string, error) {
	token, err := employeeToken()
	if err != nil {
		return "", err
	}
	session := model.EmployeeSession{TokenHash: employeeTokenHash(token), EmployeeID: accountID, ExpiresAt: now.Add(12 * time.Hour), CreatedAt: now}
	if err := s.db.WithContext(ctx).Create(&session).Error; err != nil {
		return "", err
	}
	return token, nil
}

// EmployeeAccountByID returns the account row (for 2FA login completion).
func (s *MySQL) EmployeeAccountByID(ctx context.Context, id string) (model.EmployeeAccount, error) {
	var account model.EmployeeAccount
	err := s.db.WithContext(ctx).First(&account, "id = ?", id).Error
	return account, err
}

// RevokeEmployeeSession deletes a session by its plaintext token.
func (s *MySQL) RevokeEmployeeSession(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Delete(&model.EmployeeSession{}, "token_hash = ?", employeeTokenHash(token)).Error
}

// ============ 行 115: 双因素验证 (TOTP, RFC 6238) ============

type Employee2FA struct {
	EmployeeID string `gorm:"primaryKey;size:64"`
	Secret     string
	Enabled    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (Employee2FA) TableName() string { return "employee_2fa" }

// SetupEmployee2FA generates a new TOTP secret (disabled until verified).
// Returns the base32 secret for the authenticator app.
func (s *MySQL) SetupEmployee2FA(ctx context.Context, employeeID string, now time.Time) (string, error) {
	if strings.TrimSpace(employeeID) == "" {
		return "", Err2FAInput
	}
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row Employee2FA
		if err := tx.First(&row, "employee_id = ?", employeeID).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return err
			}
			return tx.Create(&Employee2FA{EmployeeID: employeeID, Secret: secret, CreatedAt: now, UpdatedAt: now}).Error
		}
		row.Secret = secret
		row.Enabled = false
		row.UpdatedAt = now
		return tx.Save(&row).Error
	})
	return secret, err
}

// VerifyEmployee2FA checks a TOTP code (±1 step window) and enables 2FA.
func (s *MySQL) VerifyEmployee2FA(ctx context.Context, employeeID, code string, now time.Time) error {
	secret, enabled, err := s.employee2FASecret(ctx, employeeID)
	if err != nil {
		return err
	}
	if !verifyTOTP(secret, code, now) {
		return Err2FACode
	}
	if !enabled {
		return s.db.WithContext(ctx).Model(&Employee2FA{}).
			Where("employee_id = ?", employeeID).
			Updates(map[string]interface{}{"enabled": true, "updated_at": now}).Error
	}
	return nil
}

func (s *MySQL) DisableEmployee2FA(ctx context.Context, employeeID, code string, now time.Time) error {
	secret, _, err := s.employee2FASecret(ctx, employeeID)
	if err != nil {
		return err
	}
	if !verifyTOTP(secret, code, now) {
		return Err2FACode
	}
	return s.db.WithContext(ctx).Delete(&Employee2FA{}, "employee_id = ?", employeeID).Error
}

func (s *MySQL) Employee2FAEnabled(ctx context.Context, employeeID string) (bool, error) {
	_, enabled, err := s.employee2FASecret(ctx, employeeID)
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	return enabled, err
}

// CheckEmployee2FACode verifies a code without changing state (for login).
func (s *MySQL) CheckEmployee2FACode(ctx context.Context, employeeID, code string, now time.Time) error {
	secret, enabled, err := s.employee2FASecret(ctx, employeeID)
	if err != nil {
		return err
	}
	if !enabled {
		return Err2FANotEnabled
	}
	if !verifyTOTP(secret, code, now) {
		return Err2FACode
	}
	return nil
}

func (s *MySQL) employee2FASecret(ctx context.Context, employeeID string) (string, bool, error) {
	var row Employee2FA
	if err := s.db.WithContext(ctx).First(&row, "employee_id = ?", employeeID).Error; err != nil {
		return "", false, err
	}
	return row.Secret, row.Enabled, nil
}

var (
	Err2FAInput     = &shareLinkError{"invalid 2fa input"}
	Err2FACode      = &shareLinkError{"invalid 2fa code"}
	Err2FANotEnabled = &shareLinkError{"2fa not enabled"}
)

// verifyTOTP implements RFC 6238 (SHA1, 30s step, 6 digits) with ±1 window.
func verifyTOTP(secret, code string, now time.Time) bool {
	if len(code) != 6 {
		return false
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return false
	}
	counter := now.Unix() / 30
	for _, c := range []int64{counter - 1, counter, counter + 1} {
		if totpCode(key, c) == code {
			return true
		}
	}
	return false
}

func totpCode(key []byte, counter int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return strings.Repeat("0", 6-len(itoa(bin%1000000))) + itoa(bin%1000000)
}

func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
