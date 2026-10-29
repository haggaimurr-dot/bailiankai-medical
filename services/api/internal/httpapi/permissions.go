package httpapi

import (
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"strings"
)

func allowedEmployeeOperation(role, method, path string) bool {
	if path == "/api/v1/admin/assessments" || strings.HasPrefix(path, "/api/v1/admin/assessments/") {
		if role != "manager" && role != "doctor" {
			return false
		}
		switch path {
		case "/api/v1/admin/assessments", "/api/v1/admin/assessments/:assessmentId", "/api/v1/admin/assessments/:assessmentId/events", "/api/v1/admin/assessments/:assessmentId/photos/:photoId/content", "/api/v1/admin/assessments/review-stats":
			return method == "GET"
		case "/api/v1/admin/assessments/reviewers", "/api/v1/admin/assessments/access-audit":
			return role == "manager" && method == "GET"
		case "/api/v1/admin/assessments/:assessmentId/assignment":
			return role == "manager" && method == "POST"
		case "/api/v1/admin/assessments/:assessmentId/reviews":
			return role == "doctor" && method == "POST"
		default:
			return false
		}
	}
	if path == "/api/v1/admin/patients/:patientId/wechat-binding" {
		return (role == "manager" || role == "doctor" || role == "clinician") && (method == "POST" || method == "DELETE")
	}
	if path == "/api/v1/admin/consultation-templates" || path == "/api/v1/admin/consultation-templates/:templateId" {
		return (role == "manager" || role == "doctor" || role == "clinician") && (method == "GET" && path == "/api/v1/admin/consultation-templates" || method == "POST" && path == "/api/v1/admin/consultation-templates" || method == "DELETE" && path == "/api/v1/admin/consultation-templates/:templateId")
	}
	if method == "POST" && path == "/api/v1/admin/patients" || method == "PUT" && path == "/api/v1/admin/patients/:patientId/profile" {
		return role == "manager" || role == "doctor" || role == "clinician"
	}
	if path == "/api/v1/admin/care-plan-templates" || path == "/api/v1/admin/care-plan-templates/:templateId" {
		return (role == "manager" || store.ClinicalRole(role)) && (method == "GET" || method == "POST" || method == "DELETE")
	}
	if path == "/api/v1/admin/support" || path == "/api/v1/admin/support/agents" || path == "/api/v1/admin/support/stats" || path == "/api/v1/admin/support/:ticketId" || path == "/api/v1/admin/support/:ticketId/actions" || path == "/api/v1/admin/support/:ticketId/attachments/:attachmentId" {
		return (role == "manager" || role == "customer_service") && (method == "GET" || method == "POST" && path == "/api/v1/admin/support/:ticketId/actions")
	}
	if !store.ValidEmployeeRole(role) {
		return false
	}
	if path == "/api/v1/admin/session" || method == "GET" && path == "/api/v1/admin/permissions" || method == "POST" && path == "/api/v1/admin/session/password" {
		return true
	}
	if allowed, exports := recordExportPermission(role, method, path); exports {
		return allowed
	}
	if allowed, knowledge := knowledgePermission(role, method, path); knowledge {
		return allowed
	}
	if allowed, cases := casePermission(role, method, path); cases {
		return allowed
	}
	if path == "/api/v1/admin/handoff-notifications" && method == "GET" || path == "/api/v1/admin/handoff-notifications/:notificationId/read" && method == "POST" {
		return role == "manager" || store.ClinicalRole(role)
	}
	if path == "/api/v1/admin/appointment-notifications" && method == "GET" || path == "/api/v1/admin/appointment-notifications/:notificationId/read" && method == "POST" {
		return role == "manager" || role == "customer_service"
	}
	if path == "/api/v1/admin/reviews" || path == "/api/v1/admin/reviews/:reviewId/review" {
		return (role == "manager" || role == "customer_service") && (method == "GET" && path == "/api/v1/admin/reviews" || method == "POST" && path == "/api/v1/admin/reviews/:reviewId/review")
	}
	if path == "/api/v1/admin/arrival-scans/resolve" || path == "/api/v1/admin/arrival-scans/confirm" {
		return method == "POST" && (role == "manager" || role == "customer_service" || role == "doctor" || role == "clinician")
	}
	if path == "/api/v1/admin/appointments/:appointmentId/suspension-reschedule" {
		return method == "POST" && (role == "manager" || role == "customer_service")
	}
	if role == "manager" {
		return true
	}
	if role == "operations" {
		return method == "GET" && (path == "/api/v1/admin/overview" || path == "/api/v1/admin/analytics" || path == "/api/v1/admin/analytics/funnel" || path == "/api/v1/admin/appointments/cancellation-stats")
	}
	if role == "customer_service" {
		return (method == "GET" && (path == "/api/v1/admin/appointments" || path == "/api/v1/admin/appointments/:appointmentId" || path == "/api/v1/admin/patients" || path == "/api/v1/admin/waitlist" || path == "/api/v1/admin/appointments/cancellation-stats")) || (method == "POST" && (path == "/api/v1/admin/appointments/:appointmentId/review" || path == "/api/v1/admin/appointments/:appointmentId/arrival" || path == "/api/v1/admin/appointments/remind"))
	}
	if !store.ClinicalRole(role) {
		return false
	}
	if method == "GET" {
		switch path {
		case "/api/v1/admin/followup-templates", "/api/v1/admin/appointments/:appointmentId/nursing-feedback":
			return true
		case "/api/v1/admin/appointments", "/api/v1/admin/appointments/:appointmentId", "/api/v1/admin/patients", "/api/v1/admin/patients/:patientId", "/api/v1/admin/appointments/:appointmentId/photos", "/api/v1/admin/appointments/:appointmentId/care-plan", "/api/v1/admin/photos/:photoId/content", "/api/v1/admin/appointments/:appointmentId/followups", "/api/v1/admin/followup-staff", "/api/v1/admin/followups", "/api/v1/admin/handoff-receipts", "/api/v1/admin/followups/:taskId/assignments", "/api/v1/admin/followup-photos/:photoId/content":
			return true
		}
	}
	if method == "POST" && path == "/api/v1/admin/appointments/:appointmentId/nursing-feedback" {
		return true
	}
	if method == "POST" && (path == "/api/v1/admin/followups/:taskId/review" || path == "/api/v1/admin/handoff-receipts/:receiptId/confirm") {
		return true
	}
	if role == "nurse" {
		return false
	}
	if method == "POST" && path == "/api/v1/admin/appointments/:appointmentId/arrival" {
		return true
	}
	return method == "PUT" && path == "/api/v1/admin/appointments/:appointmentId/care-plan/draft" || method == "POST" && (path == "/api/v1/admin/appointments/:appointmentId/care-plan/publish" || path == "/api/v1/admin/appointments/:appointmentId/treatment" || path == "/api/v1/admin/appointments/:appointmentId/treatment/corrections" || path == "/api/v1/admin/patients/:patientId/consultations")

}

