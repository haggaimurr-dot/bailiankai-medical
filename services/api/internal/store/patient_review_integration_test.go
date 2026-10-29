package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/store"
)

// 新增-P1–P2：医生患者评价。仅已治疗患者可评，每单一次；评价先待审核，
// 通过后才公开显示；驳回的不公开；重复提交与未治疗提交均被拒绝。
func TestPatientReviewLifecycle(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	patient := f.patient()
	appt := f.create(patient, "review-1")
	doctorID := f.slot.DoctorID

	// 未治疗时提交应被拒绝。
	if _, err := f.repo.CreatePatientReview(ctx, patient.Patient.ID, doctorID, appt.ID, 5, "很好", f.now); !errors.Is(err, store.ErrReviewNotEligible) {
		t.Fatalf("untreated submit err=%v want ErrReviewNotEligible", err)
	}
	// 造一条治疗记录（测试库专用：直插并临时关闭外键检查，plan_version 仅占位）。
	if err := f.db.Exec("SET FOREIGN_KEY_CHECKS=0").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`INSERT INTO treatments (id, appointment_id, patient_id, plan_version_id, summary, notes, treated_at, recorded_at, request_hash)
		VALUES (?, ?, ?, 'test-plan-version', '测试治疗', '', ?, ?, 'x')`, "treatment-review-1", appt.ID, patient.Patient.ID, f.now, f.now).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec("SET FOREIGN_KEY_CHECKS=1").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.db.Exec("DELETE FROM doctor_reviews WHERE appointment_id = ?", appt.ID)
		f.db.Exec("DELETE FROM treatments WHERE id = 'treatment-review-1'")
	})

	// 评分与内容校验。
	if _, err := f.repo.CreatePatientReview(ctx, patient.Patient.ID, doctorID, appt.ID, 6, "很好", f.now); err == nil {
		t.Fatal("rating 6 accepted")
	}
	if _, err := f.repo.CreatePatientReview(ctx, patient.Patient.ID, doctorID, appt.ID, 5, "  ", f.now); err == nil {
		t.Fatal("empty content accepted")
	}
	// 他人提交同一预约应被拒绝。
	other := f.patient()
	if _, err := f.repo.CreatePatientReview(ctx, other.Patient.ID, doctorID, appt.ID, 5, "很好", f.now); !errors.Is(err, store.ErrReviewNotEligible) {
		t.Fatalf("other patient submit err=%v want ErrReviewNotEligible", err)
	}
	// 本人提交成功，待审核，不公开。
	review, err := f.repo.CreatePatientReview(ctx, patient.Patient.ID, doctorID, appt.ID, 5, "沟通耐心，方案讲得清楚", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if review.Status != "pending" {
		t.Fatalf("status=%s want pending", review.Status)
	}
	if public, err := f.repo.PublicPatientReviews(ctx, doctorID); err != nil || len(public) != 0 {
		t.Fatalf("public before approval=%d err=%v want 0", len(public), err)
	}
	// 每单一次。
	if _, err := f.repo.CreatePatientReview(ctx, patient.Patient.ID, doctorID, appt.ID, 4, "再评一次", f.now); !errors.Is(err, store.ErrReviewDuplicate) {
		t.Fatalf("duplicate submit err=%v want ErrReviewDuplicate", err)
	}
	// 已评价的预约不再出现在可评价列表。
	if list, err := f.repo.ReviewableAppointments(ctx, patient.Patient.ID, doctorID); err != nil || len(list) != 0 {
		t.Fatalf("reviewable after review=%d err=%v want 0", len(list), err)
	}
	// 管理端待审核队列可见。
	pending, err := f.repo.AdminPatientReviews(ctx, "pending")
	if err != nil || len(pending) != 1 || pending[0].ID != review.ID {
		t.Fatalf("admin pending=%v err=%v", pending, err)
	}
	// 通过后公开可见，且带患者署名。
	decided, err := f.repo.DecidePatientReview(ctx, review.ID, "approved", "", f.now.Add(time.Minute))
	if err != nil || decided.Status != "approved" {
		t.Fatalf("decide approved err=%v status=%s", err, decided.Status)
	}
	public, err := f.repo.PublicPatientReviews(ctx, doctorID)
	if err != nil || len(public) != 1 || public[0].Content != "沟通耐心，方案讲得清楚" || public[0].PatientDisplayName == "" {
		t.Fatalf("public after approval=%v err=%v", public, err)
	}
	// 已审核的不能再审。
	if _, err := f.repo.DecidePatientReview(ctx, review.ID, "rejected", "晚了", f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("re-decide err=%v want ErrConflict", err)
	}
	// 本人列表可见已通过状态。
	mine, err := f.repo.MyPatientReviews(ctx, patient.Patient.ID, doctorID)
	if err != nil || len(mine) != 1 || mine[0].Status != "approved" {
		t.Fatalf("my reviews=%v err=%v", mine, err)
	}
}
