package httpapi

import (
	"time"

	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) registerP2Batch3Routes(r *gin.Engine) {
	// 行 109：档案临时授权（患者自助）
	// 行 110：患者标签分层（管理端）
	r.GET("/api/v1/admin/patients/:patientId/tier", a.authenticate, a.getPatientTier)
	r.PUT("/api/v1/admin/patients/:patientId/tier", a.authenticate, a.setPatientTier)
	// 公开分享链接（无鉴权，token 即凭证）
	r.GET("/api/v1/shared-records/:token", a.sharedRecord)
}

func (a *API) registerP2Batch3PatientRoutes(patient *gin.RouterGroup) {
	// 行 109：患者生成/管理分享链接
	patient.GET("/record-share-links", a.listRecordShareLinks)
	patient.POST("/record-share-links", a.createRecordShareLink)
	patient.POST("/record-share-links/:linkId/revoke", a.revokeRecordShareLink)
	// 行 111：复诊带上次要点
	patient.GET("/last-visit-summary", a.lastVisitSummary)
}

// ============ 行 109 ============

func (a *API) listRecordShareLinks(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	patient := currentPatient(c)
	links, err := a.repo.(*store.MySQL).RecordShareLinks(c.Request.Context(), patient.ID)
	if err != nil {
		fail(c, 500, "SERVER_ERROR", "查询失败")
		return
	}
	data := make([]gin.H, 0, len(links))
	for _, l := range links {
		data = append(data, gin.H{"id": l.ID, "expiresAt": l.ExpiresAt, "revoked": l.Revoked, "createdAt": l.CreatedAt})
	}
	c.JSON(200, gin.H{"data": data})
}

func (a *API) createRecordShareLink(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	patient := currentPatient(c)
	var body struct {
		TTLHours int `json:"ttlHours"`
	}
	if !decodeObject(c, &body, []string{"ttlHours"}, nil) {
		return
	}
	link, token, err := a.repo.(*store.MySQL).CreateRecordShareLink(c.Request.Context(), patient.ID, time.Duration(body.TTLHours)*time.Hour, patient.ID, a.now())
	if err != nil {
		fail(c, 400, "INVALID_BODY", "有效期须为 1 小时至 90 天")
		return
	}
	c.JSON(201, gin.H{"data": gin.H{"id": link.ID, "token": token, "expiresAt": link.ExpiresAt}})
}

func (a *API) revokeRecordShareLink(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	patient := currentPatient(c)
	if !validID(c.Param("linkId")) {
		fail(c, 400, "INVALID_ID", "标识无效")
		return
	}
	if err := a.repo.(*store.MySQL).RevokeRecordShareLink(c.Request.Context(), c.Param("linkId"), patient.ID); err != nil {
		fail(c, 500, "SERVER_ERROR", "撤销失败")
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"revoked": true}})
}

// sharedRecord 是公开端点：token 即凭证，过期或撤销后返回 404。
func (a *API) sharedRecord(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	token := c.Param("token")
	if len(token) < 16 || len(token) > 128 {
		fail(c, 404, "NOT_FOUND", "分享链接无效或已过期")
		return
	}
	link, err := a.repo.(*store.MySQL).ResolveRecordShareLink(c.Request.Context(), token, a.now())
	if err != nil {
		fail(c, 404, "NOT_FOUND", "分享链接无效或已过期")
		return
	}
	repo := a.repo.(*store.MySQL)
	timeline, _, err := repo.PatientTimeline(c.Request.Context(), link.PatientID, 1, 50)
	if err != nil {
		fail(c, 500, "SERVER_ERROR", "查询失败")
		return
	}
	c.JSON(200, gin.H{"data": gin.H{
		"expiresAt": link.ExpiresAt,
		"timeline":  timeline,
	}})
}

// ============ 行 110 ============

func (a *API) getPatientTier(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !validID(c.Param("patientId")) {
		fail(c, 400, "INVALID_ID", "患者标识无效")
		return
	}
	tier, err := a.repo.(*store.MySQL).PatientTier(c.Request.Context(), c.Param("patientId"))
	if err != nil {
		fail(c, 500, "SERVER_ERROR", "查询失败")
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"patientId": tier.PatientID, "tier": tier.Tier, "reason": tier.Reason, "updatedAt": tier.UpdatedAt}})
}

func (a *API) setPatientTier(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	employee := currentEmployee(c)
	if !validID(c.Param("patientId")) {
		fail(c, 400, "INVALID_ID", "患者标识无效")
		return
	}
	var body struct {
		Tier   string `json:"tier"`
		Reason string `json:"reason"`
	}
	if !decodeObject(c, &body, []string{"tier"}, []string{"reason"}) {
		return
	}
	if err := a.repo.(*store.MySQL).SetPatientTier(c.Request.Context(), c.Param("patientId"), body.Tier, body.Reason, employee.ID, a.now()); err != nil {
		fail(c, 400, "INVALID_BODY", "标签须为 vip/normal/high_risk")
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"patientId": c.Param("patientId"), "tier": body.Tier}})
}

// ============ 行 111 ============

func (a *API) lastVisitSummary(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	patient := currentPatient(c)
	summary, err := a.repo.(*store.MySQL).LastVisitSummary(c.Request.Context(), patient.ID)
	if err != nil {
		fail(c, 500, "SERVER_ERROR", "查询失败")
		return
	}
	if summary.AppointmentID == "" {
		c.JSON(200, gin.H{"data": nil})
		return
	}
	c.JSON(200, gin.H{"data": gin.H{
		"appointmentId": summary.AppointmentID,
		"visitedAt":     summary.VisitedAt,
		"doctorName":    summary.DoctorName,
		"summary":       summary.Summary,
		"carePlan":      summary.CarePlan,
	}})
}
