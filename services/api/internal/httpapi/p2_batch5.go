package httpapi

import (
	"os"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) registerP2Batch5Routes(r *gin.Engine) {
	// 行117: 模板效果统计（主管）
	r.GET("/api/v1/admin/message-templates/:templateId/effect-stats", a.authenticate, a.requireManager, a.templateEffectStats)
	// 行118: 接口调用监控与慢请求告警（主管）
	r.GET("/api/v1/admin/monitoring/routes", a.authenticate, a.requireManager, a.monitoringRoutes)
	r.GET("/api/v1/admin/monitoring/slow-alerts", a.authenticate, a.requireManager, a.monitoringSlowAlerts)
	r.GET("/api/v1/admin/monitoring/settings/slow-threshold", a.authenticate, a.requireManager, a.getSlowThreshold)
	r.PUT("/api/v1/admin/monitoring/settings/slow-threshold", a.authenticate, a.requireManager, a.setSlowThreshold)
	// 行119: 数据变更日报（主管）
	r.POST("/api/v1/admin/monitoring/data-daily/generate", a.authenticate, a.requireManager, a.generateDataDaily)
	r.GET("/api/v1/admin/monitoring/data-daily", a.authenticate, a.requireManager, a.getDataDaily)
	// 行123: 异常访问行为告警（主管）
	r.GET("/api/v1/admin/security/alerts", a.authenticate, a.requireManager, a.securityAlerts)
	r.POST("/api/v1/admin/security/alerts/:alertId/read", a.authenticate, a.requireManager, a.readSecurityAlert)
}

func (a *API) registerP2Batch5PatientRoutes(patient *gin.RouterGroup) {
	// 行117: 消息内链接点击回统计（患者）
	patient.POST("/notifications/:notificationId/click", a.clickNotification)
}

// ============ 行118: 请求监控中间件 ============

// requestMonitor records per-route call counts, error rates and latency into
// api_request_stats, and files a slow-request alert when a request exceeds
// the configured threshold. Only the route pattern is stored, never query
// strings or bodies. Best-effort: monitoring must never break the request.
func (a *API) requestMonitor() gin.HandlerFunc {
	// 测试环境可通过 BLK_DISABLE_MONITOR=1 关闭，避免干扰现有测试
	disabled := os.Getenv("BLK_DISABLE_MONITOR") == "1"
	return func(c *gin.Context) {
		if disabled {
			c.Next()
			return
		}
		if c.Request.URL.Path == "/healthz" || c.Request.URL.Path == "/readyz" {
			c.Next()
			return
		}
		start := time.Now()
		c.Next()
		durationMs := int(time.Since(start).Milliseconds())
		route := c.FullPath()
		if route == "" {
			route = "404 NOT_FOUND"
		}
		mysql, ok := a.repo.(*store.MySQL)
		if !ok {
			return
		}
		ctx := c.Request.Context()
		now := a.now()
		if err := mysql.RecordApiRequest(ctx, route, c.Request.Method, c.Writer.Status(), durationMs, now); err != nil {
			slog.Warn("api request stat not recorded", "route", route, "error", err)
		}
		threshold := mysql.SlowRequestThreshold(ctx)
		if durationMs >= threshold {
			if err := mysql.RecordSlowAlert(ctx, route, c.Request.Method, c.Writer.Status(), durationMs, threshold, now); err != nil {
				slog.Warn("slow alert not recorded", "route", route, "error", err)
			}
		}
	}
}

// ============ 行117 ============

func (a *API) templateEffectStats(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	mysql, ok := a.repo.(*store.MySQL)
	if !ok {
		a.unavailable(c, errors.New("monitoring unavailable"))
		return
	}
	templateID := c.Param("templateId")
	if _, err := mysql.MessageTemplate(c.Request.Context(), templateID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(c, http.StatusNotFound, "NOT_FOUND", "模板不存在")
			return
		}
		a.unavailable(c, err)
		return
	}
	stats, err := mysql.TemplateEffectStats(c.Request.Context(), templateID)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": stats})
}

func (a *API) clickNotification(c *gin.Context) {
	mysql, ok := a.repo.(*store.MySQL)
	if !ok {
		a.unavailable(c, errors.New("monitoring unavailable"))
		return
	}
	patient := currentPatient(c)
	notificationID := c.Param("notificationId")
	n, err := mysql.GetNotification(c.Request.Context(), notificationID, patient.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(c, http.StatusNotFound, "NOT_FOUND", "消息不存在")
			return
		}
		a.unavailable(c, err)
		return
	}
	if err := mysql.RecordMessageClick(c.Request.Context(), n.TemplateID, n.ID, patient.ID, a.now()); err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"clicked": true}})
}

// ============ 行118 ============

