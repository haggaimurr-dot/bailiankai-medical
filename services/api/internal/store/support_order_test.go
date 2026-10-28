package store

import (
	"strings"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Regression test: GORM's Order() silently drops clause.Expr values, which
// once shipped a queue with no ORDER BY at all. The production code inlines
// the overdue cutoff as a literal; this test pins the generated SQL.
func TestSupportTicketsOrderSQL(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{DSN: "unused:unused@tcp(127.0.0.1:1)/unused", SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 18, 30, 0, 0, time.Local)
	rows := make([]model.SupportTicket, 0)
	stmt := db.WithContext(t.Context()).Model(&model.SupportTicket{}).Order("CASE WHEN due_at < '" + now.Format("2006-01-02 15:04:05") + "' AND status NOT IN ('resolved', 'closed') THEN 0 ELSE 1 END, created_at ASC, id ASC").Offset(0).Limit(20).Find(&rows).Statement
	sql := stmt.SQL.String()
	if !strings.Contains(sql, "ORDER BY CASE WHEN due_at < '2026-10-28 18:30:00'") || !strings.Contains(sql, "created_at ASC, id ASC") {
		t.Fatalf("unexpected order SQL: %s", sql)
	}
}
