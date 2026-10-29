package store

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
)

// TemplateEntry is one weekly rule inside a schedule template's pattern JSON.
// Weekday follows ISO: 1=Monday .. 7=Sunday.
type TemplateEntry struct {
	DoctorID        string   `json:"doctorId"`
	Weekday         int      `json:"weekday"`
	StartTimes      []string `json:"startTimes"`
	DurationMinutes int      `json:"durationMinutes"`
	Capacity        int      `json:"capacity"`
}

// TemplatePattern is the decoded schedule_templates.pattern_json.
type TemplatePattern struct {
	Entries []TemplateEntry `json:"entries"`
}

// TemplateDraft is an expanded slot candidate from a template.
type TemplateDraft struct {
	DoctorID        string
	Date            string
	StartsAt        time.Time
	EndsAt          time.Time
	DurationMinutes int
	Capacity        int
}

// TemplateConflict is a slot conflict with the owning doctor attached.
type TemplateConflict struct {
	DoctorID string
	SlotConflict
}

func (s *MySQL) CreateScheduleTemplate(ctx context.Context, tpl model.ScheduleTemplate) (model.ScheduleTemplate, error) {
	if tpl.ID == "" {
		tpl.ID = randomID("sched-tpl-")
	}
	tpl.CreatedAt = time.Now()
	tpl.CreatedBy = employeeActor(ctx)
	if err := s.db.WithContext(ctx).Create(&tpl).Error; err != nil {
		return model.ScheduleTemplate{}, err
	}
	return tpl, nil
}

func (s *MySQL) ListScheduleTemplates(ctx context.Context, clinicID string) ([]model.ScheduleTemplate, error) {
	var rows []model.ScheduleTemplate
	q := s.db.WithContext(ctx).Order("created_at DESC")
	if clinicID != "" {
		q = q.Where("clinic_id = ?", clinicID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *MySQL) GetScheduleTemplate(ctx context.Context, id string) (model.ScheduleTemplate, error) {
	var tpl model.ScheduleTemplate
	if err := s.db.WithContext(ctx).First(&tpl, "id = ?", id).Error; err != nil {
		return model.ScheduleTemplate{}, followupDBError(err)
	}
	return tpl, nil
}

func (s *MySQL) UpdateScheduleTemplate(ctx context.Context, id, name, patternJSON string) (model.ScheduleTemplate, error) {
	tpl, err := s.GetScheduleTemplate(ctx, id)
	if err != nil {
		return model.ScheduleTemplate{}, err
	}
	tpl.Name = name
	tpl.PatternJSON = patternJSON
	if err := s.db.WithContext(ctx).Save(&tpl).Error; err != nil {
		return model.ScheduleTemplate{}, err
	}
	return tpl, nil
}

func (s *MySQL) DeleteScheduleTemplate(ctx context.Context, id string) error {
	tpl, err := s.GetScheduleTemplate(ctx, id)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Delete(&tpl).Error
}

// ParseTemplatePattern decodes and validates a template pattern JSON document.
func ParseTemplatePattern(patternJSON string) (TemplatePattern, error) {
	var pattern TemplatePattern
	if err := json.Unmarshal([]byte(patternJSON), &pattern); err != nil {
		return TemplatePattern{}, ErrScheduleInput
	}
	if len(pattern.Entries) == 0 || len(pattern.Entries) > 50 {
		return TemplatePattern{}, ErrScheduleInput
	}
	for i := range pattern.Entries {
		e := &pattern.Entries[i]
		if e.DoctorID == "" || len(e.DoctorID) > 64 {
			return TemplatePattern{}, ErrScheduleInput
		}
		if e.Weekday < 1 || e.Weekday > 7 {
			return TemplatePattern{}, ErrScheduleInput
		}
		if e.DurationMinutes == 0 {
			e.DurationMinutes = 30
		}
		if e.DurationMinutes < 15 || e.DurationMinutes > 120 || e.Capacity < 1 || e.Capacity > 100 {
			return TemplatePattern{}, ErrScheduleInput
		}
		if len(e.StartTimes) == 0 || len(e.StartTimes) > 24 {
			return TemplatePattern{}, ErrScheduleInput
		}
		seen := map[string]bool{}
		times := make([]string, 0, len(e.StartTimes))
		for _, raw := range e.StartTimes {
			if _, err := time.Parse("15:04", raw); err != nil {
				return TemplatePattern{}, ErrScheduleInput
			}
			if seen[raw] {
				continue
			}
			seen[raw] = true
			times = append(times, raw)
		}
		sort.Strings(times)
		e.StartTimes = times
	}
	return pattern, nil
}

// ExpandTemplateDrafts expands a validated pattern over [startDate, endDate]
// (inclusive, YYYY-MM-DD, at most 31 days) into slot drafts.
func ExpandTemplateDrafts(pattern TemplatePattern, startDate, endDate string) ([]TemplateDraft, error) {
	start, err := time.ParseInLocation("2006-01-02", startDate, database.Shanghai)
	if err != nil {
		return nil, ErrScheduleInput
	}
	end, err := time.ParseInLocation("2006-01-02", endDate, database.Shanghai)
	if err != nil {
		return nil, ErrScheduleInput
	}
	if end.Before(start) || end.Sub(start) > 31*24*time.Hour {
		return nil, ErrScheduleInput
	}
	drafts := make([]TemplateDraft, 0, 512)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		weekday := (int(day.Weekday()) + 6) % 7 // Go Sunday=0 -> ISO Monday=1..Sunday=7
		weekday++
		for _, e := range pattern.Entries {
			if e.Weekday != weekday {
				continue
			}
			for _, raw := range e.StartTimes {
				clock, _ := time.Parse("15:04", raw)
				startsAt := time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, database.Shanghai)
				drafts = append(drafts, TemplateDraft{
					DoctorID:        e.DoctorID,
					Date:            day.Format("2006-01-02"),
					StartsAt:        startsAt,
					EndsAt:          startsAt.Add(time.Duration(e.DurationMinutes) * time.Minute),
					DurationMinutes: e.DurationMinutes,
					Capacity:        e.Capacity,
				})
			}
		}
	}
	if len(drafts) == 0 || len(drafts) > 2000 {
		return nil, ErrScheduleInput
	}
	return drafts, nil
}

