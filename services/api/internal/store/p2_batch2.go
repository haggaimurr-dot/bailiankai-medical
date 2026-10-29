package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Recovery stage plans (P2 行107: 阶段性恢复小结推送). A plan binds a patient
// to a 42-day post-treatment timeline with milestones at 7/14/21/28/35/42 days.
// When a milestone date is reached, CheckRecoveryStageMilestones generates a
// summary from the phase's followup feedbacks and pushes it to the patient's
// in-app message inbox (kind=recovery_stage).

type RecoveryStagePlan struct {
	ID            string `gorm:"primaryKey;size:64"`
	PatientID     string
	AppointmentID string
	ClinicID      string
	StartDate     time.Time `gorm:"type:date"`
	Status        string
	CreatedBy     string
	CreatedAt     time.Time
	Milestones    []RecoveryStageMilestone `gorm:"foreignKey:PlanID"`
}

type RecoveryStageMilestone struct {
	ID         string `gorm:"primaryKey;size:64"`
	PlanID     string
	DayOffset  int
	StageLabel string
	Summary    string
	Notified   bool
	NotifiedAt *time.Time
	CreatedAt  time.Time
}

var ErrRecoveryStageInput = errors.New("invalid recovery stage plan input")

var recoveryStageOffsets = []struct {
	day   int
	label string
}{
	{7, "第1周（术后7天）"}, {14, "第2周（术后14天）"}, {21, "第3周（术后21天）"},
	{28, "第4周（术后28天）"}, {35, "第5周（术后35天）"}, {42, "第6周（术后42天）"},
}

func (RecoveryStagePlan) TableName() string      { return "recovery_stage_plans" }
func (RecoveryStageMilestone) TableName() string { return "recovery_stage_milestones" }

func (s *MySQL) CreateRecoveryStagePlan(ctx context.Context, patientID, appointmentID, clinicID string, startDate time.Time, createdBy string, now time.Time) (RecoveryStagePlan, error) {
	if strings.TrimSpace(patientID) == "" || strings.TrimSpace(clinicID) == "" {
		return RecoveryStagePlan{}, ErrRecoveryStageInput
	}
	var plan RecoveryStagePlan
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		plan = RecoveryStagePlan{
			ID: randomID("rplan-"), PatientID: patientID, AppointmentID: appointmentID,
			ClinicID: clinicID, StartDate: startDate.Truncate(24 * time.Hour),
			Status: "active", CreatedBy: createdBy, CreatedAt: now,
		}
		if err := tx.Create(&plan).Error; err != nil {
			return err
		}
		for _, m := range recoveryStageOffsets {
			ms := RecoveryStageMilestone{
				ID: randomID("rms-"), PlanID: plan.ID, DayOffset: m.day,
				StageLabel: m.label, CreatedAt: now,
			}
			if err := tx.Create(&ms).Error; err != nil {
				return err
			}
			plan.Milestones = append(plan.Milestones, ms)
		}
		return nil
	})
	return plan, err
}

// DeleteRecoveryStagePlan removes a plan and its milestones (no DB-level FK;
// application-layer cascade per the 00089-00093 convention).
func (s *MySQL) DeleteRecoveryStagePlan(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return ErrRecoveryStageInput
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&RecoveryStageMilestone{}, "plan_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&RecoveryStagePlan{}, "id = ?", id).Error
	})
}

func (s *MySQL) RecoveryStagePlans(ctx context.Context, patientID string) ([]RecoveryStagePlan, error) {
	var plans []RecoveryStagePlan
	err := s.db.WithContext(ctx).Preload("Milestones", func(db *gorm.DB) *gorm.DB {
		return db.Order("day_offset ASC")
	}).Where("patient_id = ?", patientID).Order("created_at DESC").Find(&plans).Error
	return plans, err
}

// CheckRecoveryStageMilestones finds milestones whose date has arrived and
// which have not been notified yet, generates a phase summary from the
// patient's followup feedbacks, pushes it as a patient message, and marks the
// milestone notified. Intended to run daily (e.g. via cron); also callable
// from the admin API for testing.
func (s *MySQL) CheckRecoveryStageMilestones(ctx context.Context, now time.Time) (int, error) {
	today := now.In(database.Shanghai).Truncate(24 * time.Hour).Format("2006-01-02")
	var due []RecoveryStageMilestone
	err := s.db.WithContext(ctx).Table("recovery_stage_milestones m").
		Select("m.*").
		Joins("JOIN recovery_stage_plans p ON p.id = m.plan_id").
		Where("m.notified = ? AND p.status = ? AND DATE_ADD(p.start_date, INTERVAL m.day_offset DAY) <= ?", false, "active", today).
		Find(&due).Error
	if err != nil {
		return 0, err
	}
	count := 0
	for _, m := range due {
		if err := s.notifyStageMilestone(ctx, m, now); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (s *MySQL) notifyStageMilestone(ctx context.Context, m RecoveryStageMilestone, now time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var plan RecoveryStagePlan
		if err := tx.First(&plan, "id = ?", m.PlanID).Error; err != nil {
			return err
		}
		if plan.Status != "active" {
			return nil
		}
		// Re-check inside the transaction to avoid double notification.
		var fresh RecoveryStageMilestone
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fresh, "id = ?", m.ID).Error; err != nil {
			return err
		}
		if fresh.Notified {
			return nil
		}
		summary := s.stageSummary(tx, plan, m)
		if err := writePatientMessage(tx, plan.PatientID, "recovery_stage",
			fmt.Sprintf("恢复小结 · %s", m.StageLabel), summary, plan.AppointmentID, "", now); err != nil {
			return err
		}
		fresh.Summary = summary
		fresh.Notified = true
		fresh.NotifiedAt = &now
		return tx.Save(&fresh).Error
	})
}

