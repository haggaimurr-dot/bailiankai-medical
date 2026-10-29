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
	VideoURL      string     `gorm:"size:2048" json:"videoUrl"`
	QuizJSON      *string    `gorm:"type:json" json:"-"`
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

// KnowledgeQuizQuestion is one self-test question attached to a knowledge
// article. The quiz is科普互动 only and never a diagnostic conclusion.
type KnowledgeQuizQuestion struct {
	Question    string   `json:"question"`
	Options     []string `json:"options"`
	AnswerIndex int      `json:"answerIndex"`
	Explanation string   `json:"explanation,omitempty"`
}

// Public snapshots deliberately exclude draft state and employee account IDs.
type KnowledgePublicArticle struct {
	ID           string                  `json:"id"`
	CategoryID   string                  `json:"categoryId"`
	Title        string                  `json:"title"`
	Summary      string                  `json:"summary"`
	Body         string                  `json:"body,omitempty"`
	SourceText   string                  `json:"sourceText,omitempty"`
	VideoURL     string                  `json:"videoUrl,omitempty"`
	Quiz         []KnowledgeQuizQuestion `json:"quiz,omitempty"`
	SortOrder    int                     `json:"sortOrder"`
	ReviewerName string                  `json:"reviewerName"`
	ReviewedAt   time.Time               `json:"reviewedAt"`
	PublishedAt  time.Time               `json:"publishedAt"`
}

// KnowledgeOpen is one fire-and-forget article-open beacon hit. Only opens of
// published snapshots are recorded; drafts never count.
type KnowledgeOpen struct {
	ID        string  `gorm:"primaryKey;size:64"`
	ArticleID string  `gorm:"size:64;index:idx_knowledge_opens_article,priority:1"`
	ClinicID  string  `gorm:"size:64"`
	PatientID *string `gorm:"size:64"`
	OpenedAt  time.Time
}

// TableName pins the beacon table to the goose-managed name; GORM's default
// pluralization (knowledge_opens) would miss migration 00090's table.
func (KnowledgeOpen) TableName() string { return "knowledge_article_opens" }

type KnowledgeOpenArticleStat struct {
	ArticleID      string `json:"articleId"`
	Title          string `json:"title"`
	Opens          int64  `json:"opens"`
	UniquePatients int64  `json:"uniquePatients"`
}

type KnowledgeOpenStats struct {
	Total          int64                      `json:"total"`
	UniquePatients int64                      `json:"uniquePatients"`
	Articles       []KnowledgeOpenArticleStat `json:"articles"`
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
