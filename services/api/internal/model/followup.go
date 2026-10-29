package model

import "time"

type TreatmentFields struct {
	StageIndex int    `json:"stageIndex"`
	Procedure  string `json:"procedure"`
	BodyArea   string `json:"bodyArea"`
	Device     string `json:"device"`
	Parameters string `json:"parameters"`
}

type Treatment struct {
	TreatmentFields `gorm:"embedded"`
	ID              string `gorm:"primaryKey;size:64"`
	AppointmentID   string
	PatientID       string
	PlanVersionID   string
	Summary         string
	Notes           string
	TreatedAt       time.Time
	RecordedAt      time.Time
	CorrectedAt     *time.Time
	Version         int
	RequestHash     string
}

type TreatmentRevision struct {
	TreatmentFields  `gorm:"embedded"`
	ID               string `gorm:"primaryKey;size:64"`
	TreatmentID      string
	Version          int
	PlanVersionID    string
	Summary          string
	Notes            string
	TreatedAt        time.Time
	ChangedAt        time.Time
	ActorID          string
	CorrectionReason string
}

type FollowupTask struct {
	ID            string `gorm:"primaryKey;size:64"`
	AppointmentID string
	// TreatmentID is nil for advice-derived todos: confirming a published
	// plan's instructions creates follow-up tasks with no treatment record.
	TreatmentID *string
	PatientID     string
	AssigneeID    string
	AssignedAt    time.Time
	Title         string
	DueAt         time.Time
	Status        string
	Version       int
	Symptoms      string
	Severity      int
	Concern       bool
	Response      string
	Disposition   string
	SubmittedAt   *time.Time
	ReviewedAt    *time.Time
	PatientName   string `gorm:"->;-:migration"`
	AssigneeName  string `gorm:"->;-:migration"`
}

type FollowupStaff struct {
	ID              string `gorm:"primaryKey;size:64"`
	DisplayName     string
	Role            string
	Status          string
	Version         int
	LastHandoffTo   *string
	LastLeaveReason string
}

func (FollowupStaff) TableName() string { return "followup_staff" }

type FollowupAssignmentEvent struct {
	TaskVersion int    `json:"-"`
	ID          string `gorm:"primaryKey;size:64"`
	TaskID      string
	FromStaffID *string
	ToStaffID   string
	EventType   string
	Reason      string
	ActorID     string
	ActorName   string `gorm:"->;-:migration"`
	CreatedAt   time.Time
	FromName    string `gorm:"->;-:migration"`
	ToName      string `gorm:"->;-:migration"`
}

type FollowupStaffEvent struct {
	StaffVersion     int    `json:"-"`
	ID               string `gorm:"primaryKey;size:64"`
	StaffID          string
	EventType        string
	ToStaffID        *string
	Reason           string
	TransferredCount int
	ActorID          string
	ActorName        string `gorm:"->;-:migration"`
	CreatedAt        time.Time
	ToName           string `gorm:"->;-:migration"`
}

type FollowupHandoffReceipt struct {
	ID              string `gorm:"primaryKey;size:64"`
	BatchID         *string `gorm:"column:batch_id"`
	TaskID          string
	FromStaffID     string
	ToStaffID       string
	Reason          string
	AssignedBy      string
	CreatedAt       time.Time
	Status          string
	FollowupStatus  string
	FollowupVersion int
	ConfirmedBy     *string
	ConfirmedAt     *time.Time
	SupersededAt    *time.Time
	Version         int
	PatientName     string    `gorm:"->;-:migration"`
	TaskTitle       string    `gorm:"->;-:migration"`
	DueAt           time.Time `gorm:"->;-:migration"`
	FromName        string    `gorm:"->;-:migration"`
	ToName          string    `gorm:"->;-:migration"`
	AssignedByName  string    `gorm:"->;-:migration"`
	ConfirmedByName string    `gorm:"->;-:migration"`
	FollowupCount   int       `gorm:"->;-:migration"`
}

type HandoffFollowupEvent struct {
	ID          string `gorm:"primaryKey;size:64"`
	ReceiptID   string
	RequestKey  string
	ActionText  string
	ResultText  string
	StatusAfter string
	ActorID     string
	CreatedAt   time.Time
	ActorName   string `gorm:"->;-:migration"`
}

type FollowupHandoffSetting struct {
	ID           int `gorm:"primaryKey"`
	OverdueHours int
	Version      int
	UpdatedBy    *string
	UpdatedAt    time.Time
}

type FollowupPhoto struct {
	ID            string `gorm:"primaryKey;size:64"`
	TaskID        string
	AppointmentID string
	PatientID     string
	Angle         string
	SizeBytes     int64
	SHA256        string
	UploadedAt    time.Time
}

type FollowupEvent struct {
	ID            string `gorm:"primaryKey;size:64"`
	AppointmentID string
	TaskID        string
	ActorType     string
	ActorID       string
	Action        string
	DetailJSON    string
	CreatedAt     time.Time
}
