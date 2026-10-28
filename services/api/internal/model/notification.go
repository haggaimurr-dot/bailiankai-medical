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

// PatientMessage is an in-app inbox message. Kind is appointment_status for
// appointment status changes; ReadAt is nil until the patient opens it.
type PatientMessage struct {
	ID            string     `gorm:"primaryKey;size:64" json:"id"`
	PatientID     string     `json:"patientId"`
	Kind          string     `json:"kind"`
	Title         string     `json:"title"`
	Body          string     `json:"body"`
	AppointmentID string     `json:"appointmentId"`
	// TicketID links a support-reply message to its ticket ("" for other kinds).
	TicketID      string     `json:"ticketId"`
	ReadAt        *time.Time `json:"readAt"`
	CreatedAt     time.Time  `json:"createdAt"`
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
	// Urgent marks an overdue reminder: it is sent immediately and alone,
	// never folded into a digest (P2 消息-患者端：提醒合并发送）.
	Urgent bool `json:"urgent"`
	// MergedInto points at the queued digest row this reminder was folded
	// into; "" (NULL) means the row stands alone. Status "merged" rows are
	// sent together with their digest row.
	MergedInto *string `json:"mergedInto,omitempty"`
	// FailureCategory classifies LastCode for display and filtering; it is
	// derived at read time and never persisted.
	FailureCategory string `gorm:"-" json:"failureCategory"`
	// DigestCount is how many reminders ride on this digest row (itself
	// excluded); derived at read time, never persisted.
	DigestCount int `gorm:"-" json:"digestCount,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// NotificationMergeSetting is the global merge window for the reminder
// queue (P2 消息-患者端：提醒合并发送）: ordinary reminders for one patient
// whose send times fall inside MergeWindowMinutes are folded into a single
// digest instead of being sent one by one. Singleton row id=1, like the
// handoff overdue setting.
type NotificationMergeSetting struct {
	ID                 int       `gorm:"primaryKey"`
	MergeWindowMinutes int       `json:"mergeWindowMinutes"`
	MergeEnabled       bool      `json:"mergeEnabled"`
	Version            int       `json:"version"`
	UpdatedBy          *string   `json:"updatedBy"`
	UpdatedAt          time.Time `json:"updatedAt"`
}
type NotificationAttempt struct {
	ID             string    `gorm:"primaryKey" json:"id"`
	NotificationID string    `json:"notificationId"`
	Status         string    `json:"status"`
	Code           string    `json:"code"`
	// FailureCategory classifies Code for display; derived at read time.
	FailureCategory string `gorm:"-" json:"failureCategory"`
	CreatedAt      time.Time `json:"createdAt"`
}
