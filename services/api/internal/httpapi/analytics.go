package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/store"
	openapi_types "github.com/oapi-codegen/runtime/types"
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

func (a *API) analyticsFunnel(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if _, err := url.ParseQuery(c.Request.URL.RawQuery); err != nil {
		fail(c, http.StatusBadRequest, "INVALID_QUERY", "查询参数编码无效")
		return
	}
	if !validQuery(c, "dateFrom", "dateTo", "clinicId") {
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
	repo, ok := a.repo.(interface {
		ChannelFunnel(context.Context, store.ChannelFunnelFilter, time.Time) (store.ChannelFunnelReport, error)
	})
	if !ok {
		fail(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "渠道转化漏斗暂不可用")
		return
	}
	report, err := repo.ChannelFunnel(c.Request.Context(), store.ChannelFunnelFilter{DateRange: dateRange, ClinicID: c.Query("clinicId")}, a.now())
	switch {
	case err == nil:
		dateFrom, _ := time.Parse("2006-01-02", report.DateFrom)
		dateTo, _ := time.Parse("2006-01-02", report.DateTo)
		c.JSON(http.StatusOK, gin.H{"data": apitypes.ChannelFunnelReport{
			DateFrom: openapi_types.Date{Time: dateFrom}, DateTo: openapi_types.Date{Time: dateTo}, ClinicId: report.ClinicID, GeneratedAt: report.GeneratedAt,
			Rows:  funnelRows(report.Rows),
			Total: funnelRowDTO(report.Total),
		}})
	case errors.Is(err, store.ErrForbidden):
		fail(c, http.StatusForbidden, "FORBIDDEN", "当前角色无权查看运营分析")
	case errors.Is(err, store.ErrNotFound):
		fail(c, http.StatusNotFound, "NOT_FOUND", "院区不存在或无权访问")
	case errors.Is(err, store.ErrAnalyticsInput):
		fail(c, http.StatusBadRequest, "INVALID_QUERY", "统计筛选参数无效")
	default:
		a.unavailable(c, err)
	}
}

func funnelRowDTO(row store.ChannelFunnelRow) apitypes.ChannelFunnelRow {
	return apitypes.ChannelFunnelRow{
		Source: row.Source, SourceLabel: row.SourceLabel,
		Created: int32(row.Created), Confirmed: int32(row.Confirmed), Arrived: int32(row.Arrived), Treated: int32(row.Treated),
		ConfirmRate: float32Ptr(row.ConfirmRate), ArrivalRate: float32Ptr(row.ArrivalRate), TreatmentRate: float32Ptr(row.TreatmentRate),
	}
}

func float32Ptr(value *float64) *float32 {
	if value == nil {
		return nil
	}
	converted := float32(*value)
	return &converted
}

func funnelRows(rows []store.ChannelFunnelRow) []apitypes.ChannelFunnelRow {
	out := make([]apitypes.ChannelFunnelRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, funnelRowDTO(row))
	}
	return out
}
