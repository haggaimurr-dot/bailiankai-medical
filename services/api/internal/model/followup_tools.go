package model

import "time"

type FollowupTemplate struct {
	ID                  string    `gorm:"primaryKey" json:"id"`
	ClinicID            string    `json:"clinicId"`
	Name                string    `json:"name"`
	StandardStatus      string    `json:"standardStatus"`
	ApplicableTreatment string    `json:"applicableTreatment"`
	NursingInstructions string    `json:"nursingInstructions"`
	FollowupChecklist   string    `json:"followupChecklist"`
	Offsets             []int     `gorm:"serializer:json;type:json" json:"offsets"`
	Response            string    `json:"response"`
	Disposition         string    `json:"disposition"`
	Active              bool      `json:"active"`
	Version             int       `json:"version"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type FollowupTemplateEvent struct {
	ID          string `gorm:"primaryKey"`
	TemplateID  string
	ActorID     string
	ContentJSON string
	CreatedAt   time.Time
}

type NursingFeedback struct {
	ID            string    `gorm:"primaryKey" json:"id"`
	AppointmentID string    `json:"appointmentId"`
	RequestKey    string    `json:"-"`
	ObservedAt    time.Time `json:"observedAt"`
	Content       string    `json:"content"`
	Action        string    `json:"action"`
	ActorID       string    `json:"actorId"`
	ActorName     string    `json:"actorName"`
	CreatedAt     time.Time `json:"createdAt"`
}
