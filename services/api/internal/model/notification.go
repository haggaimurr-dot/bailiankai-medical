package model

import "time"

type MessageTemplate struct {
	ID          string `gorm:"primaryKey;size:64" json:"id"`
	ClinicID    string `json:"clinicId"`
	Kind        string `json:"kind"`
	TemplateID  string `json:"templateId"`
	TitleKey    string `json:"titleKey"`
	TimeKey     string `json:"timeKey"`
	Page        string `json:"page"`
	State       string `json:"state"`
	LeadMinutes int    `json:"leadMinutes"`
	Enabled     bool   `json:"enabled"`
	Version     int    `json:"version"`
}
type MessageConsent struct {
	PatientID  string    `gorm:"primaryKey" json:"patientId"`
	TemplateID string    `gorm:"primaryKey" json:"templateId"`
	Status     string    `json:"status"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
type MessageConsentEvent struct {
	ID         string    `gorm:"primaryKey" json:"id"`
	PatientID  string    `json:"patientId"`
	TemplateID string    `json:"templateId"`
	RequestKey string    `json:"requestKey"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}
type Notification struct {
	ID         string    `gorm:"primaryKey" json:"id"`
	ClinicID   string    `json:"clinicId"`
	PatientID  string    `json:"patientId"`
	TemplateID string    `json:"templateId"`
	Kind       string    `json:"kind"`
	SourceID   string    `json:"sourceId"`
	DueAt      time.Time `json:"dueAt"`
	SendAt     time.Time `json:"sendAt"`
	Status     string    `json:"status"`
	Attempts   int       `json:"attempts"`
	LastCode   string    `json:"lastCode"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
type NotificationAttempt struct {
	ID             string    `gorm:"primaryKey" json:"id"`
	NotificationID string    `json:"notificationId"`
	Status         string    `json:"status"`
	Code           string    `json:"code"`
	CreatedAt      time.Time `json:"createdAt"`
}
