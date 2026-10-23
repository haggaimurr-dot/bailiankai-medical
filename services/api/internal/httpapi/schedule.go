package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) registerScheduleRoutes(r *gin.Engine) {
	group := r.Group("/api/v1/admin/slots", a.authenticate, a.requireManager)
	group.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := a.repo.(store.ScheduleRepository); !ok {
			fail(c, 503, "SERVICE_UNAVAILABLE", "排班服务暂不可用")
			return
		}
		c.Next()
	})
	group.POST("", a.createSlots)
	group.POST("/suspend", a.suspendSlots)
	a.registerScheduleManagementRoutes(r)
}
func scheduleFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrScheduleInput):
		fail(c, 400, "INVALID_BODY", "排班参数无效")
	case errors.Is(err, store.ErrNotFound):
		fail(c, 404, "SCHEDULE_NOT_FOUND", "院区、医生或排班不存在或无权访问")
	case errors.Is(err, store.ErrSlotOverlap):
		fail(c, 409, "SLOT_OVERLAP", "时段重复或重叠，整批未创建，请调整后重试")
	case errors.Is(err, store.ErrPast):
		fail(c, 409, "SLOT_ALREADY_STARTED", "已开始的时段不能创建或停诊，整批未保存")
	case errors.Is(err, store.ErrConflict):
		fail(c, 409, "VERSION_CONFLICT", "排班已变化或已关闭，整批未保存，请刷新后重试")
	default:
		fail(c, 503, "SERVICE_UNAVAILABLE", "排班服务暂不可用")
	}
}
func scheduleResult(c *gin.Context, code int, rows []model.Slot) {
	data := make([]apitypes.Slot, 0, len(rows))
	for _, row := range rows {
		data = append(data, slotDTO(row))
	}
	c.JSON(code, apitypes.SlotList{Data: data})
}
func (a *API) createSlots(c *gin.Context) {
	var raw struct {
		Slots []json.RawMessage `json:"slots"`
	}
	if !decodeObjectLimit(c, &raw, []string{"slots"}, nil, 262144) {
		return
	}
	var body apitypes.SlotBatchCreate
	for _, item := range raw.Slots {
		var slot apitypes.SlotCreate
		if !scheduleItem(item, &slot, []string{"clinicId", "doctorId", "startsAt", "endsAt", "capacity", "status"}) {
			fail(c, 400, "INVALID_BODY", "每个时段须提供完整、有效的院区、医生、时间、容量和状态")
			return
		}
		body.Slots = append(body.Slots, slot)
	}
	rows := make([]model.Slot, 0, len(body.Slots))
	for _, input := range body.Slots {
		if !validID(input.ClinicId) || !validID(input.DoctorId) || input.StartsAt.Nanosecond()%int(time.Millisecond) != 0 || input.EndsAt.Nanosecond()%int(time.Millisecond) != 0 {
			fail(c, 400, "INVALID_BODY", "院区、医生或时间格式无效")
			return
		}
		rows = append(rows, model.Slot{ClinicID: input.ClinicId, DoctorID: input.DoctorId, StartsAt: input.StartsAt, EndsAt: input.EndsAt, Capacity: int(input.Capacity), Status: string(input.Status)})
	}
	saved, err := a.repo.(store.ScheduleRepository).CreateSlots(c.Request.Context(), rows, a.now())
	if err != nil {
		scheduleFailure(c, err)
		return
	}
	scheduleResult(c, 201, saved)
}
func (a *API) suspendSlots(c *gin.Context) {
	var raw struct {
		Slots  []json.RawMessage `json:"slots"`
		Reason string            `json:"reason"`
	}
	if !decodeObjectLimit(c, &raw, []string{"slots", "reason"}, nil, 65536) {
		return
	}
	var body apitypes.SlotSuspension
	body.Reason = raw.Reason
	for _, item := range raw.Slots {
		var ref apitypes.SlotReference
		if !scheduleItem(item, &ref, []string{"id", "version"}) {
			fail(c, 400, "INVALID_BODY", "每个停诊时段须提供标识和版本")
			return
		}
		body.Slots = append(body.Slots, ref)
	}
	refs := make([]store.SlotReference, 0, len(body.Slots))
	for _, ref := range body.Slots {
		if !validID(ref.Id) {
			fail(c, 400, "INVALID_BODY", "排班标识格式无效")
			return
		}
		refs = append(refs, store.SlotReference{ID: ref.Id, Version: int(ref.Version)})
	}
	saved, err := a.repo.(store.ScheduleRepository).SuspendSlots(c.Request.Context(), refs, body.Reason, a.now())
	if err != nil {
		scheduleFailure(c, err)
		return
	}
	scheduleResult(c, 200, saved)
}

func scheduleItem(raw json.RawMessage, target any, required []string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != len(required) {
		return false
	}
	for _, key := range required {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	return json.Unmarshal(raw, target) == nil
}
