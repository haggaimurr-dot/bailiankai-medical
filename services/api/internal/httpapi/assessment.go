package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) registerAssessmentRoutes(r *gin.Engine) {
	r.GET("/api/v1/assessment-capabilities", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		quality, model := false, false
		if a.assessments != nil {
			quality = a.assessments.QualityAvailable()
			model = a.assessments.ModelAvailable()
		}
		c.JSON(200, gin.H{"data": gin.H{"qualityAvailable": quality, "modelAvailable": model, "maxAttempts": 3, "consentVersion": store.AssessmentConsentVersion}})
	})
	patient := r.Group("/api/v1/me/assessments", a.bookingAvailable, a.authenticatePatient, a.assessmentAvailable)
	patient.GET("", a.listAssessments)
	patient.POST("", a.createAssessment)
	patient.GET("/:assessmentId", a.getAssessment)
	patient.GET("/:assessmentId/consent", a.assessmentConsent)
	patient.POST("/:assessmentId/consent", a.changeAssessment)
	patient.PUT("/:assessmentId/photos/:angle", a.uploadAssessmentPhoto)
	patient.POST("/:assessmentId/photos/:angle", a.uploadAssessmentPhoto)
	patient.GET("/:assessmentId/photos/:photoId/content", a.assessmentPhoto)
	for _, action := range []string{"quality-check", "submit", "retry", "cancel"} {
		patient.POST("/:assessmentId/"+action, a.changeAssessment)
	}
	admin := r.Group("/api/v1/admin/assessments", a.authenticate, a.assessmentAvailable)
	admin.GET("", a.listAssessments)
	admin.GET("/review-stats", a.assessmentReviewStats)
	admin.GET("/reviewers", a.assessmentReviewers)
	admin.GET("/access-audit", a.requireManager, a.assessmentAccessAudit)
	admin.GET("/:assessmentId", a.getAssessment)
	admin.GET("/:assessmentId/photos/:photoId/content", a.assessmentPhoto)
	admin.GET("/:assessmentId/events", a.assessmentEvents)
	admin.POST("/:assessmentId/assignment", a.changeAssessment)
	admin.POST("/:assessmentId/reviews", a.changeAssessment)
	// Consent text versioning lives outside the per-assessment routes so the
	// path matches the contract (/admin/assessment-consent-policy).
	policyAdmin := r.Group("/api/v1/admin/assessment-consent-policy", a.authenticate, a.assessmentAvailable)
	policyAdmin.GET("", a.getAssessmentConsentPolicy)
	policyAdmin.POST("", a.createAssessmentConsentPolicy)
}
func (a *API) assessmentAvailable(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if a.assessments == nil {
		fail(c, 503, "ASSESSMENT_UNAVAILABLE", "评估服务暂不可用")
		return
	}
	if id := c.Param("assessmentId"); id != "" && !validID(id) {
		fail(c, 400, "INVALID_ASSESSMENT", "评估编号无效")
		return
	}
	c.Next()
}
func assessmentPatient(c *gin.Context) string {
	if strings.HasPrefix(c.FullPath(), "/api/v1/me/") {
		return currentPatient(c).ID
	}
	return ""
}
func (a *API) assessmentReviewStats(c *gin.Context) {
	stats, err := a.assessments.ReviewStats(c.Request.Context())
	if err != nil {
		a.assessmentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": stats})
}

func (a *API) assessmentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrAssessmentPrivacy):
		fail(c, 403, "PRIVACY_CONSENT_REQUIRED", "请阅读并同意当前隐私说明后再提交资料")
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrForbidden):
		fail(c, 404, "ASSESSMENT_NOT_FOUND", "任务不存在或无权访问")
	case errors.Is(err, store.ErrAssessmentInput):
		fail(c, 400, "INVALID_ASSESSMENT_INPUT", "评估请求内容无效，请检查输入")
	case errors.Is(err, store.ErrAssessmentConsent):
		fail(c, 403, "AI_CONSENT_REQUIRED", "请先同意本任务的AI用途授权")
	case errors.Is(err, store.ErrAssessmentDuplicatePhoto):
		fail(c, 422, "DUPLICATE_PHOTO", "这张照片已用于另一角度，请换一张不同的照片")
	case errors.Is(err, store.ErrAssessmentQuality):
		fail(c, 422, "INPUT_QUALITY_REJECTED", "请补齐三张不同角度照片，并等待全部服务端质检通过")
	case errors.Is(err, store.ErrAssessmentConfiguration):
		fail(c, 503, "MODEL_CONFIGURATION_ERROR", "评估检查器或模型尚未配置，请稍后重试或直接预约面诊")
	case errors.Is(err, store.ErrAssessmentRateLimit):
		c.Header("Retry-After", "5")
		fail(c, 429, "ASSESSMENT_RATE_LIMITED", "操作过于频繁，请稍后重试")
	case errors.Is(err, store.ErrIdempotency):
		fail(c, 409, "IDEMPOTENCY_CONFLICT", "原请求编号对应其他内容，请核对原操作")
	case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrCareState):
		fail(c, 409, "ASSESSMENT_VERSION_CONFLICT", "任务状态已变化，请刷新核对后再操作")
	default:
		var mediaError *store.AssessmentProviderError
		if errors.As(err, &mediaError) && mediaError.Code == "MEDIA_UNAVAILABLE" {
			a.mediaUnavailable(c)
			return
		}
		fail(c, 503, "ASSESSMENT_UNAVAILABLE", "评估服务暂不可用，请稍后重试")
	}
}
func assessmentBody(c *gin.Context, target any, required, optional []string) bool {
	// decodeObject enforces top-level required fields; the strict pass also checks
	// nested fields, duplicate keys and trailing JSON using the same request bytes.
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		fail(c, 400, "INVALID_BODY", "请求内容无效")
		return false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(data))
	if !decodeObject(c, target, required, optional) {
		return false
	}
	if store.DecodeAssessmentJSON(data, target) != nil {
		fail(c, 400, "INVALID_BODY", "请求包含未知、重复或无效字段")
		return false
	}
	return true
}
func (a *API) createAssessment(c *gin.Context) {
	var body store.AssessmentCreate
	if !assessmentBody(c, &body, []string{"clinicId", "requestKey"}, []string{"appointmentId", "supersedesAssessmentId"}) {
		return
	}
	view, replayed, err := a.assessments.Create(c.Request.Context(), currentPatient(c).ID, body, a.careNow())
	if err != nil {
		a.assessmentError(c, err)
		return
	}
	status := 201
	if replayed {
		status = 200
	}
	c.JSON(status, gin.H{"data": view})
}
func (a *API) getAssessment(c *gin.Context) {
	if !validQuery(c) {
		return
	}
	view, err := a.assessments.Get(c.Request.Context(), c.Param("assessmentId"), assessmentPatient(c))
	if err != nil {
		a.assessmentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": view})
}
func (a *API) assessmentConsent(c *gin.Context) {
	view, err := a.assessments.Get(c.Request.Context(), c.Param("assessmentId"), assessmentPatient(c))
	if err != nil {
		a.assessmentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": view.Consent})
}
func (a *API) listAssessments(c *gin.Context) {
	if !validQuery(c, "status", "clinicId", "q", "page", "pageSize") {
		return
	}
	page, size := 1, 20
	var err error
	if raw := c.Query("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil {
			fail(c, 400, "INVALID_PAGE", "页码无效")
			return
		}
	}
	if raw := c.Query("pageSize"); raw != "" {
		size, err = strconv.Atoi(raw)
		if err != nil {
			fail(c, 400, "INVALID_PAGE", "每页数量无效")
			return
		}
	}
	out, err := a.assessments.List(c.Request.Context(), assessmentPatient(c), store.AssessmentFilter{Status: c.Query("status"), ClinicID: c.Query("clinicId"), Query: c.Query("q"), Page: page, PageSize: size})
	if err != nil {
		a.assessmentError(c, err)
		return
	}
	c.JSON(200, out)
}
func (a *API) changeAssessment(c *gin.Context) {
	action := c.FullPath()[strings.LastIndex(c.FullPath(), "/")+1:]
	required := []string{"version"}
	optional := []string{}
	switch action {
	case "consent":
		required = append(required, "policyVersion", "granted")
	case "assignment":
		required = append(required, "reviewerEmployeeId")
	case "reviews":
		action = "review"
		required = append(required, "resultId", "decision", "note")
		optional = append(optional, "finalContent")
	}
	var body store.AssessmentChange
	if !assessmentBody(c, &body, required, optional) {
		return
	}
	view, err := a.assessments.Change(c.Request.Context(), c.Param("assessmentId"), assessmentPatient(c), c.GetHeader("Idempotency-Key"), action, body, a.careNow())
	if err != nil {
		a.assessmentError(c, err)
		return
	}
	// 新增-P2 行 100：复核发布后（status=completed）给患者发站内消息，点击直达结果页。
	// best-effort，失败不影响发布本身。
	if action == "review" && body.Decision == "publish" && view.Task.Status == "completed" && view.PublishedReview != nil {
		if notifier, ok := a.repo.(interface {
			NotifyAssessmentPublished(assessmentID string, now time.Time)
		}); ok {
			go notifier.NotifyAssessmentPublished(c.Param("assessmentId"), a.careNow())
		}
	}
	code := 200
	if action == "submit" || action == "retry" || action == "quality-check" {
		code = 202
	}
	c.JSON(code, gin.H{"data": view})
}
func (a *API) uploadAssessmentPhoto(c *gin.Context) {
	angle := c.Param("angle")
	if angle != "front" && angle != "left" && angle != "right" {
		fail(c, 400, "INVALID_PHOTO_ANGLE", "照片角度无效")
		return
	}
	initial, err := a.assessments.Get(c.Request.Context(), c.Param("assessmentId"), currentPatient(c).ID)
	if err != nil {
		a.assessmentError(c, err)
		return
	}
	if !initial.Consent.Granted {
		a.assessmentError(c, store.ErrAssessmentConsent)
		return
	}
	if initial.Task.Status != "draft" {
		a.assessmentError(c, store.ErrCareState)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPhotoBytes+(1<<20))
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(c, 413, "PHOTO_TOO_LARGE", "照片过大")
		} else {
			fail(c, 400, "INVALID_PHOTO", "请选择有效照片文件")
		}
		return
	}
	defer file.Close()
	if header.Size < 1 || header.Size > maxPhotoBytes {
		fail(c, 413, "PHOTO_TOO_LARGE", "请选择不超过8MB的JPG或PNG照片")
		return
	}
	form := c.Request.MultipartForm
	if form == nil || len(form.File["file"]) != 1 || len(form.Value["version"]) != 1 {
		fail(c, 400, "INVALID_PHOTO", "照片或版本字段重复、缺失")
		return
	}
	for key := range form.File {
		if key != "file" {
			fail(c, 400, "INVALID_PHOTO", "照片请求包含未知文件字段")
			return
		}
	}
	for key := range form.Value {
		if key != "version" {
			fail(c, 400, "INVALID_PHOTO", "照片请求包含未知字段")
			return
		}
	}
	version, err := strconv.Atoi(c.PostForm("version"))
	if err != nil || version < 1 {
		fail(c, 400, "INVALID_VERSION", "任务版本无效")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxPhotoBytes+1))
	if err != nil || len(raw) > maxPhotoBytes {
		fail(c, 413, "PHOTO_TOO_LARGE", "照片过大")
		return
	}
	data, width, height, err := normalizeAssessmentPhoto(raw)
	if err != nil {
		fail(c, 400, "INVALID_PHOTO", "请使用短边至少512像素、最长边不超过4096像素的有效JPG或PNG照片")
		return
	}
	hash := sha256.Sum256(data)
	photo := store.AssessmentPhoto{ID: randomCareID(), Angle: angle, MediaType: "image/jpeg", SizeBytes: int64(len(data)), Width: width, Height: height, SHA256: hex.EncodeToString(hash[:])}
	view, err := a.assessments.Upload(c.Request.Context(), c.Param("assessmentId"), currentPatient(c).ID, c.GetHeader("Idempotency-Key"), version, photo, data, a.careNow())
	if err != nil {
		a.assessmentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": view})
}
func (a *API) assessmentPhoto(c *gin.Context) {
	id, photoID, patientID := c.Param("assessmentId"), c.Param("photoId"), assessmentPatient(c)
	photo, err := a.assessments.Photo(c.Request.Context(), id, photoID, patientID)
	if err != nil {
		a.assessmentError(c, err)
		return
	}
	if a.media == nil {
		a.mediaUnavailable(c)
		return
	}
	// 行120: ?variant=display 取压缩水印展示副本（鉴权逻辑与原图一致）。
	mediaID, verify := photo.ID, true
	if c.Query("variant") == "display" {
		mediaID, verify = photo.ID+store.DisplayVariantSuffix, false
	}
	data, err := a.media.Read(c.Request.Context(), mediaID)
	if err != nil {
		a.mediaUnavailable(c)
		return
	}
	if verify && store.VerifyMedia(data, store.MediaReference{ID: photo.ID, SHA256: photo.SHA256, SizeBytes: photo.SizeBytes}) != nil {
		a.mediaUnavailable(c)
		return
	}
	if _, err := a.assessments.Photo(c.Request.Context(), id, photoID, patientID); err != nil {
		a.assessmentError(c, err)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, "image/jpeg", data)
}
func (a *API) assessmentEvents(c *gin.Context) {
	rows, err := a.assessments.Events(c.Request.Context(), c.Param("assessmentId"))
	if err != nil {
		a.assessmentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": rows})
}

// assessmentAccessAudit is the manager's periodic access audit: every staff
// photo read refused after consent withdrawal, newest first. A withdrawal
// that still lets staff through would show up here as a missing denial
// next to a successful photo-read in the per-assessment events.
func (a *API) assessmentAccessAudit(c *gin.Context) {
	if !validQuery(c, "page", "pageSize") {
		return
	}
	page, pageSize := 1, 20
	for key, target := range map[string]*int{"page": &page, "pageSize": &pageSize} {
		if value := c.Query(key); value != "" {
			parsed, err := strconv.Atoi(value)
			limit := 100000
			if key == "pageSize" {
				limit = 100
			}
			if err != nil || parsed < 1 || parsed > limit {
				fail(c, 400, "INVALID_QUERY", "分页参数无效")
				return
			}
			*target = parsed
		}
	}
	rows, total, err := a.assessments.DeniedPhotoReads(c.Request.Context(), page, pageSize)
	if err != nil {
		a.assessmentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": rows, "total": total, "page": page, "pageSize": pageSize})
}
func (a *API) assessmentReviewers(c *gin.Context) {
	if !validQuery(c, "clinicId") {
		return
	}
	rows, err := a.assessments.Reviewers(c.Request.Context(), c.Query("clinicId"))
	if err != nil {
		a.assessmentError(c, err)
		return
	}
	data := []gin.H{}
	for _, row := range rows {
		data = append(data, gin.H{"id": row.ID, "displayName": row.DisplayName, "clinicIds": row.ClinicIDs})
	}
	c.JSON(200, gin.H{"data": data})
}
func consentPolicyDTO(row store.AssessmentConsentPolicy) apitypes.AssessmentConsentPolicy {
	return apitypes.AssessmentConsentPolicy{Version: row.Version, Content: row.Content, CreatedAt: row.CreatedAt, CreatedBy: row.CreatedBy}
}
func (a *API) getAssessmentConsentPolicy(c *gin.Context) {
	out, err := a.assessments.GetConsentPolicy(c.Request.Context())
	if err != nil {
		a.assessmentError(c, err)
		return
	}
	history := []apitypes.AssessmentConsentPolicy{}
	for _, row := range out.History {
		history = append(history, consentPolicyDTO(row))
	}
	c.JSON(200, gin.H{"data": apitypes.AssessmentConsentPolicyList{Policy: consentPolicyDTO(out.Policy), History: history}})
}
func (a *API) createAssessmentConsentPolicy(c *gin.Context) {
	var body apitypes.AssessmentConsentPolicyInput
	if !assessmentBody(c, &body, []string{"content"}, []string{"version"}) {
		return
	}
	version := ""
	if body.Version != nil {
		version = *body.Version
	}
	row, err := a.assessments.CreateConsentPolicy(c.Request.Context(), body.Content, version, a.now())
	if err != nil {
		a.assessmentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": apitypes.AssessmentConsentPolicyList{Policy: consentPolicyDTO(row), History: []apitypes.AssessmentConsentPolicy{consentPolicyDTO(row)}}})
}

func normalizeAssessmentPhoto(raw []byte) ([]byte, int, int, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "jpeg" && format != "png") || cfg.Width < 512 || cfg.Height < 512 || cfg.Width > 4096 || cfg.Height > 4096 {
		return nil, 0, 0, store.ErrAssessmentInput
	}
	data, err := normalizeOrientedAssessmentPhoto(raw)
	if err != nil {
		return nil, 0, 0, err
	}
	cfg, _, err = image.DecodeConfig(bytes.NewReader(data))
	return data, cfg.Width, cfg.Height, err
}
