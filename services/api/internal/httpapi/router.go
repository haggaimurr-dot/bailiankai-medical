package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

type API struct {
	assessments *store.AssessmentService
	repo        store.Repository
	booking     store.BookingRepository
	care        store.CareRepository
	followup    store.FollowupRepository
	employees   store.EmployeeRepository
	media       store.Media
	now         func() time.Time
}

func New(repo store.Repository, _ string) *gin.Engine {
	booking, _ := repo.(store.BookingRepository)
	care, _ := repo.(store.CareRepository)
	followup, _ := repo.(store.FollowupRepository)
	employees, _ := repo.(store.EmployeeRepository)
	return newRouter(&API{repo: repo, booking: booking, care: care, followup: followup, employees: employees, now: time.Now})
}

func NewWithMediaRoot(repo store.Repository, _, mediaRoot string) (*gin.Engine, error) {
	media, err := store.NewPrivateMedia(mediaRoot)
	if err != nil {
		return nil, err
	}
	return NewWithMedia(repo, media), nil
}

func NewWithMedia(repo store.Repository, media store.Media) *gin.Engine {
	booking, _ := repo.(store.BookingRepository)
	care, _ := repo.(store.CareRepository)
	followup, _ := repo.(store.FollowupRepository)
	employees, _ := repo.(store.EmployeeRepository)
	return newRouter(&API{repo: repo, booking: booking, care: care, followup: followup, employees: employees, media: media, now: time.Now})
}

func NewWithAssessmentService(repo store.Repository, media store.Media, assessments *store.AssessmentService) *gin.Engine {
	booking, _ := repo.(store.BookingRepository)
	care, _ := repo.(store.CareRepository)
	followup, _ := repo.(store.FollowupRepository)
	employees, _ := repo.(store.EmployeeRepository)
	return newRouter(&API{repo: repo, booking: booking, care: care, followup: followup, employees: employees, media: media, assessments: assessments, now: time.Now})
}

func newRouter(a *API) *gin.Engine {
	if a.assessments == nil {
		if repo, ok := a.repo.(*store.MySQL); ok {
			a.assessments = store.NewAssessmentService(repo, a.media, nil, store.AssessmentPipelineConfig{})
		}
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.CustomRecovery(func(c *gin.Context, _ any) {
		fail(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "服务暂不可用，请稍后重试")
	}), cors(), requestTimeout())
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, apitypes.Health{Status: apitypes.Ok}) })
	r.GET("/readyz", func(c *gin.Context) {
		if a.media != nil {
			if err := a.media.Ready(c.Request.Context()); err != nil {
				a.mediaUnavailable(c)
				return
			}
		}
		if err := a.repo.Ready(c.Request.Context()); err != nil {
			a.unavailable(c, err)
			return
		}
		c.JSON(http.StatusOK, apitypes.Health{Status: apitypes.Ok})
	})
	r.GET("/api/v1/privacy-policy", a.privacyPolicy)
	r.GET("/api/v1/clinics", a.clinics)
	r.GET("/api/v1/doctors", a.doctors)
	r.GET("/api/v1/slots", a.slots)
	r.POST("/api/v1/admin/session", a.loginEmployee)
	r.GET("/api/v1/admin/session", a.authenticate, a.employeeSession)
	r.GET("/api/v1/admin/handoff-notifications", a.authenticate, a.handoffNotifications)
	r.POST("/api/v1/admin/handoff-notifications/:notificationId/read", a.authenticate, a.readHandoffNotification)
	r.GET("/api/v1/admin/permissions", a.authenticate, a.employeePermissions)
	r.POST("/api/v1/admin/session/password", a.authenticate, a.changeEmployeePassword)
	r.POST("/api/v1/admin/employees/:employeeId/access", a.authenticate, a.requireManager, a.setEmployeeAccess)
	r.GET("/api/v1/admin/overview", a.authenticate, a.overview)
	r.GET("/api/v1/admin/analytics", a.authenticate, a.analytics)
	r.DELETE("/api/v1/admin/session", a.authenticate, a.logoutEmployee)
	r.GET("/api/v1/admin/employees", a.authenticate, a.requireManager, a.listEmployees)
	r.GET("/api/v1/admin/employees/:employeeId/events", a.authenticate, a.requireManager, a.employeeEvents)
	r.POST("/api/v1/admin/employees", a.authenticate, a.requireManager, a.createEmployee)
	r.POST("/api/v1/admin/employees/:employeeId/status", a.authenticate, a.requireManager, a.setEmployeeStatus)
	r.POST("/api/v1/admin/employees/:employeeId/password", a.authenticate, a.requireManager, a.resetEmployeePassword)
	r.POST("/api/v1/admin/employees/:employeeId/clinics", a.authenticate, a.requireManager, a.setEmployeeClinics)
	r.PATCH("/api/v1/admin/slots/:slotId", a.authenticate, a.requireManager, a.updateSlot)
	a.registerBookingRoutes(r)
	a.registerScheduleRoutes(r)
	a.registerDirectoryRoutes(r)
	a.registerKnowledgeRoutes(r)
	a.registerFollowupToolsRoutes(r)
	a.registerCaseRoutes(r)
	a.registerNotificationRoutes(r)
	a.registerSupportRoutes(r)
	a.registerCareRoutes(r)
	a.registerFollowupRoutes(r)
	a.registerPatientRecordRoutes(r)
	a.registerPatientTimelineRoutes(r)
	a.registerRecordExportRoutes(r)
	a.registerAssessmentRoutes(r)
	r.NoRoute(func(c *gin.Context) { fail(c, http.StatusNotFound, "NOT_FOUND", "接口不存在") })
	r.HandleMethodNotAllowed = true
	r.NoMethod(func(c *gin.Context) {
		fail(c, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "请求方法不支持")
	})
	return r
}

