package store

import (
	"context"
	"time"
)

// 新增-P1：取消原因收集。原因码由患者端取消时自选，不填不阻断取消；
// 未填原因的取消在统计里归入 "unspecified"。原因码不在库里做 CHECK 约束
// （MariaDB 与 MySQL 的约束语法差异），只在接口层校验枚举。
var CancelReasons = []string{
	"schedule_conflict",
	"personal",
	"found_elsewhere",
	"price",
	"changed_mind",
	"other",
}

// ValidCancelReason reports whether code is an accepted cancellation reason.
// An empty code is valid too: it means the patient chose not to give a reason.
func ValidCancelReason(code string) bool {
	if code == "" {
		return true
	}
	for _, r := range CancelReasons {
		if r == code {
			return true
		}
	}
	return false
}

// CancellationReasonCount is one row of the cancellation-reason breakdown.
type CancellationReasonCount struct {
	Reason string
	Count  int64
}

// CancellationStatsResult aggregates cancelled appointments by reason.
type CancellationStatsResult struct {
	Total    int64
	ByReason []CancellationReasonCount
}

// CancellationStats counts appointments cancelled in [from, to) by reason,
// honouring the employee clinic scope and optional clinic/doctor filters.
// Cancelled time is appointments.cancelled_at, set atomically with the cancel.
func (s *MySQL) CancellationStats(ctx context.Context, from, to time.Time, clinicID, doctorID string) (CancellationStatsResult, error) {
	result := CancellationStatsResult{ByReason: []CancellationReasonCount{}}
	q := clinicScope(ctx, s.db.WithContext(ctx).Table("appointments").Joins("JOIN slots ON slots.id = appointments.slot_id"), "slots.clinic_id").
		Where("appointments.status = ? AND appointments.cancelled_at >= ? AND appointments.cancelled_at < ?", "cancelled", from, to)
	if clinicID != "" {
		q = q.Where("slots.clinic_id = ?", clinicID)
	}
	if doctorID != "" {
		q = q.Where("slots.doctor_id = ?", doctorID)
	}
	type row struct {
		Reason string
		Count  int64
	}
	var rows []row
	if err := q.Select("COALESCE(NULLIF(appointments.cancel_reason, ''), 'unspecified') AS reason, COUNT(*) AS count").Group("reason").Order("count DESC, reason ASC").Scan(&rows).Error; err != nil {
		return result, err
	}
	for _, r := range rows {
		result.Total += r.Count
		result.ByReason = append(result.ByReason, CancellationReasonCount{Reason: r.Reason, Count: r.Count})
	}
	return result, nil
}
