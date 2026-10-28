package model

import "time"

type EmployeeAccount struct {
	ID           string `gorm:"primaryKey;size:64"`
	Username     string
	DisplayName  string
	PasswordHash string
	Role         string
	ClinicIDs    []string `gorm:"serializer:json"`
	StaffID      *string
	Active       bool
	Version      int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type EmployeeSession struct {
	TokenHash  string `gorm:"primaryKey;size:64"`
	EmployeeID string
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

type EmployeeAccountEvent struct {
	ID         string `gorm:"primaryKey;size:64"`
	EmployeeID string
	ActorID    string
	Action     string
	// Details is a JSON object {"before": {...}, "after": {...}} recording the
	// permission change, empty for actions without a before/after diff.
	Details   string `gorm:"type:text"`
	CreatedAt time.Time
	ActorName string `gorm:"->;-:migration"`
}
