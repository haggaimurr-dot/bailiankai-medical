package httpapi

import (
	"errors"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"strings"
)

func requiresPrivacyConsent(method, path string) bool {
	if method == "GET" || method == "DELETE" {
		return false
	}
	if path == "/api/v1/me/assessments" || strings.HasPrefix(path, "/api/v1/me/assessments/") && (strings.HasSuffix(path, "/retry") || strings.HasSuffix(path, "/quality-check")) {
		return true
	}
	if path == "/api/v1/me/profile" || path == "/api/v1/me/phone" || path == "/api/v1/me/appointments" || strings.HasPrefix(path, "/api/v1/me/support") {
		return true
	}
	return strings.Contains(path, "/photos/") || strings.HasSuffix(path, "/submit")
}
func (a *API) privacyPolicy(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"data": gin.H{"version": store.PrivacyPolicyVersion, "title": "百莲凯隐私说明（试点草案）", "content": store.PrivacyPolicyContent, "effectiveAt": "2026-10-02", "status": "draft"}})
}
func privacyEventDTO(row model.PrivacyConsentEvent) gin.H {
	return gin.H{"id": row.ID, "policyVersion": row.PolicyVersion, "action": row.Action, "source": row.Source, "recordedAt": row.RecordedAt}
}
func (a *API) privacyConsent(c *gin.Context) {
	if !validQuery(c) {
		return
	}
	repo, ok := a.repo.(store.PrivacyRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "隐私服务暂不可用")
		return
	}
	rows, err := repo.PrivacyConsentEvents(c.Request.Context(), currentPatient(c).ID)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	events := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		events = append(events, privacyEventDTO(row))
	}
	active := len(rows) > 0 && rows[0].PolicyVersion == store.PrivacyPolicyVersion && rows[0].Action == "accepted"
	c.JSON(200, gin.H{"data": gin.H{"policyVersion": store.PrivacyPolicyVersion, "accepted": active, "events": events}})
}
func (a *API) setPrivacyConsent(c *gin.Context) {
	var body struct {
		PolicyVersion string `json:"policyVersion"`
		Action        string `json:"action"`
	}
	if !decodeObject(c, &body, []string{"policyVersion", "action"}, nil) {
		return
	}
	if body.Action != "accepted" && body.Action != "revoked" {
		fail(c, 400, "INVALID_PRIVACY_ACTION", "隐私同意操作无效")
		return
	}
	repo, ok := a.repo.(store.PrivacyRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "隐私服务暂不可用")
		return
	}
	row, err := repo.RecordPrivacyConsent(c.Request.Context(), currentPatient(c).ID, body.PolicyVersion, body.Action, a.now())
	if errors.Is(err, store.ErrPrivacyVersion) {
		fail(c, 409, "PRIVACY_VERSION_CHANGED", "隐私协议已更新，请阅读新版本后重新确认")
		return
	}
	if errors.Is(err, store.ErrCareState) {
		fail(c, 400, "INVALID_PRIVACY_ACTION", "隐私同意操作无效")
		return
	}
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(200, gin.H{"data": privacyEventDTO(row)})
}
