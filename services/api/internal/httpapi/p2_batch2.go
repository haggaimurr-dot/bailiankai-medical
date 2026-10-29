package httpapi

import (
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) registerP2Batch2Routes(r *gin.Engine) {
	// 行 107：阶段性恢复小结推送
	r.GET("/api/v1/admin/patients/:patientId/recovery-stage-plans", a.authenticate, a.adminRecoveryStagePlans)
	r.POST("/api/v1/admin/patients/:patientId/recovery-stage-plans", a.authenticate, a.createRecoveryStagePlan)
	r.POST("/api/v1/admin/recovery-stage-plans/check", a.authenticate, a.checkRecoveryStagePlans)
}

func (a *API) registerP2Batch2PatientRoutes(patient *gin.RouterGroup) {
	// 行 107：患者端查看自己的恢复阶段计划
	patient.GET("/recovery-stage-plans", a.patientRecoveryStagePlans)
}

// ============ 行 107 ============

func (a *API) adminRecoveryStagePlans(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !validID(c.Param("patientId")) {
		fail(c, 400, "INVALID_ID", "患者标识无效")
		return
	}
	plans, err := a.repo.(*store.MySQL).RecoveryStagePlans(c.Request.Context(), c.Param("patientId"))
	if err != nil {
		fail(c, 500, "SERVER_ERROR", "查询失败")
		return
	}
	c.JSON(200, gin.H{"data": recoveryStagePlanList(plans)})
}

func (a *API) createRecoveryStagePlan(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	employee := currentEmployee(c)
	if !validID(c.Param("patientId")) {
		fail(c, 400, "INVALID_ID", "患者标识无效")
		return
	}
	var body struct {
		AppointmentID string `json:"appointmentId"`
		StartDate     string `json:"startDate"`
	}
	if !decodeObject(c, &body, []string{"appointmentId", "startDate"}, nil) {
		return
	}
	start, err := time.ParseInLocation("2006-01-02", body.StartDate, database.Shanghai)
	if err != nil {
		fail(c, 400, "INVALID_BODY", "开始日期格式应为 YYYY-MM-DD")
		return
	}
	clinicID := ""
	if len(employee.ClinicIDs) > 0 {
		clinicID = employee.ClinicIDs[0]
	}
	plan, err := a.repo.(*store.MySQL).CreateRecoveryStagePlan(c.Request.Context(),
		c.Param("patientId"), body.AppointmentID, clinicID, start, employee.ID, a.now())
	if err != nil {
		fail(c, 400, "INVALID_BODY", "创建失败："+err.Error())
		return
	}
	c.JSON(201, gin.H{"data": recoveryStagePlanItem(plan)})
}

func (a *API) checkRecoveryStagePlans(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if currentEmployee(c).Role != "manager" {
		fail(c, 403, "FORBIDDEN", "仅主管可触发阶段检查")
		return
	}
	count, err := a.repo.(*store.MySQL).CheckRecoveryStageMilestones(c.Request.Context(), a.now())
	if err != nil {
		fail(c, 500, "SERVER_ERROR", "检查失败")
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"notified": count}})
}

func (a *API) patientRecoveryStagePlans(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	patient := currentPatient(c)
	plans, err := a.repo.(*store.MySQL).RecoveryStagePlans(c.Request.Context(), patient.ID)
	if err != nil {
		fail(c, 500, "SERVER_ERROR", "查询失败")
		return
	}
	c.JSON(200, gin.H{"data": recoveryStagePlanList(plans)})
}

func recoveryStagePlanList(plans []store.RecoveryStagePlan) []gin.H {
	data := make([]gin.H, 0, len(plans))
	for _, p := range plans {
		data = append(data, recoveryStagePlanItem(p))
	}
	return data
}

func recoveryStagePlanItem(p store.RecoveryStagePlan) gin.H {
	ms := make([]gin.H, 0, len(p.Milestones))
	for _, m := range p.Milestones {
		ms = append(ms, gin.H{
			"id": m.ID, "dayOffset": m.DayOffset, "stageLabel": m.StageLabel,
			"summary": m.Summary, "notified": m.Notified, "notifiedAt": m.NotifiedAt,
		})
	}
	return gin.H{
		"id": p.ID, "patientId": p.PatientID, "appointmentId": p.AppointmentID,
		"clinicId": p.ClinicID, "startDate": p.StartDate.Format("2006-01-02"),
		"status": p.Status, "createdAt": p.CreatedAt, "milestones": ms,
	}
}

