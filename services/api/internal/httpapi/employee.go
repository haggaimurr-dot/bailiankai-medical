package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

var usernamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{2,39}$`)

func employeeDTO(row model.EmployeeAccount) gin.H {
	return gin.H{"id": row.ID, "username": row.Username, "displayName": row.DisplayName, "role": row.Role, "staffId": row.StaffID, "clinicIds": row.ClinicIDs, "active": row.Active, "version": row.Version, "capabilities": employeeCapabilities(row.Role)}
}
func employeeFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrForbidden):
		fail(c, 403, "FORBIDDEN", "无权管理目标账号或院区")
	case errors.Is(err, store.ErrAccountExists):
		fail(c, 409, "ACCOUNT_EXISTS", "账号或医护人员已被使用")
	case errors.Is(err, store.ErrConflict):
		fail(c, 409, "VERSION_CONFLICT", "账号状态已变化，请刷新")
	case errors.Is(err, store.ErrNotFound):
		fail(c, 404, "EMPLOYEE_NOT_FOUND", "员工不存在")
	case errors.Is(err, store.ErrCareState):
		fail(c, 409, "ACCOUNT_STATE", "账号状态不允许此操作")
	default:
		fail(c, 503, "SERVICE_UNAVAILABLE", "员工服务暂不可用")
	}
}
func (a *API) loginEmployee(c *gin.Context) {
	if a.employees == nil {
		fail(c, 503, "SERVICE_UNAVAILABLE", "员工服务暂不可用")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeObject(c, &body, []string{"username", "password"}, nil) {
		return
	}
	username := strings.ToLower(strings.TrimSpace(body.Username))
	if !usernamePattern.MatchString(username) || len(body.Password) < 1 || len(body.Password) > 128 {
		fail(c, 400, "INVALID_LOGIN", "账号或密码格式无效")
		return
	}
	account, token, err := a.employees.LoginEmployee(c.Request.Context(), username, body.Password, a.now())
	if err != nil {
		if errors.Is(err, store.ErrInvalidCredentials) {
			fail(c, 401, "INVALID_CREDENTIALS", "账号或密码错误，或账号已停用")
			return
		}
		a.unavailable(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"data": gin.H{"employee": employeeDTO(account), "token": token}})
}
func (a *API) employeeSession(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"data": employeeDTO(currentEmployee(c))})
}
func (a *API) logoutEmployee(c *gin.Context) {
	if err := a.employees.LogoutEmployee(c.Request.Context(), strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "), a.now()); err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"loggedOut": true}})
}
func (a *API) listEmployees(c *gin.Context) {
	if !validQuery(c) {
		return
	}
	rows, err := a.employees.Employees(c.Request.Context())
	if err != nil {
		a.unavailable(c, err)
		return
	}
	data := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		data = append(data, employeeDTO(row))
	}
	c.JSON(200, gin.H{"data": data})
}
func (a *API) createEmployee(c *gin.Context) {
	var body struct {
		Username    string   `json:"username"`
		DisplayName string   `json:"displayName"`
		Password    string   `json:"password"`
		Role        string   `json:"role"`
		StaffID     string   `json:"staffId"`
		ClinicIDs   []string `json:"clinicIds"`
	}
	if !decodeObject(c, &body, []string{"username", "displayName", "password", "role", "clinicIds"}, []string{"staffId"}) {
		return
	}
	username, name := strings.ToLower(strings.TrimSpace(body.Username)), strings.TrimSpace(body.DisplayName)
	if !usernamePattern.MatchString(username) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 80 || len(body.Password) < 12 || len(body.Password) > 128 || !store.ValidEmployeeRole(body.Role) || (store.ClinicalRole(body.Role) && body.StaffID == "") || (body.StaffID != "" && !validID(body.StaffID)) {
		fail(c, 400, "INVALID_EMPLOYEE", "请填写有效账号、姓名、角色及至少 12 位密码")
		return
	}
	if !validClinicAssignment(c, body.ClinicIDs) {
		return
	}
	row, err := a.employees.CreateEmployee(store.WithClinicAssignment(c.Request.Context(), body.ClinicIDs), username, name, body.Password, body.Role, body.StaffID, a.now())
	if err != nil {
		employeeFailure(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": employeeDTO(row)})
}
func (a *API) setEmployeeStatus(c *gin.Context) {
	id := c.Param("employeeId")
	if !validID(id) {
		fail(c, 400, "INVALID_EMPLOYEE", "员工编号无效")
		return
	}
	var body struct {
		Version int  `json:"version"`
		Active  bool `json:"active"`
	}
	if !decodeObject(c, &body, []string{"version", "active"}, nil) {
		return
	}
	if body.Version < 1 || (id == currentEmployee(c).ID && !body.Active) {
		fail(c, 400, "INVALID_EMPLOYEE", "不能停用当前登录账号")
		return
	}
	row, err := a.employees.SetEmployeeActive(c.Request.Context(), id, body.Version, body.Active, a.now())
	if err != nil {
		employeeFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": employeeDTO(row)})
}

func (a *API) resetEmployeePassword(c *gin.Context) {
	id := c.Param("employeeId")
	if !validID(id) {
		fail(c, 400, "INVALID_EMPLOYEE", "员工编号无效")
		return
	}
	var body struct {
		Version  int    `json:"version"`
		Password string `json:"password"`
	}
	if !decodeObject(c, &body, []string{"version", "password"}, nil) {
		return
	}
	if body.Version < 1 || len(body.Password) < 12 || len(body.Password) > 128 {
		fail(c, 400, "INVALID_PASSWORD", "密码须为 12–128 位")
		return
	}
	row, err := a.employees.ResetEmployeePassword(c.Request.Context(), id, body.Version, body.Password, a.now())
	if err != nil {
		employeeFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": employeeDTO(row)})
}

func (a *API) employeeEvents(c *gin.Context) {
	id := c.Param("employeeId")
	if !validID(id) {
		fail(c, 400, "INVALID_EMPLOYEE", "员工编号无效")
		return
	}
	if !validQuery(c) {
		return
	}
	rows, err := a.employees.EmployeeEvents(c.Request.Context(), id)
	if err != nil {
		employeeFailure(c, err)
		return
	}
	data := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		var details any
		if strings.TrimSpace(row.Details) != "" {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(row.Details), &parsed); err == nil {
				details = parsed
			}
		}
		data = append(data, gin.H{"id": row.ID, "employeeId": row.EmployeeID, "actorId": row.ActorID, "actorName": row.ActorName, "action": row.Action, "details": details, "createdAt": row.CreatedAt})
	}
	c.JSON(200, gin.H{"data": data})
}

func (a *API) changeEmployeePassword(c *gin.Context) {
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		Password        string `json:"password"`
	}
	if !decodeObject(c, &body, []string{"currentPassword", "password"}, nil) {
		return
	}
	if len(body.CurrentPassword) < 1 || len(body.CurrentPassword) > 128 || len(body.Password) < 12 || len(body.Password) > 72 || body.Password == body.CurrentPassword {
		fail(c, 400, "INVALID_PASSWORD", "新密码须为 12–72 字节且与原密码不同")
		return
	}
	repo, ok := a.repo.(interface {
		ChangeEmployeePassword(context.Context, string, int, string, string, time.Time) (model.EmployeeAccount, error)
	})
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "员工服务暂不可用")
		return
	}
	employee := currentEmployee(c)
	row, err := repo.ChangeEmployeePassword(c.Request.Context(), employee.ID, employee.Version, body.CurrentPassword, body.Password, a.now())
	if errors.Is(err, store.ErrInvalidCredentials) {
		fail(c, 400, "INVALID_CURRENT_PASSWORD", "原密码错误")
		return
	}
	if err != nil {
		employeeFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": employeeDTO(row)})
}
func (a *API) setEmployeeAccess(c *gin.Context) {
	var body struct {
		Version int    `json:"version"`
		Role    string `json:"role"`
		StaffID string `json:"staffId"`
	}
	id := c.Param("employeeId")
	if !decodeObject(c, &body, []string{"version", "role", "staffId"}, nil) {
		return
	}
	if id == "" || !validID(id) || body.Version < 1 || !store.ValidEmployeeRole(body.Role) || (store.ClinicalRole(body.Role) && body.StaffID == "") || !validID(body.StaffID) {
		fail(c, 400, "INVALID_EMPLOYEE", "请选择有效角色及匹配的关联人员")
		return
	}
	if id == currentEmployee(c).ID && body.Role != "manager" {
		fail(c, 400, "INVALID_EMPLOYEE", "不能降低当前登录主管的角色权限")
		return
	}
	repo, ok := a.repo.(interface {
		SetEmployeeAccess(context.Context, string, int, string, string, time.Time) (model.EmployeeAccount, error)
	})
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "员工服务暂不可用")
		return
	}
	row, err := repo.SetEmployeeAccess(c.Request.Context(), id, body.Version, body.Role, body.StaffID, a.now())
	if err != nil {
		employeeFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": employeeDTO(row)})
}
