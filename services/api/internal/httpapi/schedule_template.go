package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/runtime/types"
)

// 排班模板与节假日：主管一键套用模板生成排班，节假日当天自动停诊。
// 与 slots 批量生成同为 requireManager，主管在白名单中全通，无需额外登记。

func (a *API) registerScheduleTemplateRoutes(r *gin.Engine) {
	group := r.Group("/api/v1/admin/schedule-templates", a.authenticate, a.requireManager)
	group.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := a.repo.(store.ScheduleTemplateRepository); !ok {
			fail(c, 503, "SERVICE_UNAVAILABLE", "排班服务暂不可用")
			return
		}
		c.Next()
	})
	group.GET("", a.listScheduleTemplates)
	group.POST("", a.createScheduleTemplate)
	group.GET("/:templateId", a.getScheduleTemplate)
	group.PUT("/:templateId", a.updateScheduleTemplate)
	group.DELETE("/:templateId", a.deleteScheduleTemplate)
	group.POST("/:templateId/apply-preview", a.previewScheduleTemplateApply)
	group.POST("/:templateId/apply", a.applyScheduleTemplate)

	holidays := r.Group("/api/v1/admin/holidays", a.authenticate, a.requireManager)
	holidays.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	})
	holidays.GET("", a.listHolidays)
	holidays.POST("", a.createHoliday)
	holidays.DELETE("/:date", a.deleteHoliday)
}

func templateRecordDTO(row model.ScheduleTemplate) (apitypes.ScheduleTemplateRecord, error) {
	pattern, err := store.ParseTemplatePattern(row.PatternJSON)
	if err != nil {
		return apitypes.ScheduleTemplateRecord{}, err
	}
	entries := make([]apitypes.ScheduleTemplateEntry, 0, len(pattern.Entries))
	for _, e := range pattern.Entries {
		duration := int32(e.DurationMinutes)
		entries = append(entries, apitypes.ScheduleTemplateEntry{
			DoctorId:        e.DoctorID,
			Weekday:         int32(e.Weekday),
			StartTimes:      append([]string{}, e.StartTimes...),
			DurationMinutes: &duration,
			Capacity:        int32(e.Capacity),
		})
	}
	return apitypes.ScheduleTemplateRecord{
		Id:        row.ID,
		ClinicId:  row.ClinicID,
		Name:      row.Name,
		Pattern:   apitypes.ScheduleTemplatePattern{Entries: entries},
		CreatedBy: row.CreatedBy,
		CreatedAt: row.CreatedAt,
	}, nil
}

func templateInput(c *gin.Context) (string, string, bool) {
	var body struct {
		Name    string          `json:"name"`
		Pattern json.RawMessage `json:"pattern"`
	}
	if !decodeObjectLimit(c, &body, []string{"name", "pattern"}, nil, 65536) {
		return "", "", false
	}
	if len(body.Name) == 0 || len(body.Name) > 100 {
		fail(c, 400, "INVALID_BODY", "模板名称须为 1–100 个字符")
		return "", "", false
	}
	pattern, err := store.ParseTemplatePattern(string(body.Pattern))
	if err != nil {
		fail(c, 400, "INVALID_BODY", "模板每周排班模式无效")
		return "", "", false
	}
	canonical, err := json.Marshal(pattern)
	if err != nil {
		fail(c, 400, "INVALID_BODY", "模板每周排班模式无效")
		return "", "", false
	}
	return body.Name, string(canonical), true
}

func (a *API) listScheduleTemplates(c *gin.Context) {
	clinicID := c.Query("clinicId")
	if !validID(clinicID) {
		fail(c, 400, "INVALID_QUERY", "院区标识无效")
		return
	}
	rows, err := a.repo.(store.ScheduleTemplateRepository).ListScheduleTemplates(c.Request.Context(), clinicID)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	data := make([]apitypes.ScheduleTemplateRecord, 0, len(rows))
	for _, row := range rows {
		dto, err := templateRecordDTO(row)
		if err != nil {
			a.unavailable(c, err)
			return
		}
		data = append(data, dto)
	}
	c.JSON(200, gin.H{"data": data})
}

