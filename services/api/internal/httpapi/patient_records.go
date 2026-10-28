package httpapi

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) registerPatientRecordRoutes(r *gin.Engine) {
	admin := r.Group("/api/v1/admin/patients", a.authenticate, a.patientRecordsAvailable)
	admin.GET("", a.patientDirectory)
	admin.POST("", a.createPatientChart)
	admin.PUT("/:patientId/profile", a.savePatientChart)
	admin.POST("/:patientId/wechat-binding", a.createPatientBindingCode)
	admin.DELETE("/:patientId/wechat-binding", a.revokePatientBindingCode)
	templates := r.Group("/api/v1/admin/consultation-templates", a.authenticate, a.patientRecordsAvailable)
	templates.GET("", a.consultationTemplates)
	templates.POST("", a.createConsultationTemplate)
	templates.DELETE("/:templateId", a.deleteConsultationTemplate)
	admin.GET("/:patientId", a.patientRecord)
	admin.POST("/:patientId/consultations", a.createConsultation)
}

func (a *API) patientRecordsAvailable(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if _, ok := a.repo.(store.PatientRecordRepository); !ok || a.care == nil || a.followup == nil {
		fail(c, 503, "SERVICE_UNAVAILABLE", "患者档案服务暂不可用")
		return
	}
	c.Next()
}

func (a *API) patientRecordError(c *gin.Context, err error) {
	if errors.Is(err, store.ErrConflict) {
		fail(c, 409, "VERSION_CONFLICT", "档案已被其他人修改，请刷新后重试")
	} else if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrForbidden) {
		fail(c, 404, "NOT_FOUND", "患者不存在或无权访问")
	} else if errors.Is(err, store.ErrCareState) {
		fail(c, 400, "INVALID_CLINICAL_RECORD", "问诊时间、复诊关联或阶段进度无效")
	} else {
		a.unavailable(c, err)
	}
}

