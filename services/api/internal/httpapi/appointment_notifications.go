package httpapi

import (
	"strconv"

	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

// 新增-P1：超时未确认预约的自动升级提醒，镜像交接通知接口。
func (a *API) appointmentNotifications(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	repo, ok := a.repo.(store.AppointmentNotificationRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "待确认提醒暂不可用")
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
	result, err := repo.AppointmentNotifications(c.Request.Context(), page, size)
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result.Data, "meta": gin.H{"page": page, "pageSize": size, "total": result.Total, "unreadTotal": result.UnreadTotal}})
}

func (a *API) readAppointmentNotification(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	repo, ok := a.repo.(store.AppointmentNotificationRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "待确认提醒暂不可用")
		return
	}
	id := c.Param("notificationId")
	if !validID(id) {
		fail(c, 400, "INVALID_ID", "通知编号无效")
		return
	}
	if err := repo.ReadAppointmentNotification(c.Request.Context(), id, a.now()); err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"read": true}})
}
