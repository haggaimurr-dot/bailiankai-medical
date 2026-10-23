package httpapi

import (
	"strings"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func validStaffID(c *gin.Context) bool {
	id := c.Param("staffId")
	if id == "" || !validID(id) {
		fail(c, 400, "INVALID_STAFF_ID", "医护人员编号无效")
		return false
	}
	return true
}

func staffDTO(row model.FollowupStaff) apitypes.FollowupStaff {
	return apitypes.FollowupStaff{Id: row.ID, DisplayName: row.DisplayName, Role: apitypes.FollowupStaffRole(row.Role), Status: apitypes.FollowupStaffStatus(row.Status), Version: int32(row.Version)}
}

func assignmentDTO(row model.FollowupAssignmentEvent) apitypes.FollowupAssignmentEvent {
	return apitypes.FollowupAssignmentEvent{Id: row.ID, TaskId: row.TaskID, FromStaffId: row.FromStaffID, FromName: row.FromName, ToStaffId: row.ToStaffID, ToName: row.ToName, EventType: apitypes.FollowupAssignmentEventEventType(row.EventType), Reason: row.Reason, ActorId: row.ActorID, ActorName: row.ActorName, CreatedAt: row.CreatedAt}
}

func staffEventDTO(row model.FollowupStaffEvent) apitypes.FollowupStaffEvent {
	return apitypes.FollowupStaffEvent{Id: row.ID, StaffId: row.StaffID, EventType: apitypes.FollowupStaffEventEventType(row.EventType), ToStaffId: row.ToStaffID, ToName: row.ToName, Reason: row.Reason, TransferredCount: row.TransferredCount, ActorId: row.ActorID, ActorName: row.ActorName, CreatedAt: row.CreatedAt}
}

func (a *API) followupStaff(c *gin.Context) {
	if !validQuery(c) {
		return
	}
	rows, err := a.followup.FollowupStaff(c.Request.Context())
	if err != nil {
		a.careError(c, err)
		return
	}
	data := make([]apitypes.FollowupStaff, 0, len(rows))
	for _, row := range rows {
		data = append(data, staffDTO(row))
	}
	c.JSON(200, apitypes.FollowupStaffList{Data: data})
}

func (a *API) assignFollowup(c *gin.Context) {
	if !validTaskID(c) {
		return
	}
	var body apitypes.FollowupAssignment
	if !decodeObject(c, &body, []string{"version", "assigneeId", "reason"}, nil) {
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if body.Version < 1 || body.AssigneeId == "" || !validID(body.AssigneeId) || utf8.RuneCountInString(reason) < 1 || utf8.RuneCountInString(reason) > 300 {
		fail(c, 400, "INVALID_ASSIGNMENT", "请选择负责人并填写不超过 300 字的改派原因")
		return
	}
	row, err := a.followup.AssignFollowup(c.Request.Context(), c.Param("taskId"), store.FollowupAssignmentInput{Version: int(body.Version), AssigneeID: body.AssigneeId, Reason: reason, ActorID: currentEmployee(c).ID}, a.careNow())
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

func (a *API) followupAssignmentHistory(c *gin.Context) {
	if !validTaskID(c) || !validQuery(c) {
		return
	}
	rows, err := a.followup.FollowupAssignmentHistory(c.Request.Context(), c.Param("taskId"))
	if err != nil {
		a.careError(c, err)
		return
	}
	data := make([]apitypes.FollowupAssignmentEvent, 0, len(rows))
	for _, row := range rows {
		data = append(data, assignmentDTO(row))
	}
	c.JSON(200, apitypes.FollowupAssignmentEventList{Data: data})
}

func (a *API) leaveFollowupStaff(c *gin.Context) {
	if !validStaffID(c) {
		return
	}
	var body apitypes.FollowupLeave
	if !decodeObject(c, &body, []string{"version", "toStaffId", "reason"}, nil) {
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if body.Version < 1 || body.ToStaffId == "" || !validID(body.ToStaffId) || body.ToStaffId == c.Param("staffId") || utf8.RuneCountInString(reason) < 1 || utf8.RuneCountInString(reason) > 300 {
		fail(c, 400, "INVALID_HANDOFF", "请选择其他在岗医护并填写不超过 300 字的交接原因")
		return
	}
	staff, transferred, err := a.followup.LeaveFollowupStaff(c.Request.Context(), c.Param("staffId"), store.FollowupLeaveInput{Version: int(body.Version), ToStaffID: body.ToStaffId, Reason: reason, ActorID: currentEmployee(c).ID}, a.careNow())
	if err != nil {
		a.careError(c, err)
		return
	}
	var result apitypes.FollowupLeaveResult
	result.Data.Staff, result.Data.Transferred = staffDTO(staff), transferred
	c.JSON(200, result)
}

func (a *API) returnFollowupStaff(c *gin.Context) {
	if !validStaffID(c) {
		return
	}
	var body apitypes.FollowupReturn
	if !decodeObject(c, &body, []string{"version"}, nil) {
		return
	}
	if body.Version < 1 {
		fail(c, 400, "INVALID_STAFF_VERSION", "医护状态版本无效")
		return
	}
	row, err := a.followup.ReturnFollowupStaff(c.Request.Context(), c.Param("staffId"), int(body.Version), a.careNow(), currentEmployee(c).ID)
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, apitypes.FollowupStaffResult{Data: staffDTO(row)})
}

func (a *API) followupStaffEvents(c *gin.Context) {
	if !validStaffID(c) || !validQuery(c) {
		return
	}
	rows, err := a.followup.FollowupStaffEvents(c.Request.Context(), c.Param("staffId"))
	if err != nil {
		a.careError(c, err)
		return
	}
	data := make([]apitypes.FollowupStaffEvent, 0, len(rows))
	for _, row := range rows {
		data = append(data, staffEventDTO(row))
	}
	c.JSON(200, apitypes.FollowupStaffEventList{Data: data})
}
