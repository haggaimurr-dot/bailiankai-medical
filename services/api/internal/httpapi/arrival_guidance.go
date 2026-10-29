package httpapi

import (
	"context"
	"errors"
	"time"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 新增-P2 行 98：到诊指引配置。主管按院区配置面诊流程指引文案，
// 到诊登记成功后自动推送给患者（站内消息 kind=arrival_guidance）。

func (a *API) registerArrivalGuidanceRoutes(r *gin.Engine) {
	group := r.Group("/api/v1/admin/arrival-guidance", a.authenticate, a.requireManager)
	group.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	})
	group.GET("", a.getArrivalGuidance)
	group.PUT("", a.saveArrivalGuidance)
}

func arrivalGuidanceDTO(row store.ArrivalGuidanceSetting) apitypes.ArrivalGuidanceSetting {
	enabled := row.Enabled
	updatedBy := row.UpdatedBy
	return apitypes.ArrivalGuidanceSetting{
		ClinicId:  row.ClinicID,
		Content:   row.Content,
		Enabled:   enabled,
		UpdatedAt: row.UpdatedAt,
		UpdatedBy: &updatedBy,
	}
}

func (a *API) getArrivalGuidance(c *gin.Context) {
	clinicID := c.Query("clinicId")
	if !validID(clinicID) {
		fail(c, 400, "INVALID_QUERY", "院区标识无效")
		return
	}
	row, err := a.repo.(interface {
		GetArrivalGuidance(ctx context.Context, clinicID string) (store.ArrivalGuidanceSetting, error)
	}).GetArrivalGuidance(c.Request.Context(), clinicID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(c, 404, "NOT_FOUND", "该院区尚未配置到诊指引")
			return
		}
		a.unavailable(c, err)
		return
	}
	c.JSON(200, gin.H{"data": arrivalGuidanceDTO(row)})
}

func (a *API) saveArrivalGuidance(c *gin.Context) {
	var body struct {
		ClinicID string `json:"clinicId"`
		Content  string `json:"content"`
		Enabled  *bool  `json:"enabled"`
	}
	if !decodeObjectLimit(c, &body, []string{"clinicId", "content"}, []string{"enabled"}, 8192) {
		return
	}
	if !validID(body.ClinicID) {
		fail(c, 400, "INVALID_BODY", "院区标识无效")
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	row, err := a.repo.(interface {
		SaveArrivalGuidance(ctx context.Context, clinicID, content string, enabled bool, now time.Time) (store.ArrivalGuidanceSetting, error)
	}).SaveArrivalGuidance(c.Request.Context(), body.ClinicID, body.Content, enabled, a.now())
	if err != nil {
		if errors.Is(err, store.ErrForbidden) {
			fail(c, 403, "FORBIDDEN", "无权限")
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			fail(c, 404, "NOT_FOUND", "院区不存在或无权限")
			return
		}
		if errors.Is(err, store.ErrScheduleInput) {
			fail(c, 400, "INVALID_BODY", "指引内容须为 1–2000 个字符")
			return
		}
		a.unavailable(c, err)
		return
	}
	c.JSON(200, gin.H{"data": arrivalGuidanceDTO(row)})
}
