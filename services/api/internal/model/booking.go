package model

import "time"

type Patient struct {
	ID                 string `gorm:"primaryKey;size:64"`
	DisplayName        string
	CreatedAt          time.Time
	WechatAppID        *string
	WechatOpenID       *string
	Phone              string
	PhoneAuthorization string `gorm:"default:not_granted"`
	PhoneAuthorizedAt  *time.Time
	Version            int `gorm:"default:1"`
}

type PatientSession struct {
	TokenHash string `gorm:"primaryKey;size:64"`
	PatientID string
	ExpiresAt time.Time
	CreatedAt time.Time
}

func (PatientSession) TableName() string { return "sessions" }

type Appointment struct {
	ClinicNameSnapshot   *string
	DoctorNameSnapshot   *string
	ID                   string `gorm:"primaryKey;size:64"`
	PatientID            string
	SlotID               string
	ContactName          string
	ContactPhone         string
	Note                 string
	Status               string
	RejectionReason      string
	CancelReason         string
	CancelReasonDetail   string
	CancelledAt          *time.Time
	ConfirmationDeadline time.Time
	RequestKey           string
	RequestHash          string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Version              int
	ArrivedAt            *time.Time
	ArrivedBy            string
	// Joined display fields are read-only. The slot itself is immutable in M1.
	TreatmentRecorded bool      `gorm:"->;-:migration"`
	SlotStatus        string    `gorm:"->;-:migration"`
	SuspensionReason  string    `gorm:"->;-:migration"`
	ClinicID          string    `gorm:"->;-:migration"`
	ClinicName        string    `gorm:"->;-:migration"`
	DoctorID          string    `gorm:"->;-:migration"`
	DoctorName        string    `gorm:"->;-:migration"`
	StartsAt          time.Time `gorm:"->;-:migration"`
	EndsAt            time.Time `gorm:"->;-:migration"`
}

// WaitlistOffer is a time-limited vacancy offer for one waiting appointment.
// While status is "offered" the seat is held in the slot's reserved count, so
// no other patient can take it; confirming converts the waiting appointment
// to pending without touching the count, expiring releases it and the next
// waiter is offered in the same transaction. Statuses: offered|accepted|expired.
type WaitlistOffer struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	AppointmentID string    `json:"appointmentId"`
	SlotID        string    `json:"slotId"`
	PatientID     string    `json:"patientId"`
	Status        string    `json:"status"`
	ExpiresAt     time.Time `json:"expiresAt"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type AppointmentEvent struct {
	Sequence      uint64 `gorm:"autoIncrement"`
	RequestKey    *string
	RequestHash   string
	Detail        string
	ID            string `gorm:"primaryKey;size:64"`
	AppointmentID string
	ActorType     string
	ActorID       string
	Action        string
	FromStatus    string
	ToStatus      string
	CreatedAt     time.Time
}

type CarePhoto struct {
	ID            string `gorm:"primaryKey;size:64"`
	AppointmentID string `gorm:"size:64;uniqueIndex:care_photos_appointment_angle,priority:1"`
	PatientID     string `gorm:"size:64;index"`
	Angle         string `gorm:"size:16;uniqueIndex:care_photos_appointment_angle,priority:2"`
	MediaType     string `gorm:"size:32"`
	SizeBytes     int64
	SHA256        string `gorm:"size:64"`
	UploadedAt    time.Time
}

type PhotoConsentEvent struct {
	ID            string    `gorm:"primaryKey;size:64"`
	AppointmentID string    `gorm:"size:64;index:photo_consent_latest,priority:1"`
	PatientID     string    `gorm:"size:64;index:photo_consent_latest,priority:2"`
	Action        string    `gorm:"size:16"`
	Version       string    `gorm:"size:32"`
	RecordedAt    time.Time `gorm:"index:photo_consent_latest,priority:3,sort:desc"`
}

type CarePlan struct {
	ID            string `gorm:"primaryKey;size:64"`
	AppointmentID string `gorm:"size:64;uniqueIndex"`
	DraftVersion  int
	DraftJSON     *string `gorm:"type:json"`
	UpdatedAt     time.Time
}

type CarePlanVersion struct {
	ID           string    `gorm:"primaryKey;size:64"`
	CarePlanID   string    `gorm:"size:64;uniqueIndex:care_plan_versions_plan_version,priority:1"`
	Version      int       `gorm:"uniqueIndex:care_plan_versions_plan_version,priority:2"`
	ContentJSON  string    `gorm:"type:json"`
	ChangeReason string    `gorm:"size:300"`
	PublishedBy  string    `gorm:"size:64"`
	PublishedAt  time.Time `gorm:"index"`
}

type CareAccessEvent struct {
	ID            string    `gorm:"primaryKey;size:64"`
	AppointmentID string    `gorm:"size:64;index:care_access_timeline,priority:1"`
	PatientID     string    `gorm:"size:64;index:care_access_patient,priority:1"`
	ResourceType  string    `gorm:"size:24"`
	ResourceID    string    `gorm:"size:64"`
	Action        string    `gorm:"size:24"`
	ActorType     string    `gorm:"size:16"`
	ActorID       string    `gorm:"size:64"`
	CreatedAt     time.Time `gorm:"index:care_access_timeline,priority:2"`
}
