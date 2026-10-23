package httpapi

import (
	"errors"
	"strings"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/store"
	"example.com/bailiankai/api/internal/wechat"
	"github.com/gin-gonic/gin"
)

func (a *API) patientRepository(c *gin.Context) store.PatientRepository {
	repo, _ := a.repo.(store.PatientRepository)
	if repo == nil {
		fail(c, 503, "PATIENT_LOGIN_UNAVAILABLE", "患者账号服务暂不可用")
	}
	return repo
}
func (a *API) patientError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrPatientLoginUnavailable):
		fail(c, 503, "PATIENT_LOGIN_UNAVAILABLE", "微信登录尚未配置，请联系管理员")
	case errors.Is(err, wechat.ErrRejected):
		fail(c, 400, "WECHAT_CODE_INVALID", "微信凭证无效或已使用，请重新操作")
	case errors.Is(err, store.ErrConflict):
		fail(c, 409, "VERSION_CONFLICT", "资料已在其他设备修改，请刷新后重试")
	case errors.Is(err, store.ErrUnauthenticated):
		fail(c, 401, "PATIENT_SESSION_INVALID", "请登录微信患者账号")
	default:
		a.unavailable(c, err)
	}
}
func validWechatCode(code string) bool {
	return len(code) > 0 && len(code) <= 512 && strings.TrimSpace(code) == code
}

func (a *API) loginPatient(c *gin.Context) {
	repo := a.patientRepository(c)
	if repo == nil {
		return
	}
	var body apitypes.PatientLogin
	if !decodeObject(c, &body, []string{"code"}, nil) {
		return
	}
	if !validWechatCode(body.Code) {
		fail(c, 400, "INVALID_BODY", "微信登录凭证格式无效")
		return
	}
	session, err := repo.LoginPatient(c.Request.Context(), body.Code, a.now())
	if err != nil {
		a.patientError(c, err)
		return
	}
	c.JSON(200, apitypes.PatientSessionResult{Data: apitypes.PatientSession{Token: session.Token, ExpiresAt: session.ExpiresAt, Patient: patientIdentity(session.Patient)}})
}
func (a *API) updatePatientProfile(c *gin.Context) {
	repo := a.patientRepository(c)
	if repo == nil {
		return
	}
	var body apitypes.PatientProfileUpdate
	if !decodeObject(c, &body, []string{"displayName", "version"}, nil) {
		return
	}
	name := strings.TrimSpace(body.DisplayName)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 40 || body.Version < 1 {
		fail(c, 400, "INVALID_BODY", "请填写1–40字姓名和有效资料版本")
		return
	}
	patient, err := repo.UpdatePatientProfile(c.Request.Context(), currentPatient(c).ID, name, int(body.Version))
	if err != nil {
		a.patientError(c, err)
		return
	}
	c.JSON(200, apitypes.PatientIdentityResult{Data: patientIdentity(patient)})
}
func (a *API) authorizePatientPhone(c *gin.Context) {
	repo := a.patientRepository(c)
	if repo == nil {
		return
	}
	var body apitypes.PatientPhoneAuthorization
	if !decodeObject(c, &body, []string{"code", "version"}, nil) {
		return
	}
	if !validWechatCode(body.Code) || body.Version < 1 {
		fail(c, 400, "INVALID_BODY", "手机号授权参数无效")
		return
	}
	patient, err := repo.AuthorizePatientPhone(c.Request.Context(), currentPatient(c).ID, body.Code, int(body.Version), a.now())
	if err != nil {
		a.patientError(c, err)
		return
	}
	c.JSON(200, apitypes.PatientIdentityResult{Data: patientIdentity(patient)})
}
func (a *API) revokePatientPhone(c *gin.Context) {
	repo := a.patientRepository(c)
	if repo == nil {
		return
	}
	var body apitypes.PatientVersionAction
	if !decodeObject(c, &body, []string{"version"}, nil) {
		return
	}
	if body.Version < 1 {
		fail(c, 400, "INVALID_BODY", "资料版本无效")
		return
	}
	patient, err := repo.RevokePatientPhone(c.Request.Context(), currentPatient(c).ID, int(body.Version))
	if err != nil {
		a.patientError(c, err)
		return
	}
	c.JSON(200, apitypes.PatientIdentityResult{Data: patientIdentity(patient)})
}
func (a *API) logoutPatient(c *gin.Context) {
	repo := a.patientRepository(c)
	if repo == nil {
		return
	}
	if err := repo.LogoutPatient(c.Request.Context(), strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")); err != nil {
		a.patientError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": true})
}
