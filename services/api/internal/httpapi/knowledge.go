package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/bailiankai/api/internal/model"
	"strconv"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func knowledgePermission(role, method, path string) (bool, bool) {
	if !strings.HasPrefix(path, "/api/v1/admin/knowledge/") {
		return false, false
	}
	category := "/api/v1/admin/knowledge/categories"
	article := "/api/v1/admin/knowledge/articles"
	read := method == "GET" && (path == category || path == article || path == article+"/:resourceId" || path == category+"/:resourceId/history" || path == article+"/:resourceId/history" || path == "/api/v1/admin/knowledge/stats")
	review := method == "POST" && path == article+"/:resourceId/review"
	if role == "doctor" || role == "clinician" {
		return read || review, true
	}
	if role != "manager" {
		return false, true
	}
	write := method == "POST" && (path == category || path == article || path == article+"/:resourceId/publish" || path == article+"/:resourceId/unpublish" || path == article+"/:resourceId/sort") || method == "PUT" && (path == category+"/:resourceId" || path == article+"/:resourceId")
	return read || write, true
}
func (a *API) registerKnowledgeRoutes(r *gin.Engine) {
	available := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := a.repo.(store.KnowledgeRepository); !ok {
			fail(c, 503, "SERVICE_UNAVAILABLE", "知识库暂不可用")
			return
		}
		c.Next()
	}
	public := r.Group("/api/v1/knowledge", available)
	public.GET("/categories", a.knowledgeCategories(true))
	public.GET("/articles", a.knowledgeArticles(true))
	public.GET("/articles/:resourceId", a.knowledgeArticle(true))
	public.GET("/articles/:resourceId/related", a.knowledgeRelated)
	public.POST("/articles/:resourceId/open", a.knowledgeOpen)
	patient := r.Group("/api/v1/me/knowledge", available, a.bookingAvailable, a.authenticatePatient)
	patient.GET("/favorites", a.knowledgeArticles(true))
	patient.GET("/favorites/:resourceId", a.knowledgeFavorite)
	patient.PUT("/favorites/:resourceId", a.knowledgeFavorite)
	patient.DELETE("/favorites/:resourceId", a.knowledgeFavorite)
	admin := r.Group("/api/v1/admin/knowledge", a.authenticate, available)
	admin.GET("/categories", a.knowledgeCategories(false))
	admin.GET("/articles", a.knowledgeArticles(false))
	admin.GET("/articles/:resourceId", a.knowledgeArticle(false))
	admin.GET("/stats", a.knowledgeStats)
	for _, kind := range []string{"categories", "articles"} {
		admin.POST("/"+kind, a.knowledgeChange(kind, "create"))
		admin.PUT("/"+kind+"/:resourceId", a.knowledgeChange(kind, "update"))
		admin.GET("/"+kind+"/:resourceId/history", a.knowledgeHistory(kind))
	}
	for _, action := range []string{"review", "publish", "unpublish", "sort"} {
		admin.POST("/articles/:resourceId/"+action, a.knowledgeChange("articles", action))
	}
}
func knowledgeFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrForbidden):
		fail(c, 403, "FORBIDDEN", "当前员工无权执行此操作；审核须为在岗医生")
	case errors.Is(err, store.ErrNotFound):
		fail(c, 404, "NOT_FOUND", "内容不存在或无权访问")
	case errors.Is(err, store.ErrConflict):
		fail(c, 409, "VERSION_CONFLICT", "内容已变化，请刷新后重试")
	case errors.Is(err, store.ErrCareState):
		fail(c, 409, "KNOWLEDGE_STATE", "审核通过须补全参考来源；发布须通过医生审核，且分类及院区启用并已公示")
	default:
		fail(c, 503, "SERVICE_UNAVAILABLE", "知识库暂不可用")
	}
}
func (a *API) knowledgeCategories(public bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !validQuery(c, "clinicId") {
			return
		}
		rows, err := a.repo.(store.KnowledgeRepository).KnowledgeCategories(c.Request.Context(), store.KnowledgeFilter{Public: public, ClinicID: c.Query("clinicId")})
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": rows})
	}
}
func (a *API) knowledgeArticles(public bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		keys := []string{"clinicId", "categoryId", "q", "page", "pageSize"}
		if !public {
			keys = append(keys, "status", "reviewStatus")
		}
		if !validQuery(c, keys...) {
			return
		}
		f := store.KnowledgeFilter{Public: public, ClinicID: c.Query("clinicId"), CategoryID: c.Query("categoryId"), Status: c.Query("status"), Page: 1, PageSize: 20}
		f.Query = strings.TrimSpace(c.Query("q"))
		f.ReviewStatus = c.Query("reviewStatus")
		if f.ReviewStatus != "" && f.ReviewStatus != "pending" && f.ReviewStatus != "approved" && f.ReviewStatus != "rejected" {
			fail(c, 400, "INVALID_QUERY", "审核状态必须为 pending、approved 或 rejected")
			return
		}
		if !textLength(f.Query, 0, 100) {
			fail(c, 400, "INVALID_QUERY", "搜索词最多100字")
			return
		}
		if strings.HasPrefix(c.FullPath(), "/api/v1/me/") {
			f.FavoritePatientID = currentPatient(c).ID
		}
		if f.Status != "" && f.Status != "published" && f.Status != "unpublished" {
			fail(c, 400, "INVALID_QUERY", "状态必须为 published 或 unpublished")
			return
		}
		for key, target := range map[string]*int{"page": &f.Page, "pageSize": &f.PageSize} {
			if value := c.Query(key); value != "" {
				n, err := strconv.Atoi(value)
				if err != nil || n < 1 || n > 1000000 || (key == "pageSize" && n > 100) {
					fail(c, 400, "INVALID_QUERY", "分页参数无效，pageSize 最大为 100")
					return
				}
				*target = n
			}
		}
		rows, total, err := a.repo.(store.KnowledgeRepository).KnowledgeArticles(c.Request.Context(), f)
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": rows, "meta": gin.H{"page": f.Page, "pageSize": f.PageSize, "total": total}})
	}
}

