package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type KnowledgeFilter struct {
	ClinicID, CategoryID, Status, Query, ExcludeID, FavoritePatientID string
	Page, PageSize                                                    int
	Public                                                            bool
	ReviewStatus                                                      string
}
type KnowledgeChange struct {
	ClinicID   string                        `json:"clinicId"`
	CategoryID string                        `json:"categoryId"`
	Name       string                        `json:"name"`
	Title      string                        `json:"title"`
	Summary    string                        `json:"summary"`
	Body       string                        `json:"body"`
	SourceText string                        `json:"sourceText"`
	VideoURL   string                        `json:"videoUrl"`
	Quiz       []model.KnowledgeQuizQuestion `json:"quiz"`
	SortOrder  int                           `json:"sortOrder"`
	Active     bool                          `json:"active"`
	Version    int                           `json:"version"`
	Decision   string                        `json:"decision"`
	Note       string                        `json:"note"`
}

// encodeKnowledgeQuiz marshals the quiz for the quiz_json column; nil means
// "no quiz attached".
func encodeKnowledgeQuiz(quiz []model.KnowledgeQuizQuestion) (*string, error) {
	if quiz == nil {
		return nil, nil
	}
	raw, err := json.Marshal(quiz)
	if err != nil {
		return nil, err
	}
	encoded := string(raw)
	return &encoded, nil
}

func decodeKnowledgeQuiz(raw *string) ([]model.KnowledgeQuizQuestion, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	var quiz []model.KnowledgeQuizQuestion
	if err := json.Unmarshal([]byte(*raw), &quiz); err != nil {
		return nil, err
	}
	return quiz, nil
}

type KnowledgeRepository interface {
	KnowledgeCategories(context.Context, KnowledgeFilter) ([]model.KnowledgeCategory, error)
	KnowledgeArticles(context.Context, KnowledgeFilter) (any, int64, error)
	KnowledgeArticle(context.Context, string, bool) (any, error)
	ChangeKnowledge(context.Context, string, string, string, KnowledgeChange, time.Time) (any, error)
	KnowledgeHistory(context.Context, string, string) ([]model.KnowledgeEvent, error)
}

