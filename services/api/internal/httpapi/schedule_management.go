package httpapi

import (
	"encoding/json"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) registerScheduleManagementRoutes(r *gin.Engine) {
	group := r.Group("/api/v1/admin/schedule", a.authenticate, a.requireManager)
	group.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := a.repo.(store.ScheduleManagementRepository); !ok {
			fail(c, 503, "SERVICE_UNAVAILABLE", "排班管理服务暂不可用")
			return
		}
		c.Next()
	})
	group.GET("/settings/:clinicId", func(c *gin.Context) {
		if !validID(c.Param("clinicId")) {
			fail(c, 400, "INVALID_QUERY", "院区标识无效")
			return
		}
		row, err := a.repo.(store.ScheduleManagementRepository).GetScheduleSetting(c.Request.Context(), c.Param("clinicId"))
		if err != nil {
			scheduleFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": row})
	})
	group.PUT("/settings/:clinicId", func(c *gin.Context) {
		var raw struct {
			Templates []json.RawMessage `json:"templates"`
			Holidays  []json.RawMessage `json:"holidays"`
			Version   int               `json:"version"`
		}
		if !validID(c.Param("clinicId")) {
			fail(c, 400, "INVALID_BODY", "院区标识无效")
			return
		}
		if !decodeObjectLimit(c, &raw, []string{"templates", "holidays", "version"}, nil, 262144) {
			return
		}
		in := store.ScheduleSetting{ClinicID: c.Param("clinicId"), Version: raw.Version, Templates: []store.ScheduleTemplate{}, Holidays: []store.HolidayRule{}}
		for _, item := range raw.Templates {
			var t store.ScheduleTemplate
			if !scheduleItem(item, &t, []string{"id", "name", "doctorId", "weekdays", "start", "end", "duration", "capacity", "open"}) {
				fail(c, 400, "INVALID_BODY", "排班模板字段不完整或无效")
				return
			}
			in.Templates = append(in.Templates, t)
		}
		for _, item := range raw.Holidays {
			var h store.HolidayRule
			if !scheduleItem(item, &h, []string{"date", "name", "kind"}) {
				fail(c, 400, "INVALID_BODY", "节假日规则字段不完整或无效")
				return
			}
			in.Holidays = append(in.Holidays, h)
		}
		row, err := a.repo.(store.ScheduleManagementRepository).SaveScheduleSetting(c.Request.Context(), in, a.now())
		if err != nil {
			scheduleFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": row})
	})
	group.GET("/patients", func(c *gin.Context) {
		if !validID(c.Query("clinicId")) {
			fail(c, 400, "INVALID_QUERY", "请选择院区")
			return
		}
		rows, err := a.repo.(store.ScheduleManagementRepository).SchedulePatientTasks(c.Request.Context(), c.Query("clinicId"), a.now())
		if err != nil {
			scheduleFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": rows})
	})
	group.PUT("/patients/:taskId/contact", func(c *gin.Context) {
		var in store.ScheduleContactInput
		if !decodeObject(c, &in, []string{"version", "contactStatus", "note"}, nil) {
			return
		}
		if err := a.repo.(store.ScheduleManagementRepository).UpdateScheduleContact(c.Request.Context(), c.Param("taskId"), in, a.now()); err != nil {
			scheduleFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"saved": true}})
	})
	group.POST("/patients/:taskId/reschedule", func(c *gin.Context) {
		var in store.RescheduleInput
		if !decodeObject(c, &in, []string{"slotId", "version"}, nil) {
			return
		}
		key := c.GetHeader("Idempotency-Key")
		if !requestKeyPattern.MatchString(key) || !validID(in.SlotID) || in.Version < 1 {
			fail(c, 400, "INVALID_BODY", "改期参数无效")
			return
		}
		row, err := a.repo.(store.ScheduleManagementRepository).RescheduleAffectedPatient(c.Request.Context(), c.Param("taskId"), key, in, a.now())
		if err != nil {
			a.bookingError(c, err, false)
			return
		}
		c.JSON(200, gin.H{"data": appointmentDTO(row, a.now(), true)})
	})
}
