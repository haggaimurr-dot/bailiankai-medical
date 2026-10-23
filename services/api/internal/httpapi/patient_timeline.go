package httpapi

import (
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"strconv"
)

func (a *API) registerPatientTimelineRoutes(r *gin.Engine) {
	me := r.Group("/api/v1/me/records", a.bookingAvailable, a.authenticatePatient, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := a.repo.(store.PatientTimelineRepository); !ok {
			fail(c, 503, "SERVICE_UNAVAILABLE", "治疗档案暂不可用")
			return
		}
		c.Next()
	})
	me.GET("", a.patientTimeline)
	me.GET("/:kind/:resourceId/read", a.patientAdvice)
	me.POST("/:kind/:resourceId/read", a.confirmAdviceRead)
}
func (a *API) patientTimeline(c *gin.Context) {
	if !validQuery(c, "page", "pageSize") {
		return
	}
	page, size := 1, 20
	for key, target := range map[string]*int{"page": &page, "pageSize": &size} {
		if value, ok := c.GetQuery(key); ok {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 10000 || (key == "pageSize" && n > 100) {
				fail(c, 400, "INVALID_PAGE", "分页参数无效")
				return
			}
			*target = n
		}
	}
	if _, err := a.booking.ExpirePending(c.Request.Context(), a.now()); err != nil {
		a.careError(c, err)
		return
	}
	rows, total, err := a.repo.(store.PatientTimelineRepository).PatientTimeline(c.Request.Context(), currentPatient(c).ID, page, size)
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": rows, "meta": gin.H{"page": page, "pageSize": size, "total": total}})
}
func (a *API) confirmAdviceRead(c *gin.Context) {
	if !validQuery(c) {
		return
	}
	id := c.Param("resourceId")
	if !validID(id) {
		fail(c, 400, "INVALID_RECORD_ID", "档案编号无效")
		return
	}
	var body struct {
		AdviceVersion string `json:"adviceVersion"`
	}
	if !decodeObject(c, &body, []string{"adviceVersion"}, nil) {
		return
	}
	if len(body.AdviceVersion) != 64 {
		fail(c, 400, "INVALID_BODY", "医嘱版本无效")
		return
	}
	receipt, err := a.repo.(store.PatientTimelineRepository).ConfirmAdviceRead(c.Request.Context(), currentPatient(c).ID, c.Param("kind"), id, body.AdviceVersion, a.careNow())
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": receipt})
}

func (a *API) patientAdvice(c *gin.Context) {
	if !validQuery(c) {
		return
	}
	if !validID(c.Param("resourceId")) {
		fail(c, 400, "INVALID_RECORD_ID", "档案编号无效")
		return
	}
	event, err := a.repo.(store.PatientTimelineRepository).PatientAdvice(c.Request.Context(), currentPatient(c).ID, c.Param("kind"), c.Param("resourceId"))
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": event})
}
