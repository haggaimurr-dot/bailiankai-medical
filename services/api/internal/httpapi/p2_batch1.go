package httpapi

import (
	"strconv"
	"time"

	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) registerP2Batch1Routes(r *gin.Engine) {
	// 行 103：方案模板库
	r.GET("/api/v1/admin/care-plan-templates/library", a.authenticate, a.careTemplateLibrary)
	r.POST("/api/v1/admin/care-plan-templates/:templateId/share", a.authenticate, a.shareCareTemplate)
	r.POST("/api/v1/admin/care-plan-templates/:templateId/apply", a.authenticate, a.applyCareTemplate)
	// 行 104：用药/护理提醒打卡
	r.GET("/api/v1/admin/patients/:patientId/care-reminders", a.authenticate, a.adminCareReminders)
	r.POST("/api/v1/admin/patients/:patientId/care-reminders", a.authenticate, a.createCareReminder)
	// 行 105：交接进度看板
	r.GET("/api/v1/admin/handover-batches", a.authenticate, a.handoverBatches)
	r.POST("/api/v1/admin/handover-batches", a.authenticate, a.createHandoverBatch)
	r.POST("/api/v1/admin/handover-batches/:batchId/nudge", a.authenticate, a.nudgeHandoverBatch)
}

func (a *API) registerP2Batch1PatientRoutes(patient *gin.RouterGroup) {
	// 行 104：患者端提醒打卡
	patient.GET("/care-reminders", a.patientCareReminders)
	patient.POST("/care-reminders/:reminderId/checkin", a.checkinCareReminder)
	patient.GET("/care-reminders/:reminderId/checkins", a.careReminderCheckins)
}

// ============ 行 103 ============

func (a *API) careTemplateLibrary(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	employee := currentEmployee(c)
	rows, err := a.repo.(*store.MySQL).ListCareTemplateLibrary(c.Request.Context(), employee.ID, employee.ClinicIDs)
	if err != nil {
		a.careError(c, err)
		return
	}
	data := make([]gin.H, 0, len(rows))
	for _, t := range rows {
		data = append(data, gin.H{"id": t.ID, "name": t.Name, "isShared": t.IsShared, "clinicId": t.ClinicID, "ownerName": t.OwnerName, "createdAt": t.CreatedAt})
	}
	c.JSON(200, gin.H{"data": data})
}

func (a *API) shareCareTemplate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	employee := currentEmployee(c)
	if employee.Role != "manager" {
		fail(c, 403, "FORBIDDEN", "仅主管可发布共享模板")
		return
	}
	if !validID(c.Param("templateId")) {
		fail(c, 400, "INVALID_ID", "模板标识无效")
		return
	}
	var body struct {
		ClinicID string `json:"clinicId"`
	}
	if !decodeObject(c, &body, []string{"clinicId"}, nil) {
		return
	}
	if !validID(body.ClinicID) {
		fail(c, 400, "INVALID_BODY", "院区标识无效")
		return
	}
	row, err := a.repo.(*store.MySQL).ShareCareTemplate(c.Request.Context(), c.Param("templateId"), employee.ID, body.ClinicID, a.now())
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"id": row.ID, "name": row.Name, "isShared": row.IsShared}})
}

func (a *API) applyCareTemplate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	employee := currentEmployee(c)
	if employee.Role != "manager" && employee.Role != "doctor" {
		fail(c, 403, "FORBIDDEN", "仅医生可从模板新建方案")
		return
	}
	if !validID(c.Param("templateId")) {
		fail(c, 400, "INVALID_ID", "模板标识无效")
		return
	}
	var body struct {
		AppointmentID   string `json:"appointmentId"`
		ExpectedVersion int    `json:"expectedVersion"`
	}
	if !decodeObject(c, &body, []string{"appointmentId"}, []string{"expectedVersion"}) {
		return
	}
	if !validID(body.AppointmentID) {
		fail(c, 400, "INVALID_BODY", "预约标识无效")
		return
	}
	plan, err := a.repo.(*store.MySQL).ApplyCareTemplate(c.Request.Context(), c.Param("templateId"), body.AppointmentID, body.ExpectedVersion, employee.ID, employee.ClinicIDs, a.now())
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"id": plan.ID, "appointmentId": plan.AppointmentID, "draftVersion": plan.DraftVersion}})
}

// ============ 行 104 ============

func (a *API) adminCareReminders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !validID(c.Param("patientId")) {
		fail(c, 400, "INVALID_ID", "患者标识无效")
		return
	}
	rows, err := a.repo.(*store.MySQL).ListCareReminders(c.Request.Context(), c.Param("patientId"))
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": careReminderDTOs(rows)})
}

func (a *API) createCareReminder(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	employee := currentEmployee(c)
	if !validID(c.Param("patientId")) {
		fail(c, 400, "INVALID_ID", "患者标识无效")
		return
	}
	var body struct {
		ClinicID     string  `json:"clinicId"`
		Kind         string  `json:"kind"`
		Title        string  `json:"title"`
		Detail       string  `json:"detail"`
		ScheduleJSON string  `json:"scheduleJson"`
		StartDate    string  `json:"startDate"`
		EndDate      *string `json:"endDate"`
	}
	if !decodeObject(c, &body, []string{"clinicId", "kind", "title", "startDate"}, []string{"detail", "scheduleJson", "endDate"}) {
		return
	}
	if !validID(body.ClinicID) {
		fail(c, 400, "INVALID_BODY", "院区标识无效")
		return
	}
	start, err := time.Parse("2006-01-02", body.StartDate)
	if err != nil {
		fail(c, 400, "INVALID_BODY", "开始日期格式无效")
		return
	}
	var end *time.Time
	if body.EndDate != nil && *body.EndDate != "" {
		t, err := time.Parse("2006-01-02", *body.EndDate)
		if err != nil {
			fail(c, 400, "INVALID_BODY", "结束日期格式无效")
			return
		}
		end = &t
	}
	row, err := a.repo.(*store.MySQL).CreateCareReminder(c.Request.Context(), c.Param("patientId"), body.ClinicID, body.Kind, body.Title, body.Detail, body.ScheduleJSON, start, end, employee.ID, a.now())
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": careReminderDTO(row)})
}

