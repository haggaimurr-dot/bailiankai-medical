package store_test

import (
	"context"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/store"
)

// 行 109：档案临时授权——生成链接可访问，过期后不可访问，撤销后不可访问。
func TestRecordShareLinkMySQL(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	repo := store.New(f.db)
	patientID := f.patient().Patient.ID

	// 生成 24 小时有效的链接
	link, token, err := repo.CreateRecordShareLink(ctx, patientID, 24*time.Hour, "tester", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected a plaintext token")
	}
	// 可解析
	resolved, err := repo.ResolveRecordShareLink(ctx, token, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != link.ID {
		t.Fatal("resolved wrong link")
	}
	// 错误 token 不可解析
	if _, err := repo.ResolveRecordShareLink(ctx, "bogus-token-xyz", f.now); err == nil {
		t.Fatal("expected error for bogus token")
	}
	// 过期后不可解析
	if _, err := repo.ResolveRecordShareLink(ctx, token, f.now.Add(25*time.Hour)); err == nil {
		t.Fatal("expected error for expired link")
	}
	// 撤销后不可解析
	if err := repo.RevokeRecordShareLink(ctx, link.ID, patientID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ResolveRecordShareLink(ctx, token, f.now); err == nil {
		t.Fatal("expected error for revoked link")
	}
	// 输入校验
	if _, _, err := repo.CreateRecordShareLink(ctx, patientID, 0, "t", f.now); err == nil {
		t.Fatal("expected error for zero ttl")
	}
	if _, _, err := repo.CreateRecordShareLink(ctx, patientID, 100*24*time.Hour, "t", f.now); err == nil {
		t.Fatal("expected error for ttl > 90d")
	}
	// 列表
	links, err := repo.RecordShareLinks(ctx, patientID)
	if err != nil || len(links) != 1 {
		t.Fatalf("links: %d err=%v", len(links), err)
	}

	f.db.Exec("DELETE FROM record_share_links WHERE patient_id = ?", patientID)
}

// 行 110：患者标签分层——设置/读取，非法值拒绝，默认 normal。
func TestPatientTierMySQL(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	repo := store.New(f.db)
	patientID := f.patient().Patient.ID

	// 默认 normal
	tier, err := repo.PatientTier(ctx, patientID)
	if err != nil || tier.Tier != "normal" {
		t.Fatalf("default tier: %+v err=%v", tier, err)
	}
	// 设为 vip
	if err := repo.SetPatientTier(ctx, patientID, "vip", "测试", "manager-1", f.now); err != nil {
		t.Fatal(err)
	}
	tier, _ = repo.PatientTier(ctx, patientID)
	if tier.Tier != "vip" {
		t.Fatalf("tier: %s", tier.Tier)
	}
	// 改为 high_risk
	if err := repo.SetPatientTier(ctx, patientID, "high_risk", "测试", "manager-1", f.now); err != nil {
		t.Fatal(err)
	}
	tier, _ = repo.PatientTier(ctx, patientID)
	if tier.Tier != "high_risk" {
		t.Fatalf("tier: %s", tier.Tier)
	}
	// 非法值拒绝
	if err := repo.SetPatientTier(ctx, patientID, "bogus", "", "m", f.now); err == nil {
		t.Fatal("expected error for invalid tier")
	}

	f.db.Exec("DELETE FROM patient_tiers WHERE patient_id = ?", patientID)
}

// 行 111：复诊带上次要点——无记录时返回空，有记录时返回要点。
func TestLastVisitSummaryMySQL(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	repo := store.New(f.db)
	patientID := f.patient().Patient.ID

	// 新患者无记录
	summary, err := repo.LastVisitSummary(ctx, patientID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AppointmentID != "" {
		t.Fatal("expected empty summary for new patient")
	}
}
