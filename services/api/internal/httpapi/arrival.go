package httpapi

import (
	"errors"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func arrivalAllowed(row model.Appointment) bool {
	return row.ArrivedAt == nil && (row.Status == "confirmed" || row.Status == "completed")
}

func (a *API) registerArrival(c *gin.Context) {
	if !validQuery(c) || !validID(c.Param("appointmentId")) {
		if !c.IsAborted() {
			fail(c, 400, "INVALID_ID", "预约编号无效")
		}
		return
	}
	var body apitypes.AppointmentArrival
	if !decodeObject(c, &body, []string{"version", "arrivedAt"}, nil) {
		return
	}
	if body.Version < 1 || body.ArrivedAt.IsZero() || body.ArrivedAt.After(a.now()) {
		fail(c, 400, "INVALID_ARRIVAL_TIME", "请填写已经发生的实际到诊时间")
		return
	}
	repo, ok := a.repo.(store.ArrivalRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "到诊登记暂不可用")
		return
	}
	row, err := repo.RegisterArrival(c.Request.Context(), c.Param("appointmentId"), store.ArrivalInput{Version: int(body.Version), ArrivedAt: body.ArrivedAt}, a.now())
	if errors.Is(err, store.ErrArrivalTime) {
		fail(c, 400, "INVALID_ARRIVAL_TIME", "到诊时间不能早于预约创建、晚于当前时间或晚于实际治疗时间")
		return
	}
	if errors.Is(err, store.ErrForbidden) {
		fail(c, 403, "FORBIDDEN", "无权登记到诊")
		return
	}
	if err != nil {
		a.bookingError(c, err, false)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, apitypes.AppointmentResult{Data: a.employeeAppointmentDTO(c, row)})
}
