package model

import "time"

type Consultation struct {
	MedicalHistory       string     `json:"medicalHistory"`
	AllergyHistory       string     `json:"allergyHistory"`
	TreatmentHistory     string     `json:"treatmentHistory"`
	SkinType             string     `json:"skinType"`
	Goals                string     `json:"goals"`
	VisitType            string     `json:"visitType"`
	ParentConsultationID *string    `json:"parentConsultationId,omitempty"`
	AppointmentID        *string    `json:"appointmentId,omitempty"`
	VisitedAt            time.Time  `json:"visitedAt"`
	NextVisitAt          *time.Time `json:"nextVisitAt,omitempty"`
	StageIndex           int        `json:"stageIndex"`
	StageStatus          string     `json:"stageStatus"`
	ID                   string     `gorm:"primaryKey;size:64" json:"id"`
	PatientID            string     `json:"patientId"`
	ClinicID             string     `json:"clinicId"`
	ChiefComplaint       string     `json:"chiefComplaint"`
	History              string     `json:"history"`
	Assessment           string     `json:"assessment"`
	Advice               string     `json:"advice"`
	RecordedBy           string     `json:"recordedBy"`
	RecordedAt           time.Time  `json:"recordedAt"`
}
