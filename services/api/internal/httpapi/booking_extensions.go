package httpapi

import (
	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) rescheduleAppointment(c *gin.Context) {
	if !validAppointmentID(c) {
		return
	}
	repo, ok := a.booking.(store.BookingExtensionsRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "改期服务暂不可用")
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if !requestKeyPattern.MatchString(key) {
		fail(c, 400, "INVALID_IDEMPOTENCY_KEY", "需提供有效的申请编号")
		return
	}
	var body apitypes.RescheduleMyAppointmentJSONRequestBody
	if !decodeObject(c, &body, []string{"slotId", "version"}, nil) {
		return
	}
	if !validID(body.SlotId) || body.Version < 1 {
		fail(c, 400, "INVALID_BODY", "需提供有效时段和版本号")
		return
	}
	row, err := repo.RescheduleAppointment(c.Request.Context(), c.Param("appointmentId"), currentPatient(c).ID, key, store.RescheduleInput{SlotID: body.SlotId, Version: int(body.Version)}, a.now())
	if err != nil {
		a.bookingError(c, err, false)
		return
	}
	c.JSON(200, apitypes.AppointmentResult{Data: appointmentDTO(row, a.now(), false)})
}

func (a *API) appointmentTimeline(c *gin.Context) {
	if !validAppointmentID(c) {
		return
	}
	repo, ok := a.booking.(store.BookingExtensionsRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "预约历史暂不可用")
		return
	}
	events, err := repo.AppointmentTimeline(c.Request.Context(), c.Param("appointmentId"), currentPatient(c).ID, a.now())
	if err != nil {
		a.bookingError(c, err, false)
		return
	}
	data := make([]apitypes.AppointmentTimelineEvent, 0, len(events))
	for _, e := range events {
		data = append(data, apitypes.AppointmentTimelineEvent{Id: e.ID, Action: e.Action, FromStatus: e.FromStatus, ToStatus: apitypes.AppointmentStatus(e.ToStatus), ActorType: apitypes.AppointmentTimelineEventActorType(e.ActorType), Detail: e.Detail, CreatedAt: e.CreatedAt.In(database.Shanghai)})
	}
	c.JSON(200, apitypes.AppointmentTimelineResult{Data: data})
}
