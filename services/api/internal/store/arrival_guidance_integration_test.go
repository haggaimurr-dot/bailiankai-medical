package store_test

import (
	"context"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func TestArrivalGuidancePush(t *testing.T) {
	f := newBookingFixture(t)
	ctx := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "manager", "", []string{f.slot.ClinicID}), "guidance-manager")

	// 配置到诊指引文案。
	content := "欢迎到院！请先到前台签到，等待叫号后进入诊室。"
	setting, err := f.repo.SaveArrivalGuidance(ctx, f.slot.ClinicID, content, true, f.now)
	if err != nil {
		t.Fatalf("save guidance: %v", err)
	}
	if setting.Content != content || !setting.Enabled {
		t.Fatalf("setting=%+v", setting)
	}
	t.Cleanup(func() {
		f.db.Exec("DELETE FROM patient_messages WHERE kind = ?", "arrival_guidance")
		f.db.Exec("DELETE FROM arrival_guidance_settings WHERE clinic_id = ?", f.slot.ClinicID)
	})

	// 造一个已确认预约并登记到诊。
	patient := f.patient()
	appointment := f.create(patient, "arrival-guidance-test")
	// 确认预约（RegisterArrival 要求 confirmed/completed）；把创建时间提前，保证到诊时间合法。
	if err := f.db.Model(&model.Appointment{}).Where("id = ?", appointment.ID).Updates(map[string]any{"status": "confirmed", "created_at": f.now.Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	saved, err := f.repo.RegisterArrival(ctx, appointment.ID, store.ArrivalInput{
		Version: 1, ArrivedAt: f.now.Add(-time.Minute),
	}, f.now)
	if err != nil {
		t.Fatalf("register arrival: %v", err)
	}
	if saved.ArrivedAt == nil {
		t.Fatal("arrived_at not set")
	}

	// 患者应即时收到指引，内容与后台配置一致。
	var messages []model.PatientMessage
	if err := f.db.Where("patient_id = ? AND kind = ?", patient.Patient.ID, "arrival_guidance").Find(&messages).Error; err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages=%d want 1", len(messages))
	}
	if messages[0].Body != content || messages[0].AppointmentID != appointment.ID {
		t.Fatalf("message=%+v", messages[0])
	}

	// 重复登记到诊不重复推送。
	if _, err := f.repo.RegisterArrival(ctx, appointment.ID, store.ArrivalInput{Version: 2, ArrivedAt: f.now.Add(-time.Minute)}, f.now); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	var count int64
	f.db.Model(&model.PatientMessage{}).Where("patient_id = ? AND kind = ?", patient.Patient.ID, "arrival_guidance").Count(&count)
	if count != 1 {
		t.Fatalf("duplicate push: count=%d", count)
	}

	// 未配置的院区不推送（静默跳过，不报错）。
	otherClinic := "clinic-no-guidance"
	f.db.Exec("DELETE FROM arrival_guidance_settings WHERE clinic_id = ?", otherClinic)
	// 用同一患者在新院区造预约（简化：直接验证 Get 返回 not found 时不推送）。
	if _, err := f.repo.GetArrivalGuidance(ctx, otherClinic); err == nil {
		t.Fatal("expected not found for unconfigured clinic")
	}

	// 禁用后不推送。
	if _, err := f.repo.SaveArrivalGuidance(ctx, f.slot.ClinicID, content, false, f.now); err != nil {
		t.Fatalf("disable: %v", err)
	}
	patient2 := f.patient()
	appointment2 := f.create(patient2, "arrival-guidance-test-2")
	if err := f.db.Model(&model.Appointment{}).Where("id = ?", appointment2.ID).Updates(map[string]any{"status": "confirmed", "created_at": f.now.Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.RegisterArrival(ctx, appointment2.ID, store.ArrivalInput{Version: 1, ArrivedAt: f.now.Add(-time.Minute)}, f.now); err != nil {
		t.Fatalf("register arrival 2: %v", err)
	}
	f.db.Model(&model.PatientMessage{}).Where("patient_id = ? AND kind = ?", patient2.Patient.ID, "arrival_guidance").Count(&count)
	if count != 0 {
		t.Fatalf("disabled guidance still pushed: count=%d", count)
	}

	// 非主管不能保存配置。
	staffCtx := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "customer_service", "", []string{f.slot.ClinicID}), "staff")
	if _, err := f.repo.SaveArrivalGuidance(staffCtx, f.slot.ClinicID, content, true, f.now); err == nil {
		t.Fatal("expected forbidden for non-manager")
	}
}
