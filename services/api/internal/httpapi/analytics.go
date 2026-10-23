package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) analytics(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if _, err := url.ParseQuery(c.Request.URL.RawQuery); err != nil {
		fail(c, http.StatusBadRequest, "INVALID_QUERY", "查询参数编码无效")
		return
	}
	if !validQuery(c, "dateFrom", "dateTo", "clinicId", "doctorId", "groupBy") {
		return
	}
	dateRange, ok := clinicalDateRange(c)
	if !ok {
		return
	}
	if !dateRange.Active() || dateRange.Until.After(dateRange.From.AddDate(0, 0, 366)) {
		fail(c, http.StatusBadRequest, "INVALID_DATE_RANGE", "请填写起止日期，范围最多 366 天")
		return
	}
	groupBy := c.DefaultQuery("groupBy", "doctor")
	if groupBy != "doctor" && groupBy != "clinic" && groupBy != "day" {
		fail(c, http.StatusBadRequest, "INVALID_QUERY", "统计维度必须为 doctor、clinic 或 day")
		return
	}
	repo, ok := a.repo.(interface {
		Analytics(context.Context, store.AnalyticsFilter, time.Time) (store.AnalyticsReport, error)
	})
	if !ok {
		fail(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "医生统计与运营分析暂不可用")
		return
	}
	report, err := repo.Analytics(c.Request.Context(), store.AnalyticsFilter{DateRange: dateRange, ClinicID: c.Query("clinicId"), DoctorID: c.Query("doctorId"), GroupBy: groupBy}, a.now())
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"data": report})
	case errors.Is(err, store.ErrForbidden):
		fail(c, http.StatusForbidden, "FORBIDDEN", "当前角色无权查看运营分析")
	case errors.Is(err, store.ErrNotFound):
		fail(c, http.StatusNotFound, "NOT_FOUND", "院区或医生不存在或无权访问")
	case errors.Is(err, store.ErrAnalyticsInput):
		fail(c, http.StatusBadRequest, "INVALID_QUERY", "统计筛选参数无效")
	default:
		a.unavailable(c, err)
	}
}