func (s *MySQL) loadTemplateForApply(ctx context.Context, clinicID, templateID string) (TemplatePattern, error) {
	tpl, err := s.GetScheduleTemplate(ctx, templateID)
	if err != nil {
		return TemplatePattern{}, err
	}
	if tpl.ClinicID != clinicID {
		return TemplatePattern{}, ErrNotFound
	}
	return ParseTemplatePattern(tpl.PatternJSON)
}

// PreviewTemplateApply expands the template and classifies every draft without
// writing anything. Drafts are grouped per doctor so each group reuses the same
// validation, holiday and overlap rules as batch generation.
func (s *MySQL) PreviewTemplateApply(ctx context.Context, clinicID, templateID, startDate, endDate string, now time.Time) ([]TemplateDraft, []TemplateConflict, error) {
	pattern, err := s.loadTemplateForApply(ctx, clinicID, templateID)
	if err != nil {
		return nil, nil, err
	}
	drafts, err := ExpandTemplateDrafts(pattern, startDate, endDate)
	if err != nil {
		return nil, nil, err
	}
	byDoctor := map[string][]SlotDraft{}
	order := []string{}
	for _, d := range drafts {
		if _, ok := byDoctor[d.DoctorID]; !ok {
			order = append(order, d.DoctorID)
		}
		byDoctor[d.DoctorID] = append(byDoctor[d.DoctorID], SlotDraft{Date: d.Date, StartsAt: d.StartsAt, EndsAt: d.EndsAt})
	}
	// doctor -> startsAt -> capacity, to re-attach capacities after preview
	capOf := map[string]map[string]int{}
	for _, d := range drafts {
		if capOf[d.DoctorID] == nil {
			capOf[d.DoctorID] = map[string]int{}
		}
		capOf[d.DoctorID][d.StartsAt.Format(time.RFC3339)] = d.Capacity
	}
	toCreate := make([]TemplateDraft, 0, len(drafts))
	conflicts := make([]TemplateConflict, 0)
	for _, doctorID := range order {
		ok, cf, err := s.previewSlotDrafts(ctx, clinicID, doctorID, byDoctor[doctorID], now)
		if err != nil {
			return nil, nil, err
		}
		for _, d := range ok {
			toCreate = append(toCreate, TemplateDraft{DoctorID: doctorID, Date: d.Date, StartsAt: d.StartsAt, EndsAt: d.EndsAt, Capacity: capOf[doctorID][d.StartsAt.Format(time.RFC3339)]})
		}
		for _, c := range cf {
			conflicts = append(conflicts, TemplateConflict{DoctorID: doctorID, SlotConflict: c})
		}
	}
	return toCreate, conflicts, nil
}

