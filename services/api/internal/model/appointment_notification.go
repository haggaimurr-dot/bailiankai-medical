package model

import "time"

// AppointmentNotification is a staff-side reminder about a pending appointment:
// pending_soon (confirmation deadline within 30 minutes, for reviewers) or
// pending_escalated (expired unconfirmed, for managers). Display fields are
// read-only joins, like HandoffNotification.
type AppointmentNotification struct {
	ID            string     `gorm:"primaryKey" json:"id"`
	AppointmentID string     `json:"appointmentId"`
	EmployeeID    string     `json:"-"`
	Kind          string     `json:"kind"`
	CreatedAt     time.Time  `json:"createdAt"`
	ReadAt        *time.Time `json:"readAt"`
	DoctorName    string     `gorm:"->;-:migration" json:"doctorName"`
	ClinicName    string     `gorm:"->;-:migration" json:"clinicName"`
	StartsAt      time.Time  `gorm:"->;-:migration" json:"startsAt"`
	ContactName   string     `gorm:"->;-:migration" json:"contactName"`
	Status        string     `gorm:"->;-:migration" json:"status"`
}

func (AppointmentNotification) TableName() string { return "appointment_notifications" }
