package httpapi

import (
	"context"
	"errors"
	"time"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

// 新增-P2 行 102：模型效果抽检与人工评分回流。主管抽检已复核发布的评估并打分，
// 报表按模型版本汇总，可下钻到单条。生产网关未配密钥前只在测试环境抽检。

type qualityRepository interface {
	SampleQualityAssessments(ctx context.Context, clinicID string, n int, now time.Time) ([]store.QualitySampleItem, error)
	SaveQualityScore(ctx context.Context, assessmentID string, score int, comment, modelVersion string, now time.Time) (store.AssessmentQualityScore, error)
	QualityReport(ctx context.Context, modelVersion string) ([]store.QualityReportRow, []store.QualityReportItem, error)
}

func (a *API) registerAssessmentQualityRoutes(r *gin.Engine) {
	group := r.Group("/api/v1/admin/assessments", a.authenticate, a.requireManager)
	group.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	})
	group.POST("/quality-sample", a.sampleQualityAssessments)
	group.POST("/:assessmentId/quality-score", a.saveQualityScore)
	group.GET("/quality-report", a.qualityReport)
}

func (a *API) sampleQualityAssessments(c *gin.Context) {
	var body struct {
		ClinicID string `json:"clinicId"`
		Count    int    `json:"count"`
	}
	if !decodeObjectLimit(c, &body, []string{"count"}, []string{"clinicId"}, 4096) {
		return
	}
	if body.ClinicID != "" && !validID(body.ClinicID) {
		fail(c, 400, "INVALID_BODY", "院区标识无效")
		return
	}
	repo, ok := a.repo.(qualityRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "评估服务暂不可用")
		return
	}
	items, err := repo.SampleQualityAssessments(c.Request.Context(), body.ClinicID, body.Count, a.now())
	if err != nil {
		if errors.Is(err, store.ErrForbidden) {
			fail(c, 403, "FORBIDDEN", "无权限")
			return
		}
		if errors.Is(err, store.ErrScheduleInput) {
			fail(c, 400, "INVALID_BODY", "抽检数量须为 1–50")
			return
		}
		a.unavailable(c, err)
		return
	}
	data := make([]apitypes.QualitySampleItem, 0, len(items))
	for _, it := range items {
		mv := it.ModelVersion
		data = append(data, apitypes.QualitySampleItem{
			AssessmentId: it.AssessmentID, PatientId: it.PatientID, ClinicId: it.ClinicID,
			ReviewedAt: it.ReviewedAt, ModelVersion: &mv, HasScore: it.HasScore,
		})
	}
	c.JSON(200, gin.H{"data": data})
}

func (a *API) saveQualityScore(c *gin.Context) {
	id := c.Param("assessmentId")
	if !validID(id) {
		fail(c, 400, "INVALID_BODY", "评估标识无效")
		return
	}
	var body struct {
		Score        int    `json:"score"`
		Comment      string `json:"comment"`
		ModelVersion string `json:"modelVersion"`
	}
	if !decodeObjectLimit(c, &body, []string{"score"}, []string{"comment", "modelVersion"}, 4096) {
		return
	}
	repo, ok := a.repo.(qualityRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "评估服务暂不可用")
		return
	}
	row, err := repo.SaveQualityScore(c.Request.Context(), id, body.Score, body.Comment, body.ModelVersion, a.now())
	if err != nil {
		if errors.Is(err, store.ErrForbidden) {
			fail(c, 403, "FORBIDDEN", "无权限")
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			fail(c, 404, "NOT_FOUND", "评估不存在或未发布")
			return
		}
		if errors.Is(err, store.ErrScheduleInput) {
			fail(c, 400, "INVALID_BODY", "评分须为 1–5 分")
			return
		}
		a.unavailable(c, err)
		return
	}
	comment := row.Comment
	mv := row.ModelVersion
	c.JSON(200, gin.H{"data": apitypes.QualityScore{
		Id: row.ID, AssessmentId: row.AssessmentID, Score: row.Score,
		Comment: &comment, ModelVersion: &mv, CreatedAt: row.CreatedAt,
	}})
}

func (a *API) qualityReport(c *gin.Context) {
	repo, ok := a.repo.(qualityRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "评估服务暂不可用")
		return
	}
	summary, items, err := repo.QualityReport(c.Request.Context(), c.Query("modelVersion"))
	if err != nil {
		if errors.Is(err, store.ErrForbidden) {
			fail(c, 403, "FORBIDDEN", "无权限")
			return
		}
		a.unavailable(c, err)
		return
	}
	summaryDTO := make([]gin.H, 0, len(summary))
	for _, r := range summary {
		summaryDTO = append(summaryDTO, gin.H{
			"modelVersion": r.ModelVersion, "count": r.Count, "avgScore": r.AvgScore,
			"minScore": r.MinScore, "maxScore": r.MaxScore,
		})
	}
	itemsDTO := make([]gin.H, 0, len(items))
	for _, it := range items {
		itemsDTO = append(itemsDTO, gin.H{
			"scoreId": it.ScoreID, "assessmentId": it.AssessmentID, "score": it.Score,
			"comment": it.Comment, "scorer": it.Scorer, "createdAt": it.CreatedAt,
		})
	}
	c.JSON(200, gin.H{"data": gin.H{"summary": summaryDTO, "items": itemsDTO}})
}
