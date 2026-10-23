package model

import "time"

type PatientChart struct {
	PatientID    string   `gorm:"primaryKey;size:64" json:"patientId"`
	ClinicID     string   `json:"clinicId"`
	StaffID      string   `json:"-"`
	Gender       string   `json:"gender"`
	BirthDate    string   `json:"birthDate"`
	ContactPhone string   `json:"contactPhone"`
	Tags         []string `gorm:"serializer:json" json:"tags"`
	Version      int      `json:"version"`
}

type ConsultationTemplate struct {
	ID               string    `gorm:"primaryKey;size:64" json:"id"`
	ClinicID         string    `json:"clinicId"`
	RecordedBy       string    `json:"recordedBy"`
	Name             string    `json:"name"`
	VisitType        string    `json:"visitType"`
	ChiefComplaint   string    `json:"chiefComplaint"`
	History          string    `json:"history"`
	Assessment       string    `json:"assessment"`
	Advice           string    `json:"advice"`
	MedicalHistory   string    `json:"medicalHistory"`
	AllergyHistory   string    `json:"allergyHistory"`
	TreatmentHistory string    `json:"treatmentHistory"`
	SkinType         string    `json:"skinType"`
	Goals            string    `json:"goals"`
	CreatedAt        time.Time `json:"createdAt"`
}
