package model

import "time"

type Clinic struct {
	DirectoryState
	Presentation *ClinicPresentation `gorm:"serializer:json" json:"presentation,omitempty"`
	ID           string              `gorm:"primaryKey;size:64" json:"id"`
	Name         string              `json:"name"`
	Address      string              `json:"address"`
	Phone        string              `json:"phone"`
}

type Doctor struct {
	DirectoryState
	QualificationReview *DoctorQualificationReview `gorm:"serializer:json" json:"qualificationReview,omitempty"`
	Presentation        *DoctorPresentation        `gorm:"serializer:json" json:"presentation,omitempty"`
	AvatarURL           string                     `json:"avatarUrl"`
	ServiceIntroduction string                     `json:"serviceIntroduction"`
	ID                  string                     `gorm:"primaryKey;size:64" json:"id"`
	ClinicID            string                     `json:"clinicId"`
	Name                string                     `json:"name"`
	Title               string                     `json:"title"`
	Summary             string                     `json:"summary"`
	Focus               []string                   `gorm:"serializer:json" json:"focus"`
	Initials            string                     `json:"initials"`
}

type Slot struct {
	ID       string    `gorm:"primaryKey;size:64" json:"id"`
	ClinicID string    `json:"clinicId"`
	DoctorID string    `json:"doctorId"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
	Capacity int       `json:"capacity"`
	Reserved int       `json:"reserved"`
	Status   string    `json:"status"`
	Version  int       `json:"version"`
}

type ScheduleTemplate struct {
	ID          string    `gorm:"primaryKey;size:64" json:"id"`
	ClinicID    string    `json:"clinicId"`
	Name        string    `json:"name"`
	PatternJSON string    `json:"patternJson"`
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Holiday struct {
	HolidayDate time.Time `gorm:"primaryKey" json:"holidayDate"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"createdAt"`
}
