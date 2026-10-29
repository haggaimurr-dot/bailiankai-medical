package httpapi

import (
	"net/http"

	"example.com/bailiankai/api/internal/apitypes"
	"github.com/gin-gonic/gin"
)

// 新增-P1：取消原因统计。按取消时间区间聚合已取消预约的原因分布，
// 遵循员工院区权限范围；未填原因归入 unspecified。
func (a *API) cancellationStats(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !validQuery(c, "dateFrom", "dateTo", "clinicId", "doctorId") {
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
	stats, err := a.booking.CancellationStats(c.Request.Context(), dateRange.From, dateRange.Until, c.Query("clinicId"), c.Query("doctorId"))
	if err != nil {
		a.unavailable(c, err)
		return
	}
	byReason := make([]apitypes.CancellationReasonCount, 0, len(stats.ByReason))
	for _, r := range stats.ByReason {
		byReason = append(byReason, apitypes.CancellationReasonCount{Reason: r.Reason, Count: r.Count})
	}
	c.JSON(http.StatusOK, apitypes.CancellationStatsResult{Data: apitypes.CancellationStats{Total: stats.Total, ByReason: byReason}})
}
