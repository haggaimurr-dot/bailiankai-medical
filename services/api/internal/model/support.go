package model

import "time"

type SupportTicket struct {
	ID            string     `json:"id" gorm:"primaryKey;size:64"`
	PatientID     string     `json:"patientId"`
	ClinicID      string     `json:"clinicId"`
	Subject       string     `json:"subject"`
	Category      string     `json:"category"`
	Status        string     `json:"status"`
	AssigneeID    string     `json:"assigneeId"`
	DueAt         *time.Time `json:"dueAt"`
	Version       int        `json:"version"`
	Rating        int        `json:"rating"`
	RatingComment string     `json:"ratingComment"`
	RequestKey    string     `json:"-"`
	RequestHash   string     `json:"-"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type SupportEvent struct {
	ID          string              `json:"id" gorm:"primaryKey;size:64"`
	TicketID    string              `json:"ticketId"`
	Kind        string              `json:"kind"`
	ActorType   string              `json:"actorType"`
	ActorID     string              `json:"-"`
	Content     string              `json:"content"`
	Internal    bool                `json:"internal"`
	RequestKey  string              `json:"-"`
	RequestHash string              `json:"-"`
	CreatedAt   time.Time           `json:"createdAt"`
	Attachments []SupportAttachment `json:"attachments" gorm:"-"`
}

type SupportAttachment struct {
	ID       string `json:"id" gorm:"primaryKey;size:64"`
	TicketID string `json:"-"`
	EventID  string `json:"-"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Size     int    `json:"size"`
	Data     []byte `json:"-"`
}