// ============ 行 108: 常见问题自助问答 ============

func (a *API) registerFaqRoutes(r *gin.Engine) {
	r.GET("/api/v1/admin/faqs", a.authenticate, a.adminFaqs)
	r.POST("/api/v1/admin/faqs", a.authenticate, a.createFaq)
	r.PUT("/api/v1/admin/faqs/:faqId", a.authenticate, a.updateFaq)
	r.DELETE("/api/v1/admin/faqs/:faqId", a.authenticate, a.deleteFaq)
}

func (a *API) registerFaqPatientRoutes(patient *gin.RouterGroup) {
	patient.GET("/faqs", a.patientFaqs)
}

func (a *API) adminFaqs(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	faqs, err := a.repo.(*store.MySQL).AdminFaqs(c.Request.Context(),
		c.Query("clinicId"), c.Query("category"), c.Query("keyword"))
	if err != nil {
		fail(c, 500, "SERVER_ERROR", "查询失败")
		return
	}
	c.JSON(200, gin.H{"data": faqList(faqs)})
}

func (a *API) createFaq(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var body struct {
		ClinicID  string `json:"clinicId"`
		Category  string `json:"category"`
		Question  string `json:"question"`
		Answer    string `json:"answer"`
		SortOrder int    `json:"sortOrder"`
		Enabled   bool   `json:"enabled"`
	}
	if !decodeObject(c, &body, []string{"question", "answer"}, []string{"clinicId", "category", "sortOrder", "enabled"}) {
		return
	}
	employee := currentEmployee(c)
	faq, err := a.repo.(*store.MySQL).CreateFaq(c.Request.Context(), store.FaqInput{
		ClinicID: body.ClinicID, Category: body.Category, Question: body.Question,
		Answer: body.Answer, SortOrder: body.SortOrder, Enabled: body.Enabled,
	}, employee.ID, a.now())
	if err != nil {
		fail(c, 400, "INVALID_BODY", "问题与答案不能为空")
		return
	}
	c.JSON(201, gin.H{"data": faqItem(faq)})
}

func (a *API) updateFaq(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !validID(c.Param("faqId")) {
		fail(c, 400, "INVALID_ID", "标识无效")
		return
	}
	var body struct {
		Category  string `json:"category"`
		Question  string `json:"question"`
		Answer    string `json:"answer"`
		SortOrder int    `json:"sortOrder"`
		Enabled   bool   `json:"enabled"`
	}
	if !decodeObject(c, &body, []string{"question", "answer"}, []string{"category", "sortOrder", "enabled"}) {
		return
	}
	faq, err := a.repo.(*store.MySQL).UpdateFaq(c.Request.Context(), c.Param("faqId"), store.FaqInput{
		Category: body.Category, Question: body.Question, Answer: body.Answer,
		SortOrder: body.SortOrder, Enabled: body.Enabled,
	}, a.now())
	if err != nil {
		fail(c, 400, "INVALID_BODY", "问题与答案不能为空")
		return
	}
	c.JSON(200, gin.H{"data": faqItem(faq)})
}

func (a *API) deleteFaq(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !validID(c.Param("faqId")) {
		fail(c, 400, "INVALID_ID", "标识无效")
		return
	}
	if err := a.repo.(*store.MySQL).DeleteFaq(c.Request.Context(), c.Param("faqId")); err != nil {
		fail(c, 400, "INVALID_BODY", "删除失败")
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"deleted": true}})
}

func (a *API) patientFaqs(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	_ = currentPatient(c)
	faqs, err := a.repo.(*store.MySQL).PatientFaqs(c.Request.Context(), nil, c.Query("keyword"))
	if err != nil {
		fail(c, 500, "SERVER_ERROR", "查询失败")
		return
	}
	c.JSON(200, gin.H{"data": faqList(faqs)})
}

func faqList(faqs []store.Faq) []gin.H {
	data := make([]gin.H, 0, len(faqs))
	for _, f := range faqs {
		data = append(data, faqItem(f))
	}
	return data
}

func faqItem(f store.Faq) gin.H {
	return gin.H{
		"id": f.ID, "clinicId": f.ClinicID, "category": f.Category,
		"question": f.Question, "answer": f.Answer, "sortOrder": f.SortOrder,
		"enabled": f.Enabled, "createdAt": f.CreatedAt, "updatedAt": f.UpdatedAt,
	}
}