// ApplyScheduleTemplate creates the non-conflicting drafts of a template.
// Each doctor group commits in its own transaction; conflicts (overlap, past,
// holiday) are skipped and reported, never modifying existing slots.
func (s *MySQL) ApplyScheduleTemplate(ctx context.Context, clinicID, templateID, startDate, endDate string, now time.Time) ([]model.Slot, []TemplateConflict, error) {
	pattern, err := s.loadTemplateForApply(ctx, clinicID, templateID)
	if err != nil {
		return nil, nil, err
	}
	drafts, err := ExpandTemplateDrafts(pattern, startDate, endDate)
	if err != nil {
		return nil, nil, err
	}
	byDoctor := map[string][]slotDraftFull{}
	order := []string{}
	for _, d := range drafts {
		if _, ok := byDoctor[d.DoctorID]; !ok {
			order = append(order, d.DoctorID)
		}
		byDoctor[d.DoctorID] = append(byDoctor[d.DoctorID], slotDraftFull{SlotDraft: SlotDraft{Date: d.Date, StartsAt: d.StartsAt, EndsAt: d.EndsAt}, Capacity: d.Capacity})
	}
	created := make([]model.Slot, 0, len(drafts))
	skipped := make([]TemplateConflict, 0)
	for _, doctorID := range order {
		rows, cf, err := s.createSlotDrafts(ctx, clinicID, doctorID, byDoctor[doctorID], now)
		if err != nil {
			return nil, nil, err
		}
		created = append(created, rows...)
		for _, c := range cf {
			skipped = append(skipped, TemplateConflict{DoctorID: doctorID, SlotConflict: c})
		}
	}
	return created, skipped, nil
}

// shanghaiDay normalizes a timestamp to midnight of its Shanghai calendar date.
// Truncate(24h) would truncate in UTC and shift the date for +0800 times.
func shanghaiDay(t time.Time) time.Time {
	s := t.In(database.Shanghai)
	return time.Date(s.Year(), s.Month(), s.Day(), 0, 0, 0, 0, database.Shanghai)
}

func (s *MySQL) SetHoliday(ctx context.Context, date time.Time, name string) error {
	h := model.Holiday{HolidayDate: shanghaiDay(date), Name: name, CreatedAt: time.Now()}
	return s.db.WithContext(ctx).Save(&h).Error
}

func (s *MySQL) RemoveHoliday(ctx context.Context, date time.Time) error {
	return s.db.WithContext(ctx).Delete(&model.Holiday{}, "holiday_date = ?", shanghaiDay(date)).Error
}

func (s *MySQL) ListHolidays(ctx context.Context, from, to time.Time) ([]model.Holiday, error) {
	var rows []model.Holiday
	if err := s.db.WithContext(ctx).Where("holiday_date BETWEEN ? AND ?", shanghaiDay(from), shanghaiDay(to)).Order("holiday_date ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *MySQL) IsHoliday(ctx context.Context, date time.Time) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.Holiday{}).Where("holiday_date = ?", shanghaiDay(date)).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
