package httpapi

import (
	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"unicode/utf8"
)

func (a *API) careTemplates(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	employee := currentEmployee(c)
	if employee.Role != "manager" && employee.Role != "clinician" && employee.Role != "doctor" && employee.Role != "nurse" {
		fail(c, 403, "FORBIDDEN", "当前角色不能管理方案模板")
		return
	}
	repo, ok := a.repo.(store.CareTemplateRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "方案模板服务暂不可用")
		return
	}
	if c.Request.Method == http.MethodDelete {
		if !validID(c.Param("templateId")) {
			fail(c, 400, "INVALID_BODY", "模板标识无效")
			return
		}
		if err := repo.DeleteCareTemplate(c.Request.Context(), employee.ID, c.Param("templateId")); err != nil {
			a.careError(c, err)
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"deleted": true}})
		return
	}
	if c.Request.Method == http.MethodPost {
		var body struct {
			Name    string                   `json:"name"`
			Content apitypes.CarePlanContent `json:"content"`
		}
		if !decodeObject(c, &body, []string{"name", "content"}, nil) {
			return
		}
		content, valid := careContent(body.Content)
		name := strings.TrimSpace(body.Name)
		if !valid || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 80 {
			fail(c, 400, "INVALID_BODY", "请填写模板名称及完整方案内容")
			return
		}
		row, err := repo.CreateCareTemplate(c.Request.Context(), employee.ID, name, content, a.now())
		if err != nil {
			a.careError(c, err)
			return
		}
		c.JSON(201, gin.H{"data": gin.H{"id": row.ID, "name": row.Name, "content": content, "createdAt": row.CreatedAt}})
		return
	}
	if !validQuery(c) {
		return
	}
	rows, err := repo.ListCareTemplates(c.Request.Context(), employee.ID)
	if err != nil {
		a.careError(c, err)
		return
	}
	data := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		content, err := contentDTO(row.ContentJSON)
		if err != nil {
			a.unavailable(c, err)
			return
		}
		data = append(data, gin.H{"id": row.ID, "name": row.Name, "content": content, "createdAt": row.CreatedAt})
	}
	c.JSON(200, gin.H{"data": data})
}