func (a *API) registerCareRoutes(r *gin.Engine) {
	r.GET("/api/v1/admin/care-plan-templates", a.authenticate, a.careTemplates)
	r.POST("/api/v1/admin/care-plan-templates", a.authenticate, a.careTemplates)
	r.DELETE("/api/v1/admin/care-plan-templates/:templateId", a.authenticate, a.careTemplates)
	patient := r.Group("/api/v1/me", a.bookingAvailable, a.authenticatePatient, a.careAvailable)
	patient.GET("/appointments/:appointmentId/photo-consent", a.getPhotoConsent)
	patient.POST("/appointments/:appointmentId/photo-consent", a.setPhotoConsent)
	patient.GET("/appointments/:appointmentId/photos", a.patientCarePhotos)
	patient.POST("/appointments/:appointmentId/photos/:angle", a.uploadCarePhoto)
	patient.GET("/photos/:photoId/content", a.patientCarePhotoContent)
	patient.GET("/appointments/:appointmentId/care-plan", a.patientCarePlan)
	admin := r.Group("/api/v1/admin/appointments", a.authenticate, a.careAvailable)
	admin.GET("/:appointmentId/photos", a.requireAppointmentScope, a.adminCarePhotos)
	admin.GET("/:appointmentId/care-plan", a.requireAppointmentScope, a.adminCarePlan)
	admin.PUT("/:appointmentId/care-plan/draft", a.requireDoctorScope, a.saveCarePlanDraft)
	admin.POST("/:appointmentId/care-plan/publish", a.requireDoctorScope, a.publishCarePlan)
	r.GET("/api/v1/admin/photos/:photoId/content", a.authenticate, a.careAvailable, a.requireCarePhotoScope, a.adminCarePhotoContent)
}

func (a *API) careAvailable(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if a.care == nil || a.media == nil {
		fail(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "面诊资料服务暂不可用")
		return
	}
	c.Next()
}

func requestTimeout() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			c.Writer.Header().Add("Vary", "Origin")
			u, err := url.Parse(origin)
			allowed := err == nil && u.Scheme == "http" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
			if (os.Getenv("APP_ENV") == "cloudrun" || os.Getenv("APP_ENV") == "server") && err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" {
				for _, configured := range strings.Split(os.Getenv("ADMIN_ALLOWED_ORIGINS"), ",") {
					if origin == strings.TrimSpace(configured) {
						allowed = true
					}
				}
			}
			if !allowed {
				fail(c, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "当前页面来源未获允许")
				return
			}
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")
			c.Header("Access-Control-Max-Age", "600")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func fail(c *gin.Context, status int, code, message string) {
	var response apitypes.ApiError
	response.Error.Code, response.Error.Message = code, message
	c.AbortWithStatusJSON(status, response)
}

func (a *API) unavailable(c *gin.Context, err error) {
	slog.Error("database request failed", "method", c.Request.Method, "path", c.FullPath(), "error", err)
	fail(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "数据库暂不可用，请稍后重试")
}

func (a *API) authenticate(c *gin.Context) {
	const prefix = "Bearer "
	header := c.GetHeader("Authorization")
	if a.employees == nil || !strings.HasPrefix(header, prefix) {
		fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "请登录员工账号")
		return
	}
	employee, err := a.employees.AuthenticateEmployee(c.Request.Context(), strings.TrimPrefix(header, prefix), a.now())
	if err != nil {
		fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "员工登录已失效，请重新登录")
		return
	}
	c.Set("employee", employee)
	staffID := ""
	if employee.StaffID != nil {
		staffID = *employee.StaffID
	}
	ctx := store.WithEmployeeScope(c.Request.Context(), employee.Role, staffID, employee.ClinicIDs)
	c.Request = c.Request.WithContext(store.WithEmployeeActor(ctx, employee.ID))
	if !allowedEmployeeOperation(employee.Role, c.Request.Method, c.FullPath()) {
		fail(c, http.StatusForbidden, "FORBIDDEN", "当前角色无权执行此操作")
		return
	}
	c.Next()
}

func currentEmployee(c *gin.Context) model.EmployeeAccount {
	return c.MustGet("employee").(model.EmployeeAccount)
}
func (a *API) requireManager(c *gin.Context) {
	if currentEmployee(c).Role != "manager" {
		fail(c, http.StatusForbidden, "FORBIDDEN", "仅主管可执行此操作")
		return
	}
	c.Next()
}