func (a *API) createScheduleTemplate(c *gin.Context) {
	var body struct {
		ClinicID string          `json:"clinicId"`
		Name     string          `json:"name"`
		Pattern  json.RawMessage `json:"pattern"`
	}
	if !decodeObjectLimit(c, &body, []string{"clinicId", "name", "pattern"}, nil, 65536) {
		return
	}
	if !validID(body.ClinicID) {
		fail(c, 400, "INVALID_BODY", "院区标识无效")
		return
	}
	if len(body.Name) == 0 || len(body.Name) > 100 {
		fail(c, 400, "INVALID_BODY", "模板名称须为 1–100 个字符")
		return
	}
	pattern, err := store.ParseTemplatePattern(string(body.Pattern))
	if err != nil {
		fail(c, 400, "INVALID_BODY", "模板每周排班模式无效")
		return
	}
	canonical, err := json.Marshal(pattern)
	if err != nil {
		fail(c, 400, "INVALID_BODY", "模板每周排班模式无效")
		return
	}
	row, err := a.repo.(store.ScheduleTemplateRepository).CreateScheduleTemplate(c.Request.Context(), model.ScheduleTemplate{ClinicID: body.ClinicID, Name: body.Name, PatternJSON: string(canonical)})
	if err != nil {
		a.unavailable(c, err)
		return
	}
	dto, err := templateRecordDTO(row)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(201, gin.H{"data": dto})
}

func (a *API) getScheduleTemplate(c *gin.Context) {
	id := c.Param("templateId")
	if !validID(id) {
		fail(c, 400, "INVALID_BODY", "模板标识无效")
		return
	}
	row, err := a.repo.(store.ScheduleTemplateRepository).GetScheduleTemplate(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(c, 404, "SCHEDULE_NOT_FOUND", "排班模板不存在")
			return
		}
		a.unavailable(c, err)
		return
	}
	dto, err := templateRecordDTO(row)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(200, gin.H{"data": dto})
}

func (a *API) updateScheduleTemplate(c *gin.Context) {
	id := c.Param("templateId")
	if !validID(id) {
		fail(c, 400, "INVALID_BODY", "模板标识无效")
		return
	}
	name, canonical, ok := templateInput(c)
	if !ok {
		return
	}
	row, err := a.repo.(store.ScheduleTemplateRepository).UpdateScheduleTemplate(c.Request.Context(), id, name, canonical)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(c, 404, "SCHEDULE_NOT_FOUND", "排班模板不存在")
			return
		}
		a.unavailable(c, err)
		return
	}
	dto, err := templateRecordDTO(row)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(200, gin.H{"data": dto})
}

