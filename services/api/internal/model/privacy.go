package model

import "time"

type PrivacyConsentEvent struct {
	ID            string `gorm:"primaryKey;size:64"`
	PatientID     string
	PolicyVersion string
	PolicyContent string
	Action        string
	Source        string
	RecordedAt    time.Time
}
