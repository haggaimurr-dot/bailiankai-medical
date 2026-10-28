package httpapi

import (
	"errors"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func supportFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(c, 404, "NOT_FOUND", "工单、院区或负责人不存在或无权访问")
	case errors.Is(err, store.ErrForbidden):
		fail(c, 403, "FORBIDDEN", "仅负责人或主管可操作，接手人须在本院区在岗")
	case errors.Is(err, store.ErrConflict):
		fail(c, 409, "VERSION_CONFLICT", "记录已变化或请求键已使用，请刷新并核对后重试")
	case errors.Is(err, store.ErrCareState):
		fail(c, 409, "SUPPORT_STATE", "当前状态不允许操作，请先接手工单并检查状态")
	default:
		fail(c, 503, "SERVICE_UNAVAILABLE", "咨询服务暂不可用，请保留内容后重试")
	}
}
func supportStatus(v string) bool {
	return v == "open" || v == "in_progress" || v == "waiting_patient" || v == "resolved" || v == "closed"
}
func validSupportInput(in store.SupportInput, create, patient bool, now time.Time) bool {
	if !requestKeyPattern.MatchString(in.RequestKey) || (utf8.RuneCountInString(in.Content) < 1 && len(in.Attachments) == 0) || utf8.RuneCountInString(in.Content) > 2000 {
		return false
	}
	if len(in.Attachments) > 3 || (len(in.Attachments) > 0 && (create || (in.Action != "message" && in.Action != "note"))) {
		return false
	}
	for _, file := range in.Attachments {
		if len(file.Data) < 1 || len(file.Data) > 2<<20 || strings.TrimSpace(file.Name) == "" || utf8.RuneCountInString(file.Name) > 150 || strings.ContainsAny(file.Name, "/\\\r\n\x00") {
			return false
		}
		typ := http.DetectContentType(file.Data)
		extensions := map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".pdf": "application/pdf"}
		if expected := extensions[strings.ToLower(filepath.Ext(file.Name))]; expected == "" || expected != typ {
			return false
		}
	}
	if in.Action != "rate" && in.Rating != 0 {
		return false
	}
	if create {
		return in.ClinicID != "" && len(in.ClinicID) <= 64 && utf8.RuneCountInString(in.Subject) >= 1 && utf8.RuneCountInString(in.Subject) <= 100 && (in.Category == "general" || in.Category == "booking" || in.Category == "aftercare" || in.Category == "complaint")
	}
	if in.Version < 1 {
		return false
	}
	if patient {
		return in.Action == "message" || in.Action == "rate" && in.Rating >= 1 && in.Rating <= 5 && utf8.RuneCountInString(in.Content) <= 500
	}
	switch in.Action {
	case "message", "note", "auto_assign":
		return true
	case "assign":
		// The transaction checks future deadlines after looking up committed retries.
		return in.AssigneeID != "" && len(in.AssigneeID) <= 64 && in.DueAt != nil && in.DueAt.Before(now.Add(365*24*time.Hour))
	case "status":
		return supportStatus(in.Status)
	case "followup":
		return in.DueAt != nil && in.DueAt.Before(now.Add(365*24*time.Hour))
	}
	return false
}
func (a *API) registerSupportRoutes(r *gin.Engine) {
	available := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := a.repo.(store.SupportRepository); !ok {
			fail(c, 503, "SERVICE_UNAVAILABLE", "咨询服务暂不可用")
			return
		}
		c.Next()
	}
	for _, patient := range []bool{true, false} {
		prefix := "/api/v1/admin/support"
		authenticate := a.authenticate
		if patient {
			prefix = "/api/v1/me/support"
			authenticate = a.authenticatePatient
		}
		group := r.Group(prefix, authenticate, available)
		patientID := func(c *gin.Context) string {
			if patient {
				return currentPatient(c).ID
			}
			return ""
		}
		group.GET("", func(c *gin.Context) {
			if !validQuery(c, "status", "assignment", "overdue", "page", "pageSize") {
				return
			}
			page, size, ok := supportPage(c)
			if !ok {
				return
			}
			status, assignment := c.Query("status"), c.Query("assignment")
			if (status != "" && !supportStatus(status)) || (assignment != "" && assignment != "mine" && assignment != "unassigned") || (c.Query("overdue") != "" && c.Query("overdue") != "true" && c.Query("overdue") != "false") || (patient && (assignment != "" || c.Query("overdue") != "")) {
				fail(c, 400, "INVALID_QUERY", "筛选条件无效")
				return
			}
			rows, total, err := a.repo.(store.SupportRepository).SupportTickets(c.Request.Context(), store.SupportFilter{PatientID: patientID(c), Status: status, Assignment: assignment, Overdue: c.Query("overdue") == "true", Page: page, Size: size, Now: a.now()})
			if err != nil {
				supportFailure(c, err)
				return
			}
			c.JSON(200, gin.H{"data": rows, "meta": gin.H{"page": page, "pageSize": size, "total": total}})
		})
		group.GET("/:ticketId", func(c *gin.Context) {
			if !validQuery(c, "page", "pageSize") {
				return
			}
			page, size, ok := supportPage(c)
			if !ok {
				return
			}
			row, events, total, err := a.repo.(store.SupportRepository).SupportDetail(c.Request.Context(), c.Param("ticketId"), patientID(c), page, size)
			if err != nil {
				supportFailure(c, err)
				return
			}
			// Entering the ticket detail clears its unread reply badge (P2 咨询-患者端).
			_ = a.repo.(store.NotificationRepository).MarkSupportRepliesRead(c.Request.Context(), c.Param("ticketId"), patientID(c), a.now())
			c.JSON(200, gin.H{"data": gin.H{"ticket": row, "events": events}, "meta": gin.H{"page": page, "pageSize": size, "total": total}})
		})
		group.GET("/:ticketId/attachments/:attachmentId", func(c *gin.Context) {
			repo, ok := a.repo.(store.SupportAttachmentRepository)
			if !ok {
				fail(c, 503, "SERVICE_UNAVAILABLE", "附件服务暂不可用")
				return
			}
			file, err := repo.SupportAttachment(c.Request.Context(), c.Param("ticketId"), c.Param("attachmentId"), patientID(c))
			if err != nil {
				supportFailure(c, err)
				return
			}
			c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Name}))
			c.Header("X-Content-Type-Options", "nosniff")
			c.Data(200, file.MimeType, file.Data)
		})
		group.POST("/:ticketId/actions", func(c *gin.Context) {
			var in store.SupportInput
			if !decodeObjectLimit(c, &in, []string{"action", "content", "requestKey", "version"}, []string{"assigneeId", "status", "dueAt", "rating", "attachments"}, 9<<20) {
				return
			}
			in.Content = strings.TrimSpace(in.Content)
			if !validSupportInput(in, false, patient, a.now()) {
				fail(c, 400, "INVALID_BODY", "请输入有效内容和操作参数；附件限3个JPG、PNG或PDF，每个2MB；评分为1–5星")
				return
			}
			row, err := a.repo.(store.SupportRepository).ChangeSupport(c.Request.Context(), c.Param("ticketId"), patientID(c), in, a.careNow())
			if err != nil {
				supportFailure(c, err)
				return
			}
			c.JSON(200, gin.H{"data": row})
		})
		if patient {
			group.POST("", func(c *gin.Context) {
				var in store.SupportInput
				if !decodeObject(c, &in, []string{"clinicId", "subject", "category", "content", "requestKey"}, nil) {
					return
				}
				in.Subject = strings.TrimSpace(in.Subject)
				in.Content = strings.TrimSpace(in.Content)
				if !validSupportInput(in, true, true, a.now()) {
					fail(c, 400, "INVALID_BODY", "标题限100字，咨询内容限2000字，请选择院区及咨询类型")
					return
				}
				row, err := a.repo.(store.SupportRepository).CreateSupport(c.Request.Context(), currentPatient(c).ID, in, a.careNow())
				if err != nil {
					supportFailure(c, err)
					return
				}
				c.JSON(200, gin.H{"data": row})
			})
		} else {
			group.GET("/agents", func(c *gin.Context) {
				rows, err := a.repo.(store.SupportRepository).SupportAgents(c.Request.Context())
				if err != nil {
					supportFailure(c, err)
					return
				}
				c.JSON(200, gin.H{"data": rows})
			})
		}
	}
}
func supportPage(c *gin.Context) (int, int, bool) {
	page, size := 1, 20
	for key, target := range map[string]*int{"page": &page, "pageSize": &size} {
		if v := c.Query(key); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 1000000 || (key == "pageSize" && n > 100) {
				fail(c, 400, "INVALID_QUERY", "分页参数无效")
				return 0, 0, false
			}
			*target = n
		}
	}
	return page, size, true
}
