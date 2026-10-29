package store

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrSlotOverlap = errors.New("schedule overlaps an existing slot")
var ErrScheduleInput = errors.New("invalid schedule input")

type ScheduleRepository interface {
	CreateSlots(context.Context, []model.Slot, time.Time) ([]model.Slot, error)
	PreviewBatchSlots(context.Context, SlotBatchGrid, time.Time) ([]SlotDraft, []SlotConflict, error)
	CreateBatchSlots(context.Context, SlotBatchGrid, time.Time) ([]model.Slot, []SlotConflict, error)
	SuspendSlots(context.Context, []SlotReference, string, time.Time) ([]model.Slot, error)
}

// ScheduleTemplateRepository covers schedule templates and holidays.
// MySQL implements it; the httpapi fakeRepository used by the contract test
// does not need to.
type ScheduleTemplateRepository interface {
	CreateScheduleTemplate(context.Context, model.ScheduleTemplate) (model.ScheduleTemplate, error)
	ListScheduleTemplates(context.Context, string) ([]model.ScheduleTemplate, error)
	GetScheduleTemplate(context.Context, string) (model.ScheduleTemplate, error)
	UpdateScheduleTemplate(context.Context, string, string, string) (model.ScheduleTemplate, error)
	DeleteScheduleTemplate(context.Context, string) error
	PreviewTemplateApply(ctx context.Context, clinicID, templateID, startDate, endDate string, now time.Time) ([]TemplateDraft, []TemplateConflict, error)
	ApplyScheduleTemplate(ctx context.Context, clinicID, templateID, startDate, endDate string, now time.Time) ([]model.Slot, []TemplateConflict, error)
	SetHoliday(context.Context, time.Time, string) error
	RemoveHoliday(context.Context, time.Time) error
	ListHolidays(context.Context, time.Time, time.Time) ([]model.Holiday, error)
}
type SlotReference struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}
type ScheduleEvent struct {
	ID        string `gorm:"primaryKey"`
	SlotID    string
	ActorID   string
	Action    string
	Reason    string
	CreatedAt time.Time
}