func (a *API) patientDirectory(c *gin.Context) {
	for key, values := range c.Request.URL.Query() {
		if (key != "search" && key != "tag" && key != "page" && key != "pageSize" && key != "phoneLast4") || len(values) != 1 {
			fail(c, 400, "INVALID_QUERY", "查询参数无效")
			return
		}
	}
	page, size := 1, 20
	var err error
	if value, ok := c.GetQuery("page"); ok {
		page, err = strconv.Atoi(value)
	}
	if err != nil || page < 1 || page > 100000 {
		fail(c, 400, "INVALID_QUERY", "页码无效")
		return
	}
	if value, ok := c.GetQuery("pageSize"); ok {
		size, err = strconv.Atoi(value)
	}
	search := strings.TrimSpace(c.Query("search"))
	tag := strings.TrimSpace(c.Query("tag"))
	phoneLast4 := strings.TrimSpace(c.Query("phoneLast4"))
	if phoneLast4 != "" && !regexp.MustCompile(`^[0-9]{4}$`).MatchString(phoneLast4) {
		fail(c, 400, "INVALID_QUERY", "手机号后四位须为 4 位数字")
		return
	}
	if err != nil || size < 1 || size > 100 || utf8.RuneCountInString(search) > 64 || utf8.RuneCountInString(tag) > 24 {
		fail(c, 400, "INVALID_QUERY", "查询参数无效")
		return
	}
	var rows []store.PatientRecordSummary
	var total int64
	if tag != "" {
		repo, ok := a.chartRepo(c)
		if !ok {
			return
		}
		rows, total, err = repo.PatientDirectoryWithTag(c.Request.Context(), search, page, size, tag, phoneLast4)
	} else {
		rows, total, err = a.repo.(store.PatientRecordRepository).PatientDirectory(c.Request.Context(), search, page, size, phoneLast4)
	}
	if err != nil {
		a.patientRecordError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": rows, "total": total, "page": page, "pageSize": size})
}

func (a *API) patientRecord(c *gin.Context) {
	if !validQuery(c, "dateFrom", "dateTo") {
		return
	}
	dates, ok := clinicalDateRange(c)
	if !ok {
		return
	}
	id := c.Param("patientId")
	if id == "" || !validID(id) {
		fail(c, 400, "INVALID_PATIENT_ID", "患者标识无效")
		return
	}
	ctx := c.Request.Context()
	if a.booking != nil {
		if _, err := a.booking.ExpirePending(ctx, a.now()); err != nil {
			a.patientRecordError(c, err)
			return
		}
	}
	repo := a.repo.(store.PatientRecordRepository)
	patient, appointments, err := repo.PatientRecordAppointments(ctx, id)
	if err != nil {
		a.patientRecordError(c, err)
		return
	}
	consultations, err := repo.PatientConsultations(ctx, id, dates)
	if err != nil {
		a.patientRecordError(c, err)
		return
	}
	encounters := make([]gin.H, 0, len(appointments))
	for _, appointment := range appointments {
		plan, versions, err := a.care.AdminCarePlan(ctx, appointment.ID)
		if err != nil {
			a.patientRecordError(c, err)
			return
		}
		workspace, err := planWorkspaceDTO(plan, versions)
		if err != nil {
			a.patientRecordError(c, err)
			return
		}
		photos, err := a.care.ListCarePhotos(ctx, appointment.ID, "", true)
		if errors.Is(err, store.ErrPhotoConsent) {
			photos = []model.CarePhoto{}
		} else if err != nil {
			a.patientRecordError(c, err)
			return
		}
		photoData := make([]apitypes.CarePhoto, 0, len(photos))
		for _, photo := range photos {
			photoData = append(photoData, photoDTO(photo))
		}
		treatment, revisions, tasks, err := a.followup.FollowupWorkspaceWithHistory(ctx, appointment.ID, "")
		if err != nil {
			a.patientRecordError(c, err)
			return
		}
		followup := apitypes.FollowupWorkspace{Treatment: treatmentDTO(treatment), TreatmentRevisions: []apitypes.TreatmentRevision{}, Tasks: []apitypes.FollowupTask{}}
		for _, revision := range revisions {
			followup.TreatmentRevisions = append(followup.TreatmentRevisions, treatmentRevisionDTO(revision))
		}
		for _, task := range tasks {
			data, err := a.followupDTO(c, task, true)
			if err != nil {
				a.patientRecordError(c, err)
				return
			}
			followup.Tasks = append(followup.Tasks, data)
		}
		if err := a.care.RecordCareAccess(ctx, appointment.ID, "care-plan", appointment.ID, "viewed", "clinician", currentEmployee(c).ID, a.careNow()); err != nil {
			a.patientRecordError(c, err)
			return
		}
		encounters = append(encounters, gin.H{"appointment": appointmentDTO(appointment, a.now(), true), "carePlan": workspace, "photos": photoData, "followup": followup})
	}
	c.JSON(200, gin.H{"data": gin.H{"patient": patient, "consultations": consultations, "encounters": encounters}})
}

func (a *API) createConsultation(c *gin.Context) {
	id := c.Param("patientId")
	if id == "" || !validID(id) {
		fail(c, 400, "INVALID_PATIENT_ID", "患者标识无效")
		return
	}
	var body apitypes.ConsultationCreate
	if !decodeObjectLimit(c, &body, []string{"chiefComplaint", "history", "assessment", "advice"}, []string{"clinicId", "visitType", "parentConsultationId", "appointmentId", "visitedAt", "nextVisitAt", "stageIndex", "stageStatus", "medicalHistory", "allergyHistory", "treatmentHistory", "skinType", "goals"}, 64<<10) {
		return
	}
	body.ChiefComplaint = strings.TrimSpace(body.ChiefComplaint)
	body.History = strings.TrimSpace(body.History)
	body.Assessment = strings.TrimSpace(body.Assessment)
	body.Advice = strings.TrimSpace(body.Advice)
	if body.ChiefComplaint == "" || utf8.RuneCountInString(body.ChiefComplaint) > 1000 || utf8.RuneCountInString(body.History) > 5000 || utf8.RuneCountInString(body.Assessment) > 5000 || utf8.RuneCountInString(body.Advice) > 5000 {
		fail(c, 400, "INVALID_BODY", "主诉必填且不超过1000字，其他字段不超过5000字")
		return
	}
	if (body.VisitType != nil && *body.VisitType != "initial" && *body.VisitType != "revisit") || (body.StageStatus != nil && *body.StageStatus != "not_started" && *body.StageStatus != "in_progress" && *body.StageStatus != "completed") || (body.VisitedAt != nil && body.VisitedAt.IsZero()) || (body.NextVisitAt != nil && body.NextVisitAt.IsZero()) {
		fail(c, 400, "INVALID_BODY", "就诊类型、阶段进度或问诊日期无效")
		return
	}
	var input model.Consultation
	encoded, _ := json.Marshal(body)
	if err := json.Unmarshal(encoded, &input); err != nil {
		fail(c, 400, "INVALID_BODY", "问诊字段无效")
		return
	}
	if !validStructuredFields(input.MedicalHistory, input.AllergyHistory, input.TreatmentHistory, input.SkinType, input.Goals) {
		fail(c, 400, "INVALID_BODY", "结构化问诊字段超出长度限制")
		return
	}
	for _, link := range []*string{input.ParentConsultationID, input.AppointmentID} {
		if link != nil && (*link == "" || !validID(*link)) {
			fail(c, 400, "INVALID_BODY", "关联编号无效")
			return
		}
	}
	input.PatientID, input.RecordedBy, input.RecordedAt = id, currentEmployee(c).ID, a.careNow()
	row, err := a.repo.(store.PatientRecordRepository).CreateConsultation(c.Request.Context(), input)
	if err != nil {
		a.patientRecordError(c, err)
		return
	}
	c.JSON(201, gin.H{"data": row})
}
