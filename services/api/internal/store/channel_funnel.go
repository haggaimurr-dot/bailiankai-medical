package store

import (
	"context"
	"time"

	"example.com/bailiankai/api/internal/database"
	"gorm.io/gorm"
)

// ChannelFunnelFilter scopes the funnel to an appointment-creation cohort:
// created_at in [From, Until), optionally one authorized clinic.
type ChannelFunnelFilter struct {
	DateRange DateRange
	ClinicID  string
}

type ChannelFunnelRow struct {
	Source        string   `json:"source"`
	SourceLabel   string   `json:"sourceLabel"`
	Created       int64    `json:"created"`
	Confirmed     int64    `json:"confirmed"`
	Arrived       int64    `json:"arrived"`
	Treated       int64    `json:"treated"`
	ConfirmRate   *float64 `json:"confirmRate"`
	ArrivalRate   *float64 `json:"arrivalRate"`
	TreatmentRate *float64 `json:"treatmentRate"`
}

type ChannelFunnelReport struct {
	DateFrom    string             `json:"dateFrom"`
	DateTo      string             `json:"dateTo"`
	ClinicID    string             `json:"clinicId"`
	GeneratedAt time.Time          `json:"generatedAt"`
	Rows        []ChannelFunnelRow `json:"rows"`
	Total       ChannelFunnelRow   `json:"total"`
}

// channelSourceLabels renders the stored source class in Chinese. Only the
// path class is ever stored, so no external referrer or sharer identity can
// leak through this report.
var channelSourceLabels = map[string]string{
	"direct":    "直接进入",
	"home":      "首页",
	"cases":     "案例页",
	"doctors":   "医生页",
	"knowledge": "知识库",
	"share":     "分享链接",
	"未记录":       "未记录",
	"all":       "全部渠道",
}

func channelSourceLabel(source string) string {
	if label, ok := channelSourceLabels[source]; ok {
		return label
	}
	return source
}

func funnelRow(source string, created, confirmed, arrived, treated int64) ChannelFunnelRow {
	return ChannelFunnelRow{
		Source: source, SourceLabel: channelSourceLabel(source),
		Created: created, Confirmed: confirmed, Arrived: arrived, Treated: treated,
		ConfirmRate:   analyticsPercentage(confirmed, created),
		ArrivalRate:   analyticsPercentage(arrived, confirmed),
		TreatmentRate: analyticsPercentage(treated, arrived),
	}
}

// ChannelFunnel requires an explicit employee scope, like Analytics: manager
// and operations only, never falling back to global clinic access. Each step
// is defined as a conjunction of the previous steps, so the funnel is
// monotonic by construction: confirmed includes every arrived appointment
// (arrived_at is part of the confirmed predicate), and treated requires
// arrival, so created >= confirmed >= arrived >= treated always holds.
func (s *MySQL) ChannelFunnel(ctx context.Context, filter ChannelFunnelFilter, now time.Time) (ChannelFunnelReport, error) {
	var report ChannelFunnelReport
	scope, ok := scopeFrom(ctx)
	if !ok || !scope.EnforceClinics || (scope.Role != "manager" && scope.Role != "operations") {
		return report, ErrForbidden
	}
	if !filter.DateRange.Active() || filter.DateRange.Until.After(filter.DateRange.From.AddDate(0, 0, 366)) || !analyticsIDValid(filter.ClinicID) {
		return report, ErrAnalyticsInput
	}
	now = now.In(database.Shanghai)
	report = ChannelFunnelReport{
		DateFrom: filter.DateRange.From.Format("2006-01-02"), DateTo: filter.DateRange.Until.AddDate(0, 0, -1).Format("2006-01-02"),
		ClinicID: filter.ClinicID, GeneratedAt: now, Rows: []ChannelFunnelRow{},
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var clinics []AnalyticsClinic
		if err := clinicScope(ctx, tx.Table("clinics"), "clinics.id").Select("id, name").Order("id").Scan(&clinics).Error; err != nil {
			return err
		}
		if filter.ClinicID != "" {
			found := false
			for _, clinic := range clinics {
				found = found || clinic.ID == filter.ClinicID
			}
			if !found {
				return ErrNotFound
			}
		}
		if len(clinics) == 0 {
			return nil
		}
		confirmedPredicate := `appointments.status IN ('confirmed', 'completed') OR appointments.arrived_at IS NOT NULL OR EXISTS (SELECT 1 FROM treatments t WHERE t.appointment_id = appointments.id)`
		selectSQL := `COALESCE(NULLIF(appointments.source, ''), '未记录') AS source,
			COUNT(*) AS created,
			SUM(CASE WHEN ` + confirmedPredicate + ` THEN 1 ELSE 0 END) AS confirmed,
			SUM(CASE WHEN appointments.arrived_at IS NOT NULL THEN 1 ELSE 0 END) AS arrived,
			SUM(CASE WHEN appointments.arrived_at IS NOT NULL AND EXISTS (SELECT 1 FROM treatments t WHERE t.appointment_id = appointments.id) THEN 1 ELSE 0 END) AS treated`
		query := clinicScope(ctx, tx.Table("appointments").Joins("JOIN slots ON slots.id = appointments.slot_id"), "slots.clinic_id").
			Where("appointments.created_at >= ? AND appointments.created_at < ?", filter.DateRange.From, filter.DateRange.Until)
		if filter.ClinicID != "" {
			query = query.Where("slots.clinic_id = ?", filter.ClinicID)
		}
		type row struct {
			Source    string
			Created   int64
			Confirmed int64
			Arrived   int64
			Treated   int64
		}
		var rows []row
		if err := query.Select(selectSQL).Group("source").Order("created DESC").Order("source").Scan(&rows).Error; err != nil {
			return err
		}
		var totalCreated, totalConfirmed, totalArrived, totalTreated int64
		for _, r := range rows {
			report.Rows = append(report.Rows, funnelRow(r.Source, r.Created, r.Confirmed, r.Arrived, r.Treated))
			totalCreated += r.Created
			totalConfirmed += r.Confirmed
			totalArrived += r.Arrived
			totalTreated += r.Treated
		}
		report.Total = funnelRow("all", totalCreated, totalConfirmed, totalArrived, totalTreated)
		return nil
	})
	if err != nil {
		return ChannelFunnelReport{}, err
	}
	return report, nil
}
