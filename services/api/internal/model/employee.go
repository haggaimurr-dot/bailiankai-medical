package model

import "time"

// EmployeeLoginAttempt is one employee sign-in attempt: time, username,
// result and the client IP the server saw. Failed attempts are the audit
// trail behind the login-anomaly alert (P2 后台-账号：登录异常提醒）.
type EmployeeLoginAttempt struct {
	ID         string  `gorm:"primaryKey;size:64" json:"id"`
	Username   string  `json:"username"`
	EmployeeID *string `json:"employeeId"`
	Success    bool    `json:"success"`
	ClientIP   string  `json:"clientIp"`
	CreatedAt  time.Time `json:"createdAt"`
}

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
	// The login-anomaly action instead records
	// {"consecutiveFailures": N, "clientIp": "...", "newIp": bool}.
	Details   string `gorm:"type:text"`
	CreatedAt time.Time
	ActorName string `gorm:"->;-:migration"`
}
