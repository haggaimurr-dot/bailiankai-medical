package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func TestScheduleManagementSettingsAndAffectedPatient(t *testing.T) {
	f := newBookingFixture(t)
	ctx := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "manager", "", []string{f.slot.ClinicID}), "schedule-manager")
	denied := store.WithEmployeeScope(context.Background(), "manager", "", []string{"other-clinic"})
	setting, err := f.repo.GetScheduleSetting(ctx, f.slot.ClinicID)
	if err != nil || setting.Version != 0 {
		t.Fatalf("initial setting: %+v %v", setting, err)
	}
	nextDay := f.now.AddDate(0, 0, 20).In(database.Shanghai).Format("2006-01-02")
	setting.Templates = []store.ScheduleTemplate{{ID: "morning", Name: "上午班", DoctorID: f.slot.DoctorID, Weekdays: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "12:00", Duration: 30, Capacity: 3, Open: true}}
	setting.Holidays = []store.HolidayRule{{Date: nextDay, Name: "院区休诊", Kind: "rest"}}
	saved, err := f.repo.SaveScheduleSetting(ctx, setting, f.now)
	if err != nil || saved.Version != 1 {
		t.Fatalf("save setting: %+v %v", saved, err)
	}
	if _, err = f.repo.SaveScheduleSetting(ctx, setting, f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale setting: %v", err)
	}
	if _, err = f.repo.GetScheduleSetting(denied, f.slot.ClinicID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("scope: %v", err)
	}
	start, _ := time.ParseInLocation("2006-01-02 15:04", nextDay+" 09:00", database.Shanghai)
	target := model.Slot{ClinicID: f.slot.ClinicID, DoctorID: f.slot.DoctorID, StartsAt: start, EndsAt: start.Add(30 * time.Minute), Capacity: 3, Status: "open"}
	if _, err = f.repo.CreateSlots(ctx, []model.Slot{target}, f.now); !errors.Is(err, store.ErrScheduleInput) {
		t.Fatalf("rest day: %v", err)
	}
	target.StartsAt = start.AddDate(0, 0, 1)
	target.EndsAt = target.StartsAt.Add(30 * time.Minute)
	created, err := f.repo.CreateSlots(ctx, []model.Slot{target}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	appointment := f.create(f.patient(), "schedule-affected-booking")
	original := f.savedSlot()
	if _, err = f.repo.SuspendSlots(ctx, []store.SlotReference{{ID: original.ID, Version: original.Version}}, "临时停诊", f.now); err != nil {
		t.Fatal(err)
	}
	tasks, err := f.repo.SchedulePatientTasks(ctx, f.slot.ClinicID, f.now)
	if err != nil || len(tasks) != 1 || tasks[0].AppointmentID != appointment.ID {
		t.Fatalf("affected tasks: %+v %v", tasks, err)
	}
	task := tasks[0]
	if err = f.repo.UpdateScheduleContact(denied, task.ID, store.ScheduleContactInput{Version: task.Version, ContactStatus: "contacted", Note: "确认新时间"}, f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("contact scope: %v", err)
	}
	if err = f.repo.UpdateScheduleContact(ctx, task.ID, store.ScheduleContactInput{Version: task.Version, ContactStatus: "contacted", Note: "确认新时间"}, f.now); err != nil {
		t.Fatal(err)
	}
	if err = f.repo.UpdateScheduleContact(ctx, task.ID, store.ScheduleContactInput{Version: task.Version, ContactStatus: "todo"}, f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("contact conflict: %v", err)
	}
	input := store.RescheduleInput{SlotID: created[0].ID, Version: appointment.Version}
	stale := input
	stale.Version++
	if _, err = f.repo.RescheduleAffectedPatient(ctx, task.ID, "schedule-stale-key", stale, f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale appointment: %v", err)
	}
	f.db.Model(&model.Slot{}).Where("id = ?", input.SlotID).Update("capacity", 0)
	if _, err = f.repo.RescheduleAffectedPatient(ctx, task.ID, "schedule-full-key", input, f.now); !errors.Is(err, store.ErrSlotFull) {
		t.Fatalf("full target: %v", err)
	}
	f.assertReserved(1)
	f.db.Model(&model.Slot{}).Where("id = ?", input.SlotID).Updates(map[string]any{"capacity": 3, "status": "closed"})
	if _, err = f.repo.RescheduleAffectedPatient(ctx, task.ID, "schedule-closed-key", input, f.now); !errors.Is(err, store.ErrSlotClosed) {
		t.Fatalf("closed target: %v", err)
	}
	f.assertReserved(1)
	f.db.Model(&model.Slot{}).Where("id = ?", input.SlotID).Update("status", "open")
	if _, err = f.repo.RescheduleAffectedPatient(denied, task.ID, "schedule-reschedule-key", input, f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("move scope: %v", err)
	}
	moved, err := f.repo.RescheduleAffectedPatient(ctx, task.ID, "schedule-reschedule-key", input, f.now)
	if err != nil || moved.SlotID != created[0].ID || moved.Status != "pending" {
		t.Fatalf("move: %+v %v", moved, err)
	}
	f.assertReserved(0)
	replay, err := f.repo.RescheduleAffectedPatient(ctx, task.ID, "schedule-reschedule-key", input, f.now)
	if err != nil || replay.Version != moved.Version {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	var slot model.Slot
	f.db.First(&slot, "id = ?", moved.SlotID)
	if slot.Reserved != 1 {
		t.Fatalf("target reservations=%d", slot.Reserved)
	}
	tasks, err = f.repo.SchedulePatientTasks(ctx, f.slot.ClinicID, f.now)
	if err != nil || tasks[0].CurrentSlotID == tasks[0].SlotID || tasks[0].Note != "确认新时间" {
		t.Fatalf("resolved task: %+v %v", tasks, err)
	}
	var event model.AppointmentEvent
	f.db.Where("appointment_id = ? AND action = ?", appointment.ID, "reschedule").First(&event)
	if event.ActorType != "operator" || event.ActorID != "schedule-manager" {
		t.Fatalf("audit: %+v", event)
	}
}

func TestScheduleSingleClosureCreatesPatientTask(t *testing.T) {
	f := newBookingFixture(t)
	ctx := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "manager", "", []string{f.slot.ClinicID}), "schedule-manager")
	appointment := f.create(f.patient(), "single-closure-patient")
	slot := f.savedSlot()
	if _, err := f.repo.UpdateSlot(ctx, slot.ID, store.SlotUpdate{Capacity: slot.Capacity, Status: "closed", Version: slot.Version}, f.now); err != nil {
		t.Fatal(err)
	}
	rows, err := f.repo.SchedulePatientTasks(ctx, f.slot.ClinicID, f.now)
	if err != nil || len(rows) != 1 || rows[0].AppointmentID != appointment.ID || rows[0].Reason != "排班关闭" {
		t.Fatalf("single closure tasks: %+v %v", rows, err)
	}
	taskID := rows[0].ID
	slot = f.savedSlot()
	if _, err = f.repo.UpdateSlot(ctx, slot.ID, store.SlotUpdate{Capacity: slot.Capacity, Status: "open", Version: slot.Version}, f.now); err != nil {
		t.Fatal(err)
	}
	rows, err = f.repo.SchedulePatientTasks(ctx, f.slot.ClinicID, f.now)
	if err != nil || rows[0].SlotStatus != "open" {
		t.Fatalf("reopened task: %+v %v", rows, err)
	}
	slot = f.savedSlot()
	if _, err = f.repo.SuspendSlots(ctx, []store.SlotReference{{ID: slot.ID, Version: slot.Version}}, "再次停诊", f.now); err != nil {
		t.Fatal(err)
	}
	rows, err = f.repo.SchedulePatientTasks(ctx, f.slot.ClinicID, f.now)
	if err != nil || len(rows) != 1 || rows[0].ID != taskID || rows[0].Version != 2 || rows[0].Reason != "再次停诊" || rows[0].SlotStatus != "closed" {
		t.Fatalf("repeat closure: %+v %v", rows, err)
	}
	f.assertReserved(1)
}
