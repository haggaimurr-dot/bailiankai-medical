package httpapi

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func handoffReceiptDTO(row model.FollowupHandoffReceipt, overdueHours int, now time.Time) gin.H {
	return gin.H{"id": row.ID, "taskId": row.TaskID, "patientName": row.PatientName, "taskTitle": row.TaskTitle, "dueAt": row.DueAt, "fromStaffId": row.FromStaffID, "fromName": row.FromName, "toStaffId": row.ToStaffID, "toName": row.ToName, "reason": row.Reason, "assignedBy": row.AssignedBy, "assignedByName": row.AssignedByName, "createdAt": row.CreatedAt, "status": row.Status, "overdue": row.Status == "pending" && row.CreatedAt.Before(now.Add(-time.Duration(overdueHours)*time.Hour)), "followupStatus": row.FollowupStatus, "followupVersion": row.FollowupVersion, "followupCount": row.FollowupCount, "confirmedBy": row.ConfirmedBy, "confirmedByName": row.ConfirmedByName, "confirmedAt": row.ConfirmedAt, "supersededAt": row.SupersededAt, "version": row.Version}
}
func handoffFollowupDTO(row model.HandoffFollowupEvent) gin.H {
	return gin.H{"id": row.ID, "receiptId": row.ReceiptID, "actionText": row.ActionText, "resultText": row.ResultText, "statusAfter": row.StatusAfter, "actorId": row.ActorID, "actorName": row.ActorName, "createdAt": row.CreatedAt}
}
func (a *API) handoffFollowups(c *gin.Context) {
	id := c.Param("receiptId")
	if id == "" || !validID(id) {
		fail(c, 400, "INVALID_RECEIPT_ID", "交接编号无效")
		return
	}
	rows, err := a.followup.HandoffFollowupEvents(c.Request.Context(), id)
	if err != nil {
		a.careError(c, err)
		return
	}
	data := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		data = append(data, handoffFollowupDTO(row))
	}
	c.JSON(200, gin.H{"data": data})
}
func (a *API) recordHandoffFollowup(c *gin.Context) {
	id := c.Param("receiptId")
	if id == "" || !validID(id) {
		fail(c, 400, "INVALID_RECEIPT_ID", "交接编号无效")
		return
	}
	var body struct {
		RequestKey      string `json:"requestKey"`
		ActionText      string `json:"actionText"`
		ResultText      string `json:"resultText"`
		StatusAfter     string `json:"statusAfter"`
		FollowupVersion int    `json:"followupVersion"`
	}
	if !decodeObject(c, &body, []string{"requestKey", "actionText", "resultText", "statusAfter", "followupVersion"}, nil) {
		return
	}
	body.ActionText, body.ResultText = strings.TrimSpace(body.ActionText), strings.TrimSpace(body.ResultText)
	if body.RequestKey == "" || !validID(body.RequestKey) || body.FollowupVersion < 1 || (body.StatusAfter != "awaiting_reply" && body.StatusAfter != "needs_action" && body.StatusAfter != "resolved") || utf8.RuneCountInString(body.ActionText) < 1 || utf8.RuneCountInString(body.ActionText) > 500 || utf8.RuneCountInString(body.ResultText) < 1 || utf8.RuneCountInString(body.ResultText) > 1000 {
		fail(c, 400, "INVALID_HANDOFF_FOLLOWUP", "请填写跟进措施、处理结果和跟进状态；文字分别不超过 500 和 1000 字")
		return
	}
	row, err := a.followup.RecordHandoffFollowup(c.Request.Context(), id, body.RequestKey, body.ActionText, body.ResultText, body.StatusAfter, currentEmployee(c).ID, body.FollowupVersion, a.careNow())
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": handoffFollowupDTO(row)})
}
func (a *API) handoffReceipts(c *gin.Context) {
	if !validQuery(c, "status", "toStaffId", "receiptId", "page", "pageSize") {
		return
	}
	status := c.DefaultQuery("status", "pending")
	if status != "pending" && status != "confirmed" && status != "superseded" && status != "overdue" && status != "todo" && status != "all" {
		fail(c, 400, "INVALID_STATUS", "交接状态无效")
		return
	}
	if id := c.Query("receiptId"); id != "" && !validID(id) {
		fail(c, 400, "INVALID_RECEIPT_ID", "交接编号无效")
		return
	}
	now := a.careNow()
	filter := store.HandoffReceiptFilter{ReceiptID: c.Query("receiptId"), Status: status, ToStaffID: c.Query("toStaffId"), Page: 1, PageSize: 20, Now: now}
	if status == "all" {
		filter.Status = ""
	}
	for key, target := range map[string]*int{"page": &filter.Page, "pageSize": &filter.PageSize} {
		if value := c.Query(key); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 10000 || (key == "pageSize" && n > 100) {
				fail(c, 400, "INVALID_PAGE", "分页参数无效")
				return
			}
			*target = n
		}
	}
	if employee := currentEmployee(c); store.ClinicalRole(employee.Role) && (employee.StaffID == nil || (filter.ToStaffID != "" && filter.ToStaffID != *employee.StaffID)) {
		fail(c, 403, "FORBIDDEN", "只能查看自己的交接任务")
		return
	}
	result, err := a.followup.HandoffReceipts(c.Request.Context(), filter)
	if err != nil {
		a.careError(c, err)
		return
	}
	data := make([]gin.H, 0, len(result.Data))
	for _, row := range result.Data {
		data = append(data, handoffReceiptDTO(row, result.OverdueHours, now))
	}
	c.JSON(200, gin.H{"data": data, "meta": gin.H{"page": result.Page, "pageSize": result.PageSize, "total": result.Total, "overdueTotal": result.OverdueTotal, "todoTotal": result.TodoTotal, "overdueHours": result.OverdueHours}})
}
func (a *API) confirmHandoffReceipt(c *gin.Context) {
	id := c.Param("receiptId")
	if id == "" || !validID(id) {
		fail(c, 400, "INVALID_RECEIPT_ID", "交接编号无效")
		return
	}
	var body struct {
		Version int `json:"version"`
	}
	if !decodeObject(c, &body, []string{"version"}, nil) {
		return
	}
	if body.Version < 1 {
		fail(c, 400, "INVALID_VERSION", "交接版本无效")
		return
	}
	row, err := a.followup.ConfirmHandoffReceipt(c.Request.Context(), id, body.Version, a.careNow())
	if err != nil {
		a.careError(c, err)
		return
	}
	setting, err := a.followup.HandoffSetting(c.Request.Context())
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": handoffReceiptDTO(row, setting.OverdueHours, a.careNow())})
}

func handoffSettingDTO(row model.FollowupHandoffSetting) gin.H {
	return gin.H{"overdueHours": row.OverdueHours, "version": row.Version, "updatedBy": row.UpdatedBy, "updatedAt": row.UpdatedAt}
}
func (a *API) handoffSetting(c *gin.Context) {
	row, err := a.followup.HandoffSetting(c.Request.Context())
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": handoffSettingDTO(row)})
}
func (a *API) updateHandoffSetting(c *gin.Context) {
	var body struct {
		OverdueHours int `json:"overdueHours"`
		Version      int `json:"version"`
	}
	if !decodeObject(c, &body, []string{"overdueHours", "version"}, nil) {
		return
	}
	if body.OverdueHours < 1 || body.OverdueHours > 720 || body.Version < 1 {
		fail(c, 400, "INVALID_HANDOFF_SETTING", "超时时限须为 1–720 小时")
		return
	}
	row, err := a.followup.UpdateHandoffSetting(c.Request.Context(), body.OverdueHours, body.Version, currentEmployee(c).ID, a.careNow())
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": handoffSettingDTO(row)})
}