func (s *MySQL) knowledgeCategoriesQuery(ctx context.Context, tx *gorm.DB, public bool) *gorm.DB {
	q := tx.Model(&model.KnowledgeCategory{})
	if public {
		return q.Where("active = ? AND clinic_id IN (SELECT id FROM clinics WHERE active = ? AND published_json IS NOT NULL)", true, true)
	}
	return clinicScope(ctx, q, "clinic_id")
}
func (s *MySQL) KnowledgeCategories(ctx context.Context, f KnowledgeFilter) ([]model.KnowledgeCategory, error) {
	rows := make([]model.KnowledgeCategory, 0)
	q := s.knowledgeCategoriesQuery(ctx, s.db.WithContext(ctx), f.Public)
	if f.ClinicID != "" {
		q = q.Where("clinic_id = ?", f.ClinicID)
	}
	err := q.Order("sort_order ASC, id ASC").Find(&rows).Error
	return rows, err
}
func (s *MySQL) knowledgeArticlesQuery(ctx context.Context, tx *gorm.DB, f KnowledgeFilter) *gorm.DB {
	cats := s.knowledgeCategoriesQuery(ctx, tx, f.Public).Select("id")
	if f.ClinicID != "" {
		cats = cats.Where("clinic_id = ?", f.ClinicID)
	}
	q := tx.Model(&model.KnowledgeArticle{}).Where("category_id IN (?)", cats)
	if f.CategoryID != "" {
		q = q.Where("category_id = ?", f.CategoryID)
	}
	if f.Public || f.Status == "published" {
		q = q.Where("published_json IS NOT NULL")
	}
	if f.Status == "unpublished" {
		q = q.Where("published_json IS NULL")
	}
	if !f.Public && f.ReviewStatus != "" {
		q = q.Where("review_status = ?", f.ReviewStatus)
	}
	if f.ExcludeID != "" {
		q = q.Where("id <> ?", f.ExcludeID)
	}
	if f.FavoritePatientID != "" {
		q = q.Where("id IN (SELECT article_id FROM knowledge_favorites WHERE patient_id = ?)", f.FavoritePatientID)
	}
	if f.Query != "" {
		// Escape LIKE wildcards; public search must only inspect the published snapshot.
		pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(f.Query) + "%"
		if f.Public {
			q = q.Where("(JSON_UNQUOTE(JSON_EXTRACT(published_json, '$.title')) LIKE ? ESCAPE '!' OR JSON_UNQUOTE(JSON_EXTRACT(published_json, '$.summary')) LIKE ? ESCAPE '!' OR JSON_UNQUOTE(JSON_EXTRACT(published_json, '$.body')) LIKE ? ESCAPE '!')", pattern, pattern, pattern)
		} else {
			q = q.Where("(title LIKE ? ESCAPE '!' OR summary LIKE ? ESCAPE '!' OR body LIKE ? ESCAPE '!')", pattern, pattern, pattern)
		}
	}
	return q
}
func (s *MySQL) KnowledgeArticles(ctx context.Context, f KnowledgeFilter) (any, int64, error) {
	q := s.knowledgeArticlesQuery(ctx, s.db.WithContext(ctx), f)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]model.KnowledgeArticle, 0)
	if err := q.Order("sort_order ASC, id ASC").Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	if !f.Public {
		return rows, total, nil
	}
	result := make([]model.KnowledgePublicArticle, 0, len(rows))
	for _, row := range rows {
		var item model.KnowledgePublicArticle
		if err := json.Unmarshal([]byte(*row.PublishedJSON), &item); err != nil {
			return nil, 0, err
		}
		item.Body = ""
		item.SourceText = ""
		item.Quiz = nil // 自测题仅详情接口返回，列表不透出答案
		item.SortOrder = row.SortOrder
		result = append(result, item)
	}
	return result, total, nil
}
func (s *MySQL) KnowledgeArticle(ctx context.Context, id string, public bool) (any, error) {
	var row model.KnowledgeArticle
	if err := s.knowledgeArticlesQuery(ctx, s.db.WithContext(ctx), KnowledgeFilter{Public: public}).First(&row, "id = ?", id).Error; err != nil {
		return nil, followupDBError(err)
	}
	if !public {
		return row, nil
	}
	var item model.KnowledgePublicArticle
	err := json.Unmarshal([]byte(*row.PublishedJSON), &item)
	item.SortOrder = row.SortOrder
	return item, err
}
func knowledgeEvent(ctx context.Context, tx *gorm.DB, kind, id, action string, version int, row any, now time.Time) error {
	snapshot, err := json.Marshal(row)
	if err != nil {
		return err
	}
	return tx.Create(&model.KnowledgeEvent{ID: randomID("knowledge-event-"), Kind: kind, ResourceID: id, Version: version, Action: action, ActorID: employeeActor(ctx), ContentJSON: string(snapshot), CreatedAt: now}).Error
}
func (s *MySQL) ChangeKnowledge(ctx context.Context, kind, id, action string, in KnowledgeChange, now time.Time) (any, error) {
	if kind != "categories" && kind != "articles" || kind == "categories" && action != "create" && action != "update" {
		return nil, ErrCareState
	}
	if action == "review" && (in.Decision != "approved" && in.Decision != "rejected" || in.Decision == "rejected" && in.Note == "") {
		return nil, ErrCareState
	}
	scope, ok := scopeFrom(ctx)
	if !ok || (scope.Role != "manager" && action != "review") || (action == "review" && scope.Role != "doctor" && scope.Role != "clinician") {
		return nil, ErrForbidden
	}
	var result any
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if kind == "categories" {
			var row model.KnowledgeCategory
			if action == "create" {
				var clinic model.Clinic
				if err := clinicScope(ctx, tx, "id").First(&clinic, "id = ?", in.ClinicID).Error; err != nil {
					return followupDBError(err)
				}
				row = model.KnowledgeCategory{ID: randomID("knowledge-category-"), ClinicID: in.ClinicID, Name: in.Name, SortOrder: in.SortOrder, Active: in.Active, Version: 1}
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			} else {
				if err := s.knowledgeCategoriesQuery(ctx, tx, false).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
					return followupDBError(err)
				}
				if row.Version != in.Version {
					return ErrConflict
				}
				row.Name, row.SortOrder, row.Active = in.Name, in.SortOrder, in.Active
				row.Version++
				if err := tx.Save(&row).Error; err != nil {
					return err
				}
			}
			result = row
			return knowledgeEvent(ctx, tx, kind, row.ID, action, row.Version, row, now)
		}
		// Lock category before article so disabling a category and publication serialize.
		categoryID := in.CategoryID
		if action != "create" {
			var existing model.KnowledgeArticle
			if err := tx.First(&existing, "id = ?", id).Error; err != nil {
				return followupDBError(err)
			}
			categoryID = existing.CategoryID
		}
		var category model.KnowledgeCategory
		if err := s.knowledgeCategoriesQuery(ctx, tx, false).Clauses(clause.Locking{Strength: "UPDATE"}).First(&category, "id = ?", categoryID).Error; err != nil {
			return followupDBError(err)
		}
		var row model.KnowledgeArticle
		if action == "create" {
			quizJSON, err := encodeKnowledgeQuiz(in.Quiz)
			if err != nil {
				return err
			}
			row = model.KnowledgeArticle{ID: randomID("knowledge-article-"), CategoryID: categoryID, Title: in.Title, Summary: in.Summary, Body: in.Body, SourceText: in.SourceText, VideoURL: in.VideoURL, QuizJSON: quizJSON, SortOrder: in.SortOrder, Version: 1, ReviewStatus: "pending"}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
				return followupDBError(err)
			}
			if row.Version != in.Version {
				return ErrConflict
			}
			switch action {
			case "update":
				row.Title, row.Summary, row.Body = in.Title, in.Summary, in.Body
				row.SourceText = in.SourceText
				row.VideoURL = in.VideoURL
				quizJSON, err := encodeKnowledgeQuiz(in.Quiz)
				if err != nil {
					return err
				}
				row.QuizJSON = quizJSON
				// Resubmission resets the status to pending, but keeps the last
				// review's note, reviewer and time: the author revises against
				// the rejection opinion, and the next reviewer still sees what
				// was asked to change. The next review decision overwrites it.
				row.ReviewStatus = "pending"
			case "sort":
				row.SortOrder = in.SortOrder
			case "review":
				var doctor model.FollowupStaff
				if scope.StaffID == "" {
					return ErrForbidden
				}
				if err := tx.First(&doctor, "id = ? AND role = ? AND status = ?", scope.StaffID, "doctor", "on_duty").Error; err != nil {
					return ErrForbidden
				}
				if in.Decision == "approved" && strings.TrimSpace(row.SourceText) == "" {
					return ErrCareState
				}
				row.ReviewStatus, row.ReviewNote, row.ReviewerID, row.ReviewerName, row.ReviewedAt = in.Decision, in.Note, employeeActor(ctx), doctor.DisplayName, &now
			case "publish":
				if row.ReviewStatus != "approved" || row.ReviewedAt == nil || strings.TrimSpace(row.SourceText) == "" || !category.Active {
					return ErrCareState
				}
				var clinic model.Clinic
				if err := tx.First(&clinic, "id = ? AND active = ? AND published_json IS NOT NULL", category.ClinicID, true).Error; err != nil {
					return ErrCareState
				}
				quiz, err := decodeKnowledgeQuiz(row.QuizJSON)
				if err != nil {
					return err
				}
				snapshot, err := json.Marshal(model.KnowledgePublicArticle{ID: row.ID, CategoryID: row.CategoryID, Title: row.Title, Summary: row.Summary, Body: row.Body, SourceText: row.SourceText, VideoURL: row.VideoURL, Quiz: quiz, SortOrder: row.SortOrder, ReviewerName: row.ReviewerName, ReviewedAt: *row.ReviewedAt, PublishedAt: now})
				if err != nil {
					return err
				}
				encoded := string(snapshot)
				row.PublishedJSON, row.PublishedAt = &encoded, &now
			case "unpublish":
				row.PublishedJSON, row.PublishedAt = nil, nil
			default:
				return ErrCareState
			}
			row.Version++
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		}
		result = row
		// Include published snapshot in audit to retain every publication after downlisting.
		return knowledgeEvent(ctx, tx, kind, row.ID, action, row.Version, struct {
			model.KnowledgeArticle
			PublishedSnapshot *string `json:"publishedSnapshot"`
		}{row, row.PublishedJSON}, now)
	})
	return result, err
}
func (s *MySQL) KnowledgeHistory(ctx context.Context, kind, id string) ([]model.KnowledgeEvent, error) {
	if kind == "articles" {
		if _, err := s.KnowledgeArticle(ctx, id, false); err != nil {
			return nil, err
		}
	} else {
		var category model.KnowledgeCategory
		if err := s.knowledgeCategoriesQuery(ctx, s.db.WithContext(ctx), false).First(&category, "id = ?", id).Error; err != nil {
			return nil, followupDBError(err)
		}
	}
	rows := make([]model.KnowledgeEvent, 0)
	err := s.db.WithContext(ctx).Where("kind = ? AND resource_id = ?", kind, id).Order("version DESC").Find(&rows).Error
	return rows, err
}
