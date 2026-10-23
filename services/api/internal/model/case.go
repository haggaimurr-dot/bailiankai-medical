package model

import "time"

type CaseRecord struct {
	ID              string     `gorm:"primaryKey;size:64" json:"id"`
	AppointmentID   string     `json:"appointmentId"`
	PatientID       string     `json:"patientId"`
	ClinicID        string     `json:"clinicId"`
	ContentJSON     string     `json:"-"`
	Version         int        `json:"version"`
	Revision        int        `json:"revision"`
	ConsentRevision int        `json:"consentRevision"`
	ReviewStatus    string     `json:"reviewStatus"`
	ReviewNote      string     `json:"reviewNote"`
	ReviewerID      string     `json:"reviewerId"`
	ReviewerName    string     `json:"reviewerName"`
	ReviewedAt      *time.Time `json:"reviewedAt"`
	PublishedAt     *time.Time `json:"publishedAt"`
}
type CaseImage struct {
	ID        string `gorm:"primaryKey;size:64"`
	CaseID    string
	SHA256    string
	SizeBytes int64
}
type CaseEvent struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	CaseID        string    `json:"caseId"`
	Version       int       `json:"version"`
	Action        string    `json:"action"`
	ActorID       string    `json:"actorId"`
	SnapshotJSON  string    `json:"snapshot"`
	PolicyVersion string    `json:"policyVersion"`
	PolicyContent string    `json:"policyContent"`
	CreatedAt     time.Time `json:"createdAt"`
}