func validID(id string) bool {
	if len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func validQuery(c *gin.Context, keys ...string) bool {
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		allowed[key] = true
	}
	for key, values := range c.Request.URL.Query() {
		if !allowed[key] || len(values) != 1 || values[0] == "" {
			fail(c, http.StatusBadRequest, "INVALID_QUERY", "查询参数无效")
			return false
		}
		if key != "date" && key != "q" && key != "category" && key != "tag" && !validID(values[0]) {
			fail(c, http.StatusBadRequest, "INVALID_QUERY", "查询标识格式无效")
			return false
		}
	}
	return true
}

func (a *API) clinics(c *gin.Context) {
	if !validQuery(c) {
		return
	}
	rows, err := a.repo.Clinics(c.Request.Context())
	if err != nil {
		a.unavailable(c, err)
		return
	}
	data := make([]apitypes.Clinic, 0, len(rows))
	for _, row := range rows {
		data = append(data, clinicDTO(row))
	}
	c.JSON(http.StatusOK, apitypes.ClinicList{Data: data})
}

func (a *API) doctors(c *gin.Context) {
	if !validQuery(c, "clinicId") {
		return
	}
	rows, err := a.repo.Doctors(c.Request.Context(), c.Query("clinicId"))
	if err != nil {
		a.unavailable(c, err)
		return
	}
	data := make([]apitypes.Doctor, 0, len(rows))
	for _, row := range rows {
		data = append(data, doctorDTO(row))
	}
	c.JSON(http.StatusOK, apitypes.DoctorList{Data: data})
}

func (a *API) slots(c *gin.Context) {
	if !validQuery(c, "clinicId", "doctorId", "date") {
		return
	}
	f := store.SlotFilter{ClinicID: c.Query("clinicId"), DoctorID: c.Query("doctorId"), Now: a.now().In(database.Shanghai)}
	if date := c.Query("date"); date != "" {
		parsed, err := time.ParseInLocation("2006-01-02", date, database.Shanghai)
		if err != nil || parsed.Format("2006-01-02") != date {
			fail(c, http.StatusBadRequest, "INVALID_QUERY", "日期必须是有效的 YYYY-MM-DD 格式")
			return
		}
		f.Date = parsed
	}
	rows, err := a.repo.Slots(c.Request.Context(), f)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	data := make([]apitypes.Slot, 0, len(rows))
	for _, row := range rows {
		data = append(data, slotDTO(row))
	}
	c.JSON(http.StatusOK, apitypes.SlotList{Data: data})
}

func (a *API) updateSlot(c *gin.Context) {
	id := c.Param("slotId")
	if id == "" || !validID(id) {
		fail(c, http.StatusBadRequest, "INVALID_SLOT_ID", "排班标识格式无效")
		return
	}
	if c.ContentType() != "application/json" {
		fail(c, http.StatusBadRequest, "INVALID_BODY", "请求内容必须是 application/json")
		return
	}
	var raw json.RawMessage
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(&raw); err != nil {
		fail(c, http.StatusBadRequest, "INVALID_BODY", "排班修改参数无效")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		fail(c, http.StatusBadRequest, "INVALID_BODY", "请求只能包含一个 JSON 对象")
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 3 {
		fail(c, http.StatusBadRequest, "INVALID_BODY", "需提供容量、排班状态和有效版本号")
		return
	}
	for _, key := range []string{"capacity", "status", "version"} {
		value, exists := fields[key]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			fail(c, http.StatusBadRequest, "INVALID_BODY", "需提供容量、排班状态和有效版本号")
			return
		}
	}
	var body apitypes.UpdateSlotJSONRequestBody
	if err := json.Unmarshal(raw, &body); err != nil || body.Capacity < 0 || body.Capacity > 100 || body.Version < 1 || (body.Status != "open" && body.Status != "closed") {
		fail(c, http.StatusBadRequest, "INVALID_BODY", "需提供容量（0–100）、排班状态和有效版本号")
		return
	}
	row, err := a.repo.UpdateSlot(c.Request.Context(), id, store.SlotUpdate{Capacity: int(body.Capacity), Status: string(body.Status), Version: int(body.Version)}, a.now().In(database.Shanghai))
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(c, http.StatusNotFound, "SLOT_NOT_FOUND", "排班不存在")
	case errors.Is(err, store.ErrConflict):
		fail(c, http.StatusConflict, "VERSION_CONFLICT", "排班已被其他操作修改，请刷新后重试")
	case errors.Is(err, store.ErrCapacity):
		fail(c, http.StatusConflict, "CAPACITY_BELOW_RESERVED", "容量不能小于已占用名额")
	case errors.Is(err, store.ErrPast):
		fail(c, http.StatusConflict, "SLOT_ALREADY_STARTED", "已开始的时段不能修改")
	case err != nil:
		a.unavailable(c, err)
	default:
		c.JSON(http.StatusOK, apitypes.SlotResult{Data: slotDTO(row)})
	}
}
