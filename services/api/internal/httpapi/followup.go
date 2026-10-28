package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) registerFollowupRoutes(r *gin.Engine) {
	patient := r.Group("/api/v1/me", a.bookingAvailable, a.authenticatePatient, a.careAvailable, a.followupAvailable)
	patient.GET("/appointments/:appointmentId/followups", func(c *gin.Context) { a.followupWorkspace(c, false) })
	patient.GET("/followups", func(c *gin.Context) { a.followupQueue(c, false) })
	patient.POST("/followups/:taskId/submit", a.submitFollowup)
	patient.POST("/followups/:taskId/photos/:angle", a.uploadFollowupPhoto)
	patient.GET("/followup-photos/:photoId/content", func(c *gin.Context) { a.followupPhotoContent(c, false) })
	admin := r.Group("/api/v1/admin", a.authenticate, a.careAvailable, a.followupAvailable)
	admin.GET("/appointments/:appointmentId/followups", a.requireAppointmentScope, func(c *gin.Context) { a.followupWorkspace(c, true) })
	admin.POST("/appointments/:appointmentId/treatment", a.requireDoctorScope, a.recordTreatment)
	admin.POST("/appointments/:appointmentId/treatment/corrections", a.requireDoctorScope, a.correctTreatment)
	admin.GET("/followup-staff", a.followupStaff)
	admin.POST("/followup-staff/:staffId/leave", a.requireManager, a.leaveFollowupStaff)
	admin.POST("/followup-staff/:staffId/return", a.requireManager, a.returnFollowupStaff)
	admin.GET("/followup-staff/:staffId/events", a.requireManager, a.followupStaffEvents)
	admin.GET("/followups", func(c *gin.Context) { a.followupQueue(c, true) })
	admin.GET("/handoff-receipts", a.handoffReceipts)
	admin.POST("/handoff-receipts/:receiptId/confirm", a.confirmHandoffReceipt)
	admin.GET("/handoff-receipts/:receiptId/followups", a.requireManager, a.handoffFollowups)
	admin.POST("/handoff-receipts/:receiptId/followups", a.requireManager, a.recordHandoffFollowup)
	admin.GET("/handoff-settings", a.requireManager, a.handoffSetting)
	admin.PUT("/handoff-settings", a.requireManager, a.updateHandoffSetting)
	admin.POST("/followups/:taskId/assignment", a.requireManager, a.requireTaskScope, a.assignFollowup)
	admin.GET("/followups/:taskId/assignments", a.requireTaskScope, a.followupAssignmentHistory)
	admin.POST("/followups/:taskId/review", a.requireTaskScope, a.reviewFollowup)
	admin.GET("/followup-photos/:photoId/content", a.requireFollowupPhotoScope, func(c *gin.Context) { a.followupPhotoContent(c, true) })
}
func (a *API) followupAvailable(c *gin.Context) {
	if a.followup == nil {
		fail(c, 503, "SERVICE_UNAVAILABLE", "随访服务暂不可用")
		return
	}
	c.Next()
}
func (a *API) careNow() time.Time { return a.now().In(database.Shanghai).Truncate(time.Millisecond) }

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func validTaskID(c *gin.Context) bool {
	id := c.Param("taskId")
	if id == "" || !validID(id) {
		fail(c, 400, "INVALID_TASK_ID", "随访任务编号无效")
		return false
	}
	return true
}
func followupPhotoDTO(row model.FollowupPhoto) apitypes.CarePhoto {
	return photoDTO(model.CarePhoto{ID: row.ID, AppointmentID: row.AppointmentID, Angle: row.Angle, SizeBytes: row.SizeBytes, UploadedAt: row.UploadedAt})
}
func treatmentDTO(row *model.Treatment) *apitypes.Treatment {
	if row == nil {
		return nil
	}
	return &apitypes.Treatment{Id: row.ID, AppointmentId: row.AppointmentID, StageIndex: clinicalPtr(int32(row.StageIndex)), Procedure: &row.Procedure, BodyArea: &row.BodyArea, Device: &row.Device, Parameters: &row.Parameters, PlanVersionId: row.PlanVersionID, Summary: row.Summary, Notes: row.Notes, TreatedAt: row.TreatedAt, RecordedAt: row.RecordedAt, CorrectedAt: row.CorrectedAt, Version: int32(row.Version)}
}
func treatmentRevisionDTO(row model.TreatmentRevision) apitypes.TreatmentRevision {
	return apitypes.TreatmentRevision{Version: int32(row.Version), StageIndex: clinicalPtr(int32(row.StageIndex)), Procedure: &row.Procedure, BodyArea: &row.BodyArea, Device: &row.Device, Parameters: &row.Parameters, PlanVersionId: row.PlanVersionID, Summary: row.Summary, Notes: row.Notes, TreatedAt: row.TreatedAt, ChangedAt: row.ChangedAt, ChangedBy: row.ActorID, CorrectionReason: row.CorrectionReason}
}
func (a *API) followupDTO(c *gin.Context, row model.FollowupTask, admin bool) (apitypes.FollowupTask, error) {
	patientID := row.PatientID
	actorType, actorID := "patient", patientID
	if admin {
		patientID = ""
		actorType, actorID = "clinician", currentEmployee(c).ID
	}
	photos, granted, err := a.followup.FollowupPhotos(c.Request.Context(), row.ID, patientID, admin)
	if err != nil {
		return apitypes.FollowupTask{}, err
	}
	result := apitypes.FollowupTask{Id: row.ID, AppointmentId: row.AppointmentID, TreatmentId: strVal(row.TreatmentID), PatientName: row.PatientName, AssigneeId: row.AssigneeID, AssigneeName: row.AssigneeName, AssignedAt: row.AssignedAt, Title: row.Title, DueAt: row.DueAt, Status: apitypes.FollowupTaskStatus(row.Status), Version: int32(row.Version), Symptoms: row.Symptoms, Severity: int32(row.Severity), Concern: row.Concern, Response: row.Response, Disposition: row.Disposition, SubmittedAt: row.SubmittedAt, ReviewedAt: row.ReviewedAt, Overdue: row.Status == "pending" && row.DueAt.Before(a.now()), Photos: make([]apitypes.CarePhoto, 0, len(photos)), PhotosGranted: granted}
	for _, photo := range photos {
		result.Photos = append(result.Photos, followupPhotoDTO(photo))
	}
	err = a.followup.RecordFollowupAccess(c.Request.Context(), row.AppointmentID, row.ID, actorType, actorID, "viewed", a.careNow())
	return result, err
}
func (a *API) followupWorkspace(c *gin.Context, admin bool) {
	if !validAppointmentID(c) {
		return
	}
	patientID := ""
	if !admin {
		patientID = currentPatient(c).ID
	}
	treatment, revisions, rows, err := a.followup.FollowupWorkspaceWithHistory(c.Request.Context(), c.Param("appointmentId"), patientID)
	if err != nil {
		a.careError(c, err)
		return
	}
	data := apitypes.FollowupWorkspace{Treatment: treatmentDTO(treatment), TreatmentRevisions: []apitypes.TreatmentRevision{}, Tasks: make([]apitypes.FollowupTask, 0, len(rows))}
	for _, revision := range revisions {
		data.TreatmentRevisions = append(data.TreatmentRevisions, treatmentRevisionDTO(revision))
	}
	for _, row := range rows {
		task, err := a.followupDTO(c, row, admin)
		if err != nil {
			a.careError(c, err)
			return
		}
		data.Tasks = append(data.Tasks, task)
	}
	c.JSON(200, apitypes.FollowupWorkspaceResult{Data: data})
}
func (a *API) followupQueue(c *gin.Context, admin bool) {
	allowed := []string{"status", "page", "pageSize", "dateFrom", "dateTo"}
	if admin {
		allowed = append(allowed, "assigneeId")
	}
	if !validQuery(c, allowed...) {
		return
	}
	dates, ok := clinicalDateRange(c)
	if !ok {
		return
	}
	status := c.Query("status")
	switch status {
	case "", "pending", "submitted", "alert", "reviewed", "handled", "overdue":
	default:
		fail(c, 400, "INVALID_STATUS", "随访状态无效")
		return
	}
	page, size := 1, 20
	for key, target := range map[string]*int{"page": &page, "pageSize": &size} {
		if value := c.Query(key); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 10000 || (key == "pageSize" && n > 100) {
				fail(c, 400, "INVALID_PAGE", "分页参数无效")
				return
			}
			*target = n
		}
	}
	patientID := ""
	if !admin {
		patientID = currentPatient(c).ID
	} else if employee := currentEmployee(c); employee.Role == "clinician" {
		if employee.StaffID == nil || (c.Query("assigneeId") != "" && c.Query("assigneeId") != *employee.StaffID) {
			fail(c, 403, "FORBIDDEN", "只能查看本人负责的随访任务")
			return
		}
	}
	rows, total, err := a.followup.FollowupQueue(c.Request.Context(), store.FollowupFilter{Dates: dates, PatientID: patientID, AssigneeID: c.Query("assigneeId"), Status: status, Page: page, PageSize: size, Now: a.now()})
	if err != nil {
		a.careError(c, err)
		return
	}
	data := make([]apitypes.FollowupTask, 0, len(rows))
	for _, row := range rows {
		task, err := a.followupDTO(c, row, admin)
		if err != nil {
			a.careError(c, err)
			return
		}
		data = append(data, task)
	}
	c.JSON(200, apitypes.FollowupTaskList{Data: data, Meta: apitypes.PageMeta{Page: int32(page), PageSize: int32(size), Total: int32(total)}})
}
func (a *API) recordTreatment(c *gin.Context) {
	if !validAppointmentID(c) {
		return
	}
	var body apitypes.TreatmentCreate
	if !decodeObjectLimit(c, &body, []string{"version", "planVersionId", "summary", "notes", "treatedAt", "offsets"}, []string{"assigneeId", "stageIndex", "procedure", "bodyArea", "device", "parameters"}, 64<<10) {
		return
	}
	fields := (store.TreatmentFieldPatch{StageIndex: body.StageIndex, Procedure: body.Procedure, BodyArea: body.BodyArea, Device: body.Device, Parameters: body.Parameters}).Apply(model.TreatmentFields{})
	if !store.ValidTreatmentFields(fields) {
		fail(c, 400, "INVALID_TREATMENT", "治疗字段或阶段序号无效")
		return
	}
	summary, notes := strings.TrimSpace(body.Summary), strings.TrimSpace(body.Notes)
	if body.Version < 1 || body.PlanVersionId == "" || !validID(body.PlanVersionId) || utf8.RuneCountInString(summary) < 1 || utf8.RuneCountInString(summary) > 1000 || utf8.RuneCountInString(notes) > 2000 || body.TreatedAt.IsZero() || body.TreatedAt.After(a.now()) || len(body.Offsets) < 1 || len(body.Offsets) > 8 {
		fail(c, 400, "INVALID_TREATMENT", "请填写有效的已发布方案、实际治疗时间、治疗摘要和随访天数")
		return
	}
	offsets := make([]int, 0, len(body.Offsets))
	assigneeID := ""
	if body.AssigneeId != nil {
		assigneeID = *body.AssigneeId
		if assigneeID == "" || !validID(assigneeID) {
			fail(c, 400, "INVALID_ASSIGNEE", "随访负责人编号无效")
			return
		}
	}
	seen := map[int32]bool{}
	for _, n := range body.Offsets {
		if n < 1 || n > 90 || seen[n] {
			fail(c, 400, "INVALID_OFFSETS", "随访天数须为 1–90，不能重复，最多 8 次")
			return
		}
		seen[n] = true
		offsets = append(offsets, int(n))
	}
	_, err := a.followup.RecordTreatment(c.Request.Context(), c.Param("appointmentId"), store.TreatmentInput{TreatmentFields: fields, Version: int(body.Version), PlanVersionID: body.PlanVersionId, Summary: summary, Notes: notes, TreatedAt: body.TreatedAt.In(database.Shanghai).Truncate(time.Millisecond), Offsets: offsets, AssigneeID: assigneeID, ActorID: currentEmployee(c).ID}, a.careNow())
	if err != nil {
		a.careError(c, err)
		return
	}
	a.followupWorkspace(c, true)
}
func (a *API) correctTreatment(c *gin.Context) {
	if !validAppointmentID(c) {
		return
	}
	var body apitypes.TreatmentCorrection
	if !decodeObjectLimit(c, &body, []string{"version", "planVersionId", "summary", "notes", "treatedAt", "correctionReason"}, []string{"stageIndex", "procedure", "bodyArea", "device", "parameters"}, 64<<10) {
		return
	}
	patch := store.TreatmentFieldPatch{StageIndex: body.StageIndex, Procedure: body.Procedure, BodyArea: body.BodyArea, Device: body.Device, Parameters: body.Parameters}
	if !store.ValidTreatmentFields(patch.Apply(model.TreatmentFields{})) {
		fail(c, 400, "INVALID_TREATMENT_CORRECTION", "治疗字段或阶段序号无效")
		return
	}
	summary, notes, reason := strings.TrimSpace(body.Summary), strings.TrimSpace(body.Notes), strings.TrimSpace(body.CorrectionReason)
	if body.Version < 1 || body.PlanVersionId == "" || !validID(body.PlanVersionId) || utf8.RuneCountInString(summary) < 1 || utf8.RuneCountInString(summary) > 1000 || utf8.RuneCountInString(notes) > 2000 || utf8.RuneCountInString(reason) < 1 || utf8.RuneCountInString(reason) > 300 || body.TreatedAt.IsZero() || body.TreatedAt.After(a.now()) {
		fail(c, 400, "INVALID_TREATMENT_CORRECTION", "请填写有效的更正原因、方案、实际治疗时间和摘要")
		return
	}
	_, err := a.followup.CorrectTreatment(c.Request.Context(), c.Param("appointmentId"), store.TreatmentCorrectionInput{Fields: patch, Version: int(body.Version), PlanVersionID: body.PlanVersionId, Summary: summary, Notes: notes, TreatedAt: body.TreatedAt.In(database.Shanghai).Truncate(time.Millisecond), CorrectionReason: reason, ActorID: currentEmployee(c).ID}, a.careNow())
	if err != nil {
		a.careError(c, err)
		return
	}
	a.followupWorkspace(c, true)
}
func (a *API) submitFollowup(c *gin.Context) {
	if !validTaskID(c) {
		return
	}
	var body apitypes.FollowupSubmit
	if !decodeObjectLimit(c, &body, []string{"version", "symptoms", "severity", "concern"}, nil, 32<<10) {
		return
	}
	symptoms := strings.TrimSpace(body.Symptoms)
	if body.Version < 1 || utf8.RuneCountInString(symptoms) < 1 || utf8.RuneCountInString(symptoms) > 2000 || body.Severity < 0 || body.Severity > 3 {
		fail(c, 400, "INVALID_SYMPTOMS", "请填写症状描述并选择当前感受")
		return
	}
	row, err := a.followup.SubmitFollowup(c.Request.Context(), c.Param("taskId"), currentPatient(c).ID, store.FollowupInput{Version: int(body.Version), Symptoms: symptoms, Severity: int(body.Severity), Concern: body.Concern}, a.careNow())
	if err != nil {
		a.careError(c, err)
		return
	}
	data, err := a.followupDTO(c, row, false)
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, apitypes.FollowupTaskResult{Data: data})
}
func (a *API) reviewFollowup(c *gin.Context) {
	if !validTaskID(c) {
		return
	}
	var body apitypes.FollowupReview
	if !decodeObjectLimit(c, &body, []string{"version", "action", "response", "disposition"}, nil, 32<<10) {
		return
	}
	response, disposition := strings.TrimSpace(body.Response), strings.TrimSpace(body.Disposition)
	if body.Version < 1 || (body.Action != "review" && body.Action != "handle") || utf8.RuneCountInString(response) < 1 || utf8.RuneCountInString(response) > 2000 || utf8.RuneCountInString(disposition) > 1000 || (body.Action == "handle" && disposition == "") {
		fail(c, 400, "INVALID_REVIEW", "请填写回评；异常任务还需填写处理记录")
		return
	}
	row, err := a.followup.ReviewFollowup(c.Request.Context(), c.Param("taskId"), store.FollowupReviewInput{Version: int(body.Version), Action: string(body.Action), Response: response, Disposition: disposition}, a.careNow())
	if err != nil {
		a.careError(c, err)
		return
	}
	data, err := a.followupDTO(c, row, true)
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, apitypes.FollowupTaskResult{Data: data})
}
func (a *API) uploadFollowupPhoto(c *gin.Context) {
	if !validTaskID(c) {
		return
	}
	angle := c.Param("angle")
	if angle != "front" && angle != "left" && angle != "right" {
		fail(c, 400, "INVALID_PHOTO_ANGLE", "照片角度无效")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPhotoBytes+(1<<20))
	file, header, err := c.Request.FormFile("file")
	if err != nil || header.Size < 1 || header.Size > maxPhotoBytes {
		fail(c, 400, "INVALID_PHOTO", "请选择不超过 8 MB 的 JPG 或 PNG")
		return
	}
	defer file.Close()
	input, err := io.ReadAll(io.LimitReader(file, maxPhotoBytes+1))
	if err != nil || len(input) > maxPhotoBytes {
		fail(c, 400, "INVALID_PHOTO", "图片读取失败")
		return
	}
	data, err := normalizePhoto(input)
	if err != nil {
		fail(c, 400, "INVALID_PHOTO", "请选择有效的 JPG 或 PNG")
		return
	}
	hash := sha256.Sum256(data)
	photo := model.FollowupPhoto{ID: randomCareID(), Angle: angle, SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(hash[:]), UploadedAt: a.careNow()}
	if err := a.media.Save(c.Request.Context(), photo.ID, data); err != nil {
		a.mediaUnavailable(c)
		return
	}
	saved, _, err := a.followup.SaveFollowupPhoto(c.Request.Context(), c.Param("taskId"), currentPatient(c).ID, photo, a.careNow())
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, apitypes.CarePhotoResult{Data: followupPhotoDTO(saved)})
}
func (a *API) followupPhotoContent(c *gin.Context, admin bool) {
	id := c.Param("photoId")
	if id == "" || !validID(id) {
		fail(c, 400, "INVALID_PHOTO_ID", "照片编号无效")
		return
	}
	patientID, actorType, actorID := "", "clinician", "demo-clinician"
	if !admin {
		patientID = currentPatient(c).ID
		actorType, actorID = "patient", patientID
	} else {
		actorID = currentEmployee(c).ID
	}
	row, err := a.followup.FollowupPhoto(c.Request.Context(), id, patientID, admin)
	if err != nil {
		a.careError(c, err)
		return
	}
	data, err := a.media.Read(c.Request.Context(), row.ID)
	if err != nil {
		a.mediaUnavailable(c)
		return
	}
	if err := store.VerifyMedia(data, store.MediaReference{ID: row.ID, SHA256: row.SHA256, SizeBytes: row.SizeBytes}); err != nil {
		a.mediaUnavailable(c)
		return
	}
	if _, err := a.followup.FollowupPhoto(c.Request.Context(), id, patientID, admin); err != nil {
		a.careError(c, err)
		return
	}
	if err := a.followup.RecordFollowupAccess(c.Request.Context(), row.AppointmentID, row.TaskID, actorType, actorID, "photo-viewed", a.careNow()); err != nil {
		a.careError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, "image/jpeg", data)
}

func clinicalPtr[T any](value T) *T { return &value }
