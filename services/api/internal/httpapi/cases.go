package httpapi

import (
	"errors"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"strconv"
	"strings"
)

func casePermission(role, method, path string) (bool, bool) {
	base := "/api/v1/admin/cases"
	if path != base && !strings.HasPrefix(path, base+"/") {
		return false, false
	}
	read := method == "GET"
	if role == "doctor" || role == "clinician" {
		return read || method == "POST" && path == base+"/:resourceId/review", true
	}
	if role == "manager" {
		return !(method == "POST" && path == base+"/:resourceId/review"), true
	}
	return false, true
}
func (a *API) registerCaseRoutes(r *gin.Engine) {
	available := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := a.repo.(store.CaseRepository); !ok {
			fail(c, 503, "SERVICE_UNAVAILABLE", "案例库暂不可用")
			return
		}
		c.Next()
	}
	for _, audience := range []string{"public", "patient", "admin"} {
		prefix := "/api/v1/cases"
		middleware := []gin.HandlerFunc{available}
		if audience == "admin" {
			prefix = "/api/v1/admin/cases"
			middleware = append(middleware, a.authenticate)
		}
		if audience == "patient" {
			prefix = "/api/v1/me/cases"
			middleware = append(middleware, a.bookingAvailable, a.authenticatePatient)
		}
		group := r.Group(prefix, middleware...)
		group.GET("", a.caseList(audience))
		group.GET("/:resourceId", a.caseDetail(audience))
		group.GET("/:resourceId/images/:imageId/content", a.caseImage(audience))
		if audience == "patient" {
			group.POST("/:resourceId/consent", a.caseChange("consent"))
		}
		if audience == "admin" {
			group.POST("", a.caseChange("create"))
			group.PUT("/:resourceId", a.caseChange("update"))
			group.GET("/:resourceId/history", a.caseHistory)
			for _, action := range []string{"review", "publish", "unpublish"} {
				group.POST("/:resourceId/"+action, a.caseChange(action))
			}
		}
	}
}
func casePatient(c *gin.Context, audience string) string {
	if audience == "patient" {
		return currentPatient(c).ID
	}
	return ""
}
func caseFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrPhotoConsent):
		fail(c, 403, "PHOTO_CONSENT_REQUIRED", "关联诊疗图片须有当前诊疗照片授权；案例展示仍需独立授权")
	case errors.Is(err, store.ErrForbidden):
		fail(c, 403, "FORBIDDEN", "无操作权限；审核须为在岗医生")
	case errors.Is(err, store.ErrNotFound):
		fail(c, 404, "NOT_FOUND", "案例或图片不存在、已撤回或无权访问")
	case errors.Is(err, store.ErrConflict):
		fail(c, 409, "VERSION_CONFLICT", "案例已变化，请刷新后重试")
	case errors.Is(err, store.ErrCareState):
		fail(c, 409, "CASE_STATE", "请检查治疗前后图片关联、独立展示授权和医生审核状态")
	default:
		fail(c, 503, "SERVICE_UNAVAILABLE", "案例或图片服务暂不可用")
	}
}
func (a *API) caseList(audience string) gin.HandlerFunc {
	return func(c *gin.Context) {
		keys := []string{"page", "pageSize", "clinicId", "category", "tag"}
		if audience == "admin" {
			keys = append(keys, "status")
		}
		if !validQuery(c, keys...) {
			return
		}
		f := store.CaseFilter{Page: 1, PageSize: 20, ClinicID: c.Query("clinicId"), Status: c.Query("status"), Category: strings.TrimSpace(c.Query("category")), Tag: strings.TrimSpace(c.Query("tag"))}
		if !textLength(f.Category, 0, 40) || !textLength(f.Tag, 0, 30) {
			fail(c, 400, "INVALID_QUERY", "分类或标签过长")
			return
		}
		if f.Status != "" && f.Status != "published" && f.Status != "unpublished" {
			fail(c, 400, "INVALID_QUERY", "案例状态无效")
			return
		}
		for key, target := range map[string]*int{"page": &f.Page, "pageSize": &f.PageSize} {
			if v := c.Query(key); v != "" {
				n, err := strconv.Atoi(v)
				if err != nil || n < 1 || n > 1000000 || key == "pageSize" && n > 100 {
					fail(c, 400, "INVALID_QUERY", "分页参数无效")
					return
				}
				*target = n
			}
		}
		rows, total, err := a.repo.(store.CaseRepository).Cases(c.Request.Context(), audience, casePatient(c, audience), f)
		if err != nil {
			caseFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": rows, "meta": gin.H{"page": f.Page, "pageSize": f.PageSize, "total": total}})
	}
}
func (a *API) caseDetail(audience string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !directoryID(c) || !validQuery(c) {
			return
		}
		row, err := a.repo.(store.CaseRepository).Case(c.Request.Context(), c.Param("resourceId"), audience, casePatient(c, audience))
		if err != nil {
			caseFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": row})
	}
}
func (a *API) caseChange(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !validQuery(c) || action != "create" && !directoryID(c) {
			return
		}
		required := []string{"version"}
		switch action {
		case "create":
			required = []string{"appointmentId", "title", "summary", "body", "pairs"}
		case "update":
			required = append(required, "title", "summary", "body", "pairs")
		case "review":
			required = append(required, "decision", "note")
		case "consent":
			required = append(required, "granted", "policyVersion")
		}
		var optional []string
		if action == "create" || action == "update" {
			optional = []string{"category", "tags"}
		}
		var in store.CaseChange
		if !decodeObjectLimit(c, &in, required, optional, 262144) {
			return
		}
		in.Title, in.Summary, in.Body, in.Note = strings.TrimSpace(in.Title), strings.TrimSpace(in.Summary), strings.TrimSpace(in.Body), strings.TrimSpace(in.Note)
		valid := action == "create" || in.Version > 0
		if action == "create" || action == "update" {
			valid = valid && store.NormalizeCaseMetadata(&in) && textLength(in.Title, 1, 200) && textLength(in.Summary, 0, 1000) && textLength(in.Body, 1, 20000) && len(in.Pairs) > 0 && len(in.Pairs) <= 3
			for _, pair := range in.Pairs {
				valid = valid && pair.BeforeSourceID != "" && validID(pair.BeforeSourceID) && pair.AfterSourceID != "" && validID(pair.AfterSourceID) && pair.BeforeImageID == "" && pair.AfterImageID == "" && pair.Angle == "" && pair.BeforeAt.IsZero() && pair.AfterAt.IsZero()
			}
			if action == "create" {
				valid = valid && in.AppointmentID != "" && validID(in.AppointmentID)
			}
		}
		if action == "review" {
			valid = valid && (in.Decision == "approved" || in.Decision == "rejected") && textLength(in.Note, 0, 1000) && (in.Decision != "rejected" || in.Note != "")
		}
		if action == "consent" {
			valid = valid && in.PolicyVersion == store.CasePolicyVersion
		}
		if !valid {
			fail(c, 400, "INVALID_BODY", "案例字段、版本、图片关联或授权版本无效")
			return
		}
		patient := ""
		if action == "consent" {
			patient = currentPatient(c).ID
		}
		row, err := a.repo.(store.CaseRepository).ChangeCase(c.Request.Context(), c.Param("resourceId"), action, patient, in, a.media, a.now())
		if err != nil {
			caseFailure(c, err)
			return
		}
		status := 200
		if action == "create" {
			status = 201
		}
		c.JSON(status, gin.H{"data": row})
	}
}
func (a *API) caseHistory(c *gin.Context) {
	if !directoryID(c) || !validQuery(c) {
		return
	}
	rows, err := a.repo.(store.CaseRepository).CaseHistory(c.Request.Context(), c.Param("resourceId"))
	if err != nil {
		caseFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": rows})
}
func (a *API) caseImage(audience string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !directoryID(c) || !validQuery(c) {
			return
		}
		id := c.Param("imageId")
		if id == "" || !validID(id) {
			fail(c, 400, "INVALID_PATH", "图片编号无效")
			return
		}
		if a.media == nil {
			fail(c, 503, "SERVICE_UNAVAILABLE", "图片服务暂不可用")
			return
		}
		repo := a.repo.(store.CaseRepository)
		row, err := repo.CaseImage(c.Request.Context(), c.Param("resourceId"), id, audience, casePatient(c, audience))
		if err != nil {
			caseFailure(c, err)
			return
		}
		data, err := a.media.Read(c.Request.Context(), id)
		if err != nil {
			caseFailure(c, err)
			return
		}
		if err := store.VerifyMedia(data, store.MediaReference{ID: id, SHA256: row.SHA256, SizeBytes: row.SizeBytes}); err != nil {
			caseFailure(c, err)
			return
		}
		// Recheck after storage I/O so withdrawal during a slow read stops delivery.
		if _, err := repo.CaseImage(c.Request.Context(), c.Param("resourceId"), id, audience, casePatient(c, audience)); err != nil {
			caseFailure(c, err)
			return
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(200, "image/jpeg", data)
	}
}
