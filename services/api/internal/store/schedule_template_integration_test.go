package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func TestScheduleTemplateApply(t *testing.T) {
	f := newBookingFixture(t)
	ctx := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "manager", "", []string{f.slot.ClinicID}), "template-manager")

	// 用 7–13 天后的周一，避免与种子排班及“过去”冲突分类撞车。
	now := f.now.In(database.Shanghai)
	daysAhead := ((8 - int(now.Weekday())) % 7 + 7) % 7
	if daysAhead < 7 {
		daysAhead += 7
	}
	monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, database.Shanghai).AddDate(0, 0, daysAhead)
	mondayStr := monday.Format("2006-01-02")
	weekEndStr := monday.AddDate(0, 0, 6).Format("2006-01-02")

	pattern := store.TemplatePattern{Entries: []store.TemplateEntry{
		{DoctorID: f.slot.DoctorID, Weekday: 1, StartTimes: []string{"09:00"}, DurationMinutes: 30, Capacity: 3},
	}}
	patternJSON, err := json.Marshal(pattern)
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := f.repo.CreateScheduleTemplate(ctx, model.ScheduleTemplate{ClinicID: f.slot.ClinicID, Name: "测试模板", PatternJSON: string(patternJSON)})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	if tpl.ID == "" || tpl.CreatedBy != "template-manager" {
		t.Fatalf("template meta: %+v", tpl)
	}
	t.Cleanup(func() {
		f.db.Exec("DELETE FROM slots WHERE doctor_id = ? AND starts_at >= ?", f.slot.DoctorID, monday)
		f.db.Exec("DELETE FROM schedule_templates WHERE id = ?", tpl.ID)
		f.db.Exec("DELETE FROM holidays WHERE holiday_date >= ?", mondayStr)
	})

	// 1. 节假日当天：预检全冲突（holiday），确认零新建。
	if err := f.repo.SetHoliday(ctx, monday, "测试节假日"); err != nil {
		t.Fatalf("set holiday: %v", err)
	}
	toCreate, conflicts, err := f.repo.PreviewTemplateApply(ctx, f.slot.ClinicID, tpl.ID, mondayStr, weekEndStr, f.now)
	if err != nil {
		t.Fatalf("preview with holiday: %v", err)
	}
	if len(toCreate) != 0 || len(conflicts) != 1 || conflicts[0].Reason != store.SlotConflictHoliday || conflicts[0].DoctorID != f.slot.DoctorID {
		t.Fatalf("holiday preview: toCreate=%d conflicts=%+v", len(toCreate), conflicts)
	}
	created, skipped, err := f.repo.ApplyScheduleTemplate(ctx, f.slot.ClinicID, tpl.ID, mondayStr, weekEndStr, f.now)
	if err != nil {
		t.Fatalf("apply with holiday: %v", err)
	}
	if len(created) != 0 || len(skipped) != 1 || skipped[0].Reason != store.SlotConflictHoliday {
		t.Fatalf("holiday apply: created=%d skipped=%+v", len(created), skipped)
	}

	// 2. 撤掉节假日：预检可建 1 个，确认生成，容量/医生正确。
	if err := f.repo.RemoveHoliday(ctx, monday); err != nil {
		t.Fatalf("remove holiday: %v", err)
	}
	toCreate, conflicts, err = f.repo.PreviewTemplateApply(ctx, f.slot.ClinicID, tpl.ID, mondayStr, weekEndStr, f.now)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(toCreate) != 1 || len(conflicts) != 0 || toCreate[0].Capacity != 3 || toCreate[0].DoctorID != f.slot.DoctorID {
		t.Fatalf("preview: toCreate=%+v conflicts=%+v", toCreate, conflicts)
	}
	created, skipped, err = f.repo.ApplyScheduleTemplate(ctx, f.slot.ClinicID, tpl.ID, mondayStr, weekEndStr, f.now)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(created) != 1 || len(skipped) != 0 {
		t.Fatalf("apply: created=%d skipped=%+v", len(created), skipped)
	}
	if created[0].Capacity != 3 || created[0].DoctorID != f.slot.DoctorID || created[0].Status != "open" || created[0].Reserved != 0 {
		t.Fatalf("created slot=%+v", created[0])
	}

	// 3. 二次套用：零新建，全为 overlap。
	created, skipped, err = f.repo.ApplyScheduleTemplate(ctx, f.slot.ClinicID, tpl.ID, mondayStr, weekEndStr, f.now)
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if len(created) != 0 || len(skipped) != 1 || skipped[0].Reason != store.SlotConflictOverlap {
		t.Fatalf("re-apply: created=%d skipped=%+v", len(created), skipped)
	}

	// 4. 节假日表同样约束单条手工建号：节假日当天建开放时段应被拒绝。
	nextMonday := monday.AddDate(0, 0, 7)
	if err := f.repo.SetHoliday(ctx, nextMonday, "测试节假日2"); err != nil {
		t.Fatalf("set holiday 2: %v", err)
	}
	single := model.Slot{ClinicID: f.slot.ClinicID, DoctorID: f.slot.DoctorID,
		StartsAt: time.Date(nextMonday.Year(), nextMonday.Month(), nextMonday.Day(), 9, 0, 0, 0, database.Shanghai),
		EndsAt:   time.Date(nextMonday.Year(), nextMonday.Month(), nextMonday.Day(), 9, 30, 0, 0, database.Shanghai),
		Capacity: 3, Status: "open"}
	if _, err := f.repo.CreateSlots(ctx, []model.Slot{single}, f.now); !errors.Is(err, store.ErrScheduleInput) {
		t.Fatalf("holiday single create: err=%v", err)
	}
	if err := f.repo.RemoveHoliday(ctx, nextMonday); err != nil {
		t.Fatalf("remove holiday 2: %v", err)
	}

	// 5. 非法 pattern 与非法套用参数整体拒绝。
	for _, bad := range []string{
		`{"entries":[]}`,
		`{"entries":[{"doctorId":"d","weekday":8,"startTimes":["09:00"],"capacity":3}]}`,
		`{"entries":[{"doctorId":"d","weekday":1,"startTimes":["25:00"],"capacity":3}]}`,
		`{"entries":[{"doctorId":"d","weekday":1,"startTimes":["09:00"],"capacity":0}]}`,
		`not-json`,
	} {
		if _, err := store.ParseTemplatePattern(bad); !errors.Is(err, store.ErrScheduleInput) {
			t.Fatalf("bad pattern %q: err=%v", bad, err)
		}
	}
	if _, _, err := f.repo.PreviewTemplateApply(ctx, f.slot.ClinicID, tpl.ID, weekEndStr, mondayStr, f.now); !errors.Is(err, store.ErrScheduleInput) {
		t.Fatalf("reversed range: err=%v", err)
	}
	if _, _, err := f.repo.PreviewTemplateApply(ctx, f.slot.ClinicID, "no-such-template", mondayStr, weekEndStr, f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing template: err=%v", err)
	}

	// 6. 模板 CRUD。
	updated, err := f.repo.UpdateScheduleTemplate(ctx, tpl.ID, "改名模板", string(patternJSON))
	if err != nil || updated.Name != "改名模板" {
		t.Fatalf("update: %v %+v", err, updated)
	}
	got, err := f.repo.GetScheduleTemplate(ctx, tpl.ID)
	if err != nil || got.Name != "改名模板" {
		t.Fatalf("get: %v %+v", err, got)
	}
	listed, err := f.repo.ListScheduleTemplates(ctx, f.slot.ClinicID)
	if err != nil || len(listed) == 0 {
		t.Fatalf("list: %v %d", err, len(listed))
	}
	if err := f.repo.DeleteScheduleTemplate(ctx, tpl.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := f.repo.GetScheduleTemplate(ctx, tpl.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get after delete: err=%v", err)
	}
}
