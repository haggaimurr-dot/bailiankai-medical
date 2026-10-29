package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ============ 行 109: 档案临时授权 ============
// A patient can generate a time-limited share link for their record.
// The link is a secret token; anyone with the link can view a read-only
// summary until it expires or is revoked.

type RecordShareLink struct {
	ID        string `gorm:"primaryKey;size:64"`
	PatientID string
	TokenHash string
	ExpiresAt time.Time
	Revoked   bool
	CreatedBy string
	CreatedAt time.Time
}

func (RecordShareLink) TableName() string { return "record_share_links" }

var ErrShareLinkInput = &shareLinkError{"invalid share link input"}

type shareLinkError struct{ msg string }

func (e *shareLinkError) Error() string { return e.msg }

// CreateRecordShareLink generates a link valid for ttl (e.g. 24h/7d/30d).
// It returns the link row and the plaintext token (shown once).
func (s *MySQL) CreateRecordShareLink(ctx context.Context, patientID string, ttl time.Duration, createdBy string, now time.Time) (RecordShareLink, string, error) {
	if strings.TrimSpace(patientID) == "" || ttl <= 0 || ttl > 90*24*time.Hour {
		return RecordShareLink{}, "", ErrShareLinkInput
	}
	token := randomID("share-") + randomID("")
	sum := sha256.Sum256([]byte(token))
	link := RecordShareLink{
		ID: randomID("rsl-"), PatientID: patientID,
		TokenHash: hex.EncodeToString(sum[:]),
		ExpiresAt: now.Add(ttl), CreatedBy: createdBy, CreatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(&link).Error; err != nil {
		return RecordShareLink{}, "", err
	}
	return link, token, nil
}

func (s *MySQL) RevokeRecordShareLink(ctx context.Context, id, patientID string) error {
	return s.db.WithContext(ctx).Model(&RecordShareLink{}).
		Where("id = ? AND patient_id = ?", id, patientID).
		Update("revoked", true).Error
}

func (s *MySQL) RecordShareLinks(ctx context.Context, patientID string) ([]RecordShareLink, error) {
	var links []RecordShareLink
	err := s.db.WithContext(ctx).Where("patient_id = ?", patientID).
		Order("created_at DESC").Limit(20).Find(&links).Error
	return links, err
}

// ResolveRecordShareLink validates a token and returns the link if it is
// active and unexpired.
func (s *MySQL) ResolveRecordShareLink(ctx context.Context, token string, now time.Time) (RecordShareLink, error) {
	sum := sha256.Sum256([]byte(token))
	var link RecordShareLink
	err := s.db.WithContext(ctx).
		Where("token_hash = ? AND revoked = ? AND expires_at > ?", hex.EncodeToString(sum[:]), false, now).
		First(&link).Error
	if err != nil {
		return RecordShareLink{}, ErrNotFound
	}
	return link, nil
}

// ============ 行 110: 患者标签分层 ============

type PatientTier struct {
	PatientID string `gorm:"primaryKey;size:64"`
	Tier      string
	Reason    string
	UpdatedBy string
	UpdatedAt time.Time
}

func (PatientTier) TableName() string { return "patient_tiers" }

var validTiers = map[string]bool{"vip": true, "normal": true, "high_risk": true}

func (s *MySQL) SetPatientTier(ctx context.Context, patientID, tier, reason, updatedBy string, now time.Time) error {
	tier = strings.TrimSpace(tier)
	if strings.TrimSpace(patientID) == "" || !validTiers[tier] || len([]rune(reason)) > 300 {
		return ErrTierInput
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row PatientTier
		if err := tx.First(&row, "patient_id = ?", patientID).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return err
			}
			return tx.Create(&PatientTier{PatientID: patientID, Tier: tier, Reason: reason, UpdatedBy: updatedBy, UpdatedAt: now}).Error
		}
		row.Tier = tier
		row.Reason = reason
		row.UpdatedBy = updatedBy
		row.UpdatedAt = now
		return tx.Save(&row).Error
	})
}

func (s *MySQL) PatientTier(ctx context.Context, patientID string) (PatientTier, error) {
	var row PatientTier
	err := s.db.WithContext(ctx).First(&row, "patient_id = ?", patientID).Error
	if err == gorm.ErrRecordNotFound {
		return PatientTier{PatientID: patientID, Tier: "normal"}, nil
	}
	return row, err
}

var ErrTierInput = &shareLinkError{"invalid patient tier input"}

// ============ 行 111: 复诊带上次要点 ============
// LastVisitSummary returns the most recent completed appointment's key points:
// diagnosis/treatment summary + latest published care plan summary.
type LastVisitSummary struct {
	AppointmentID string
	VisitedAt     time.Time
	DoctorName    string
	Summary       string
	CarePlan      string
}

func (s *MySQL) LastVisitSummary(ctx context.Context, patientID string) (LastVisitSummary, error) {
	var out LastVisitSummary
	// Most recent completed/confirmed appointment.
	var appt struct {
		ID        string
		SlotID    string
		CreatedAt time.Time
	}
	err := s.db.WithContext(ctx).Table("appointments").
		Select("appointments.id, appointments.slot_id, appointments.created_at").
		Where("appointments.patient_id = ? AND appointments.status IN ?", patientID, []string{"completed", "confirmed"}).
		Order("appointments.created_at DESC").Limit(1).
		Scan(&appt).Error
	if err != nil || appt.ID == "" {
		return out, err
	}
	out.AppointmentID = appt.ID
	// Doctor name via slot.
	var slot struct {
		DoctorID  string
		StartsAt  time.Time
		ClinicID  string
	}
	s.db.WithContext(ctx).Table("slots").Select("doctor_id, starts_at, clinic_id").
		Where("id = ?", appt.SlotID).Scan(&slot)
	out.VisitedAt = slot.StartsAt
	var doctor struct{ DisplayName string }
	s.db.WithContext(ctx).Table("doctors").Select("display_name").
		Where("id = ?", slot.DoctorID).Scan(&doctor)
	out.DoctorName = doctor.DisplayName
	// Latest treatment summary (actual treatment record).
	var treatment struct{ Summary string }
	s.db.WithContext(ctx).Table("treatments").Select("summary").
		Where("appointment_id = ?", appt.ID).Order("created_at DESC").Limit(1).Scan(&treatment)
	out.Summary = treatment.Summary
	// Latest published care plan summary.
	var plan struct{ Summary string }
	s.db.WithContext(ctx).Table("care_plan_versions").Select("summary").
		Where("appointment_id = ?", appt.ID).Order("version DESC").Limit(1).Scan(&plan)
	out.CarePlan = plan.Summary
	return out, nil
}