func careReminderDTO(r store.CareReminder) gin.H {
	return gin.H{"id": r.ID, "patientId": r.PatientID, "kind": r.Kind, "title": r.Title, "detail": r.Detail,
		"scheduleJson": r.ScheduleJSON, "startDate": r.StartDate.Format("2006-01-02"),
		"endDate": formatOptionalDate(r.EndDate), "active": r.Active, "createdAt": r.CreatedAt}
}

func formatOptionalDate(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format("2006-01-02")
}

func careReminderDTOs(rows []store.CareReminder) []gin.H {
	data := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		data = append(data, careReminderDTO(r))
	}
	return data
}

func (a *API) patientCareReminders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	patient := currentPatient(c)
	rows, err := a.repo.(*store.MySQL).ListCareReminders(c.Request.Context(), patient.ID)
	if err != nil {
		a.careError(c, err)
		return
	}
	today := a.now().Format("2006-01-02")
	data := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		d := careReminderDTO(r)
		// 当天是否已打卡
		checkins, _ := a.repo.(*store.MySQL).ReminderCheckins(c.Request.Context(), r.ID, patient.ID, 1)
		doneToday := len(checkins) > 0 && checkins[0].CheckinDate.Format("2006-01-02") == today && checkins[0].Done
		d["doneToday"] = doneToday
		data = append(data, d)
	}
	c.JSON(200, gin.H{"data": data})
}

func (a *API) checkinCareReminder(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	patient := currentPatient(c)
	if !validID(c.Param("reminderId")) {
		fail(c, 400, "INVALID_ID", "提醒标识无效")
		return
	}
	var body struct {
		Date *string `json:"date"`
	}
	_ = decodeObject(c, &body, nil, []string{"date"})
	day := a.now()
	if body.Date != nil && *body.Date != "" {
		t, err := time.Parse("2006-01-02", *body.Date)
		if err != nil {
			fail(c, 400, "INVALID_BODY", "日期格式无效")
			return
		}
		day = t
	}
	// 不允许补未来日期的卡
	today := time.Date(a.now().Year(), a.now().Month(), a.now().Day(), 0, 0, 0, 0, time.UTC)
	want := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	if want.After(today) {
		fail(c, 400, "INVALID_BODY", "不能提前打卡")
		return
	}
	if err := a.repo.(*store.MySQL).CheckinReminder(c.Request.Context(), c.Param("reminderId"), patient.ID, want, a.now()); err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"checked": true, "date": want.Format("2006-01-02")}})
}

func (a *API) careReminderCheckins(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	patient := currentPatient(c)
	if !validID(c.Param("reminderId")) {
		fail(c, 400, "INVALID_ID", "提醒标识无效")
		return
	}
	rows, err := a.repo.(*store.MySQL).ReminderCheckins(c.Request.Context(), c.Param("reminderId"), patient.ID, 60)
	if err != nil {
		a.careError(c, err)
		return
	}
	data := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		data = append(data, gin.H{"date": r.CheckinDate.Format("2006-01-02"), "done": r.Done})
	}
	c.JSON(200, gin.H{"data": data})
}

// ============ 行 105 ============

func (a *API) handoverBatches(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !validQuery(c, "page", "pageSize") {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	batches, total, err := a.repo.(*store.MySQL).HandoverBatches(c.Request.Context(), page, size)
	if err != nil {
		a.careError(c, err)
		return
	}
	data := make([]gin.H, 0, len(batches))
	for _, b := range batches {
		data = append(data, gin.H{"batchId": b.BatchID, "reason": b.Reason, "fromStaffId": b.FromStaff, "toStaffId": b.ToStaff,
			"fromName": b.FromName, "toName": b.ToName, "total": b.Total, "confirmed": b.Confirmed, "pending": b.Pending,
			"createdAt": b.CreatedAt, "assignedBy": b.AssignedBy})
	}
	c.JSON(200, gin.H{"data": data, "meta": gin.H{"page": page, "pageSize": size, "total": total}})
}

func (a *API) createHandoverBatch(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	employee := currentEmployee(c)
	var body struct {
		TaskIDs    []string `json:"taskIds"`
		ToStaffID  string   `json:"toStaffId"`
		Reason     string   `json:"reason"`
	}
	if !decodeObject(c, &body, []string{"taskIds", "toStaffId", "reason"}, nil) {
		return
	}
	if !validID(body.ToStaffID) {
		fail(c, 400, "INVALID_BODY", "接手人标识无效")
		return
	}
	batchID, err := a.repo.(*store.MySQL).CreateHandoverBatch(c.Request.Context(), body.TaskIDs, body.ToStaffID, body.Reason, employee.ID, a.now())
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"batchId": batchID, "count": len(body.TaskIDs)}})
}

func (a *API) nudgeHandoverBatch(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	employee := currentEmployee(c)
	batchID := c.Param("batchId")
	if len(batchID) == 0 || len(batchID) > 64 {
		fail(c, 400, "INVALID_ID", "批次标识无效")
		return
	}
	sent, err := a.repo.(*store.MySQL).NudgeHandoverBatch(c.Request.Context(), batchID, employee.ID, a.now())
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"nudged": sent}})
}
