package store

import (
	"context"
	"time"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
)

// FollowupTodoStats counts followup tasks in the caller's queue scope
// (taskClinicScope plus the clinician's own assignee filter, exactly like
// FollowupQueue). Completed follows the analytics definition: a task counts
// as done only once a clinician has reviewed or handled it; a patient
// submission alone does not complete it.
type FollowupTodoStats struct {
	Total          int64    `json:"total"`
	Completed      int64    `json:"completed"`
	Pending        int64    `json:"pending"`
	Submitted      int64    `json:"submitted"`
	Alert          int64    `json:"alert"`
	Overdue        int64    `json:"overdue"`
	CompletionRate *float64 `json:"completionRate"`
}

// HandoffTodoStats counts handoff receipts in the same scope, with the same
// overdue/todo predicates as HandoffReceipts. Handoffs are listed separately
// and are deliberately NOT part of the followup completion-rate denominator:
// a handoff confirmation is a handover acknowledgement, not a completed
// care task, and mixing the two would inflate or dilute the care rate.
type HandoffTodoStats struct {
	Total     int64 `json:"total"`
	Confirmed int64 `json:"confirmed"`
	Pending   int64 `json:"pending"`
	Overdue   int64 `json:"overdue"`
	Todo      int64 `json:"todo"`
}

type TodoStats struct {
	GeneratedAt time.Time         `json:"generatedAt"`
	Followup    FollowupTodoStats `json:"followup"`
	Handoffs    HandoffTodoStats  `json:"handoffs"`
}

// TodoStats aggregates the workbench queues into one completion-rate view.
// The followup rate is completed / total over the caller's scoped tasks;
// handoff counts are reported alongside, never folded into the rate.
func (s *MySQL) TodoStats(ctx context.Context, now time.Time) (TodoStats, error) {
	var stats TodoStats
	stats.GeneratedAt = now
	followupBase := func() *gorm.DB {
		q := s.taskClinicScope(ctx, s.db.WithContext(ctx).Table("followup_tasks"), "followup_tasks.id")
		if staffID := clinicianStaff(ctx); staffID != "" {
			q = q.Where("followup_tasks.assignee_id = ?", staffID)
		}
		return q
	}
	type followupCounts struct {
		Total     int64
		Completed int64
		Pending   int64
		Submitted int64
		Alert     int64
		Overdue   int64
	}
	var fc followupCounts
	if err := followupBase().Select(`COUNT(*) AS total,
		SUM(CASE WHEN followup_tasks.status IN ('reviewed', 'handled') THEN 1 ELSE 0 END) AS completed,
		SUM(CASE WHEN followup_tasks.status = 'pending' THEN 1 ELSE 0 END) AS pending,
		SUM(CASE WHEN followup_tasks.status = 'submitted' THEN 1 ELSE 0 END) AS submitted,
		SUM(CASE WHEN followup_tasks.status = 'alert' THEN 1 ELSE 0 END) AS alert,
		SUM(CASE WHEN followup_tasks.status = 'pending' AND followup_tasks.due_at < ? THEN 1 ELSE 0 END) AS overdue`, now).Scan(&fc).Error; err != nil {
		return stats, err
	}
	stats.Followup = FollowupTodoStats{
		Total: fc.Total, Completed: fc.Completed, Pending: fc.Pending,
		Submitted: fc.Submitted, Alert: fc.Alert, Overdue: fc.Overdue,
		CompletionRate: analyticsPercentage(fc.Completed, fc.Total),
	}

	setting, err := s.HandoffSetting(ctx)
	if err != nil {
		return stats, err
	}
	cutoff := now.Add(-time.Duration(setting.OverdueHours) * time.Hour)
	receiptBase := func() *gorm.DB {
		q := s.taskClinicScope(ctx, s.db.WithContext(ctx).Model(&model.FollowupHandoffReceipt{}), "followup_handoff_receipts.task_id")
		if staffID := clinicianStaff(ctx); staffID != "" {
			q = q.Where("followup_handoff_receipts.to_staff_id = ?", staffID)
		}
		return q
	}
	count := func(q *gorm.DB) (int64, error) {
		var total int64
		err := q.Count(&total).Error
		return total, err
	}
	var hc HandoffTodoStats
	if hc.Total, err = count(receiptBase()); err != nil {
		return stats, err
	}
	if hc.Confirmed, err = count(receiptBase().Where("followup_handoff_receipts.status = ?", "confirmed")); err != nil {
		return stats, err
	}
	if hc.Pending, err = count(receiptBase().Where("followup_handoff_receipts.status = ?", "pending")); err != nil {
		return stats, err
	}
	if hc.Overdue, err = count(receiptBase().Where("followup_handoff_receipts.status = ? AND followup_handoff_receipts.created_at < ?", "pending", cutoff)); err != nil {
		return stats, err
	}
	if hc.Todo, err = count(receiptBase().Where("(followup_handoff_receipts.followup_status IN ? OR (followup_handoff_receipts.status = ? AND followup_handoff_receipts.created_at < ? AND followup_handoff_receipts.followup_status = ?))", []string{"awaiting_reply", "needs_action"}, "pending", cutoff, "untracked")); err != nil {
		return stats, err
	}
	stats.Handoffs = hc
	return stats, nil
}
