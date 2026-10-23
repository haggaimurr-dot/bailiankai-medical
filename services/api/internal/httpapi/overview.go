package httpapi

import (
	"context"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) overview(c *gin.Context) {
	if !validQuery(c) {
		return
	}
	repo, ok := a.repo.(interface {
		Overview(context.Context) (store.Overview, error)
	})
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "运营统计暂不可用")
		return
	}
	data, err := repo.Overview(c.Request.Context())
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"data": data})
}
