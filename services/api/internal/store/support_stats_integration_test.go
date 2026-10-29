package store_test

import (
	"context"
	"testing"

	"example.com/bailiankai/api/internal/store"
)

// 新增-P1–P2：客服响应时长与满意度统计。首响只计首条患者可见的客服消息，
// 内部记录不计入；满意度只计已评分工单；口径与工单队列共用 supportQuery。
func TestSupportStats(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	clinic := f.slot.ClinicID
	manager := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", []string{clinic}), "support-stats-manager")
	patient := f.patient()
	// 4 张工单：首响 120/30/15 分钟各一，未首响一张（仅患者消息+内部记录）；
	// 评分 5/4 各一，另两张未评分。
	tickets := []struct {
		id, created string
		rating      int
	}{
		{"support-stats-t1", "DATE_SUB(NOW(), INTERVAL 10 HOUR)", 5},
		{"support-stats-t2", "DATE_SUB(NOW(), INTERVAL 5 HOUR)", 4},
		{"support-stats-t3", "DATE_SUB(NOW(), INTERVAL 3 HOUR)", 0},
		{"support-stats-t4", "DATE_SUB(NOW(), INTERVAL 1 HOUR)", 0},
	}
	for _, tk := range tickets {
		if err := f.db.Exec(`INSERT INTO support_tickets (id, patient_id, clinic_id, subject, category, status, rating, request_key, request_hash, created_at, updated_at)
			VALUES (?, ?, ?, '统计测试', 'other', 'open', ?, ?, 'x', `+tk.created+`, NOW())`,
			tk.id, patient.Patient.ID, clinic, tk.rating, "stats-"+tk.id).Error; err != nil {
			t.Fatal(err)
		}
	}
	events := []struct {
		id, ticket, actor, kind, at string
		internal                    bool
	}{
		{"support-stats-e1", "support-stats-t1", "employee", "message", "DATE_SUB(NOW(), INTERVAL 8 HOUR)", false},
		{"support-stats-e2", "support-stats-t2", "employee", "message", "DATE_SUB(NOW(), INTERVAL 270 MINUTE)", false},
		{"support-stats-e3", "support-stats-t3", "patient", "message", "DATE_SUB(NOW(), INTERVAL 170 MINUTE)", false},
		{"support-stats-e4", "support-stats-t3", "employee", "note", "DATE_SUB(NOW(), INTERVAL 170 MINUTE)", true},
		{"support-stats-e5", "support-stats-t4", "employee", "message", "DATE_SUB(NOW(), INTERVAL 45 MINUTE)", false},
	}
	for _, ev := range events {
		if err := f.db.Exec(`INSERT INTO support_events (id, ticket_id, kind, actor_type, actor_id, content, internal, request_key, request_hash, created_at)
			VALUES (?, ?, ?, ?, 'x', '统计测试', ?, ?, 'x', `+ev.at+`)`,
			ev.id, ev.ticket, ev.kind, ev.actor, ev.internal, "stats-"+ev.id).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		f.db.Exec("DELETE FROM support_events WHERE id LIKE 'support-stats-%'")
		f.db.Exec("DELETE FROM support_tickets WHERE id LIKE 'support-stats-%'")
	})
	stats, err := f.repo.SupportStats(manager)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalTickets != 4 || stats.RespondedTickets != 3 || stats.RatedTickets != 2 {
		t.Fatalf("stats=%+v want total=4 responded=3 rated=2", stats)
	}
	if stats.AvgFirstResponseMin == nil || *stats.AvgFirstResponseMin < 54.9 || *stats.AvgFirstResponseMin > 55.1 {
		t.Fatalf("avg first response=%v want 55", stats.AvgFirstResponseMin)
	}
	if stats.AvgRating == nil || *stats.AvgRating != 4.5 {
		t.Fatalf("avg rating=%v want 4.5", stats.AvgRating)
	}
}
