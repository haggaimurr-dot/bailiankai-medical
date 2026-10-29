package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
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

var phonePattern = regexp.MustCompile(`^1[3-9][0-9]{9}$`)
var requestKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,80}$`)

func (a *API) registerBookingRoutes(r *gin.Engine) {
	booking := r.Group("/api/v1", a.bookingAvailable)
	if os.Getenv("APP_ENV") != "cloudrun" && os.Getenv("APP_ENV") != "server" {
		booking.POST("/dev/patient-sessions", a.createPatientSession)
	}
	booking.POST("/patient/session", a.loginPatient)
	me := booking.Group("/me", a.authenticatePatient)
	me.GET("", a.me)
	me.POST("/patient-binding/preview", a.previewPatientBinding)
	me.POST("/patient-binding", a.bindPatient)
	me.GET("/privacy-consent", a.privacyConsent)
	me.POST("/privacy-consent", a.setPrivacyConsent)
	me.PUT("/profile", a.updatePatientProfile)
	me.POST("/phone", a.authorizePatientPhone)
	me.POST("/phone/revoke", a.revokePatientPhone)
	me.POST("/logout", a.logoutPatient)
	me.POST("/appointments", a.createAppointment)
	me.GET("/appointments", a.myAppointments)
	me.GET("/appointments/by-request/:requestKey", a.appointmentByRequest)
	me.GET("/appointments/:appointmentId", a.myAppointment)
	me.GET("/appointments/:appointmentId/timeline", a.appointmentTimeline)
	me.POST("/appointments/:appointmentId/reschedule", a.rescheduleAppointment)
	me.POST("/appointments/:appointmentId/check-in-code", a.newCheckInCode)
	me.POST("/appointments/:appointmentId/cancel", a.cancelAppointment)
	admin := booking.Group("/admin/appointments", a.authenticate)
	admin.GET("", a.adminAppointments)
	admin.GET("/cancellation-stats", a.cancellationStats)
	admin.GET("/:appointmentId", a.requireAppointmentScope, a.adminAppointment)
	admin.POST("/remind", a.remindAppointments)
	admin.POST("/:appointmentId/review", a.requireAppointmentScope, a.reviewAppointment)
	admin.POST("/:appointmentId/arrival", a.requireAppointmentScope, a.registerArrival)
	admin.POST("/:appointmentId/suspension-reschedule", a.requireAppointmentScope, a.suspensionReschedule)
	scans := booking.Group("/admin/arrival-scans", a.authenticate)
	scans.POST("/resolve", a.scanCheckInCode)
	scans.POST("/confirm", a.scanCheckInCode)
}

func (a *API) bookingAvailable(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if a.booking == nil {
		fail(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "预约服务暂不可用")
		return
	}
	c.Next()
}

// Decode the generated contract type, while also enforcing required/non-null
// properties (encoding/json alone treats missing scalar values as zero).
func decodeObject(c *gin.Context, target any, required, optional []string) bool {
	return decodeObjectLimit(c, target, required, optional, 8192)
}

func decodeObjectLimit(c *gin.Context, target any, required, optional []string, maxBytes int64) bool {
	if c.ContentType() != "application/json" {
		fail(c, 400, "INVALID_BODY", "请求内容必须是 application/json")
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
	d := json.NewDecoder(c.Request.Body)
	var raw json.RawMessage
	if err := d.Decode(&raw); err != nil {
		fail(c, 400, "INVALID_BODY", "请求内容无效")
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		fail(c, 400, "INVALID_BODY", "请求只能包含一个 JSON 对象")
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		fail(c, 400, "INVALID_BODY", "请求必须是 JSON 对象")
		return false
	}
	allowed := make(map[string]bool)
	for _, k := range append(append([]string{}, required...), optional...) {
		allowed[k] = true
	}
	for k, v := range fields {
		if !allowed[k] || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			fail(c, 400, "INVALID_BODY", "请求包含无效字段")
			return false
		}
	}
	for _, k := range required {
		if _, ok := fields[k]; !ok {
			fail(c, 400, "INVALID_BODY", "请求缺少必要字段")
			return false
		}
	}
	if err := json.Unmarshal(raw, target); err != nil {
		fail(c, 400, "INVALID_BODY", "请求字段类型无效")
		return false
	}
	return true
}

func patientIdentity(row model.Patient) apitypes.PatientIdentity {
	kind := apitypes.PatientIdentityLoginKind("demo")
	if row.WechatOpenID != nil {
		kind = apitypes.PatientIdentityLoginKind("wechat")
	}
	status := apitypes.PatientIdentityPhoneAuthorization(row.PhoneAuthorization)
	if status == "" {
		status = apitypes.PatientIdentityPhoneAuthorization("not_granted")
	}
	version := int32(row.Version)
	if version < 1 {
		version = 1
	}
	return apitypes.PatientIdentity{Id: row.ID, DisplayName: row.DisplayName, LoginKind: &kind, Phone: &row.Phone, PhoneAuthorization: &status, PhoneAuthorizedAt: row.PhoneAuthorizedAt, Version: &version}
}

func (a *API) createPatientSession(c *gin.Context) {
	var body apitypes.CreateDevPatientSessionJSONRequestBody
	if !decodeObject(c, &body, nil, nil) {
		return
	}
	result, err := a.booking.NewPatientSession(c.Request.Context(), a.now())
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(http.StatusCreated, apitypes.PatientSessionResult{Data: apitypes.PatientSession{Token: result.Token, ExpiresAt: result.ExpiresAt.In(database.Shanghai), Patient: patientIdentity(result.Patient)}})
}

func (a *API) authenticatePatient(c *gin.Context) {
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		fail(c, 401, "PATIENT_SESSION_INVALID", "登录已失效，请重新登录")
		return
	}
	token := strings.TrimPrefix(header, "Bearer ")
	patient, err := a.booking.AuthenticatePatient(c.Request.Context(), token, a.now())
	if errors.Is(err, store.ErrUnauthenticated) {
		fail(c, 401, "PATIENT_SESSION_INVALID", "登录已失效，请重新登录")
		return
	}
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.Set("patient", patient)
	if requiresPrivacyConsent(c.Request.Method, c.FullPath()) {
		repo, ok := a.repo.(store.PrivacyRepository)
		if !ok {
			fail(c, 503, "SERVICE_UNAVAILABLE", "隐私服务暂不可用")
			return
		}
		events, err := repo.PrivacyConsentEvents(c.Request.Context(), patient.ID)
		if err != nil {
			a.unavailable(c, err)
			return
		}
		if len(events) == 0 || events[0].Action != "accepted" || events[0].PolicyVersion != store.PrivacyPolicyVersion {
			fail(c, 403, "PRIVACY_CONSENT_REQUIRED", "请阅读并同意当前隐私说明后再提交资料")
			return
		}
	}
	c.Next()
}

func currentPatient(c *gin.Context) model.Patient { return c.MustGet("patient").(model.Patient) }
func (a *API) me(c *gin.Context) {
	c.JSON(200, apitypes.PatientIdentityResult{Data: patientIdentity(currentPatient(c))})
}

func appointmentDTO(row model.Appointment, now time.Time, admin bool) apitypes.Appointment {
	active := row.Status == "waiting" || row.Status == "pending" || row.Status == "confirmed"
	future := row.StartsAt.After(now)
	pendingValid := row.Status == "pending" && row.ConfirmationDeadline.After(now)
	canRegister := admin && arrivalAllowed(row)
	canReschedule := !admin && active && future && row.ArrivedAt == nil && !row.TreatmentRecorded && (row.Status != "pending" || pendingValid)
	var cancelledAt *time.Time
	if row.CancelledAt != nil {
		t := row.CancelledAt.In(database.Shanghai)
		cancelledAt = &t
	}
	var cancelReason *apitypes.AppointmentCancelReason
	if row.CancelReason != "" {
		v := apitypes.AppointmentCancelReason(row.CancelReason)
		cancelReason = &v
	}
	return apitypes.Appointment{Id: row.ID, PatientId: row.PatientID, SlotId: row.SlotID, ClinicId: row.ClinicID, ClinicName: row.ClinicName, DoctorId: row.DoctorID, DoctorName: row.DoctorName, StartsAt: row.StartsAt.In(database.Shanghai), EndsAt: row.EndsAt.In(database.Shanghai), ContactName: row.ContactName, ContactPhone: row.ContactPhone, Note: row.Note, Source: &row.Source, Status: apitypes.AppointmentStatus(row.Status), RejectionReason: row.RejectionReason, CancelReason: cancelReason, CancelReasonDetail: &row.CancelReasonDetail, CancelledAt: cancelledAt, ConfirmationDeadline: row.ConfirmationDeadline.In(database.Shanghai), CreatedAt: row.CreatedAt.In(database.Shanghai), UpdatedAt: row.UpdatedAt.In(database.Shanghai), Version: int32(row.Version), ArrivedAt: row.ArrivedAt, CanRegisterArrival: &canRegister, CanReschedule: &canReschedule, CanCancel: row.ArrivedAt == nil && !row.TreatmentRecorded && !admin && active && future && (row.Status != "pending" || pendingValid), SlotStatus: func() *apitypes.AppointmentSlotStatus {
		value := apitypes.AppointmentSlotStatus(row.SlotStatus)
		return &value
	}(), SuspensionReason: &row.SuspensionReason, CanReview: admin && pendingValid && future}
}

func (a *API) bookingError(c *gin.Context, err error, creating bool) {
	switch {
	case errors.Is(err, store.ErrForbidden):
		fail(c, 403, "FORBIDDEN", "当前账号无权处理该预约")
	case errors.Is(err, store.ErrNotFound):
		if creating {
			fail(c, 404, "SLOT_NOT_FOUND", "排班不存在")
		} else {
			fail(c, 404, "APPOINTMENT_NOT_FOUND", "预约不存在")
		}
	case errors.Is(err, store.ErrUnauthenticated):
		fail(c, 401, "PATIENT_SESSION_INVALID", "演示会话已失效，请重新建立身份")
	case errors.Is(err, store.ErrConflict):
		fail(c, 409, "VERSION_CONFLICT", "预约已被其他操作修改，请刷新后重试")
	case errors.Is(err, store.ErrIdempotency):
		fail(c, 409, "IDEMPOTENCY_CONFLICT", "该提交标识已用于另一份申请，请查询原申请结果")
	case errors.Is(err, store.ErrActiveAppointment):
		fail(c, 409, "ACTIVE_APPOINTMENT_EXISTS", "你已申请该时段，请在我的预约中查看")
	case errors.Is(err, store.ErrSlotFull):
		fail(c, 409, "SLOT_FULL", "该时段名额已满，请选择其他时段")
	case errors.Is(err, store.ErrSlotClosed):
		fail(c, 409, "SLOT_CLOSED", "该时段已关闭，请选择其他时段")
	case errors.Is(err, store.ErrPast):
		fail(c, 409, "SLOT_ALREADY_STARTED", "该时段已开始，请选择其他时段")
	case errors.Is(err, store.ErrTransition):
		fail(c, 409, "APPOINTMENT_STATE_CONFLICT", "当前预约状态不允许此操作，请刷新查看")
	default:
		a.unavailable(c, err)
	}
}

func (a *API) createAppointment(c *gin.Context) {
	key := c.GetHeader("Idempotency-Key")
	if !requestKeyPattern.MatchString(key) {
		fail(c, 400, "INVALID_IDEMPOTENCY_KEY", "需提供有效的 Idempotency-Key")
		return
	}
	var body apitypes.CreateAppointmentJSONRequestBody
	if !decodeObject(c, &body, []string{"slotId", "contactName", "contactPhone"}, []string{"note", "waitlist", "source"}) {
		return
	}
	input := store.AppointmentInput{SlotID: body.SlotId, ContactName: body.ContactName, ContactPhone: body.ContactPhone}
	if body.Note != nil {
		input.Note = *body.Note
	}
	if body.Waitlist != nil {
		input.Waitlist = *body.Waitlist
	}
	if body.Source != nil {
		input.Source = string(*body.Source)
	}
	input = store.NormalizeAppointmentInput(input)
	if input.SlotID == "" || !validID(input.SlotID) || utf8.RuneCountInString(input.ContactName) < 1 || utf8.RuneCountInString(input.ContactName) > 40 || !phonePattern.MatchString(input.ContactPhone) || utf8.RuneCountInString(input.Note) > 300 {
		fail(c, 400, "INVALID_BODY", "请填写有效的时段、姓名（1–40字）、大陆手机号和备注（最多300字）")
		return
	}
	row, replayed, err := a.booking.CreateAppointment(c.Request.Context(), currentPatient(c).ID, key, input, a.now())
	if err != nil {
		a.bookingError(c, err, true)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, apitypes.AppointmentResult{Data: appointmentDTO(row, a.now(), false)})
}

func validAppointmentID(c *gin.Context) bool {
	id := c.Param("appointmentId")
	if id == "" || !validID(id) {
		fail(c, 400, "INVALID_APPOINTMENT_ID", "预约标识格式无效")
		return false
	}
	return true
}

func (a *API) appointmentByRequest(c *gin.Context) {
	key := c.Param("requestKey")
	if !requestKeyPattern.MatchString(key) {
		fail(c, 400, "INVALID_IDEMPOTENCY_KEY", "提交标识格式无效")
		return
	}
	row, err := a.booking.AppointmentByRequest(c.Request.Context(), currentPatient(c).ID, key, a.now())
	if err != nil {
		a.bookingError(c, err, false)
		return
	}
	c.JSON(200, apitypes.AppointmentResult{Data: appointmentDTO(row, a.now(), false)})
}

func (a *API) myAppointment(c *gin.Context)    { a.getAppointment(c, false) }
func (a *API) adminAppointment(c *gin.Context) { a.getAppointment(c, true) }
func (a *API) getAppointment(c *gin.Context, admin bool) {
	if !validAppointmentID(c) {
		return
	}
	patientID := ""
	if !admin {
		patientID = currentPatient(c).ID
	}
	row, err := a.booking.GetAppointment(c.Request.Context(), c.Param("appointmentId"), patientID, a.now())
	if err != nil {
		a.bookingError(c, err, false)
		return
	}
	c.JSON(200, apitypes.AppointmentResult{Data: a.viewAppointmentDTO(c, row, admin)})
}

func parseAppointmentFilter(c *gin.Context, admin bool) (store.AppointmentFilter, bool) {
	keys := []string{"status", "page", "pageSize"}
	if admin {
		keys = append(keys, "date", "doctorId", "q", "clinicId", "arrival", "needsReschedule")
	}
	if !validQuery(c, keys...) {
		return store.AppointmentFilter{}, false
	}
	if admin && ((c.Query("arrival") != "" && c.Query("arrival") != "arrived" && c.Query("arrival") != "expected") || (c.Query("needsReschedule") != "" && c.Query("needsReschedule") != "true")) {
		fail(c, 400, "INVALID_QUERY", "到诊筛选无效")
		return store.AppointmentFilter{}, false
	}
	f := store.AppointmentFilter{ClinicID: c.Query("clinicId"), Arrival: c.Query("arrival"), NeedsReschedule: c.Query("needsReschedule") == "true", Query: strings.TrimSpace(c.Query("q")), Status: c.Query("status"), DoctorID: c.Query("doctorId"), Page: 1, PageSize: 20}
	if !textLength(f.Query, 0, 100) {
		fail(c, 400, "INVALID_QUERY", "搜索词最多100字")
		return f, false
	}
	if f.Status != "" && f.Status != "waiting" && f.Status != "pending" && f.Status != "confirmed" && f.Status != "completed" && f.Status != "rejected" && f.Status != "cancelled" && f.Status != "expired" {
		fail(c, 400, "INVALID_QUERY", "预约状态无效")
		return f, false
	}
	for key, target := range map[string]*int{"page": &f.Page, "pageSize": &f.PageSize} {
		if value := c.Query(key); value != "" {
			parsed, err := strconv.Atoi(value)
			limit := 100000
			if key == "pageSize" {
				limit = 100
			}
			if err != nil || parsed < 1 || parsed > limit {
				fail(c, 400, "INVALID_QUERY", "分页参数无效")
				return f, false
			}
			*target = parsed
		}
	}
	if value := c.Query("date"); value != "" {
		parsed, err := time.ParseInLocation("2006-01-02", value, database.Shanghai)
		if err != nil || parsed.Format("2006-01-02") != value {
			fail(c, 400, "INVALID_QUERY", "日期必须是有效的 YYYY-MM-DD 格式")
			return f, false
		}
		f.Date = parsed
	}
	return f, true
}

func (a *API) myAppointments(c *gin.Context)    { a.listAppointments(c, false) }
func (a *API) adminAppointments(c *gin.Context) { a.listAppointments(c, true) }
func (a *API) listAppointments(c *gin.Context, admin bool) {
	f, ok := parseAppointmentFilter(c, admin)
	if !ok {
		return
	}
	if !admin {
		f.PatientID = currentPatient(c).ID
	}
	result, err := a.booking.ListAppointments(c.Request.Context(), f, a.now())
	if err != nil {
		a.bookingError(c, err, false)
		return
	}
	data := make([]apitypes.Appointment, 0, len(result.Data))
	for _, row := range result.Data {
		data = append(data, a.viewAppointmentDTO(c, row, admin))
	}
	c.JSON(200, apitypes.AppointmentPage{Data: data, Meta: apitypes.PageMeta{Page: int32(result.Page), PageSize: int32(result.PageSize), Total: int32(result.Total)}})
}

func (a *API) cancelAppointment(c *gin.Context) {
	if !validAppointmentID(c) {
		return
	}
	var body apitypes.CancelMyAppointmentJSONRequestBody
	if !decodeObject(c, &body, []string{"version"}, []string{"cancelReason", "reasonDetail"}) {
		return
	}
	reason := ""
	if body.CancelReason != nil {
		reason = strings.TrimSpace(string(*body.CancelReason))
	}
	reasonDetail := ""
	if body.ReasonDetail != nil {
		reasonDetail = strings.TrimSpace(*body.ReasonDetail)
	}
	if body.Version < 1 || !store.ValidCancelReason(reason) || utf8.RuneCountInString(reasonDetail) > 200 {
		fail(c, 400, "INVALID_BODY", "取消原因无效")
		return
	}
	row, err := a.booking.CancelAppointment(c.Request.Context(), c.Param("appointmentId"), currentPatient(c).ID, int(body.Version), reason, reasonDetail, a.now())
	if err != nil {
		a.bookingError(c, err, false)
		return
	}
	c.JSON(200, apitypes.AppointmentResult{Data: appointmentDTO(row, a.now(), false)})
}

func (a *API) reviewAppointment(c *gin.Context) {
	if !validAppointmentID(c) {
		return
	}
	var body apitypes.ReviewAppointmentJSONRequestBody
	if !decodeObject(c, &body, []string{"action", "version"}, []string{"reason"}) {
		return
	}
	reason := ""
	if body.Reason != nil {
		reason = strings.TrimSpace(*body.Reason)
	}
	if body.Version < 1 || (body.Action != apitypes.Confirm && body.Action != apitypes.Reject) || utf8.RuneCountInString(reason) > 200 || (body.Action == apitypes.Reject && reason == "") || (body.Action == apitypes.Confirm && reason != "") {
		fail(c, 400, "INVALID_BODY", "拒绝需填写原因（1–200字）；确认时原因应留空，并提供有效版本号")
		return
	}
	row, err := a.booking.ReviewAppointment(c.Request.Context(), c.Param("appointmentId"), store.ReviewInput{Action: string(body.Action), Reason: reason, Version: int(body.Version)}, a.now())
	if err != nil {
		a.bookingError(c, err, false)
		return
	}
	c.JSON(200, apitypes.AppointmentResult{Data: a.employeeAppointmentDTO(c, row)})
}

// remindAppointments sends a visit-reminder inbox message for each listed
// appointment and reports the per-item outcome.
func (a *API) remindAppointments(c *gin.Context) {
	var body apitypes.RemindAppointmentsJSONRequestBody
	if !decodeObject(c, &body, []string{"appointmentIds"}, nil) {
		return
	}
	ids := make([]string, 0, len(body.AppointmentIds))
	for _, id := range body.AppointmentIds {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, strings.TrimSpace(id))
		}
	}
	if len(ids) == 0 || len(ids) > 50 {
		fail(c, 400, "INVALID_BODY", "请勾选 1–50 条预约")
		return
	}
	results, err := a.booking.RemindAppointments(c.Request.Context(), ids, a.now())
	if err != nil {
		a.bookingError(c, err, false)
		return
	}
	dtos := make([]apitypes.AppointmentRemindResult, 0, len(results))
	for _, r := range results {
		dtos = append(dtos, apitypes.AppointmentRemindResult{AppointmentId: r.AppointmentID, Sent: r.Sent, Reason: r.Reason})
	}
	c.JSON(200, gin.H{"data": dtos})
}