func (s *MySQL) CreateSlots(ctx context.Context, inputs []model.Slot, now time.Time) ([]model.Slot, error) {
	if len(inputs) < 1 || len(inputs) > 500 {
		return nil, ErrScheduleInput
	}
	rows := append([]model.Slot(nil), inputs...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].DoctorID != rows[j].DoctorID {
			return rows[i].DoctorID < rows[j].DoctorID
		}
		return rows[i].StartsAt.Before(rows[j].StartsAt)
	})
	for i, row := range rows {
		if row.ClinicID == "" || row.DoctorID == "" || !row.EndsAt.After(row.StartsAt) || row.EndsAt.Sub(row.StartsAt) > 24*time.Hour || row.Capacity < 0 || row.Capacity > 100 || (row.Status != "open" && row.Status != "closed") {
			return nil, ErrScheduleInput
		}
		if !row.StartsAt.After(now) {
			return nil, ErrPast
		}
		if i > 0 && rows[i-1].DoctorID == row.DoctorID && rows[i-1].EndsAt.After(row.StartsAt) {
			return nil, ErrSlotOverlap
		}
	}
	started := time.Now()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize creation for each doctor, including when no slots yet exist.
		locked := map[string]string{}
		for _, row := range rows {
			if clinic, ok := locked[row.DoctorID]; ok {
				if clinic != row.ClinicID {
					return ErrNotFound
				}
				continue
			}
			var doctor model.Doctor
			if err := clinicScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"}), "clinic_id").Where("id = ? AND clinic_id = ? AND active = TRUE AND published_json IS NOT NULL", row.DoctorID, row.ClinicID).First(&doctor).Error; err != nil {
				return followupDBError(err)
			}
			var clinic model.Clinic
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND active = TRUE AND published_json IS NOT NULL", row.ClinicID).First(&clinic).Error; err != nil {
				return followupDBError(err)
			}
			locked[row.DoctorID] = row.ClinicID
		}
		for i := range rows {
			row := &rows[i]
			if err := scheduleHolidayCheck(tx, *row); err != nil {
				return err
			}
			if !row.StartsAt.After(now.Add(time.Since(started))) {
				return ErrPast
			}
			// A locking read observes any creation committed while waiting for the doctor lock.
			var overlaps []model.Slot
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("doctor_id = ? AND starts_at < ? AND ends_at > ?", row.DoctorID, row.EndsAt, row.StartsAt).Find(&overlaps).Error; err != nil {
				return err
			}
			if len(overlaps) > 0 {
				return ErrSlotOverlap
			}
			row.ID, row.Reserved, row.Version = randomID("slot-"), 0, 1
			if err := tx.Create(row).Error; err != nil {
				return err
			}
			if err := tx.Create(&ScheduleEvent{ID: randomID("schedule-"), SlotID: row.ID, ActorID: employeeActor(ctx), Action: "created", CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *MySQL) SuspendSlots(ctx context.Context, refs []SlotReference, reason string, now time.Time) ([]model.Slot, error) {
	reason = strings.TrimSpace(reason)
	if len(refs) < 1 || len(refs) > 500 || utf8.RuneCountInString(reason) < 1 || utf8.RuneCountInString(reason) > 200 {
		return nil, ErrScheduleInput
	}
	refs = append([]SlotReference(nil), refs...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	for i, ref := range refs {
		if ref.ID == "" || ref.Version < 1 || (i > 0 && refs[i-1].ID == ref.ID) {
			return nil, ErrScheduleInput
		}
	}
	started := time.Now()
	rows := make([]model.Slot, 0, len(refs))
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, ref := range refs {
			var row model.Slot
			if err := clinicScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"}), "clinic_id").First(&row, "id = ?", ref.ID).Error; err != nil {
				return followupDBError(err)
			}
			if row.Version != ref.Version {
				return ErrConflict
			}
			if !row.StartsAt.After(now.Add(time.Since(started))) {
				return ErrPast
			}
			if row.Status == "closed" {
				return ErrConflict
			}
			if err := tx.Model(&row).Updates(map[string]any{"status": "closed", "version": row.Version + 1}).Error; err != nil {
				return err
			}
			if err := tx.Create(&ScheduleEvent{ID: randomID("schedule-"), SlotID: row.ID, ActorID: employeeActor(ctx), Action: "suspended", Reason: reason, CreatedAt: now}).Error; err != nil {
				return err
			}
			if err := createSchedulePatientTasks(tx, ctx, row.ID, reason, now); err != nil {
				return err
			}
			// 改期推荐消息只记日志、不阻断停诊：待联系清单已同步建任务兜底人工联系。
			if err := notifySuspensionReschedule(tx, row, reason, now); err != nil {
				slog.Error("suspension reschedule notification failed", "slot", row.ID, "error", err)
			}
			rows = append(rows, row)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Batch slot generation: preview conflicts before confirming, then create
// only the non-conflicting part. Existing slots are never modified, so a
// batch can never lower an existing slot's capacity below its reservations.

const (
	SlotConflictOverlap = "overlap"
	SlotConflictPast    = "past"
	SlotConflictHoliday = "holiday"
)

type SlotBatchGrid struct {
	ClinicID        string
	DoctorID        string
	StartDate       string
	EndDate         string
	StartTimes      []string
	DurationMinutes int
	Capacity        int
}

type SlotDraft struct {
	Date     string
	StartsAt time.Time
	EndsAt   time.Time
}

type SlotConflict struct {
	Date             string
	StartsAt         time.Time
	EndsAt           time.Time
	Reason           string
	ExistingSlotID   string
	ExistingCapacity int
	ExistingReserved int
}

func expandSlotBatchGrid(grid SlotBatchGrid) ([]SlotDraft, error) {
	if grid.ClinicID == "" || grid.DoctorID == "" {
		return nil, ErrScheduleInput
	}
	duration := grid.DurationMinutes
	if duration == 0 {
		duration = 30
	}
	if duration < 15 || duration > 120 || grid.Capacity < 1 || grid.Capacity > 100 {
		return nil, ErrScheduleInput
	}
	start, err := time.ParseInLocation("2006-01-02", grid.StartDate, database.Shanghai)
	if err != nil {
		return nil, ErrScheduleInput
	}
	end, err := time.ParseInLocation("2006-01-02", grid.EndDate, database.Shanghai)
	if err != nil {
		return nil, ErrScheduleInput
	}
	if end.Before(start) || end.Sub(start) > 31*24*time.Hour {
		return nil, ErrScheduleInput
	}
	if len(grid.StartTimes) < 1 || len(grid.StartTimes) > 24 {
		return nil, ErrScheduleInput
	}
	seen := map[string]bool{}
	times := make([]time.Time, 0, len(grid.StartTimes))
	for _, raw := range grid.StartTimes {
		parsed, err := time.Parse("15:04", raw)
		if err != nil {
			return nil, ErrScheduleInput
		}
		if seen[raw] {
			continue
		}
		seen[raw] = true
		times = append(times, parsed)
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	drafts := make([]SlotDraft, 0, 512)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		for _, clock := range times {
			startsAt := time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, database.Shanghai)
			drafts = append(drafts, SlotDraft{Date: day.Format("2006-01-02"), StartsAt: startsAt, EndsAt: startsAt.Add(time.Duration(duration) * time.Minute)})
		}
	}
	if len(drafts) == 0 || len(drafts) > 500 {
		return nil, ErrScheduleInput
	}
	return drafts, nil
}

func batchRestHolidays(tx *gorm.DB, clinicID string) (map[string]bool, error) {
	days := map[string]bool{}
	var setting ScheduleSetting
	err := tx.First(&setting, "clinic_id = ?", clinicID).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	if err == nil {
		for _, h := range setting.Holidays {
			if h.Kind != "rest" {
				continue
			}
			day, err := time.ParseInLocation("2006-01-02", h.Date, database.Shanghai)
			if err != nil {
				return nil, ErrScheduleInput
			}
			days[day.Format("2006-01-02")] = true
		}
	}
	// 节假日表（全局）：节假日当天自动停诊，批量生成与模板套用均跳过。
	var dates []time.Time
	if err := tx.Model(&model.Holiday{}).Pluck("holiday_date", &dates).Error; err != nil {
		return nil, err
	}
	for _, d := range dates {
		days[d.In(database.Shanghai).Format("2006-01-02")] = true
	}
	return days, nil
}

func slotWindowsOverlap(aStart, aEnd, bStart, bEnd time.Time) bool {
	return aStart.Before(bEnd) && aEnd.After(bStart)
}

func classifySlotDraft(draft SlotDraft, existing []model.Slot, accepted []SlotDraft, holidays map[string]bool, now time.Time) (SlotDraft, *SlotConflict) {
	if !draft.StartsAt.After(now) {
		return SlotDraft{}, &SlotConflict{Date: draft.Date, StartsAt: draft.StartsAt, EndsAt: draft.EndsAt, Reason: SlotConflictPast}
	}
	if holidays[draft.Date] {
		return SlotDraft{}, &SlotConflict{Date: draft.Date, StartsAt: draft.StartsAt, EndsAt: draft.EndsAt, Reason: SlotConflictHoliday}
	}
	for _, row := range existing {
		if slotWindowsOverlap(draft.StartsAt, draft.EndsAt, row.StartsAt, row.EndsAt) {
			return SlotDraft{}, &SlotConflict{Date: draft.Date, StartsAt: draft.StartsAt, EndsAt: draft.EndsAt, Reason: SlotConflictOverlap, ExistingSlotID: row.ID, ExistingCapacity: row.Capacity, ExistingReserved: row.Reserved}
		}
	}
	for _, row := range accepted {
		if slotWindowsOverlap(draft.StartsAt, draft.EndsAt, row.StartsAt, row.EndsAt) {
			return SlotDraft{}, &SlotConflict{Date: draft.Date, StartsAt: draft.StartsAt, EndsAt: draft.EndsAt, Reason: SlotConflictOverlap}
		}
	}
	return draft, nil
}

func (s *MySQL) PreviewBatchSlots(ctx context.Context, grid SlotBatchGrid, now time.Time) ([]SlotDraft, []SlotConflict, error) {
	drafts, err := expandSlotBatchGrid(grid)
	if err != nil {
		return nil, nil, err
	}
	return s.previewSlotDrafts(ctx, grid.ClinicID, grid.DoctorID, drafts, now)
}

// previewSlotDrafts classifies an explicit draft list for one doctor. It backs
// both batch generation and schedule-template apply so the two stay consistent.
func (s *MySQL) previewSlotDrafts(ctx context.Context, clinicID, doctorID string, drafts []SlotDraft, now time.Time) ([]SlotDraft, []SlotConflict, error) {
	toCreate := make([]SlotDraft, 0, len(drafts))
	conflicts := make([]SlotConflict, 0)
	if len(drafts) == 0 {
		return toCreate, conflicts, nil
	}
	var doctor model.Doctor
	if err := clinicScope(ctx, s.db.WithContext(ctx), "clinic_id").Where("id = ? AND clinic_id = ? AND active = TRUE AND published_json IS NOT NULL", doctorID, clinicID).First(&doctor).Error; err != nil {
		return nil, nil, followupDBError(err)
	}
	var clinic model.Clinic
	if err := s.db.WithContext(ctx).Where("id = ? AND active = TRUE AND published_json IS NOT NULL", clinicID).First(&clinic).Error; err != nil {
		return nil, nil, followupDBError(err)
	}
	var existing []model.Slot
	if err := s.db.WithContext(ctx).Where("doctor_id = ? AND starts_at < ? AND ends_at > ?", doctorID, drafts[len(drafts)-1].EndsAt, drafts[0].StartsAt).Find(&existing).Error; err != nil {
		return nil, nil, err
	}
	holidays, err := batchRestHolidays(s.db.WithContext(ctx), clinicID)
	if err != nil {
		return nil, nil, err
	}
	for _, draft := range drafts {
		accepted, conflict := classifySlotDraft(draft, existing, toCreate, holidays, now)
		if conflict != nil {
			conflicts = append(conflicts, *conflict)
			continue
		}
		toCreate = append(toCreate, accepted)
	}
	return toCreate, conflicts, nil
}

// slotDraftFull is a batch draft with its own capacity. Template entries may
// vary capacity per weekday; the plain batch grid uses one capacity for all.
type slotDraftFull struct {
	SlotDraft
	Capacity int
}

func (s *MySQL) CreateBatchSlots(ctx context.Context, grid SlotBatchGrid, now time.Time) ([]model.Slot, []SlotConflict, error) {
	drafts, err := expandSlotBatchGrid(grid)
	if err != nil {
		return nil, nil, err
	}
	full := make([]slotDraftFull, 0, len(drafts))
	for _, d := range drafts {
		full = append(full, slotDraftFull{SlotDraft: d, Capacity: grid.Capacity})
	}
	return s.createSlotDrafts(ctx, grid.ClinicID, grid.DoctorID, full, now)
}

// createSlotDrafts creates the non-conflicting drafts for one doctor inside a
// transaction, re-checking overlaps with a locking read. It backs both batch
// generation and schedule-template apply.
func (s *MySQL) createSlotDrafts(ctx context.Context, clinicID, doctorID string, drafts []slotDraftFull, now time.Time) ([]model.Slot, []SlotConflict, error) {
	created := make([]model.Slot, 0, len(drafts))
	skipped := make([]SlotConflict, 0)
	started := time.Now()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var doctor model.Doctor
		if err := clinicScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"}), "clinic_id").Where("id = ? AND clinic_id = ? AND active = TRUE AND published_json IS NOT NULL", doctorID, clinicID).First(&doctor).Error; err != nil {
			return followupDBError(err)
		}
		var clinic model.Clinic
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND active = TRUE AND published_json IS NOT NULL", clinicID).First(&clinic).Error; err != nil {
			return followupDBError(err)
		}
		holidays, err := batchRestHolidays(tx, clinicID)
		if err != nil {
			return err
		}
		accepted := make([]SlotDraft, 0, len(drafts))
		for _, draft := range drafts {
			// A locking read observes any creation committed while waiting for the doctor lock.
			var overlapping []model.Slot
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("doctor_id = ? AND starts_at < ? AND ends_at > ?", doctorID, draft.EndsAt, draft.StartsAt).Find(&overlapping).Error; err != nil {
				return err
			}
			acceptedDraft, conflict := classifySlotDraft(draft.SlotDraft, overlapping, accepted, holidays, now.Add(time.Since(started)))
			if conflict != nil {
				skipped = append(skipped, *conflict)
				continue
			}
			row := model.Slot{ClinicID: clinicID, DoctorID: doctorID, StartsAt: acceptedDraft.StartsAt, EndsAt: acceptedDraft.EndsAt, Capacity: draft.Capacity, Status: "open", ID: randomID("slot-"), Reserved: 0, Version: 1}
			if err := scheduleHolidayCheck(tx, row); err != nil {
				if errors.Is(err, ErrScheduleInput) {
					skipped = append(skipped, SlotConflict{Date: draft.Date, StartsAt: draft.StartsAt, EndsAt: draft.EndsAt, Reason: SlotConflictHoliday})
					continue
				}
				return err
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			if err := tx.Create(&ScheduleEvent{ID: randomID("schedule-"), SlotID: row.ID, ActorID: employeeActor(ctx), Action: "created", CreatedAt: now}).Error; err != nil {
				return err
			}
			accepted = append(accepted, acceptedDraft)
			created = append(created, row)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return created, skipped, nil
}
