package model

import "time"

type KnowledgeFavorite struct {
	PatientID string `gorm:"primaryKey;size:64"`
	ArticleID string `gorm:"primaryKey;size:64"`
	CreatedAt time.Time
}

type KnowledgeCategory struct {
	ID        string `gorm:"primaryKey;size:64" json:"id"`
	ClinicID  string `json:"clinicId"`
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
	Active    bool   `json:"active"`
	Version   int    `json:"version"`
}

type KnowledgeArticle struct {
	ID            string     `gorm:"primaryKey;size:64" json:"id"`
	CategoryID    string     `json:"categoryId"`
	Title         string     `json:"title"`
	Summary       string     `json:"summary"`
	Body          string     `gorm:"type:longtext" json:"body"`
	SourceText    string     `gorm:"size:5000" json:"sourceText"`
	SortOrder     int        `json:"sortOrder"`
	Version       int        `json:"version"`
	ReviewStatus  string     `json:"reviewStatus"`
	ReviewNote    string     `json:"reviewNote"`
	ReviewerID    string     `json:"reviewerId"`
	ReviewerName  string     `json:"reviewerName"`
	ReviewedAt    *time.Time `json:"reviewedAt"`
	PublishedJSON *string    `gorm:"type:json" json:"-"`
	PublishedAt   *time.Time `json:"publishedAt"`
}

// Public snapshots deliberately exclude draft state and employee account IDs.
type KnowledgePublicArticle struct {
	ID           string    `json:"id"`
	CategoryID   string    `json:"categoryId"`
	Title        string    `json:"title"`
	Summary      string    `json:"summary"`
	Body         string    `json:"body,omitempty"`
	SourceText   string    `json:"sourceText,omitempty"`
	SortOrder    int       `json:"sortOrder"`
	ReviewerName string    `json:"reviewerName"`
	ReviewedAt   time.Time `json:"reviewedAt"`
	PublishedAt  time.Time `json:"publishedAt"`
}

type KnowledgeEvent struct {
	ID          string    `gorm:"primaryKey;size:64" json:"id"`
	Kind        string    `json:"kind"`
	ResourceID  string    `json:"resourceId"`
	Version     int       `json:"version"`
	Action      string    `json:"action"`
	ActorID     string    `json:"actorId"`
	ContentJSON string    `gorm:"type:json" json:"contentJson"`
	CreatedAt   time.Time `json:"createdAt"`
}