func (a *API) knowledgeRelated(c *gin.Context) {
	if !directoryID(c) || !validQuery(c) {
		return
	}
	repo := a.repo.(store.KnowledgeRepository)
	value, err := repo.KnowledgeArticle(c.Request.Context(), c.Param("resourceId"), true)
	if err != nil {
		knowledgeFailure(c, err)
		return
	}
	article := value.(model.KnowledgePublicArticle)
	rows, _, err := repo.KnowledgeArticles(c.Request.Context(), store.KnowledgeFilter{Public: true, CategoryID: article.CategoryID, ExcludeID: article.ID, Page: 1, PageSize: 3})
	if err != nil {
		knowledgeFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": rows})
}

func (a *API) knowledgeFavorite(c *gin.Context) {
	if !directoryID(c) || !validQuery(c) {
		return
	}
	repo, ok := a.repo.(store.KnowledgeFavoriteRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "收藏暂不可用")
		return
	}
	if c.Request.Method != "GET" && c.Request.ContentLength != 0 {
		fail(c, 400, "INVALID_BODY", "收藏操作不接受请求正文")
		return
	}
	id, patient := c.Param("resourceId"), currentPatient(c).ID
	var saved bool
	var err error
	if c.Request.Method == "GET" {
		saved, err = repo.KnowledgeFavorite(c.Request.Context(), patient, id)
	} else {
		saved = c.Request.Method == "PUT"
		err = repo.SetKnowledgeFavorite(c.Request.Context(), patient, id, saved, a.now())
	}
	if err != nil {
		knowledgeFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": saved})
}
func (a *API) knowledgeArticle(public bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !directoryID(c) || !validQuery(c) {
			return
		}
		row, err := a.repo.(store.KnowledgeRepository).KnowledgeArticle(c.Request.Context(), c.Param("resourceId"), public)
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": row})
	}
}
func (a *API) knowledgeHistory(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !directoryID(c) || !validQuery(c) {
			return
		}
		rows, err := a.repo.(store.KnowledgeRepository).KnowledgeHistory(c.Request.Context(), kind, c.Param("resourceId"))
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": rows})
	}
}
func (a *API) knowledgeChange(kind, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !validQuery(c) || (action != "create" && !directoryID(c)) {
			return
		}
		required := []string{}
		if action != "create" {
			required = append(required, "version")
		}
		switch action {
		case "create", "update":
			if kind == "categories" {
				required = append(required, "name", "sortOrder", "active")
				if action == "create" {
					required = append(required, "clinicId")
				}
			} else {
				required = append(required, "title", "summary", "body")
				if action == "create" {
					required = append(required, "categoryId", "sortOrder")
				}
			}
		case "review":
			required = append(required, "decision", "note")
		case "sort":
			required = append(required, "sortOrder")
		}
		var in store.KnowledgeChange
		optional := []string{}
		if kind == "articles" && (action == "create" || action == "update") {
			optional = append(optional, "sourceText", "videoUrl", "quiz")
		}
		if !decodeObjectLimit(c, &in, required, optional, 262144) {
			return
		}
		in.Name, in.Title, in.Summary, in.Body, in.Note = strings.TrimSpace(in.Name), strings.TrimSpace(in.Title), strings.TrimSpace(in.Summary), strings.TrimSpace(in.Body), strings.TrimSpace(in.Note)
		in.SourceText = strings.TrimSpace(in.SourceText)
		in.VideoURL = strings.TrimSpace(in.VideoURL)
		for i := range in.Quiz {
			in.Quiz[i].Question = strings.TrimSpace(in.Quiz[i].Question)
			in.Quiz[i].Explanation = strings.TrimSpace(in.Quiz[i].Explanation)
			for j := range in.Quiz[i].Options {
				in.Quiz[i].Options[j] = strings.TrimSpace(in.Quiz[i].Options[j])
			}
		}
		valid := (action == "create" || in.Version > 0) && in.SortOrder >= 0 && in.SortOrder <= 1000000
		if action == "create" || action == "update" {
			if kind == "categories" {
				valid = valid && textLength(in.Name, 1, 120)
				if action == "create" {
					valid = valid && in.ClinicID != "" && validID(in.ClinicID)
				}
			} else {
				valid = valid && textLength(in.Title, 1, 200) && textLength(in.Summary, 0, 1000) && textLength(in.Body, 1, 50000) && textLength(in.SourceText, 0, 5000) && validAvatar(in.VideoURL) && validKnowledgeQuiz(in.Quiz)
				if action == "create" {
					valid = valid && in.CategoryID != "" && validID(in.CategoryID)
				}
			}
		}
		if action == "review" {
			valid = valid && (in.Decision == "approved" || in.Decision == "rejected") && textLength(in.Note, 0, 1000) && (in.Decision != "rejected" || in.Note != "")
		}
		if !valid {
			fail(c, 400, "INVALID_BODY", "内容字段、排序、审核决定或版本无效；拒绝须填写原因")
			return
		}
		row, err := a.repo.(store.KnowledgeRepository).ChangeKnowledge(c.Request.Context(), kind, c.Param("resourceId"), action, in, a.now())
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		status := 200
		if action == "create" {
			status = 201
		}
		c.JSON(status, gin.H{"data": row})
	}
}