// Page access is derived from the same operation checks used by authentication.

type accessRule struct {
	Key    string
	Label  string
	Method string
	Path   string
}

var employeeAccessRules = []accessRule{
	{"/assessments", "AI 辅助评估", "GET", "/api/v1/admin/assessments"},
	{"/support", "客服工作台", "GET", "/api/v1/admin/support"},
	{"/knowledge", "知识库", "GET", "/api/v1/admin/knowledge/articles"},
	{"/arrivals", "到诊工作台", "POST", "/api/v1/admin/arrival-scans/resolve"},
	{"/workbench", "待办工作台", "GET", "/api/v1/admin/handoff-notifications"},
	{"/handoffs", "人员交接", "GET", "/api/v1/admin/handoff-receipts"},
	{"/clinics", "院区资料维护", "GET", "/api/v1/admin/clinics"},
	{"/overview", "医生统计与运营分析", "GET", "/api/v1/admin/analytics"},
	{"/appointments", "预约管理", "GET", "/api/v1/admin/appointments"},
	{"/waitlist", "候补队列", "GET", "/api/v1/admin/waitlist"},
	{"/reviews", "患者评价审核", "GET", "/api/v1/admin/reviews"},
	{"/patients", "患者档案", "GET", "/api/v1/admin/patients/:patientId"},
	{"/care", "方案与照片", "GET", "/api/v1/admin/appointments/:appointmentId/care-plan"},
	{"/followups", "治疗与随访", "GET", "/api/v1/admin/followups"},
	{"/schedule", "排班工作台", "PATCH", "/api/v1/admin/slots/:slotId"},
	{"/employees", "员工与权限", "GET", "/api/v1/admin/employees"},
	{"/messages", "消息与复诊提醒", "GET", "/api/v1/admin/messages/templates"},
	{"/cases", "案例库", "GET", "/api/v1/admin/cases"},

	{"/doctors", "医生信息（公开资料）", "GET", "/api/v1/doctors"},
	{"/account", "个人修改密码", "POST", "/api/v1/admin/session/password"},
	{"appointment.review", "确认 / 拒绝预约", "POST", "/api/v1/admin/appointments/:appointmentId/review"},
	{"care.write", "编写 / 发布方案", "PUT", "/api/v1/admin/appointments/:appointmentId/care-plan/draft"},
	{"treatment.write", "登记治疗", "POST", "/api/v1/admin/appointments/:appointmentId/treatment"},
	{"followup.review", "审核随访", "POST", "/api/v1/admin/followups/:taskId/review"},
	{"employee.access", "调整员工角色与关联人员", "POST", "/api/v1/admin/employees/:employeeId/access"},
}

func employeeCapabilities(role string) []string {
	result := make([]string, 0)
	for _, rule := range employeeAccessRules {
		if store.ValidEmployeeRole(role) && (rule.Path == "/api/v1/doctors" || allowedEmployeeOperation(role, rule.Method, rule.Path)) {
			result = append(result, rule.Key)
		}
	}
	return result

}

func (a *API) employeePermissions(c *gin.Context) {
	roles := []string{"manager", "doctor", "nurse", "customer_service", "operations", "clinician"}
	rows := make([]gin.H, 0)
	for _, rule := range employeeAccessRules {
		allowed := make(map[string]bool)
		for _, role := range roles {
			for _, key := range employeeCapabilities(role) {
				if key == rule.Key {
					allowed[role] = true
				}
			}
			if _, ok := allowed[role]; !ok {
				allowed[role] = false
			}
		}
		rows = append(rows, gin.H{"key": rule.Key, "label": rule.Label, "roles": allowed})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"data": rows})

}
