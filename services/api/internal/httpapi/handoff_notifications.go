package httpapi

import (
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"strconv"
)

func (a *API) handoffNotifications(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	repo, ok := a.repo.(store.HandoffNotificationRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "交接通知暂不可用")
		return
	}
	if !validQuery(c, "page", "pageSize") {
		return
	}
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, sizeErr := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if err != nil || sizeErr != nil || page < 1 || page > 10000 || size < 1 || size > 100 {
		fail(c, 400, "INVALID_PAGE", "分页参数无效")
		return
	}
	result, err := repo.HandoffNotifications(c.Request.Context(), page, size)
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result.Data, "meta": gin.H{"page": page, "pageSize": size, "total": result.Total, "unreadTotal": result.UnreadTotal}})
}

func (a *API) readHandoffNotification(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := c.Param("notificationId")
	if !validID(id) {
		fail(c, 400, "INVALID_ID", "通知编号无效")
		return
	}
	repo, ok := a.repo.(store.HandoffNotificationRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "交接通知暂不可用")
		return
	}
	if err := repo.ReadHandoffNotification(c.Request.Context(), id, a.careNow()); err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"read": true}})
}
