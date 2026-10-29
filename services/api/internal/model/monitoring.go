package model

import "time"

// MessageClick records a patient clicking a link inside a message. The link
// carries the template marker so clicks roll up into per-template effect
// stats (P2 后台-消息管理：模板效果统计）.
type MessageClick struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	TemplateID     string    `gorm:"size:64" json:"templateId"`
	NotificationID string    `gorm:"size:64" json:"notificationId"`
	PatientID      string    `gorm:"size:64" json:"patientId"`
	ClickedAt      time.Time `json:"clickedAt"`
}

// TemplateEffectStats aggregates send/deliver/click/fail counts for one
// message template. Delivered = notifications in sent status; failed reuses
// the notification failure-category口径.
type TemplateEffectStats struct {
	TemplateID string `json:"templateId"`
	Sent       int64  `json:"sent"`
	Delivered  int64  `json:"delivered"`
	Clicked    int64  `json:"clicked"`
	Failed     int64  `json:"failed"`
}

// ApiRequestStat is a per-day aggregate of API calls for one route pattern.
// Only the route pattern is stored (never query strings or bodies), so no
// sensitive request fields end up in the log.
type ApiRequestStat struct {
	Route    string    `gorm:"primaryKey;size:160" json:"route"`
	Method   string    `gorm:"primaryKey;size:16" json:"method"`
	StatDate time.Time `gorm:"primaryKey;type:date" json:"statDate"`
	Total    int64     `json:"total"`
	Err4xx   int64     `gorm:"column:err_4xx" json:"err4xx"`
	Err5xx   int64     `gorm:"column:err_5xx" json:"err5xx"`
	SumMs    int64     `json:"sumMs"`
	MaxMs    int       `json:"maxMs"`
}

// ApiSlowAlert is one request that exceeded the slow threshold.
type ApiSlowAlert struct {
	ID          string    `gorm:"primaryKey;size:64" json:"id"`
	Route       string    `gorm:"size:160" json:"route"`
	Method      string    `gorm:"size:16" json:"method"`
	Status      int       `json:"status"`
	DurationMs  int       `json:"durationMs"`
	ThresholdMs int       `json:"thresholdMs"`
	CreatedAt   time.Time `json:"createdAt"`
}

// MonitoringSetting is a small key/value store for ops knobs
// (currently: slow_request_threshold_ms).
type MonitoringSetting struct {
	K         string    `gorm:"primaryKey;size:64" json:"k"`
	V         string    `gorm:"size:255" json:"v"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// DataDailyReport is one row-count snapshot of a key table for a day
// (P2 后端-数据：数据变更日报）. Counts only, never patient details.
type DataDailyReport struct {
	ReportDate time.Time `gorm:"primaryKey;type:date" json:"reportDate"`
	TableName  string    `gorm:"primaryKey;size:64" json:"tableName"`
	RowCount   int64     `json:"rowCount"`
	CreatedAt  time.Time `json:"createdAt"`
}

// SecuritySignal is one abnormal-access signal, e.g. a denied photo access
// (P2 安全-访问：异常访问行为告警）. Only behavior counts and account ids
// are recorded, never photo content.
type SecuritySignal struct {
	ID           string    `gorm:"primaryKey;size:64" json:"id"`
	ActorType    string    `gorm:"size:16" json:"actorType"`
	ActorID      string    `gorm:"size:64" json:"actorId"`
	Kind         string    `gorm:"size:32" json:"kind"`
	ResourceType string    `gorm:"size:32" json:"resourceType"`
	ResourceID   string    `gorm:"size:64" json:"resourceId"`
	CreatedAt    time.Time `json:"createdAt"`
}

// SecurityAlert is raised when signals cross the threshold; managers clear
// it after reviewing (it doubles as the security todo item).
type SecurityAlert struct {
	ID        string     `gorm:"primaryKey;size:64" json:"id"`
	ActorType string     `gorm:"size:16" json:"actorType"`
	ActorID   string     `gorm:"size:64" json:"actorId"`
	Kind      string     `gorm:"size:32" json:"kind"`
	Detail    string     `gorm:"size:500" json:"detail"`
	AlertedAt time.Time  `json:"alertedAt"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
}
