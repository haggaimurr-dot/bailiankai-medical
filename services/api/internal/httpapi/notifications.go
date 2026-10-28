package httpapi

import (
	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"regexp"
	"strconv"
	"strings"
)

var messageTitleKey = regexp.MustCompile(`^thing[0-9]{1,2}$`)
var messageTimeKey = regexp.MustCompile(`^time[0-9]{1,2}$`)

func mergeSettingDTO(row model.NotificationMergeSetting) gin.H {
	return gin.H{"mergeWindowMinutes": row.MergeWindowMinutes, "mergeEnabled": row.MergeEnabled, "version": row.Version, "updatedBy": row.UpdatedBy, "updatedAt": row.UpdatedAt}
}

func (a *API) registerNotificationRoutes(r *gin.Engine) {
	available := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := a.repo.(store.NotificationRepository); !ok {
			fail(c, 503, "SERVICE_UNAVAILABLE", "消息服务暂不可用")
			return
		}
		c.Next()
	}
	me := r.Group("/api/v1/me/messages", a.bookingAvailable, a.authenticatePatient, available)
	me.GET("/templates", func(c *gin.Context) {
		rows, err := a.repo.(store.NotificationRepository).MessageTemplates(c.Request.Context(), true)
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": rows})
	})
	me.GET("/consents", func(c *gin.Context) {
		rows, err := a.repo.(store.NotificationRepository).MessageConsents(c.Request.Context(), currentPatient(c).ID)
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": rows})
	})
	me.POST("/consents", func(c *gin.Context) {
		var in struct {
			TemplateID string `json:"templateId"`
			Status     string `json:"status"`
			RequestKey string `json:"requestKey"`
		}
		if !decodeObject(c, &in, []string{"templateId", "status", "requestKey"}, nil) {
			return
		}
		if len(in.TemplateID) > 100 || in.TemplateID == "" || !requestKeyPattern.MatchString(in.RequestKey) || (in.Status != "accepted" && in.Status != "rejected" && in.Status != "revoked") {
			fail(c, 400, "INVALID_BODY", "授权参数无效")
			return
		}
		err := a.repo.(store.NotificationRepository).SetMessageConsent(c.Request.Context(), currentPatient(c).ID, in.TemplateID, in.Status, in.RequestKey, a.now())
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"saved": true}})
	})
	me.GET("", func(c *gin.Context) {
		if !validQuery(c, "page", "pageSize") {
			return
		}
		page, size := 1, 20
		for key, target := range map[string]*int{"page": &page, "pageSize": &size} {
			if v := c.Query(key); v != "" {
				n, e := strconv.Atoi(v)
				if e != nil || n < 1 || n > 1000000 || (key == "pageSize" && n > 100) {
					fail(c, 400, "INVALID_QUERY", "分页参数无效")
					return
				}
				*target = n
			}
		}
		repo := a.repo.(store.NotificationRepository)
		rows, total, err := repo.PatientMessages(c.Request.Context(), currentPatient(c).ID, page, size)
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		unread, err := repo.UnreadPatientMessageCount(c.Request.Context(), currentPatient(c).ID)
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		data := make([]apitypes.PatientMessage, 0, len(rows))
		for _, row := range rows {
			data = append(data, patientMessageDTO(row))
		}
		c.JSON(200, gin.H{"data": apitypes.PatientMessageList{Data: data, Total: int(total), UnreadCount: int(unread)}})
	})
	me.GET("/unread-count", func(c *gin.Context) {
		unread, err := a.repo.(store.NotificationRepository).UnreadPatientMessageCount(c.Request.Context(), currentPatient(c).ID)
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"unreadCount": unread}})
	})
	me.POST("/:messageId/read", func(c *gin.Context) {
		if id := c.Param("messageId"); id == "" || !validID(id) {
			fail(c, 400, "INVALID_ID", "标识格式无效")
			return
		}
		if err := a.repo.(store.NotificationRepository).MarkPatientMessageRead(c.Request.Context(), currentPatient(c).ID, c.Param("messageId"), a.now()); err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"saved": true}})
	})
	admin := r.Group("/api/v1/admin/messages", a.authenticate, a.requireManager, available)
	admin.GET("/templates", func(c *gin.Context) {
		rows, err := a.repo.(store.NotificationRepository).MessageTemplates(c.Request.Context(), false)
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": rows})
	})
	admin.PUT("/templates", func(c *gin.Context) {
		var in model.MessageTemplate
		if !decodeObject(c, &in, []string{"clinicId", "kind", "templateId", "titleKey", "timeKey", "page", "state", "leadMinutes", "enabled", "version"}, nil) {
			return
		}
		if !validMessageTemplate(in) {
			fail(c, 400, "INVALID_BODY", "请检查模板ID、字段映射、页面、提前分钟及版本")
			return
		}
		row, err := a.repo.(store.NotificationRepository).SaveMessageTemplate(c.Request.Context(), in)
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": row})
	})
	admin.GET("/merge-settings", func(c *gin.Context) {
		row, err := a.repo.(store.NotificationRepository).NotificationMergeSetting(c.Request.Context())
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": mergeSettingDTO(row)})
	})
	admin.PUT("/merge-settings", func(c *gin.Context) {
		var body struct {
			MergeWindowMinutes int  `json:"mergeWindowMinutes"`
			MergeEnabled       bool `json:"mergeEnabled"`
			Version            int  `json:"version"`
		}
		if !decodeObject(c, &body, []string{"mergeWindowMinutes", "mergeEnabled", "version"}, nil) {
			return
		}
		if body.MergeWindowMinutes < 5 || body.MergeWindowMinutes > 1440 || body.Version < 1 {
			fail(c, 400, "INVALID_MERGE_SETTING", "合并窗口须为 5–1440 分钟")
			return
		}
		row, err := a.repo.(store.NotificationRepository).UpdateNotificationMergeSetting(c.Request.Context(), body.MergeWindowMinutes, body.MergeEnabled, body.Version, currentEmployee(c).ID, a.now())
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": mergeSettingDTO(row)})
	})
	admin.GET("/queue", func(c *gin.Context) {
		if !validQuery(c, "status", "failureCategory", "page", "pageSize") {
			return
		}
		page, size := 1, 20
		for key, target := range map[string]*int{"page": &page, "pageSize": &size} {
			if v := c.Query(key); v != "" {
				n, e := strconv.Atoi(v)
				if e != nil || n < 1 || n > 1000000 || (key == "pageSize" && n > 100) {
					fail(c, 400, "INVALID_QUERY", "分页参数无效")
					return
				}
				*target = n
			}
		}
		status := c.Query("status")
		if status != "" && !strings.Contains("|queued|sending|sent|retry|blocked|failed|unknown|cancelled|merged|", "|"+status+"|") {
			fail(c, 400, "INVALID_QUERY", "队列状态无效")
			return
		}
		category := c.Query("failureCategory")
		if category != "" && !strings.Contains("|authorization|template|rate_limit|transient|source|other|", "|"+category+"|") {
			fail(c, 400, "INVALID_QUERY", "失败原因分类无效")
			return
		}
		rows, total, err := a.repo.(store.NotificationRepository).Notifications(c.Request.Context(), status, category, page, size)
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": rows, "meta": gin.H{"page": page, "pageSize": size, "total": total}})
	})
	admin.GET("/queue/:resourceId/attempts", func(c *gin.Context) {
		if !directoryID(c) {
			return
		}
		rows, err := a.repo.(store.NotificationRepository).NotificationAttempts(c.Request.Context(), c.Param("resourceId"))
		if err != nil {
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": rows})
	})
	admin.POST("/queue/:resourceId/retry", func(c *gin.Context) {
		if !directoryID(c) {
			return
		}
		if err := a.repo.(store.NotificationRepository).RetryNotification(c.Request.Context(), c.Param("resourceId"), a.now()); err != nil {
			if err == store.ErrCareState {
				fail(c, 409, "REMINDER_STATE", "仅可重新检查未到期且未超过尝试上限的授权阻塞任务，或重试临时失败的任务")
				return
			}
			knowledgeFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"saved": true}})
	})
}
func validMessageTemplate(in model.MessageTemplate) bool {
	return in.ClinicID != "" && len(in.ClinicID) <= 64 && (in.Kind == "revisit" || in.Kind == "followup") && in.TemplateID != "" && len(in.TemplateID) <= 100 && messageTitleKey.MatchString(in.TitleKey) && messageTimeKey.MatchString(in.TimeKey) && (in.Page == "pages/recovery-calendar/index" || in.Page == "pages/followups/index" || in.Page == "pages/appointments/index") && (in.State == "developer" || in.State == "trial" || in.State == "formal") && in.LeadMinutes >= 1 && in.LeadMinutes <= 10080 && in.Version >= 0
}
