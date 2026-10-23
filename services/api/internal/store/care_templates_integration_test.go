package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"testing"
)

func TestCareTemplatesPersistAndIsolateOwners(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	for _, id := range []string{"template-owner-a", "template-owner-b"} {
		row := model.EmployeeAccount{ID: id, Username: id, DisplayName: "模板测试", PasswordHash: "unused", Role: "manager", ClinicIDs: []string{f.slot.ClinicID}, Active: true, Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
		if err := f.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	content := store.CarePlanContent{Summary: "模板摘要", Stages: []store.CarePlanStage{{Title: "阶段", Body: "完整正文"}}, Instructions: "完整医嘱"}
	row, err := f.repo.CreateCareTemplate(ctx, "template-owner-a", "个人模板", content, f.now)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.New(f.db).ListCareTemplates(ctx, "template-owner-a")
	if err != nil || len(rows) != 1 || rows[0].ID != row.ID {
		t.Fatal(rows, err)
	}
	var persisted store.CarePlanContent
	if err := json.Unmarshal([]byte(rows[0].ContentJSON), &persisted); err != nil || persisted.Summary != content.Summary || persisted.Instructions != content.Instructions || len(persisted.Stages) != 1 || persisted.Stages[0] != content.Stages[0] {
		t.Fatalf("persisted template changed: %+v %v", persisted, err)
	}
	rows, err = f.repo.ListCareTemplates(ctx, "template-owner-b")
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	if err := f.repo.DeleteCareTemplate(ctx, "template-owner-b", row.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	if err := f.repo.DeleteCareTemplate(ctx, "template-owner-a", row.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = f.repo.ListCareTemplates(ctx, "template-owner-a")
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}