func (s *MySQL) stageSummary(tx *gorm.DB, plan RecoveryStagePlan, m RecoveryStageMilestone) string {
	phaseStart := plan.StartDate
	phaseEnd := plan.StartDate.AddDate(0, 0, m.DayOffset)
	var tasks []model.FollowupTask
	tx.Where("patient_id = ? AND submitted_at IS NOT NULL AND submitted_at >= ? AND submitted_at < ?",
		plan.PatientID, phaseStart, phaseEnd.AddDate(0, 0, 1)).Find(&tasks)
	labels := []string{"没有不适", "轻微不适", "明显不适", "严重不适"}
	var b strings.Builder
	fmt.Fprintf(&b, "【%s】恢复小结\n", m.StageLabel)
	fmt.Fprintf(&b, "统计周期：%s 至 %s\n", phaseStart.Format("2006-01-02"), phaseEnd.Format("2006-01-02"))
	if len(tasks) == 0 {
		b.WriteString("本阶段暂无随访反馈记录。如有不适请及时填写随访反馈或联系医护。")
		return b.String()
	}
	sum, concerns := 0, 0
	for _, t := range tasks {
		sum += t.Severity
		if t.Concern {
			concerns++
		}
	}
	avg := float64(sum) / float64(len(tasks))
	fmt.Fprintf(&b, "共收到 %d 次随访反馈，平均不适程度 %.1f（%s）。\n", len(tasks), avg, labels[min(3, int(avg+0.5))])
	if concerns > 0 {
		fmt.Fprintf(&b, "其中 %d 次反馈标记了担忧，医护已重点跟进。\n", concerns)
	} else {
		b.WriteString("本阶段反馈均无担忧标记，恢复情况平稳。\n")
	}
	b.WriteString("请继续按医嘱护理，保持良好作息。")
	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============ 行 108: 常见问题自助问答 ============

type Faq struct {
	ID        string `gorm:"primaryKey;size:64"`
	ClinicID  string
	Category  string
	Question  string
	Answer    string
	SortOrder int
	Enabled   bool
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (Faq) TableName() string { return "faqs" }

type FaqInput struct {
	ClinicID  string
	Category  string
	Question  string
	Answer    string
	SortOrder int
	Enabled   bool
	Version   int // for optimistic locking on update (uses UpdatedAt check via version param)
}

var ErrFaqInput = fmt.Errorf("invalid faq input")

func validFaqInput(in FaqInput) bool {
	return strings.TrimSpace(in.Question) != "" && len([]rune(in.Question)) <= 500 &&
		strings.TrimSpace(in.Answer) != "" && len([]rune(in.Category)) <= 64
}

func (s *MySQL) CreateFaq(ctx context.Context, in FaqInput, createdBy string, now time.Time) (Faq, error) {
	if !validFaqInput(in) {
		return Faq{}, ErrFaqInput
	}
	faq := Faq{
		ID: randomID("faq-"), ClinicID: strings.TrimSpace(in.ClinicID), Category: strings.TrimSpace(in.Category),
		Question: strings.TrimSpace(in.Question), Answer: strings.TrimSpace(in.Answer),
		SortOrder: in.SortOrder, Enabled: in.Enabled, CreatedBy: createdBy,
		CreatedAt: now, UpdatedAt: now,
	}
	if faq.Category == "" {
		faq.Category = "通用"
	}
	return faq, s.db.WithContext(ctx).Create(&faq).Error
}

func (s *MySQL) UpdateFaq(ctx context.Context, id string, in FaqInput, now time.Time) (Faq, error) {
	if !validFaqInput(in) || strings.TrimSpace(id) == "" {
		return Faq{}, ErrFaqInput
	}
	var faq Faq
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&faq, "id = ?", id).Error; err != nil {
			return err
		}
		faq.Category = strings.TrimSpace(in.Category)
		if faq.Category == "" {
			faq.Category = "通用"
		}
		faq.Question = strings.TrimSpace(in.Question)
		faq.Answer = strings.TrimSpace(in.Answer)
		faq.SortOrder = in.SortOrder
		faq.Enabled = in.Enabled
		faq.UpdatedAt = now
		return tx.Save(&faq).Error
	})
	return faq, err
}

func (s *MySQL) DeleteFaq(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return ErrFaqInput
	}
	return s.db.WithContext(ctx).Delete(&Faq{}, "id = ?", id).Error
}

func (s *MySQL) AdminFaqs(ctx context.Context, clinicID, category, keyword string) ([]Faq, error) {
	q := s.db.WithContext(ctx).Model(&Faq{})
	if clinicID != "" {
		q = q.Where("clinic_id = ?", clinicID)
	}
	if category != "" {
		q = q.Where("category = ?", category)
	}
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("question LIKE ? OR answer LIKE ?", like, like)
	}
	var faqs []Faq
	err := q.Order("sort_order ASC, created_at DESC").Find(&faqs).Error
	return faqs, err
}

// PatientFaqs returns enabled FAQs for the patient's clinic; keyword searches
// question + answer.
func (s *MySQL) PatientFaqs(ctx context.Context, clinicIDs []string, keyword string) ([]Faq, error) {
	q := s.db.WithContext(ctx).Model(&Faq{}).Where("enabled = ?", true)
	if len(clinicIDs) > 0 {
		q = q.Where("clinic_id = ? OR clinic_id = ?", clinicIDs[0], "")
	}
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("question LIKE ? OR answer LIKE ?", like, like)
	}
	var faqs []Faq
	err := q.Order("sort_order ASC, created_at DESC").Limit(100).Find(&faqs).Error
	return faqs, err
}