func (a *API) monitoringRoutes(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	mysql, ok := a.repo.(*store.MySQL)
	if !ok {
		a.unavailable(c, errors.New("monitoring unavailable"))
		return
	}
	date := c.DefaultQuery("date", a.now().Format("2006-01-02"))
	rows, err := mysql.RouteStats(c.Request.Context(), date)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (a *API) monitoringSlowAlerts(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	mysql, ok := a.repo.(*store.MySQL)
	if !ok {
		a.unavailable(c, errors.New("monitoring unavailable"))
		return
	}
	rows, err := mysql.SlowAlerts(c.Request.Context(), 50)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (a *API) getSlowThreshold(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	mysql, ok := a.repo.(*store.MySQL)
	if !ok {
		a.unavailable(c, errors.New("monitoring unavailable"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"thresholdMs": mysql.SlowRequestThreshold(c.Request.Context())}})
}

func (a *API) setSlowThreshold(c *gin.Context) {
	mysql, ok := a.repo.(*store.MySQL)
	if !ok {
		a.unavailable(c, errors.New("monitoring unavailable"))
		return
	}
	var body struct {
		ThresholdMs int `json:"thresholdMs"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, http.StatusBadRequest, "INVALID_BODY", "请求体无效")
		return
	}
	if err := mysql.SetSlowRequestThreshold(c.Request.Context(), body.ThresholdMs, a.now()); err != nil {
		if errors.Is(err, store.ErrMonitoringInput) {
			fail(c, http.StatusBadRequest, "INVALID_THRESHOLD", "阈值须为 1~600000 毫秒")
			return
		}
		a.unavailable(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"thresholdMs": body.ThresholdMs}})
}

// ============ 行119 ============

func (a *API) generateDataDaily(c *gin.Context) {
	mysql, ok := a.repo.(*store.MySQL)
	if !ok {
		a.unavailable(c, errors.New("monitoring unavailable"))
		return
	}
	dateStr := c.DefaultQuery("date", a.now().Format("2006-01-02"))
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		fail(c, http.StatusBadRequest, "INVALID_DATE", "日期格式应为 YYYY-MM-DD")
		return
	}
	if err := mysql.GenerateDataDailyReport(c.Request.Context(), date, a.now()); err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"date": dateStr, "generated": true}})
}

func (a *API) getDataDaily(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	mysql, ok := a.repo.(*store.MySQL)
	if !ok {
		a.unavailable(c, errors.New("monitoring unavailable"))
		return
	}
	dateStr := c.DefaultQuery("date", a.now().Format("2006-01-02"))
	rows, err := mysql.GetDataDailyReport(c.Request.Context(), dateStr)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	// Day-over-day deltas for the report view.
	prev, _ := time.Parse("2006-01-02", dateStr)
	prevRows, err := mysql.GetDataDailyReport(c.Request.Context(), prev.AddDate(0, 0, -1).Format("2006-01-02"))
	prevMap := map[string]int64{}
	if err == nil {
		for _, r := range prevRows {
			prevMap[r.TableName] = r.RowCount
		}
	}
	type rowOut struct {
		TableName string `json:"tableName"`
		RowCount  int64  `json:"rowCount"`
		Delta     *int64 `json:"delta,omitempty"`
	}
	out := make([]rowOut, 0, len(rows))
	for _, r := range rows {
		o := rowOut{TableName: r.TableName, RowCount: r.RowCount}
		if prevCount, ok := prevMap[r.TableName]; ok {
			d := r.RowCount - prevCount
			o.Delta = &d
		}
		out = append(out, o)
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"date": dateStr, "rows": out}})
}

// ============ 行123 ============

func (a *API) securityAlerts(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	mysql, ok := a.repo.(*store.MySQL)
	if !ok {
		a.unavailable(c, errors.New("monitoring unavailable"))
		return
	}
	unreadOnly := c.DefaultQuery("unread", "") == "1"
	rows, err := mysql.SecurityAlerts(c.Request.Context(), unreadOnly, 50)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (a *API) readSecurityAlert(c *gin.Context) {
	mysql, ok := a.repo.(*store.MySQL)
	if !ok {
		a.unavailable(c, errors.New("monitoring unavailable"))
		return
	}
	if err := mysql.MarkSecurityAlertRead(c.Request.Context(), c.Param("alertId"), a.now()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(c, http.StatusNotFound, "NOT_FOUND", "告警不存在")
			return
		}
		a.unavailable(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"read": true}})
}

// recordPhotoDenied logs a photo-access-denied security signal when an
// authenticated actor is refused a photo that exists but they may not access.
// Genuine 404s (no such photo) are not signals.
func (a *API) recordPhotoDenied(c *gin.Context, resourceID string, admin bool, err error) {
	mysql, ok := a.repo.(*store.MySQL)
	if !ok {
		return
	}
	denied := errors.Is(err, store.ErrPhotoConsent) || errors.Is(err, store.ErrForbidden)
	var actorType, actorID string
	if admin {
		emp, ok := c.Get("employee")
		if !ok {
			return
		}
		acc, ok := emp.(model.EmployeeAccount)
		if !ok {
			return
		}
		actorType, actorID = "employee", acc.ID
	} else {
		p, ok := c.Get("patient")
		if !ok {
			return
		}
		pat, ok := p.(model.Patient)
		if !ok {
			return
		}
		actorType, actorID = "patient", pat.ID
	}
	if !denied && !mysql.FollowupPhotoExists(c.Request.Context(), resourceID) {
		return
	}
	_ = mysql.RecordSecuritySignal(c.Request.Context(), actorType, actorID,
		"photo-access-denied", "followup-photo", resourceID, a.now())
}