// validKnowledgeQuiz bounds the self-test quiz: at most 20 questions, each
// with 2-6 non-empty options, a valid answer index and a short explanation.
// The quiz is科普互动 only and never a diagnostic conclusion.
func validKnowledgeQuiz(quiz []model.KnowledgeQuizQuestion) bool {
	if len(quiz) > 20 {
		return false
	}
	for _, q := range quiz {
		if !textLength(q.Question, 1, 200) || len(q.Options) < 2 || len(q.Options) > 6 {
			return false
		}
		for _, opt := range q.Options {
			if !textLength(opt, 1, 100) {
				return false
			}
		}
		if q.AnswerIndex < 0 || q.AnswerIndex >= len(q.Options) {
			return false
		}
		if !textLength(q.Explanation, 0, 500) {
			return false
		}
	}
	return true
}

// knowledgeOpen records the fire-and-forget open beacon the patient article
// page sends on load. It never fails the reader: unknown or unpublished
// articles simply record nothing.
func (a *API) knowledgeOpen(c *gin.Context) {
	if !directoryID(c) {
		return
	}
	var body struct {
		ClinicID string `json:"clinicId"`
	}
	if c.Request.ContentLength != 0 {
		_ = json.NewDecoder(c.Request.Body).Decode(&body)
	}
	body.ClinicID = strings.TrimSpace(body.ClinicID)
	if !textLength(body.ClinicID, 0, 64) {
		body.ClinicID = ""
	}
	patientID := ""
	if a.booking != nil {
		if header := c.GetHeader("Authorization"); strings.HasPrefix(header, "Bearer ") {
			if patient, err := a.booking.AuthenticatePatient(c.Request.Context(), strings.TrimPrefix(header, "Bearer "), a.now()); err == nil {
				patientID = patient.ID
			}
		}
	}
	if repo, ok := a.repo.(knowledgeOpenRecorder); ok {
		// Best effort: the beacon must never break reading.
		_ = repo.RecordKnowledgeOpen(c.Request.Context(), c.Param("resourceId"), body.ClinicID, patientID, a.now())
	}
	c.Status(204)
}

// knowledgeOpenRecorder is the subset of the store the open beacon needs.
type knowledgeOpenRecorder interface {
	RecordKnowledgeOpen(ctx context.Context, articleID, clinicID, patientID string, now time.Time) error
}

// knowledgeStats aggregates article-open counts for the admin knowledge view.
// Only published snapshots count; drafts never enter the stats.
func (a *API) knowledgeStats(c *gin.Context) {
	if !validQuery(c, "clinicId", "days") {
		return
	}
	clinicID := strings.TrimSpace(c.Query("clinicId"))
	if clinicID != "" && !validID(clinicID) {
		fail(c, 400, "INVALID_QUERY", "院区参数无效")
		return
	}
	days := 30
	if v := c.Query("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 365 {
			fail(c, 400, "INVALID_QUERY", "days 须为 1-365")
			return
		}
		days = n
	}
	repo, ok := a.repo.(knowledgeOpenStatsReader)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "知识库暂不可用")
		return
	}
	stats, err := repo.KnowledgeOpenStats(c.Request.Context(), clinicID, a.now().AddDate(0, 0, -days))
	if err != nil {
		knowledgeFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": stats})
}

// knowledgeOpenStatsReader is the subset of the store the stats endpoint needs.
type knowledgeOpenStatsReader interface {
	KnowledgeOpenStats(ctx context.Context, clinicID string, since time.Time) (model.KnowledgeOpenStats, error)
}
