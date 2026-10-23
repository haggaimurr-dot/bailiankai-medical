package httpapi

import (
	"encoding/base64"
	"errors"
	"strings"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/skip2/go-qrcode"
)

func (a *API) employeeAppointmentDTO(c *gin.Context, row model.Appointment) apitypes.Appointment {
	dto := appointmentDTO(row, a.now(), true)
	if employee, ok := c.Get("employee"); ok {
		role := employee.(model.EmployeeAccount).Role
		dto.CanReview = dto.CanReview && allowedEmployeeOperation(role, "POST", "/api/v1/admin/appointments/:appointmentId/review")
		allowed := *dto.CanRegisterArrival && allowedEmployeeOperation(role, "POST", "/api/v1/admin/appointments/:appointmentId/arrival")
		dto.CanRegisterArrival = &allowed
		reschedule := row.SlotStatus == "closed" && row.ArrivedAt == nil && !row.TreatmentRecorded && (row.Status == "pending" || row.Status == "confirmed") && allowedEmployeeOperation(role, "POST", "/api/v1/admin/appointments/:appointmentId/suspension-reschedule")
		dto.CanReschedule = &reschedule
	}
	return dto
}

func (a *API) workbenchError(c *gin.Context, err error) {
	if errors.Is(err, store.ErrCheckInCode) {
		fail(c, 409, "CHECKIN_CODE_INVALID", "签到码已失效，请患者刷新二维码后重新扫描")
		return
	}
	if errors.Is(err, store.ErrForbidden) {
		fail(c, 403, "FORBIDDEN", "当前账号无权处理该预约")
		return
	}
	a.bookingError(c, err, false)
}

func (a *API) newCheckInCode(c *gin.Context) {
	if !validQuery(c) || !validAppointmentID(c) {
		return
	}
	var body struct{}
	if !decodeObject(c, &body, nil, nil) {
		return
	}
	repo, ok := a.repo.(store.ArrivalWorkbenchRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "签到服务暂不可用")
		return
	}
	token, expires, err := repo.NewCheckInCode(c.Request.Context(), c.Param("appointmentId"), currentPatient(c).ID, a.now())
	if err != nil {
		a.workbenchError(c, err)
		return
	}
	png, err := qrcode.Encode(token, qrcode.Medium, 280)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"token": token, "expiresAt": expires, "imageDataUrl": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)}})
}

func (a *API) scanCheckInCode(c *gin.Context) {
	if !validQuery(c) {
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if !decodeObject(c, &body, []string{"token"}, nil) {
		return
	}
	if !strings.HasPrefix(body.Token, "BLK-CHECKIN:") || len(body.Token) != 76 {
		fail(c, 400, "INVALID_BODY", "请扫描患者预约签到码")
		return
	}
	repo, ok := a.repo.(store.ArrivalWorkbenchRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "签到服务暂不可用")
		return
	}
	row, err := repo.ResolveCheckInCode(c.Request.Context(), body.Token, a.now())
	if err != nil {
		a.workbenchError(c, err)
		return
	}
	if strings.HasSuffix(c.FullPath(), "/confirm") {
		arrival, ok := a.repo.(store.ArrivalRepository)
		if !ok {
			fail(c, 503, "SERVICE_UNAVAILABLE", "到诊服务暂不可用")
			return
		}
		row, err = arrival.RegisterArrival(c.Request.Context(), row.ID, store.ArrivalInput{Version: row.Version, ArrivedAt: a.now(), CheckInToken: body.Token}, a.now())
		if err != nil {
			a.workbenchError(c, err)
			return
		}
	}
	c.JSON(200, apitypes.AppointmentResult{Data: a.employeeAppointmentDTO(c, row)})
}

func (a *API) suspensionReschedule(c *gin.Context) {
	if !validQuery(c) || !validAppointmentID(c) {
		return
	}
	var body store.AdminRescheduleInput
	if !decodeObject(c, &body, []string{"slotId", "version", "reason", "requestKey"}, nil) {
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if !validID(body.SlotID) || body.SlotID == "" || body.Version < 1 || !requestKeyPattern.MatchString(body.RequestKey) || len([]rune(body.Reason)) < 1 || len([]rune(body.Reason)) > 200 {
		fail(c, 400, "INVALID_BODY", "请选择新时段并填写患者同意改期的沟通记录（1–200字）")
		return
	}
	repo, ok := a.repo.(store.ArrivalWorkbenchRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "改期服务暂不可用")
		return
	}
	row, err := repo.AdminRescheduleAppointment(c.Request.Context(), c.Param("appointmentId"), body, a.now())
	if err != nil {
		a.workbenchError(c, err)
		return
	}
	c.JSON(200, apitypes.AppointmentResult{Data: a.employeeAppointmentDTO(c, row)})
}

func (a *API) viewAppointmentDTO(c *gin.Context, row model.Appointment, admin bool) apitypes.Appointment {
	if admin {
		return a.employeeAppointmentDTO(c, row)
	}
	return appointmentDTO(row, a.now(), false)
}
