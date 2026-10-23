package store_test

import (
	"context"
	"errors"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"testing"
	"time"
)

func TestFollowupToolsMySQL(t *testing.T) {
	f := newBookingFixture(t)
	manager := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "manager", "", []string{"clinic-demo"}), "tools-manager")
	foreign := store.WithEmployeeScope(context.Background(), "manager", "", []string{"other-clinic"})
	template, err := f.repo.SaveFollowupTemplate(manager, model.FollowupTemplate{ClinicID: "clinic-demo", Name: "测试模板", Offsets: []int{1, 7}, Response: "请核对实际情况", StandardStatus: "formal", ApplicableTreatment: "测试范围", NursingInstructions: "测试护理要求", FollowupChecklist: "测试检查要点", Disposition: "测试上报流程", Active: true}, f.now)
	if err != nil || template.Version != 1 {
		t.Fatal(template, err)
	}
	rows, err := f.repo.FollowupTemplates(foreign)
	if err != nil || len(rows) != 0 {
		t.Fatal("template clinic leak", rows, err)
	}
	scopedRows, err := f.repo.FollowupTemplates(manager)
	if err != nil || len(scopedRows) != 1 || scopedRows[0].StandardStatus != "formal" || scopedRows[0].NursingInstructions != "测试护理要求" || scopedRows[0].FollowupChecklist != "测试检查要点" {
		t.Fatal("standard fields not persisted", scopedRows, err)
	}
	old := template
	template.Active = false
	if _, err := f.repo.SaveFollowupTemplate(manager, template, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.SaveFollowupTemplate(manager, old, f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatal("stale template accepted", err)
	}
	var events int64
	f.db.Model(&model.FollowupTemplateEvent{}).Where("template_id = ?", template.ID).Count(&events)
	if events != 2 {
		t.Fatal("missing template history", events)
	}
	patient := f.patient()
	appointment := f.create(patient, "nursing-booking-request-001")
	input := model.NursingFeedback{AppointmentID: appointment.ID, RequestKey: "nursing-request-001", ObservedAt: f.now.Add(-time.Hour), Content: "护理观察", Action: "实际措施", ActorName: "测试主管"}
	saved, err := f.repo.RecordNursingFeedback(manager, input, f.now)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.repo.RecordNursingFeedback(manager, input, f.now)
	if err != nil || replay.ID != saved.ID {
		t.Fatal("retry duplicated feedback", replay, err)
	}
	input.Content = "不同内容"
	if _, err := f.repo.RecordNursingFeedback(manager, input, f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatal("changed retry accepted", err)
	}
	if _, err := f.repo.NursingFeedbacks(foreign, appointment.ID, store.DateRange{}); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("feedback clinic leak", err)
	}
	rowsFeedback, err := f.repo.NursingFeedbacks(manager, appointment.ID, store.DateRange{From: f.now, Until: f.now.Add(time.Hour)})
	if err != nil || len(rowsFeedback) != 0 {
		t.Fatal("date filter ignored", rowsFeedback, err)
	}
	rowsFeedback, err = f.repo.NursingFeedbacks(manager, appointment.ID, store.DateRange{})
	if err != nil || len(rowsFeedback) != 1 || rowsFeedback[0].ActorID != "tools-manager" {
		t.Fatal(rowsFeedback, err)
	}
}
