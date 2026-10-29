package model

import "time"

// DoctorReview is a patient review of a doctor. It starts pending and only
// becomes visible to patients after an employee approves it. One review per
// appointment, and only appointments with a treatment record are eligible.
type DoctorReview struct {
	ID            string `gorm:"primaryKey;size:64"`
	DoctorID      string
	PatientID     string
	AppointmentID string
	Rating        int
	Content       string
	Status        string
	ReviewNote    string
	CreatedAt     time.Time
	ReviewedAt    *time.Time
}

func (DoctorReview) TableName() string { return "doctor_reviews" }
