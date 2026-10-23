package httpapi

import (
	"context"
	"example.com/bailiankai/api/internal/model"
	"github.com/gin-gonic/gin"
	"time"
)

func validClinicAssignment(c *gin.Context, ids []string) bool {
	if len(ids) < 1 || len(ids) > 100 {
		fail(c, 400, "INVALID_CLINICS", "请选择1–100个授权院区")
		return false
	}
	allowed := make(map[string]bool)
	for _, id := range currentEmployee(c).ClinicIDs {
		allowed[id] = true
	}
	seen := make(map[string]bool)
	for _, id := range ids {
		if !validID(id) || id == "" || seen[id] {
			fail(c, 400, "INVALID_CLINICS", "院区编号无效或重复")
			return false
		}
		if !allowed[id] {
			fail(c, 403, "FORBIDDEN", "不得授予自己无权管理的院区")
			return false
		}
		seen[id] = true
	}
	return true
}
func (a *API) setEmployeeClinics(c *gin.Context) {
	var body struct {
		ClinicIDs []string `json:"clinicIds"`
		Version   int      `json:"version"`
	}
	if !decodeObject(c, &body, []string{"clinicIds", "version"}, nil) {
		return
	}
	if body.Version < 1 || c.Param("employeeId") == "" || !validID(c.Param("employeeId")) {
		fail(c, 400, "INVALID_EMPLOYEE", "员工编号或版本无效")
		return
	}
	if !validClinicAssignment(c, body.ClinicIDs) {
		return
	}
	repo, ok := a.repo.(interface {
		SetEmployeeClinics(context.Context, string, int, []string, time.Time) (model.EmployeeAccount, error)
	})
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "院区授权服务暂不可用")
		return
	}
	row, err := repo.SetEmployeeClinics(c.Request.Context(), c.Param("employeeId"), body.Version, body.ClinicIDs, a.now())
	if err != nil {
		employeeFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": employeeDTO(row)})
}
