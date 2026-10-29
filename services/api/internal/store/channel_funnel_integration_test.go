package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/store"
)

func TestChannelFunnel(t *testing.T) {
	f := newAnalyticsFixture(t)
	// Creation cohort 2026-10-01..2026-10-05 (Shanghai dates, Until exclusive).
	created := func(label, source, when string) {
		t.Helper()
		createdAt, err := time.ParseInLocation("2006-01-02T15:04:05", when, database.Shanghai)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.db.Table("appointments").Where("id = ?", f.prefix+label).Updates(map[string]any{"source": source, "created_at": createdAt}).Error; err != nil {
			t.Fatal(err)
		}
	}
	deadline := f.now.Add(24 * time.Hour)
	// cases: confirmed + arrived-and-treated + pending.
	f.appointment("funnel-cases-confirmed", f.doctor, "2026-10-03T10:00:00+08:00", "confirmed", deadline, false)
	treated := f.appointment("funnel-cases-treated", f.doctor, "2026-10-03T11:00:00+08:00", "confirmed", deadline, true)
	f.treatment(treated)
	f.appointment("funnel-cases-pending", f.doctor, "2026-10-03T12:00:00+08:00", "pending", deadline, false)
	// direct: cancelled, counts only as created.
	f.appointment("funnel-direct-cancelled", f.doctor, "2026-10-03T13:00:00+08:00", "cancelled", deadline, false)
	// Hidden clinic: must not leak into the authorized-clinic funnel.
	f.appointment("funnel-hidden", f.other, "2026-10-03T14:00:00+08:00", "confirmed", deadline, false)
	// Out of cohort: created in September, excluded even with the same source.
	f.appointment("funnel-old", f.doctor, "2026-09-20T10:00:00+08:00", "confirmed", deadline, false)
	for _, label := range []string{"funnel-cases-confirmed", "funnel-cases-treated", "funnel-cases-pending"} {
		created(label, "cases", "2026-10-02T09:00:00")
	}
	created("funnel-direct-cancelled", "direct", "2026-10-02T10:00:00")
	created("funnel-hidden", "cases", "2026-10-02T11:00:00")
	created("funnel-old", "cases", "2026-09-15T09:00:00")

	report, err := f.repo.ChannelFunnel(f.ctx(f.clinic), store.ChannelFunnelFilter{
		DateRange: store.DateRange{From: mustShanghaiDate(t, "2026-10-01"), Until: mustShanghaiDate(t, "2026-10-06")},
	}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (cases + direct): %+v", len(report.Rows), report.Rows)
	}
	cases := report.Rows[0]
	if cases.Source != "cases" || cases.SourceLabel != "案例页" || cases.Created != 3 || cases.Confirmed != 2 || cases.Arrived != 1 || cases.Treated != 1 {
		t.Fatalf("cases row = %+v, want created 3 confirmed 2 arrived 1 treated 1", cases)
	}
	if cases.ConfirmRate == nil || *cases.ConfirmRate < 66.6 || *cases.ConfirmRate > 66.7 {
		t.Fatalf("cases confirmRate = %v, want ~66.67", cases.ConfirmRate)
	}
	if cases.ArrivalRate == nil || *cases.ArrivalRate != 50 {
		t.Fatalf("cases arrivalRate = %v, want 50", cases.ArrivalRate)
	}
	if cases.TreatmentRate == nil || *cases.TreatmentRate != 100 {
		t.Fatalf("cases treatmentRate = %v, want 100", cases.TreatmentRate)
	}
	direct := report.Rows[1]
	if direct.Source != "direct" || direct.Created != 1 || direct.Confirmed != 0 || direct.Arrived != 0 || direct.Treated != 0 {
		t.Fatalf("direct row = %+v, want created 1, rest 0", direct)
	}
	if direct.ConfirmRate == nil || *direct.ConfirmRate != 0 || direct.ArrivalRate != nil || direct.TreatmentRate != nil {
		t.Fatalf("direct rates = %v/%v/%v, want 0/nil/nil", direct.ConfirmRate, direct.ArrivalRate, direct.TreatmentRate)
	}
	total := report.Total
	if total.Source != "all" || total.Created != 4 || total.Confirmed != 2 || total.Arrived != 1 || total.Treated != 1 {
		t.Fatalf("total = %+v, want created 4 confirmed 2 arrived 1 treated 1", total)
	}
	// Monotonicity: every step is a subset of the previous one.
	for _, row := range append(report.Rows, report.Total) {
		if !(row.Created >= row.Confirmed && row.Confirmed >= row.Arrived && row.Arrived >= row.Treated) {
			t.Fatalf("funnel not monotonic for %+v", row)
		}
	}
	// Clinic filter outside the grant is a 404, not a leak.
	if _, err := f.repo.ChannelFunnel(f.ctx(f.clinic), store.ChannelFunnelFilter{
		DateRange: store.DateRange{From: mustShanghaiDate(t, "2026-10-01"), Until: mustShanghaiDate(t, "2026-10-06")},
		ClinicID:  f.hidden,
	}, f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unauthorized clinic filter err = %v, want ErrNotFound", err)
	}
	// A non-analytics role is forbidden.
	if _, err := f.repo.ChannelFunnel(store.WithEmployeeScope(context.Background(), "customer_service", "", []string{f.clinic}), store.ChannelFunnelFilter{
		DateRange: store.DateRange{From: mustShanghaiDate(t, "2026-10-01"), Until: mustShanghaiDate(t, "2026-10-06")},
	}, f.now); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("customer_service err = %v, want ErrForbidden", err)
	}
}

func mustShanghaiDate(t *testing.T, value string) time.Time {
	t.Helper()
	day, err := time.ParseInLocation("2006-01-02", value, database.Shanghai)
	if err != nil {
		t.Fatal(err)
	}
	return day
}

func TestValidAppointmentSource(t *testing.T) {
	cases := []struct{ in, want string }{
		{"cases", "cases"}, {"home", "home"}, {"doctors", "doctors"}, {"knowledge", "knowledge"}, {"share", "share"},
		{"", "direct"}, {"  ", "direct"}, {"https://evil.example/x", "direct"}, {"CASES", "direct"},
	}
	for _, c := range cases {
		if got := store.NormalizeAppointmentInput(store.AppointmentInput{Source: c.in}).Source; got != c.want {
			t.Errorf("source %q = %q, want %q", c.in, got, c.want)
		}
	}
}
