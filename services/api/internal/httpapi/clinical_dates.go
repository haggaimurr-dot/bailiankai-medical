package httpapi

import (
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"time"
)

func clinicalDateRange(c *gin.Context) (store.DateRange, bool) {
	var result store.DateRange
	values := c.Request.URL.Query()
	_, hasFrom := values["dateFrom"]
	_, hasTo := values["dateTo"]
	from, to := c.Query("dateFrom"), c.Query("dateTo")
	if !hasFrom && !hasTo {
		return result, true
	}
	start, errStart := time.ParseInLocation("2006-01-02", from, database.Shanghai)
	end, errEnd := time.ParseInLocation("2006-01-02", to, database.Shanghai)
	if !hasFrom || !hasTo || errStart != nil || errEnd != nil || start.Year() < 1 || end.Before(start) || end.Year() >= 9999 {
		fail(c, 400, "INVALID_DATE_RANGE", "请同时填写有效的起止日期，结束日期不能早于开始日期")
		return result, false
	}
	return store.DateRange{From: start, Until: end.AddDate(0, 0, 1)}, true
}
