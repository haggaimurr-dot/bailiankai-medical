package store_test

import (
	"context"
	"strings"
	"testing"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

// 新增-P1：停诊自动触发改期推荐消息。停诊已有预约的时段后，每位待处理
// 预约（待确认、已确认、候补中）的患者都收到 suspension_reschedule 消息，
// 正文含停诊原因与同医生最近 3 个有空位的未来时段；无可推荐时段时明说暂无。
func TestSuspensionSendsRescheduleRecommendations(t *testing.T) {
	f := newBookingFixture(t)
	ctx := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "manager", "", []string{f.slot.ClinicID}), "schedule-manager")
	if err := f.db.Exec("UPDATE slots SET capacity = 2 WHERE id = ?", f.slot.ID).Error; err != nil {
		t.Fatal(err)
	}
	a := f.patient()
	b := f.patient()
	w := f.patient()
	bookedA := f.create(a, "suspension-a")
	bookedB := f.create(b, "suspension-b")
	waitInput := store.AppointmentInput{SlotID: f.slot.ID, ContactName: "演示访客", ContactPhone: "13800000000", Waitlist: true}
	waitW, _, err := f.repo.CreateAppointment(ctx, w.Patient.ID, "suspension-w", waitInput, f.now)
	if err != nil || waitW.Status != "waiting" {
		t.Fatalf("waitlist failed: status=%s err=%v", waitW.Status, err)
	}

	// 期望的推荐时段：同医生未来有空位的开放时段（不含被停时段）的前 3 个。
	var alts []model.Slot
	if err := f.db.Where("doctor_id = ? AND clinic_id = ? AND id <> ? AND status = ? AND starts_at > ? AND reserved < capacity", f.slot.DoctorID, f.slot.ClinicID, f.slot.ID, "open", f.now).Order("starts_at, id").Limit(3).Find(&alts).Error; err != nil || len(alts) != 3 {
		t.Fatalf("alternatives setup wrong: %d err=%v", len(alts), err)
	}
	slot := f.savedSlot()
	if _, err := f.repo.SuspendSlots(ctx, []store.SlotReference{{ID: slot.ID, Version: slot.Version}}, "医生临时停诊", f.now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		patient store.SessionResult
		apptID  string
	}{ {a, bookedA.ID}, {b, bookedB.ID}, {w, waitW.ID} } {
		var msgs []model.PatientMessage
		if err := f.db.Where("patient_id = ? AND kind = ? AND appointment_id = ?", tc.patient.Patient.ID, "suspension_reschedule", tc.apptID).Find(&msgs).Error; err != nil || len(msgs) != 1 {
			t.Fatalf("patient %s should have exactly one suspension message: %d err=%v", tc.apptID, len(msgs), err)
		}
		body := msgs[0].Body
		if msgs[0].Title != "原预约时段停诊，可改期" {
			t.Fatalf("message title wrong: %q", msgs[0].Title)
		}
		for _, alt := range alts {
			want := alt.StartsAt.In(database.Shanghai).Format("2006-01-02 15:04")
			if !strings.Contains(body, want) {
				t.Fatalf("message body should recommend %s, got %q", want, body)
			}
		}
		if !strings.Contains(body, "医生临时停诊") {
			t.Fatalf("message body should carry the suspension reason, got %q", body)
		}
	}
	var tasks int64
	if err := f.db.Model(&store.SchedulePatientTask{}).Where("slot_id = ?", f.slot.ID).Count(&tasks).Error; err != nil || tasks != 3 {
		t.Fatalf("suspension tasks wrong: %d err=%v", tasks, err)
	}
}

func TestSuspensionNoAlternativeMessage(t *testing.T) {
	f := newBookingFixture(t)
	ctx := store.WithEmployeeActor(store.WithEmployeeScope(context.Background(), "manager", "", []string{f.slot.ClinicID}), "schedule-manager")
	// 关掉同医生除目标时段外的全部未来开放时段，造出无可推荐时段的场景。
	if err := f.db.Exec("UPDATE slots SET status = 'closed' WHERE doctor_id = ? AND id <> ? AND status = 'open'", f.slot.DoctorID, f.slot.ID).Error; err != nil {
		t.Fatal(err)
	}
	a := f.patient()
	f.create(a, "suspension-noalt")
	slot := f.savedSlot()
	if _, err := f.repo.SuspendSlots(ctx, []store.SlotReference{{ID: slot.ID, Version: slot.Version}}, "医生临时停诊", f.now); err != nil {
		t.Fatal(err)
	}
	var msgs []model.PatientMessage
	if err := f.db.Where("patient_id = ? AND kind = ?", a.Patient.ID, "suspension_reschedule").Find(&msgs).Error; err != nil || len(msgs) != 1 {
		t.Fatalf("should have exactly one suspension message: %d err=%v", len(msgs), err)
	}
	if !strings.Contains(msgs[0].Body, "暂无同医生可推荐时段") {
		t.Fatalf("no-alternative message should say so, got %q", msgs[0].Body)
	}
}

