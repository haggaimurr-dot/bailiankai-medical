package httpapi

import (
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func validFollowupOffsets(offsets []int) bool {
	if len(offsets) < 1 || len(offsets) > 8 {
		return false
	}
	seen := map[int]bool{}
	for _, day := range offsets {
		if day < 1 || day > 90 || seen[day] {
			return false
		}
		seen[day] = true
	}
	return true
}
func validFollowupStandard(in model.FollowupTemplate) bool {
	if in.StandardStatus != "draft" && in.StandardStatus != "formal" {
		return false
	}
	if utf8.RuneCountInString(in.ApplicableTreatment) > 200 || utf8.RuneCountInString(in.NursingInstructions) > 2000 || utf8.RuneCountInString(in.FollowupChecklist) > 2000 {
		return false
	}
	if in.StandardStatus == "draft" {
		return !in.Active
	}
	for _, value := range []string{in.ApplicableTreatment, in.NursingInstructions, in.FollowupChecklist, in.Response, in.Disposition} {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}
func (a *API) registerFollowupToolsRoutes(r *gin.Engine) {
	available := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := a.repo.(store.FollowupToolsRepository); !ok {
			fail(c, 503, "SERVICE_UNAVAILABLE", "护理与模板服务暂不可用")
			return
		}
		c.Next()
	}
	admin := r.Group("/api/v1/admin", a.authenticate, available)
	admin.GET("/followup-templates", func(c *gin.Context) {
		if !validQuery(c) {
			return
		}
		rows, err := a.repo.(store.FollowupToolsRepository).FollowupTemplates(c.Request.Context())
		if err != nil {
			a.careError(c, err)
			return
		}
		c.JSON(200, gin.H{"data": rows})
	})
	save := func(c *gin.Context) {
		var in model.FollowupTemplate
		if !decodeObjectLimit(c, &in, []string{"clinicId", "name", "offsets", "response", "disposition", "active", "version"}, []string{"standardStatus", "applicableTreatment", "nursingInstructions", "followupChecklist"}, 32768) {
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		in.Response = strings.TrimSpace(in.Response)
		in.Disposition = strings.TrimSpace(in.Disposition)
		if !validID(in.ClinicID) || in.Name == "" || utf8.RuneCountInString(in.Name) > 100 || utf8.RuneCountInString(in.Response) > 2000 || utf8.RuneCountInString(in.Disposition) > 1000 || !validFollowupOffsets(in.Offsets) || (c.Param("templateId") == "" && in.Version != 0) || (c.Param("templateId") != "" && (in.Version < 1 || !validID(c.Param("templateId")))) {
			fail(c, 400, "INVALID_TEMPLATE", "请填写模板名称、院区及 1–90 天不重复的随访天数（最多 8 次）")
			return
		}
		in.ApplicableTreatment = strings.TrimSpace(in.ApplicableTreatment)
		in.NursingInstructions = strings.TrimSpace(in.NursingInstructions)
		in.FollowupChecklist = strings.TrimSpace(in.FollowupChecklist)
		if in.StandardStatus == "" {
			in.StandardStatus = "draft"
		}
		if !validFollowupStandard(in) {
			fail(c, 400, "INVALID_STANDARD", "正式规范须填写适用治疗、护理要求、随访检查要点、回评文本及异常处理；草稿不可启用")
			return
		}
		in.ID = c.Param("templateId")
		sort.Ints(in.Offsets)
		row, err := a.repo.(store.FollowupToolsRepository).SaveFollowupTemplate(c.Request.Context(), in, a.careNow())
		if err != nil {
			a.careError(c, err)
			return
		}
		c.JSON(200, gin.H{"data": row})
	}
	admin.POST("/followup-templates", a.requireManager, save)
	admin.PUT("/followup-templates/:templateId", a.requireManager, save)
	admin.GET("/appointments/:appointmentId/nursing-feedback", a.requireAppointmentScope, func(c *gin.Context) {
		if !validQuery(c, "dateFrom", "dateTo") {
			return
		}
		dates, ok := clinicalDateRange(c)
		if !ok {
			return
		}
		rows, err := a.repo.(store.FollowupToolsRepository).NursingFeedbacks(c.Request.Context(), c.Param("appointmentId"), dates)
		if err != nil {
			a.careError(c, err)
			return
		}
		c.JSON(200, gin.H{"data": rows})
	})
	admin.POST("/appointments/:appointmentId/nursing-feedback", a.requireAppointmentScope, func(c *gin.Context) {
		var in model.NursingFeedback
		if !decodeObjectLimit(c, &in, []string{"requestKey", "observedAt", "content", "action"}, nil, 32768) {
			return
		}
		in.Content = strings.TrimSpace(in.Content)
		in.Action = strings.TrimSpace(in.Action)
		if !validID(in.RequestKey) || in.Content == "" || utf8.RuneCountInString(in.Content) > 2000 || in.Action == "" || utf8.RuneCountInString(in.Action) > 1000 || in.ObservedAt.IsZero() || in.ObservedAt.After(a.now()) || in.ObservedAt.Year() < 1 {
			fail(c, 400, "INVALID_FEEDBACK", "请填写护理情况（最多 2000 字）、实际措施（最多 1000 字）及非未来时间")
			return
		}
		in.ObservedAt = in.ObservedAt.Truncate(time.Millisecond)
		in.AppointmentID = c.Param("appointmentId")
		in.ActorName = currentEmployee(c).DisplayName
		row, err := a.repo.(store.FollowupToolsRepository).RecordNursingFeedback(c.Request.Context(), in, a.careNow())
		if err != nil {
			a.careError(c, err)
			return
		}
		c.JSON(200, gin.H{"data": row})
	})
}
