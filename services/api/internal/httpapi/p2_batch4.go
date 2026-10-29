package httpapi

import (
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) registerP2Batch4Routes(r *gin.Engine) {
	// 行 115：双因素验证（主管/员工自助）
	r.POST("/api/v1/admin/2fa/setup", a.authenticate, a.setup2FA)
	r.POST("/api/v1/admin/2fa/enable", a.authenticate, a.enable2FA)
	r.POST("/api/v1/admin/2fa/disable", a.authenticate, a.disable2FA)
	r.GET("/api/v1/admin/2fa/status", a.authenticate, a.twoFAStatus)
	// 2FA 登录第二步（公开，需 pending 的 employeeId + 验证码）
	r.POST("/api/v1/admin/session/2fa", a.verify2FALogin)
}

func (a *API) registerP2Batch4PatientRoutes(patient *gin.RouterGroup) {
	// 行 114：提醒偏好
	patient.GET("/notification-preferences", a.getNotificationPreferences)
	patient.PUT("/notification-preferences", a.updateNotificationPreferences)
}

// ============ 行 115 ============

func (a *API) setup2FA(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	employee := currentEmployee(c)
	secret, err := a.repo.(*store.MySQL).SetupEmployee2FA(c.Request.Context(), employee.ID, a.now())
	if err != nil {
		fail(c, 500, "SERVER_ERROR", "生成失败")
		return
	}
	// otpauth URL 可直接被验证器 App 扫码
	c.JSON(200, gin.H{"data": gin.H{
		"secret": secret,
		"otpauthUrl": "otpauth://totp/百莲凯:" + employee.Username + "?secret=" + secret + "&issuer=百莲凯",
	}})
}

func (a *API) enable2FA(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	employee := currentEmployee(c)
	var body struct {
		Code string `json:"code"`
	}
	if !decodeObject(c, &body, []string{"code"}, nil) {
		return
	}
	if err := a.repo.(*store.MySQL).VerifyEmployee2FA(c.Request.Context(), employee.ID, body.Code, a.now()); err != nil {
		fail(c, 400, "INVALID_CODE", "验证码错误")
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"enabled": true}})
}

func (a *API) disable2FA(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	employee := currentEmployee(c)
	var body struct {
		Code string `json:"code"`
	}
	if !decodeObject(c, &body, []string{"code"}, nil) {
		return
	}
	if err := a.repo.(*store.MySQL).DisableEmployee2FA(c.Request.Context(), employee.ID, body.Code, a.now()); err != nil {
		fail(c, 400, "INVALID_CODE", "验证码错误")
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"enabled": false}})
}

func (a *API) twoFAStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	employee := currentEmployee(c)
	enabled, err := a.repo.(*store.MySQL).Employee2FAEnabled(c.Request.Context(), employee.ID)
	if err != nil {
		fail(c, 500, "SERVER_ERROR", "查询失败")
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"enabled": enabled}})
}

func (a *API) verify2FALogin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var body struct {
		EmployeeID string `json:"employeeId"`
		Code       string `json:"code"`
	}
	if !decodeObject(c, &body, []string{"employeeId", "code"}, nil) {
		return
	}
	if !validID(body.EmployeeID) {
		fail(c, 400, "INVALID_ID", "标识无效")
		return
	}
	repo := a.repo.(*store.MySQL)
	if err := repo.CheckEmployee2FACode(c.Request.Context(), body.EmployeeID, body.Code, a.now()); err != nil {
		fail(c, 401, "INVALID_CODE", "验证码错误")
		return
	}
	account, err := repo.EmployeeAccountByID(c.Request.Context(), body.EmployeeID)
	if err != nil || !account.Active {
		fail(c, 401, "INVALID_CODE", "账号无效")
		return
	}
	token, err := repo.CreateEmployeeSession(c.Request.Context(), body.EmployeeID, a.now())
	if err != nil {
		fail(c, 500, "SERVER_ERROR", "登录失败")
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"employee": employeeDTO(account), "token": token}})
}

// ============ 行 114 ============

func (a *API) getNotificationPreferences(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	patient := currentPatient(c)
	pref, err := a.repo.(*store.MySQL).NotificationPreferences(c.Request.Context(), patient.ID)
	if err != nil {
		fail(c, 500, "SERVER_ERROR", "查询失败")
		return
	}
	c.JSON(200, gin.H{"data": preferenceItem(pref)})
}

func (a *API) updateNotificationPreferences(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	patient := currentPatient(c)
	var body struct {
		AppointmentReminder bool `json:"appointmentReminder"`
		FollowupReminder    bool `json:"followupReminder"`
		CareReminder        bool `json:"careReminder"`
		LeadHours           int  `json:"leadHours"`
	}
	if !decodeObject(c, &body, []string{"appointmentReminder", "followupReminder", "careReminder", "leadHours"}, nil) {
		return
	}
	pref, err := a.repo.(*store.MySQL).UpdateNotificationPreferences(c.Request.Context(), patient.ID,
		body.AppointmentReminder, body.FollowupReminder, body.CareReminder, body.LeadHours, a.now())
	if err != nil {
		fail(c, 400, "INVALID_BODY", "提前量须为 0–168 小时")
		return
	}
	c.JSON(200, gin.H{"data": preferenceItem(pref)})
}

func preferenceItem(p store.NotificationPreference) gin.H {
	return gin.H{
		"appointmentReminder": p.AppointmentReminder,
		"followupReminder":    p.FollowupReminder,
		"careReminder":        p.CareReminder,
		"leadHours":           p.LeadHours,
		"updatedAt":           p.UpdatedAt,
	}
}
