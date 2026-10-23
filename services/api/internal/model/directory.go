package model

import "time"

type DirectoryState struct {
	Active        bool       `json:"active"`
	Version       int        `json:"version"`
	PublishedJSON *string    `gorm:"type:json" json:"-"`
	PublishedAt   *time.Time `json:"publishedAt"`
}

// Directory revisions are append-only, including drafts and status changes.
type DirectoryRevision struct {
	ID          string    `gorm:"primaryKey;size:64" json:"id"`
	Kind        string    `json:"kind"`
	ResourceID  string    `json:"resourceId"`
	Version     int       `json:"version"`
	Action      string    `json:"action"`
	ContentJSON string    `gorm:"type:json" json:"contentJson"`
	ActorID     string    `json:"actorId"`
	CreatedAt   time.Time `json:"createdAt"`
}
