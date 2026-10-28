package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"example.com/bailiankai/api/internal/database"
	"gorm.io/gorm"
)

var ErrAnalyticsInput = errors.New("invalid analytics filter")

type AnalyticsFilter struct {
	DateRange DateRange
	ClinicID  string
	DoctorID  string
	GroupBy   string
}

// AnalyticsMetrics v1 measures the current lifecycle of two independent date
// cohorts: appointments by slot start, and followup tasks by due date.
type AnalyticsMetrics struct {
	AppointmentCount          int64    `json:"appointmentCount"`
	PendingAppointmentCount   int64    `json:"pendingAppointmentCount"`
	ConfirmedAppointmentCount int64    `json:"confirmedAppointmentCount"`
	ArrivedAppointmentCount   int64    `json:"arrivedAppointmentCount"`
	TreatmentCount            int64    `json:"treatmentCount"`
	FollowupCount             int64    `json:"followupCount"`
	FollowupCompletedCount    int64    `json:"followupCompletedCount"`
	FollowupOverdueCount      int64    `json:"followupOverdueCount"`
	AlertCount                int64    `json:"alertCount"`
	AlertHandledCount         int64    `json:"alertHandledCount"`
	PendingAlertCount         int64    `json:"pendingAlertCount"`
	ArrivalRate               *float64 `json:"arrivalRate"`
	FollowupCompletionRate    *float64 `json:"followupCompletionRate"`
	AlertHandlingRate         *float64 `json:"alertHandlingRate"`
}

type AnalyticsRow struct {
	Key        string           `json:"key"`
	ClinicID   string           `json:"clinicId"`
	ClinicName string           `json:"clinicName"`
	DoctorID   string           `json:"doctorId"`
	DoctorName string           `json:"doctorName"`
	Date       string           `json:"date"`
	Metrics    AnalyticsMetrics `json:"metrics" gorm:"embedded"`
}

type AnalyticsClinic struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AnalyticsDoctor struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ClinicID string `json:"clinicId"`
}

type AnalyticsReport struct {
	MetricVersion string            `json:"metricVersion"`
	Timezone      string            `json:"timezone"`
	GeneratedAt   time.Time         `json:"generatedAt"`
	DateFrom      string            `json:"dateFrom"`
	DateTo        string            `json:"dateTo"`
	GroupBy       string            `json:"groupBy"`
	Summary       AnalyticsMetrics  `json:"summary"`
	Rows          []AnalyticsRow    `json:"rows"`
	Clinics       []AnalyticsClinic `json:"clinics"`
	Doctors       []AnalyticsDoctor `json:"doctors"`
	// Previous is the环比 window: the equal-length period immediately before
	// DateFrom. YearAgo is the同比 window: the same dates one year earlier.
	// Either is nil when its window cannot be computed.
	Previous *AnalyticsPeriod `json:"previous"`
	YearAgo  *AnalyticsPeriod `json:"yearAgo"`
}

// AnalyticsPeriod is one comparison window's date range and summary metrics.
type AnalyticsPeriod struct {
	DateFrom string           `json:"dateFrom"`
	DateTo   string           `json:"dateTo"`
	Summary  AnalyticsMetrics `json:"summary"`
}

