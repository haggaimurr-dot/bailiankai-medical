package model

import "time"

// RecordExportJob deliberately keeps its selection and generated archive private.
// The API can report a job without revealing a patient or a source record ID.
type RecordExportJob struct {
	ID            string     `gorm:"primaryKey;size:64" json:"id"`
	EmployeeID    string     `json:"employeeId"`
	PatientCount  int        `json:"patientCount"`
	Sections      []string   `gorm:"serializer:json" json:"sections"`
	DateFrom      string     `json:"dateFrom"`
	DateTo        string     `json:"dateTo"`
	Masked        bool       `json:"masked"`
	Reason        string     `json:"reason"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"createdAt"`
	CompletedAt   *time.Time `json:"completedAt"`
	ExpiresAt     *time.Time `json:"expiresAt"`
	SizeBytes     int64      `json:"sizeBytes"`
	ErrorCode     string     `json:"errorCode"`
	DownloadCount int        `json:"downloadCount"`
	SnapshotJSON  string     `gorm:"type:json" json:"-"`
	Payload       []byte     `gorm:"type:mediumblob" json:"-"`
	LeaseUntil    *time.Time `json:"-"`
	LeaseToken    string     `json:"-"`
	ReferenceKey  string     `gorm:"-" json:"-"`
}

type RecordExportEvent struct {
	ID         string    `gorm:"primaryKey;size:64" json:"id"`
	JobID      string    `json:"jobId"`
	ActorID    string    `json:"actorId"`
	Action     string    `json:"action"`
	DetailCode string    `json:"detailCode"`
	CreatedAt  time.Time `json:"createdAt"`
}
