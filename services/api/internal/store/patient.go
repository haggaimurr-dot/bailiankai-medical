package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"regexp"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/wechat"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrPatientLoginUnavailable = errors.New("patient login is not configured")

type PatientProvider interface {
	Login(context.Context, string) (string, string, error)
	Phone(context.Context, string, string) (string, error)
}
type PatientRepository interface {
	LoginPatient(context.Context, string, time.Time) (SessionResult, error)
	UpdatePatientProfile(context.Context, string, string, int) (model.Patient, error)
	AuthorizePatientPhone(context.Context, string, string, int, time.Time) (model.Patient, error)
	RevokePatientPhone(context.Context, string, int) (model.Patient, error)
	LogoutPatient(context.Context, string) error
}

func (s *MySQL) SetPatientProvider(provider PatientProvider) { s.patientProvider = provider }

func (s *MySQL) LoginPatient(ctx context.Context, code string, now time.Time) (SessionResult, error) {
	if s.patientProvider == nil {
		return SessionResult{}, ErrPatientLoginUnavailable
	}
	appID, openID, err := s.patientProvider.Login(ctx, code)
	if err != nil {
		return SessionResult{}, err
	}
	if appID == "" || openID == "" || len(appID) > 64 || len(openID) > 128 {
		return SessionResult{}, wechat.ErrRejected
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return SessionResult{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	now = now.Truncate(time.Millisecond)
	patient := model.Patient{ID: randomID("patient-"), DisplayName: "微信患者", WechatAppID: &appID, WechatOpenID: &openID, PhoneAuthorization: "not_granted", Version: 1, CreatedAt: now}
	expires := now.Add(7 * 24 * time.Hour)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The unique binary appid/openid key serializes simultaneous first logins.
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&patient).Error; err != nil {
			return err
		}
		patient = model.Patient{}
		if err := tx.Where("wechat_app_id = ? AND wechat_open_id = ?", appID, openID).First(&patient).Error; err != nil {
			return err
		}
		return tx.Create(&model.PatientSession{TokenHash: tokenHash(token), PatientID: patient.ID, ExpiresAt: expires, CreatedAt: now}).Error
	})
	return SessionResult{Token: token, ExpiresAt: expires, Patient: patient}, err
}

func (s *MySQL) updatePatient(ctx context.Context, id string, version int, changes map[string]any) (model.Patient, error) {
	var patient model.Patient
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&patient, "id = ?", id).Error; err != nil {
			return err
		}
		if patient.Version != version {
			return ErrConflict
		}
		changes["version"] = version + 1
		if err := tx.Model(&patient).Updates(changes).Error; err != nil {
			return err
		}
		return tx.First(&patient, "id = ?", id).Error
	})
	return patient, err
}
func (s *MySQL) UpdatePatientProfile(ctx context.Context, id, name string, version int) (model.Patient, error) {
	return s.updatePatient(ctx, id, version, map[string]any{"display_name": name})
}
func (s *MySQL) AuthorizePatientPhone(ctx context.Context, id, code string, version int, now time.Time) (model.Patient, error) {
	if s.patientProvider == nil {
		return model.Patient{}, ErrPatientLoginUnavailable
	}
	var patient model.Patient
	if err := s.db.WithContext(ctx).First(&patient, "id = ?", id).Error; err != nil {
		return patient, err
	}
	if patient.Version != version {
		return patient, ErrConflict
	}
	if patient.WechatOpenID == nil {
		return patient, ErrUnauthenticated
	}
	phone, err := s.patientProvider.Phone(ctx, code, *patient.WechatOpenID)
	if err != nil {
		return patient, err
	}
	if !regexp.MustCompile(`^1[3-9][0-9]{9}$`).MatchString(phone) {
		return patient, wechat.ErrRejected
	}
	return s.updatePatient(ctx, id, version, map[string]any{"phone": phone, "phone_authorization": "granted", "phone_authorized_at": now.Truncate(time.Millisecond)})
}
func (s *MySQL) RevokePatientPhone(ctx context.Context, id string, version int) (model.Patient, error) {
	return s.updatePatient(ctx, id, version, map[string]any{"phone": "", "phone_authorization": "revoked", "phone_authorized_at": nil})
}
func (s *MySQL) LogoutPatient(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Where("token_hash = ?", tokenHash(token)).Delete(&model.PatientSession{}).Error
}
