package httpapi

import (
	"errors"
	"regexp"
	"strings"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) bindingRepo(c *gin.Context) store.PatientBindingRepository {
	c.Header("Cache-Control", "no-store")
	repo, _ := a.repo.(store.PatientBindingRepository)
	if repo == nil {
		fail(c, 503, "SERVICE_UNAVAILABLE", "患者身份绑定服务暂不可用")
	}
	return repo
}
func (a *API) bindingError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrBindingInvalid):
		fail(c, 400, "BINDING_CODE_INVALID", "绑定码无效、已过期、已撤销或已使用，请联系建档医护重新生成")
	case errors.Is(err, store.ErrBindingConflict):
		fail(c, 409, "PATIENT_BINDING_CONFLICT", "档案已绑定、资料已变化，或当前微信账号已有业务记录；请联系建档医护核对，不能自动合并")
	case errors.Is(err, store.ErrUnauthenticated):
		a.patientError(c, err)
	default:
		a.patientRecordError(c, err)
	}
}
func (a *API) createPatientBindingCode(c *gin.Context) {
	repo := a.bindingRepo(c)
	if repo == nil {
		return
	}
	if !validID(c.Param("patientId")) {
		fail(c, 400, "INVALID_BODY", "患者编号无效")
		return
	}
	code, expires, err := repo.CreatePatientBindingCode(c.Request.Context(), c.Param("patientId"), currentEmployee(c).ID, a.now())
	if err != nil {
		a.bindingError(c, err)
		return
	}
	c.JSON(201, gin.H{"data": gin.H{"code": code, "expiresAt": expires}})
}
func (a *API) revokePatientBindingCode(c *gin.Context) {
	repo := a.bindingRepo(c)
	if repo == nil {
		return
	}
	if !validID(c.Param("patientId")) {
		fail(c, 400, "INVALID_BODY", "患者编号无效")
		return
	}
	if err := repo.RevokePatientBindingCodes(c.Request.Context(), c.Param("patientId"), a.now()); err != nil {
		a.bindingError(c, err)
		return
	}
	c.Status(204)
}
func (a *API) decodeBinding(c *gin.Context, confirm bool) (string, int, bool) {
	var body struct {
		Code    string `json:"code"`
		Version int    `json:"version"`
	}
	required := []string{"code"}
	if confirm {
		required = append(required, "version")
	}
	if !decodeObject(c, &body, required, nil) {
		return "", 0, false
	}
	body.Code = strings.ToLower(strings.TrimSpace(body.Code))
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(body.Code) || (confirm && body.Version < 1) {
		fail(c, 400, "INVALID_BODY", "请填写完整的32位绑定码")
		return "", 0, false
	}
	return body.Code, body.Version, true
}
func (a *API) previewPatientBinding(c *gin.Context) {
	repo := a.bindingRepo(c)
	if repo == nil {
		return
	}
	if currentPatient(c).WechatOpenID == nil {
		fail(c, 401, "PATIENT_SESSION_INVALID", "请登录微信患者账号")
		return
	}
	code, _, ok := a.decodeBinding(c, false)
	if !ok {
		return
	}
	preview, err := repo.PreviewPatientBinding(c.Request.Context(), code, a.now())
	if err != nil {
		a.bindingError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": preview})
}
func (a *API) bindPatient(c *gin.Context) {
	repo := a.bindingRepo(c)
	if repo == nil {
		return
	}
	code, version, ok := a.decodeBinding(c, true)
	if !ok {
		return
	}
	session, err := repo.BindPatient(c.Request.Context(), currentPatient(c).ID, code, version, a.now())
	if err != nil {
		a.bindingError(c, err)
		return
	}
	c.JSON(200, apitypes.PatientSessionResult{Data: apitypes.PatientSession{Token: session.Token, ExpiresAt: session.ExpiresAt, Patient: patientIdentity(session.Patient)}})
}