func (a *API) deleteScheduleTemplate(c *gin.Context) {
	id := c.Param("templateId")
	if !validID(id) {
		fail(c, 400, "INVALID_BODY", "模板标识无效")
		return
	}
	if err := a.repo.(store.ScheduleTemplateRepository).DeleteScheduleTemplate(c.Request.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(c, 404, "SCHEDULE_NOT_FOUND", "排班模板不存在")
			return
		}
		a.unavailable(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func templateApplyInput(c *gin.Context) (clinicID, startDate, endDate string, ok bool) {
	var body struct {
		ClinicID  string `json:"clinicId"`
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
	}
	if !decodeObjectLimit(c, &body, []string{"clinicId", "startDate", "endDate"}, nil, 65536) {
		return "", "", "", false
	}
	if !validID(body.ClinicID) {
		fail(c, 400, "INVALID_BODY", "院区标识无效")
		return "", "", "", false
	}
	if _, err := time.ParseInLocation("2006-01-02", body.StartDate, database.Shanghai); err != nil {
		fail(c, 400, "INVALID_BODY", "开始日期无效")
		return "", "", "", false
	}
	if _, err := time.ParseInLocation("2006-01-02", body.EndDate, database.Shanghai); err != nil {
		fail(c, 400, "INVALID_BODY", "结束日期无效")
		return "", "", "", false
	}
	return body.ClinicID, body.StartDate, body.EndDate, true
}

func templateDraftDTOs(rows []store.TemplateDraft) []apitypes.TemplateDraft {
	data := make([]apitypes.TemplateDraft, 0, len(rows))
	for _, row := range rows {
		day, _ := time.Parse("2006-01-02", row.Date)
		data = append(data, apitypes.TemplateDraft{
			DoctorId: row.DoctorID,
			Date:     types.Date{Time: day},
			StartsAt: row.StartsAt,
			EndsAt:   row.EndsAt,
			Capacity: int32(row.Capacity),
		})
	}
	return data
}

func templateConflictDTOs(rows []store.TemplateConflict) []apitypes.TemplateConflict {
	data := make([]apitypes.TemplateConflict, 0, len(rows))
	for _, row := range rows {
		day, _ := time.Parse("2006-01-02", row.Date)
		item := apitypes.TemplateConflict{
			DoctorId: row.DoctorID,
			Date:     types.Date{Time: day},
			StartsAt: row.StartsAt,
			EndsAt:   row.EndsAt,
			Reason:   apitypes.TemplateConflictReason(row.Reason),
		}
		if row.ExistingSlotID != "" {
			id := row.ExistingSlotID
			capacity := int32(row.ExistingCapacity)
			reserved := int32(row.ExistingReserved)
			item.ExistingSlotId = &id
			item.ExistingCapacity = &capacity
			item.ExistingReserved = &reserved
		}
		data = append(data, item)
	}
	return data
}

func (a *API) previewScheduleTemplateApply(c *gin.Context) {
	id := c.Param("templateId")
	if !validID(id) {
		fail(c, 400, "INVALID_BODY", "模板标识无效")
		return
	}
	clinicID, startDate, endDate, ok := templateApplyInput(c)
	if !ok {
		return
	}
	toCreate, conflicts, err := a.repo.(store.ScheduleTemplateRepository).PreviewTemplateApply(c.Request.Context(), clinicID, id, startDate, endDate, a.now())
	if err != nil {
		scheduleFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": apitypes.TemplateApplyPreview{ToCreate: templateDraftDTOs(toCreate), Conflicts: templateConflictDTOs(conflicts)}})
}

func (a *API) applyScheduleTemplate(c *gin.Context) {
	id := c.Param("templateId")
	if !validID(id) {
		fail(c, 400, "INVALID_BODY", "模板标识无效")
		return
	}
	clinicID, startDate, endDate, ok := templateApplyInput(c)
	if !ok {
		return
	}
	created, skipped, err := a.repo.(store.ScheduleTemplateRepository).ApplyScheduleTemplate(c.Request.Context(), clinicID, id, startDate, endDate, a.now())
	if err != nil {
		scheduleFailure(c, err)
		return
	}
	rows := make([]apitypes.Slot, 0, len(created))
	for _, row := range created {
		rows = append(rows, slotDTO(row))
	}
	c.JSON(201, gin.H{"data": apitypes.TemplateApplyResult{Created: rows, Skipped: templateConflictDTOs(skipped)}})
}

func holidayDTO(row model.Holiday) apitypes.Holiday {
	return apitypes.Holiday{
		HolidayDate: types.Date{Time: time.Date(row.HolidayDate.Year(), row.HolidayDate.Month(), row.HolidayDate.Day(), 0, 0, 0, 0, time.UTC)},
		Name:        row.Name,
		CreatedAt:   row.CreatedAt,
	}
}

func (a *API) listHolidays(c *gin.Context) {
	from, err1 := time.ParseInLocation("2006-01-02", c.Query("from"), database.Shanghai)
	to, err2 := time.ParseInLocation("2006-01-02", c.Query("to"), database.Shanghai)
	if err1 != nil || err2 != nil || to.Before(from) || to.Sub(from) > 366*24*time.Hour {
		fail(c, 400, "INVALID_QUERY", "起止日期无效（范围至多 366 天）")
		return
	}
	rows, err := a.repo.(store.ScheduleTemplateRepository).ListHolidays(c.Request.Context(), from, to)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	data := make([]apitypes.Holiday, 0, len(rows))
	for _, row := range rows {
		data = append(data, holidayDTO(row))
	}
	c.JSON(200, gin.H{"data": data})
}

func (a *API) createHoliday(c *gin.Context) {
	var body struct {
		Date string `json:"date"`
		Name string `json:"name"`
	}
	if !decodeObjectLimit(c, &body, []string{"date", "name"}, nil, 65536) {
		return
	}
	day, err := time.ParseInLocation("2006-01-02", body.Date, database.Shanghai)
	if err != nil {
		fail(c, 400, "INVALID_BODY", "节假日日期无效")
		return
	}
	if len(body.Name) == 0 || len(body.Name) > 100 {
		fail(c, 400, "INVALID_BODY", "节假日名称须为 1–100 个字符")
		return
	}
	repo := a.repo.(store.ScheduleTemplateRepository)
	if err := repo.SetHoliday(c.Request.Context(), day, body.Name); err != nil {
		a.unavailable(c, err)
		return
	}
	rows, err := repo.ListHolidays(c.Request.Context(), day, day)
	if err != nil || len(rows) == 0 {
		a.unavailable(c, err)
		return
	}
	c.JSON(201, gin.H{"data": holidayDTO(rows[0])})
}

func (a *API) deleteHoliday(c *gin.Context) {
	day, err := time.ParseInLocation("2006-01-02", c.Param("date"), database.Shanghai)
	if err != nil {
		fail(c, 400, "INVALID_BODY", "节假日日期无效")
		return
	}
	if err := a.repo.(store.ScheduleTemplateRepository).RemoveHoliday(c.Request.Context(), day); err != nil {
		a.unavailable(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
