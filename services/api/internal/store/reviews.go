package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
)

var (
	// ErrReviewNotEligible covers wrong patient, wrong doctor, or an
	// appointment without a treatment record.
	ErrReviewNotEligible = errors.New("appointment is not eligible for review")
	// ErrReviewDuplicate covers a second review for the same appointment.
	ErrReviewDuplicate = errors.New("appointment already reviewed")
)

// PatientReviewRepository is the review slice of the store.
type PatientReviewRepository interface {
	CreatePatientReview(ctx context.Context, patientID, doctorID, appointmentID string, rating int, content string, now time.Time) (model.DoctorReview, error)
	PublicPatientReviews(ctx context.Context, doctorID string) ([]PatientReviewWithPatient, error)
	MyPatientReviews(ctx context.Context, patientID, doctorID string) ([]model.DoctorReview, error)
	ReviewableAppointments(ctx context.Context, patientID, doctorID string) ([]ReviewableAppointment, error)
	AdminPatientReviews(ctx context.Context, status string) ([]PatientReviewWithPatient, error)
	DecidePatientReview(ctx context.Context, id, decision, note string, now time.Time) (model.DoctorReview, error)
}

// PatientReviewWithPatient carries the raw patient display name; the HTTP
// layer masks it for public responses and passes it through for employees.
type PatientReviewWithPatient struct {
	model.DoctorReview
	PatientDisplayName string `gorm:"column:patient_display_name"`
}

// ReviewableAppointment is a treated appointment of the patient with the
// doctor that has no review yet.
type ReviewableAppointment struct {
	AppointmentID string    `gorm:"column:appointment_id"`
	DoctorID      string    `gorm:"column:doctor_id"`
	StartsAt      time.Time `gorm:"column:starts_at"`
}

func validReviewContent(rating int, content string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(content))
	return rating >= 1 && rating <= 5 && n >= 1 && n <= 1000
}

func (s *MySQL) CreatePatientReview(ctx context.Context, patientID, doctorID, appointmentID string, rating int, content string, now time.Time) (model.DoctorReview, error) {
	var created model.DoctorReview
	if !validReviewContent(rating, content) {
		return created, errors.New("invalid review rating or content")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var check struct {
			PatientID string `gorm:"column:patient_id"`
			DoctorID  string `gorm:"column:doctor_id"`
			Treated   bool   `gorm:"column:treated"`
		}
		if err := tx.Raw(`SELECT a.patient_id, s.doctor_id,
			EXISTS(SELECT 1 FROM treatments t WHERE t.appointment_id = a.id) AS treated
			FROM appointments a JOIN slots s ON s.id = a.slot_id WHERE a.id = ?`, appointmentID).Scan(&check).Error; err != nil {
			return err
		}
		if check.PatientID == "" {
			return ErrNotFound
		}
		if check.PatientID != patientID || check.DoctorID != doctorID || !check.Treated {
			return ErrReviewNotEligible
		}
		var existing int64
		if err := tx.Model(&model.DoctorReview{}).Where("appointment_id = ?", appointmentID).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return ErrReviewDuplicate
		}
		created = model.DoctorReview{
			ID:            randomID("review-"),
			DoctorID:      doctorID,
			PatientID:     patientID,
			AppointmentID: appointmentID,
			Rating:        rating,
			Content:       strings.TrimSpace(content),
			Status:        "pending",
			CreatedAt:     now,
		}
		if err := tx.Create(&created).Error; err != nil {
			if strings.Contains(err.Error(), "Duplicate entry") {
				return ErrReviewDuplicate
			}
			return err
		}
		return nil
	})
	return created, err
}

func (s *MySQL) PublicPatientReviews(ctx context.Context, doctorID string) ([]PatientReviewWithPatient, error) {
	rows := make([]PatientReviewWithPatient, 0)
	err := s.db.WithContext(ctx).Table("doctor_reviews").
		Select("doctor_reviews.*, patients.display_name AS patient_display_name").
		Joins("JOIN patients ON patients.id = doctor_reviews.patient_id").
		Where("doctor_reviews.doctor_id = ? AND doctor_reviews.status = ?", doctorID, "approved").
		Order("doctor_reviews.created_at DESC").Limit(50).Scan(&rows).Error
	return rows, err
}

func (s *MySQL) MyPatientReviews(ctx context.Context, patientID, doctorID string) ([]model.DoctorReview, error) {
	rows := make([]model.DoctorReview, 0)
	q := s.db.WithContext(ctx).Where("patient_id = ?", patientID)
	if doctorID != "" {
		q = q.Where("doctor_id = ?", doctorID)
	}
	err := q.Order("created_at DESC").Limit(50).Find(&rows).Error
	return rows, err
}

func (s *MySQL) ReviewableAppointments(ctx context.Context, patientID, doctorID string) ([]ReviewableAppointment, error) {
	rows := make([]ReviewableAppointment, 0)
	err := s.db.WithContext(ctx).Raw(`SELECT a.id AS appointment_id, s.doctor_id, s.starts_at
		FROM appointments a JOIN slots s ON s.id = a.slot_id
		WHERE a.patient_id = ? AND s.doctor_id = ?
		AND EXISTS(SELECT 1 FROM treatments t WHERE t.appointment_id = a.id)
		AND NOT EXISTS(SELECT 1 FROM doctor_reviews r WHERE r.appointment_id = a.id)
		ORDER BY s.starts_at DESC LIMIT 50`, patientID, doctorID).Scan(&rows).Error
	return rows, err
}

func (s *MySQL) AdminPatientReviews(ctx context.Context, status string) ([]PatientReviewWithPatient, error) {
	rows := make([]PatientReviewWithPatient, 0)
	q := s.db.WithContext(ctx).Table("doctor_reviews").
		Select("doctor_reviews.*, patients.display_name AS patient_display_name").
		Joins("JOIN patients ON patients.id = doctor_reviews.patient_id")
	if status == "pending" || status == "approved" || status == "rejected" {
		q = q.Where("doctor_reviews.status = ?", status)
	}
	err := q.Order("doctor_reviews.created_at DESC").Limit(200).Scan(&rows).Error
	return rows, err
}

func (s *MySQL) DecidePatientReview(ctx context.Context, id, decision, note string, now time.Time) (model.DoctorReview, error) {
	var row model.DoctorReview
	if decision != "approved" && decision != "rejected" {
		return row, errors.New("invalid review decision")
	}
	if utf8.RuneCountInString(note) > 500 {
		return row, errors.New("review note too long")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", id).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if row.Status != "pending" {
			return ErrConflict
		}
		return tx.Model(&row).Updates(map[string]any{"status": decision, "review_note": note, "reviewed_at": now}).Error
	})
	if err == nil {
		row.Status = decision
		row.ReviewNote = note
		row.ReviewedAt = &now
	}
	return row, err
}