func analyticsIDValid(id string) bool {
	if len(id) > 64 {
		return false
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

func validateAnalyticsFilter(filter AnalyticsFilter) (AnalyticsFilter, error) {
	if filter.GroupBy == "" {
		filter.GroupBy = "doctor"
	}
	if filter.GroupBy != "doctor" && filter.GroupBy != "clinic" && filter.GroupBy != "day" {
		return filter, ErrAnalyticsInput
	}
	if !analyticsIDValid(filter.ClinicID) || !analyticsIDValid(filter.DoctorID) || !filter.DateRange.Active() {
		return filter, ErrAnalyticsInput
	}
	from, until := filter.DateRange.From.In(database.Shanghai), filter.DateRange.Until.In(database.Shanghai)
	if from.Year() < 1 || until.AddDate(0, 0, -1).Year() >= 9999 || !until.After(from) || until.After(from.AddDate(0, 0, 366)) {
		return filter, ErrAnalyticsInput
	}
	for _, date := range []time.Time{from, until} {
		midnight := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, database.Shanghai)
		if !date.Equal(midnight) {
			return filter, ErrAnalyticsInput
		}
	}
	filter.DateRange = DateRange{From: from, Until: until}
	return filter, nil
}

func analyticsPercentage(numerator, denominator int64) *float64 {
	if denominator == 0 {
		return nil
	}
	value := float64(numerator) * 100 / float64(denominator)
	return &value
}

func (m *AnalyticsMetrics) calculateRates() {
	m.ArrivalRate = analyticsPercentage(m.ArrivedAppointmentCount, m.ConfirmedAppointmentCount)
	m.FollowupCompletionRate = analyticsPercentage(m.FollowupCompletedCount, m.FollowupCount)
	m.AlertHandlingRate = analyticsPercentage(m.AlertHandledCount, m.AlertCount)
}

func (m *AnalyticsMetrics) add(other AnalyticsMetrics) {
	m.AppointmentCount += other.AppointmentCount
	m.PendingAppointmentCount += other.PendingAppointmentCount
	m.ConfirmedAppointmentCount += other.ConfirmedAppointmentCount
	m.ArrivedAppointmentCount += other.ArrivedAppointmentCount
	m.TreatmentCount += other.TreatmentCount
	m.FollowupCount += other.FollowupCount
	m.FollowupCompletedCount += other.FollowupCompletedCount
	m.FollowupOverdueCount += other.FollowupOverdueCount
	m.AlertCount += other.AlertCount
	m.AlertHandledCount += other.AlertHandledCount
	m.PendingAlertCount += other.PendingAlertCount
}

// Analytics requires an explicit employee scope, including for managers. An
// absent/empty clinic grant never falls back to global clinic access.
func (s *MySQL) Analytics(ctx context.Context, filter AnalyticsFilter, now time.Time) (AnalyticsReport, error) {
	var report AnalyticsReport
	scope, ok := scopeFrom(ctx)
	if !ok || !scope.EnforceClinics || (scope.Role != "manager" && scope.Role != "operations") {
		return report, ErrForbidden
	}
	filter, err := validateAnalyticsFilter(filter)
	if err != nil {
		return report, err
	}
	now = now.In(database.Shanghai)
	report = AnalyticsReport{
		MetricVersion: "v1", Timezone: "Asia/Shanghai", GeneratedAt: now,
		DateFrom: filter.DateRange.From.Format("2006-01-02"), DateTo: filter.DateRange.Until.AddDate(0, 0, -1).Format("2006-01-02"), GroupBy: filter.GroupBy,
		Rows: []AnalyticsRow{}, Clinics: []AnalyticsClinic{}, Doctors: []AnalyticsDoctor{},
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := clinicScope(ctx, tx.Table("clinics"), "clinics.id").Select("id, name").Order("id").Scan(&report.Clinics).Error; err != nil {
			return err
		}
		if err := clinicScope(ctx, tx.Table("doctors"), "doctors.clinic_id").Select("id, name, clinic_id").Order("clinic_id, id").Scan(&report.Doctors).Error; err != nil {
			return err
		}
		if filter.ClinicID != "" {
			found := false
			for _, clinic := range report.Clinics {
				found = found || clinic.ID == filter.ClinicID
			}
			if !found {
				return ErrNotFound
			}
		}
		if filter.DoctorID != "" {
			found := false
			for _, doctor := range report.Doctors {
				found = found || doctor.ID == filter.DoctorID
			}
			if !found {
				return ErrNotFound
			}
		}
		if len(report.Clinics) == 0 {
			return nil
		}

		periodFilter := filter
		summary, rows, err := analyticsPeriod(tx, ctx, periodFilter, clinicNamesOf(report.Clinics), doctorNamesOf(report.Doctors), now)
		if err != nil {
			return err
		}
		report.Rows, report.Summary = rows, summary
		// Comparison periods use the same filters and the same grouping, so
		// the环比 window is exactly the preceding equal-length period and the
		//同比 window is the same dates one year earlier — never overlapping
		// the current period.
		length := filter.DateRange.Until.Sub(filter.DateRange.From)
		prevFrom, prevUntil := filter.DateRange.From.Add(-length), filter.DateRange.From
		if prevFrom.Year() >= 1 {
			periodFilter.DateRange = DateRange{From: prevFrom, Until: prevUntil}
			prevSummary, _, err := analyticsPeriod(tx, ctx, periodFilter, clinicNamesOf(report.Clinics), doctorNamesOf(report.Doctors), now)
			if err != nil {
				return err
			}
			report.Previous = &AnalyticsPeriod{DateFrom: prevFrom.Format("2006-01-02"), DateTo: prevUntil.AddDate(0, 0, -1).Format("2006-01-02"), Summary: prevSummary}
		}
		yearFrom, yearUntil := filter.DateRange.From.AddDate(-1, 0, 0), filter.DateRange.Until.AddDate(-1, 0, 0)
		if yearFrom.Year() >= 1 {
			periodFilter.DateRange = DateRange{From: yearFrom, Until: yearUntil}
			yearSummary, _, err := analyticsPeriod(tx, ctx, periodFilter, clinicNamesOf(report.Clinics), doctorNamesOf(report.Doctors), now)
			if err != nil {
				return err
			}
			report.YearAgo = &AnalyticsPeriod{DateFrom: yearFrom.Format("2006-01-02"), DateTo: yearUntil.AddDate(0, 0, -1).Format("2006-01-02"), Summary: yearSummary}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return AnalyticsReport{}, err
	}
	return report, nil
}

func clinicNamesOf(clinics []AnalyticsClinic) map[string]string {
	names := make(map[string]string, len(clinics))
	for _, clinic := range clinics {
		names[clinic.ID] = clinic.Name
	}
	return names
}

func doctorNamesOf(doctors []AnalyticsDoctor) map[string]string {
	names := make(map[string]string, len(doctors))
	for _, doctor := range doctors {
		names[doctor.ID] = doctor.Name
	}
	return names
}

// analyticsPeriod computes one period's summary and grouped rows. The caller
// supplies the period through filter.DateRange; the same grouping and clinic
// / doctor filters apply to every period, so comparison windows never mix
// cohorts.
func analyticsPeriod(tx *gorm.DB, ctx context.Context, filter AnalyticsFilter, clinicNames, doctorNames map[string]string, now time.Time) (AnalyticsMetrics, []AnalyticsRow, error) {
	var summary AnalyticsMetrics
	// EXISTS expresses appointment facts once, irrespective of confirmation
	// history, treatment revisions, or the number of followup tasks.
	appointmentSelect := `COUNT(*) AS appointment_count,
		SUM(CASE WHEN appointments.status = 'pending' AND appointments.confirmation_deadline > ? THEN 1 ELSE 0 END) AS pending_appointment_count,
		SUM(CASE WHEN appointments.status IN ('confirmed', 'completed') OR EXISTS (SELECT 1 FROM appointment_events ae WHERE ae.appointment_id = appointments.id AND ae.action = 'confirm') THEN 1 ELSE 0 END) AS confirmed_appointment_count,
		SUM(CASE WHEN appointments.arrived_at IS NOT NULL THEN 1 ELSE 0 END) AS arrived_appointment_count,
		SUM(CASE WHEN EXISTS (SELECT 1 FROM treatments t WHERE t.appointment_id = appointments.id) THEN 1 ELSE 0 END) AS treatment_count`
	appointments := tx.Table("appointments").Joins("JOIN slots ON slots.id = appointments.slot_id").
		Where("slots.starts_at >= ? AND slots.starts_at < ?", filter.DateRange.From, filter.DateRange.Until)
	appointments = analyticsFiltered(ctx, appointments, filter)
	appointments, dimensions := analyticsGroup(appointments, filter.GroupBy, "slots.starts_at")
	var appointmentRows []AnalyticsRow
	if err := appointments.Select(dimensions+", "+appointmentSelect, now).Scan(&appointmentRows).Error; err != nil {
		return summary, nil, err
	}
	// The followup cohort uses its own due date. Ownership always comes
	// from the appointment slot, never a task's reassigned staff member.
	taskSelect := `COUNT(*) AS followup_count,
		SUM(CASE WHEN followup_tasks.status IN ('reviewed', 'handled') THEN 1 ELSE 0 END) AS followup_completed_count,
		SUM(CASE WHEN followup_tasks.status = 'pending' AND followup_tasks.due_at < ? THEN 1 ELSE 0 END) AS followup_overdue_count,
		SUM(CASE WHEN followup_tasks.status IN ('alert', 'handled') THEN 1 ELSE 0 END) AS alert_count,
		SUM(CASE WHEN followup_tasks.status = 'handled' THEN 1 ELSE 0 END) AS alert_handled_count,
		SUM(CASE WHEN followup_tasks.status = 'alert' THEN 1 ELSE 0 END) AS pending_alert_count`
	tasks := tx.Table("followup_tasks").Joins("JOIN appointments ON appointments.id = followup_tasks.appointment_id").Joins("JOIN slots ON slots.id = appointments.slot_id").
		Where("followup_tasks.due_at >= ? AND followup_tasks.due_at < ?", filter.DateRange.From, filter.DateRange.Until)
	tasks = analyticsFiltered(ctx, tasks, filter)
	tasks, dimensions = analyticsGroup(tasks, filter.GroupBy, "followup_tasks.due_at")
	var taskRows []AnalyticsRow
	if err := tasks.Select(dimensions+", "+taskSelect, now).Scan(&taskRows).Error; err != nil {
		return summary, nil, err
	}
	rows := make(map[string]*AnalyticsRow)
	for _, batch := range [][]AnalyticsRow{appointmentRows, taskRows} {
		for _, row := range batch {
			key := row.Date
			switch filter.GroupBy {
			case "doctor":
				key = row.ClinicID + "/" + row.DoctorID
			case "clinic":
				key = row.ClinicID
			}
			current, exists := rows[key]
			if !exists {
				current = &AnalyticsRow{Key: key, ClinicID: row.ClinicID, DoctorID: row.DoctorID, Date: row.Date}
				rows[key] = current
			}
			current.Metrics.add(row.Metrics)
		}
	}
	if filter.GroupBy == "day" {
		for date := filter.DateRange.From; date.Before(filter.DateRange.Until); date = date.AddDate(0, 0, 1) {
			key := date.Format("2006-01-02")
			if rows[key] == nil {
				rows[key] = &AnalyticsRow{Key: key, Date: key}
			}
		}
	}
	out := make([]AnalyticsRow, 0, len(rows))
	for _, row := range rows {
		row.ClinicName, row.DoctorName = clinicNames[row.ClinicID], doctorNames[row.DoctorID]
		row.Metrics.calculateRates()
		out = append(out, *row)
		summary.add(row.Metrics)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	summary.calculateRates()
	return summary, out, nil
}

func analyticsFiltered(ctx context.Context, query *gorm.DB, filter AnalyticsFilter) *gorm.DB {
	query = clinicScope(ctx, query, "slots.clinic_id")
	if filter.ClinicID != "" {
		query = query.Where("slots.clinic_id = ?", filter.ClinicID)
	}
	if filter.DoctorID != "" {
		query = query.Where("slots.doctor_id = ?", filter.DoctorID)
	}
	return query
}

func analyticsGroup(query *gorm.DB, groupBy, dateColumn string) (*gorm.DB, string) {
	switch groupBy {
	case "doctor":
		return query.Group("slots.clinic_id, slots.doctor_id"), "slots.clinic_id AS clinic_id, slots.doctor_id AS doctor_id"
	case "clinic":
		return query.Group("slots.clinic_id"), "slots.clinic_id AS clinic_id"
	default:
		// The database connection uses Asia/Shanghai (+08:00), and stored
		// DATETIME values and query boundaries are both local clinical times.
		dimension := "DATE_FORMAT(" + dateColumn + ", '%Y-%m-%d')"
		return query.Group(dimension), dimension + " AS date"
	}
}
