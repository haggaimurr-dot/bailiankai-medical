package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func recordExportPermission(role, method, path string) (bool, bool) {
	if !strings.HasPrefix(path, "/api/v1/admin/record-exports") {
		return false, false
	}
	allowedRole := role == "manager" || store.ClinicalRole(role)
	switch path {
	case "/api/v1/admin/record-exports":
		return allowedRole && (method == http.MethodGet || method == http.MethodPost), true
	case "/api/v1/admin/record-exports/:exportId", "/api/v1/admin/record-exports/:exportId/download", "/api/v1/admin/record-exports/:exportId/events":
		return allowedRole && method == http.MethodGet, true
	default:
		return false, true
	}
}

func (a *API) registerRecordExportRoutes(r *gin.Engine) {
	admin := r.Group("/api/v1/admin/record-exports", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Next()
	}, a.authenticate, a.recordExportsAvailable)
	admin.POST("", a.createRecordExport)
	admin.GET("", a.listRecordExports)
	admin.GET("/:exportId", a.recordExport)
	admin.GET("/:exportId/download", a.downloadRecordExport)
	admin.GET("/:exportId/events", a.recordExportEvents)
}

func (a *API) recordExportsAvailable(c *gin.Context) {
	if _, ok := a.repo.(store.RecordExportRepository); !ok {
		fail(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "档案导出服务暂不可用")
		return
	}
	c.Next()
}

func (a *API) recordExportError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrRecordExportInput):
		fail(c, http.StatusBadRequest, "INVALID_EXPORT", "请选择1–20位患者及有效资料类型，填写用途；日期需成对填写且不超过366天")
	case errors.Is(err, store.ErrRecordExportQuota):
		fail(c, http.StatusTooManyRequests, "EXPORT_QUOTA", "最多同时生成3个导出任务，请等待现有任务完成")
	case errors.Is(err, store.ErrRecordExportNotReady):
		fail(c, http.StatusConflict, "EXPORT_NOT_READY", "导出尚未完成或已失败，请刷新任务状态")
	case errors.Is(err, store.ErrRecordExportExpired):
		fail(c, http.StatusGone, "EXPORT_EXPIRED", "导出文件已过期，请重新创建任务")
	case errors.Is(err, store.ErrForbidden):
		fail(c, http.StatusForbidden, "EXPORT_FORBIDDEN", "导出权限已变化或当前角色无权导出完整资料")
	case errors.Is(err, store.ErrNotFound):
		fail(c, http.StatusNotFound, "NOT_FOUND", "档案或导出任务不存在或无权访问")
	default:
		a.unavailable(c, err)
	}
}

func validRecordExportInput(input store.RecordExportInput) bool {
	if len(input.PatientIDs) < 1 || len(input.PatientIDs) > 20 || len(input.Sections) < 1 || len(input.Sections) > 5 || utf8.RuneCountInString(input.Reason) < 1 || utf8.RuneCountInString(input.Reason) > 200 {
		return false
	}
	patients := make(map[string]bool, len(input.PatientIDs))
	for _, id := range input.PatientIDs {
		if id == "" || !validID(id) || patients[id] {
			return false
		}
		patients[id] = true
	}
	sections := make(map[string]bool, len(input.Sections))
	for _, section := range input.Sections {
		switch section {
		case "appointments", "consultations", "care_plans", "treatments", "followups":
		default:
			return false
		}
		if sections[section] {
			return false
		}
		sections[section] = true
	}
	if input.DateFrom == "" && input.DateTo == "" {
		return true
	}
	from, errFrom := time.ParseInLocation("2006-01-02", input.DateFrom, database.Shanghai)
	to, errTo := time.ParseInLocation("2006-01-02", input.DateTo, database.Shanghai)
	return errFrom == nil && errTo == nil && from.Year() >= 1000 && to.Year() < 9999 && !to.Before(from) && to.Before(from.AddDate(0, 0, 366))
}

func (a *API) createRecordExport(c *gin.Context) {
	if !validQuery(c) {
		return
	}
	var body apitypes.RecordExportInput
	if !decodeObject(c, &body, []string{"patientIds", "sections", "reason"}, []string{"dateFrom", "dateTo", "masked"}) {
		return
	}
	input := store.RecordExportInput{PatientIDs: body.PatientIds, Masked: true, Reason: strings.TrimSpace(body.Reason)}
	for _, section := range body.Sections {
		input.Sections = append(input.Sections, string(section))
	}
	if body.Masked != nil {
		input.Masked = *body.Masked
	}
	if body.DateFrom != nil {
		input.DateFrom = body.DateFrom.Format("2006-01-02")
	}
	if body.DateTo != nil {
		input.DateTo = body.DateTo.Format("2006-01-02")
	}
	if !validRecordExportInput(input) {
		a.recordExportError(c, store.ErrRecordExportInput)
		return
	}
	if !input.Masked && currentEmployee(c).Role != "manager" {
		a.recordExportError(c, store.ErrForbidden)
		return
	}
	job, err := a.repo.(store.RecordExportRepository).CreateRecordExport(c.Request.Context(), currentEmployee(c).ID, input, a.now())
	if err != nil {
		a.recordExportError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"data": job})
}

func (a *API) listRecordExports(c *gin.Context) {
	if !validQuery(c, "page", "pageSize") {
		return
	}
	page, size := 1, 20
	if value := c.Query("page"); value != "" {
		var err error
		page, err = strconv.Atoi(value)
		if err != nil || page < 1 || page > 100000 {
			fail(c, http.StatusBadRequest, "INVALID_QUERY", "页码无效")
			return
		}
	}
	if value := c.Query("pageSize"); value != "" {
		var err error
		size, err = strconv.Atoi(value)
		if err != nil || size < 1 || size > 100 {
			fail(c, http.StatusBadRequest, "INVALID_QUERY", "每页任务数无效")
			return
		}
	}
	rows, total, err := a.repo.(store.RecordExportRepository).ListRecordExports(c.Request.Context(), currentEmployee(c).ID, page, size, a.now())
	if err != nil {
		a.recordExportError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "pageSize": size})
}

func recordExportID(c *gin.Context) (string, bool) {
	id := c.Param("exportId")
	if !validQuery(c) {
		return "", false
	}
	if id == "" || !validID(id) {
		fail(c, http.StatusBadRequest, "INVALID_EXPORT_ID", "导出任务标识无效")
		return "", false
	}
	return id, true
}

func (a *API) recordExport(c *gin.Context) {
	id, ok := recordExportID(c)
	if !ok {
		return
	}
	job, err := a.repo.(store.RecordExportRepository).RecordExport(c.Request.Context(), currentEmployee(c).ID, id, a.now())
	if err != nil {
		a.recordExportError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": job})
}

func (a *API) downloadRecordExport(c *gin.Context) {
	id, ok := recordExportID(c)
	if !ok {
		return
	}
	job, payload, err := a.repo.(store.RecordExportRepository).DownloadRecordExport(c.Request.Context(), currentEmployee(c).ID, id, a.now())
	if err != nil {
		a.recordExportError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="record-export-`+job.ID+`.json"`)
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
}

func (a *API) recordExportEvents(c *gin.Context) {
	id, ok := recordExportID(c)
	if !ok {
		return
	}
	rows, err := a.repo.(store.RecordExportRepository).RecordExportEvents(c.Request.Context(), currentEmployee(c).ID, id)
	if err != nil {
		a.recordExportError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}
