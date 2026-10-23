package httpapi

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) registerDirectoryRoutes(r *gin.Engine) {
	for _, kind := range []string{"clinics", "doctors"} {
		group := r.Group("/api/v1/admin/"+kind, a.authenticate, a.requireManager)
		group.Use(func(c *gin.Context) {
			c.Set("directoryKind", kind)
			c.Header("Cache-Control", "no-store")
			if _, ok := a.repo.(store.DirectoryRepository); !ok {
				fail(c, 503, "SERVICE_UNAVAILABLE", "资料管理暂不可用")
				return
			}
			c.Next()
		})
		group.GET("", a.directoryList)
		group.POST("", a.directorySave("created"))
		group.PUT("/:resourceId", a.directorySave("updated"))
		group.POST("/:resourceId/status", a.directorySave("status"))
		group.POST("/:resourceId/publish", a.directorySave("published"))
		group.GET("/:resourceId/history", a.directoryHistory)
	}
	r.POST("/api/v1/admin/doctors/:resourceId/employee", a.authenticate, a.requireManager, a.linkDoctorEmployee)
	r.POST("/api/v1/admin/doctors/:resourceId/review", a.authenticate, a.requireManager, a.reviewDoctor)
}
func directoryFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrDoctorQualifications):
		fail(c, 409, "QUALIFICATION_REVIEW_REQUIRED", "正式医生资料须填写完整证照并通过资质审核后发布")
	case errors.Is(err, store.ErrNotFound):
		fail(c, 404, "NOT_FOUND", "资料不存在或无权访问")
	case errors.Is(err, store.ErrConflict):
		fail(c, 409, "VERSION_CONFLICT", "资料已变化，请刷新后重试")
	case errors.Is(err, store.ErrCareState):
		fail(c, 409, "DIRECTORY_STATE", "当前状态不允许操作；医生院区及已有人员身份不可变更")
	case errors.Is(err, store.ErrAccountExists):
		fail(c, 409, "ACCOUNT_EXISTS", "该医生已关联员工账号")
	default:
		fail(c, 503, "SERVICE_UNAVAILABLE", "资料管理暂不可用")
	}
}
func (a *API) directoryList(c *gin.Context) {
	if !validQuery(c) {
		return
	}
	rows, err := a.repo.(store.DirectoryRepository).DirectoryList(c.Request.Context(), c.GetString("directoryKind"))
	if err != nil {
		directoryFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": rows})
}
func directoryID(c *gin.Context) bool {
	if c.Param("resourceId") == "" || !validID(c.Param("resourceId")) {
		fail(c, 400, "INVALID_ID", "标识格式无效")
		return false
	}
	return true
}
func (a *API) directoryHistory(c *gin.Context) {
	if !directoryID(c) || !validQuery(c) {
		return
	}
	rows, err := a.repo.(store.DirectoryRepository).DirectoryHistory(c.Request.Context(), c.GetString("directoryKind"), c.Param("resourceId"))
	if err != nil {
		directoryFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": rows})
}
func textLength(value string, min, max int) bool {
	n := utf8.RuneCountInString(value)
	return n >= min && n <= max
}
func validAvatar(value string) bool {
	if value == "" {
		return true
	}
	u, err := url.Parse(value)
	return err == nil && len(value) <= 2048 && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}
func (a *API) directorySave(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		kind := c.GetString("directoryKind")
		if action != "created" && !directoryID(c) {
			return
		}
		var clinic model.Clinic
		var doctor model.Doctor
		version := 0
		active := false
		if action == "status" || action == "published" {
			var body struct {
				Version int  `json:"version"`
				Active  bool `json:"active"`
			}
			required := []string{"version"}
			if action == "status" {
				required = append(required, "active")
			}
			if !decodeObject(c, &body, required, nil) {
				return
			}
			version, active = body.Version, body.Active
		} else {
			fields := []string{"name", "address", "phone"}
			var target any = &clinic
			if kind == "doctors" {
				fields = []string{"clinicId", "name", "title", "summary", "focus", "initials", "avatarUrl", "serviceIntroduction"}
				target = &doctor
			}
			if action == "updated" {
				fields = append(fields, "version")
			}
			if !decodeObjectLimit(c, target, fields, []string{"presentation"}, 131072) {
				return
			}
			valid := false
			if kind == "clinics" {
				clinic.Name, clinic.Address, clinic.Phone = strings.TrimSpace(clinic.Name), strings.TrimSpace(clinic.Address), strings.TrimSpace(clinic.Phone)
				valid = textLength(clinic.Name, 1, 120) && textLength(clinic.Address, 1, 255) && textLength(clinic.Phone, 1, 32)
				valid = valid && validClinicPresentation(clinic.Presentation)
				version = clinic.Version
			} else {
				doctor.Name, doctor.Title, doctor.Summary, doctor.Initials, doctor.ServiceIntroduction = strings.TrimSpace(doctor.Name), strings.TrimSpace(doctor.Title), strings.TrimSpace(doctor.Summary), strings.TrimSpace(doctor.Initials), strings.TrimSpace(doctor.ServiceIntroduction)
				valid = doctor.ClinicID != "" && validID(doctor.ClinicID) && textLength(doctor.Name, 1, 80) && textLength(doctor.Title, 1, 80) && textLength(doctor.Summary, 1, 500) && textLength(doctor.Initials, 1, 16) && textLength(doctor.ServiceIntroduction, 0, 5000) && validAvatar(doctor.AvatarURL) && len(doctor.Focus) <= 20
				for i, item := range doctor.Focus {
					doctor.Focus[i] = strings.TrimSpace(item)
					valid = valid && textLength(doctor.Focus[i], 1, 80)
				}
				version = doctor.Version
				valid = valid && (doctor.Presentation == nil || validCredentials(doctor.Presentation.Credentials))
			}
			if !valid {
				fail(c, 400, "INVALID_PROFILE", "资料字段为空、超长或头像地址无效（需 HTTPS）")
				return
			}
		}
		if action != "created" && version < 1 {
			fail(c, 400, "INVALID_VERSION", "需要有效版本号")
			return
		}
		row, err := a.repo.(store.DirectoryRepository).SaveDirectory(c.Request.Context(), kind, c.Param("resourceId"), version, action, clinic, doctor, active, a.now())
		if err != nil {
			directoryFailure(c, err)
			return
		}
		status := 200
		if action == "created" {
			status = 201
		}
		c.JSON(status, gin.H{"data": row})
	}
}
func (a *API) linkDoctorEmployee(c *gin.Context) {
	repo, ok := a.repo.(store.DirectoryRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "资料管理暂不可用")
		return
	}
	if !directoryID(c) {
		return
	}
	var body struct {
		EmployeeID      string `json:"employeeId"`
		Version         int    `json:"version"`
		EmployeeVersion int    `json:"employeeVersion"`
	}
	if !decodeObject(c, &body, []string{"employeeId", "version", "employeeVersion"}, nil) {
		return
	}
	if body.EmployeeID == "" || !validID(body.EmployeeID) || body.Version < 1 || body.EmployeeVersion < 1 {
		fail(c, 400, "INVALID_BODY", "账号或版本号无效")
		return
	}
	row, err := repo.LinkDoctorEmployee(c.Request.Context(), c.Param("resourceId"), body.EmployeeID, body.Version, body.EmployeeVersion, a.now())
	if err != nil {
		directoryFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": employeeDTO(row)})
}
