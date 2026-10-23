package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"example.com/bailiankai/api/internal/model"
	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrBindingInvalid = errors.New("binding code invalid or expired")
var ErrBindingConflict = errors.New("patient identity already bound or contains records")

type PatientBindingCode struct {
	TokenHash       string     `gorm:"primaryKey" json:"-"`
	PatientID       string     `json:"patientId"`
	PatientVersion  int        `json:"-"`
	CreatedBy       string     `json:"-"`
	CreatedAt       time.Time  `json:"-"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	ConsumedAt      *time.Time `json:"-"`
	SourcePatientID string     `json:"-"`
}
type PatientBindingPreview struct {
	PatientID   string `json:"patientId"`
	DisplayName string `json:"displayName"`
	ClinicName  string `json:"clinicName"`
	Version     int    `json:"version"`
}
type PatientBindingRepository interface {
	CreatePatientBindingCode(context.Context, string, string, time.Time) (string, time.Time, error)
	RevokePatientBindingCodes(context.Context, string, time.Time) error
	PreviewPatientBinding(context.Context, string, time.Time) (PatientBindingPreview, error)
	BindPatient(context.Context, string, string, int, time.Time) (SessionResult, error)
}

func (s *MySQL) bindingAccess(ctx context.Context, id string) error {
	if !chartWriter(ctx) {
		return ErrForbidden
	}
	row, _, err := s.PatientRecordAppointments(ctx, id)
	if err != nil {
		return err
	}
	if row.Profile == nil {
		return ErrBindingConflict
	}
	return nil
}
func (s *MySQL) CreatePatientBindingCode(ctx context.Context, id, actor string, now time.Time) (string, time.Time, error) {
	if err := s.bindingAccess(ctx, id); err != nil {
		return "", time.Time{}, err
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return "", time.Time{}, err
	}
	code := hex.EncodeToString(secret)
	expires := now.Add(15 * time.Minute).Truncate(time.Millisecond)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var patient model.Patient
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&patient, "id = ?", id).Error; err != nil {
			return err
		}
		if patient.WechatOpenID != nil {
			return ErrBindingConflict
		}
		if err := tx.Model(&PatientBindingCode{}).Where("patient_id = ? AND consumed_at IS NULL", id).Update("expires_at", now).Error; err != nil {
			return err
		}
		return tx.Create(&PatientBindingCode{TokenHash: tokenHash(code), PatientID: id, PatientVersion: patient.Version, CreatedBy: actor, CreatedAt: now, ExpiresAt: expires}).Error
	})
	return code, expires, err
}
func (s *MySQL) RevokePatientBindingCodes(ctx context.Context, id string, now time.Time) error {
	if err := s.bindingAccess(ctx, id); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPatient(tx, id); err != nil {
			return err
		}
		return tx.Model(&PatientBindingCode{}).Where("patient_id = ? AND consumed_at IS NULL", id).Update("expires_at", now).Error
	})
}
func bindingTarget(tx *gorm.DB, code string, now time.Time, locking bool) (PatientBindingCode, model.Patient, error) {
	var invite PatientBindingCode
	var patient model.Patient
	inviteQuery, patientQuery := tx, tx
	if locking {
		inviteQuery = tx.Clauses(clause.Locking{Strength: "UPDATE"})
		patientQuery = tx.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := inviteQuery.Where("token_hash = ? AND consumed_at IS NULL AND expires_at > ?", tokenHash(code), now).First(&invite).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = ErrBindingInvalid
		}
		return invite, patient, err
	}
	if err := patientQuery.First(&patient, "id = ?", invite.PatientID).Error; err != nil {
		return invite, patient, err
	}
	if patient.WechatOpenID != nil || patient.Version != invite.PatientVersion {
		return invite, patient, ErrBindingConflict
	}
	return invite, patient, nil
}
func (s *MySQL) PreviewPatientBinding(ctx context.Context, code string, now time.Time) (PatientBindingPreview, error) {
	invite, patient, err := bindingTarget(s.db.WithContext(ctx), code, now, false)
	if err != nil {
		return PatientBindingPreview{}, err
	}
	var clinic string
	err = s.db.WithContext(ctx).Table("patient_charts").Select("clinics.name").Joins("JOIN clinics ON clinics.id = patient_charts.clinic_id").Where("patient_charts.patient_id = ?", invite.PatientID).Scan(&clinic).Error
	return PatientBindingPreview{PatientID: patient.ID, DisplayName: patient.DisplayName, ClinicName: clinic, Version: patient.Version}, err
}
func (s *MySQL) BindPatient(ctx context.Context, sourceID, code string, version int, now time.Time) (SessionResult, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return SessionResult{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	expires := now.Add(7 * 24 * time.Hour).Truncate(time.Millisecond)
	var target model.Patient
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the two patients in a stable order. Login and profile writes share these locks.
		var invite PatientBindingCode
		if err := tx.First(&invite, "token_hash = ?", tokenHash(code)).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrBindingInvalid
			}
			return err
		}
		var patients []model.Patient
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", []string{sourceID, invite.PatientID}).Order("id").Find(&patients).Error; err != nil {
			return err
		}
		var source model.Patient
		for _, p := range patients {
			if p.ID == sourceID {
				source = p
			}
		}
		if source.WechatAppID == nil || source.WechatOpenID == nil {
			return ErrUnauthenticated
		}
		var err error
		invite, target, err = bindingTarget(tx, code, now, true)
		if err != nil {
			return err
		}
		if target.ID == sourceID || target.Version != version {
			return ErrBindingConflict
		}
		// Privacy decisions belong to this verified account; preserve their full history.
		if err := tx.Table("privacy_consent_events").Where("patient_id = ?", sourceID).Update("patient_id", target.ID).Error; err != nil {
			return err
		}
		// Delete only an empty login shell. Foreign keys reject ANY other business data, including
		// a concurrent insert; rollback restores its identity and sessions on conflict.
		if err := tx.Where("patient_id = ?", sourceID).Delete(&model.PatientSession{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&source).Error; err != nil {
			var mysqlErr *driver.MySQLError
			if errors.As(err, &mysqlErr) && mysqlErr.Number == 1451 {
				return ErrBindingConflict
			}
			return err
		}
		if err := tx.Model(&target).Updates(map[string]any{"wechat_app_id": source.WechatAppID, "wechat_open_id": source.WechatOpenID, "phone": source.Phone, "phone_authorization": source.PhoneAuthorization, "phone_authorized_at": source.PhoneAuthorizedAt, "version": target.Version + 1}).Error; err != nil {
			return err
		}
		result := tx.Model(&PatientBindingCode{}).Where("token_hash = ? AND consumed_at IS NULL AND expires_at > ?", invite.TokenHash, now).Updates(map[string]any{"consumed_at": now, "source_patient_id": sourceID})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBindingInvalid
		}
		if err := tx.First(&target, "id = ?", target.ID).Error; err != nil {
			return err
		}
		return tx.Create(&model.PatientSession{TokenHash: tokenHash(token), PatientID: target.ID, ExpiresAt: expires, CreatedAt: now}).Error
	})
	return SessionResult{Token: token, ExpiresAt: expires, Patient: target}, err
}
