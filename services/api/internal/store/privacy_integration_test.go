package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"testing"
	"time"
)

func TestPrivacyConsentHistoryAndClinicIsolation(t *testing.T) {
	fixture := newBookingFixture(t)
	ctx := context.Background()
	patient := fixture.patient()
	before, err := fixture.repo.PrivacyConsentEvents(ctx, patient.Patient.ID)
	if err != nil || len(before) != 1 {
		t.Fatalf("initial consent: %v %v", before, err)
	}
	replay, err := fixture.repo.RecordPrivacyConsent(ctx, patient.Patient.ID, store.PrivacyPolicyVersion, "accepted", fixture.now)
	if err != nil || replay.ID != before[0].ID {
		t.Fatalf("consent retry duplicated: %v %v", replay, err)
	}
	if _, err := fixture.repo.RecordPrivacyConsent(ctx, patient.Patient.ID, "old", "accepted", fixture.now); !errors.Is(err, store.ErrPrivacyVersion) {
		t.Fatalf("old version: %v", err)
	}
	revoked, err := fixture.repo.RecordPrivacyConsent(ctx, patient.Patient.ID, store.PrivacyPolicyVersion, "revoked", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if revoked.PolicyContent != store.PrivacyPolicyContent || !revoked.RecordedAt.After(before[0].RecordedAt) {
		t.Fatalf("missing snapshot or chronology: %+v", revoked)
	}
	events, err := fixture.repo.PrivacyConsentEvents(ctx, patient.Patient.ID)
	if err != nil || len(events) != 2 || events[0].Action != "revoked" {
		t.Fatalf("history: %v %v", events, err)
	}
	local := fixture.create(patient, "privacy-local-appointment")
	otherClinic := model.Clinic{ID: "clinic-other", Name: "其他演示院区"}
	otherClinic.DirectoryState = model.DirectoryState{Active: true, Version: 1, PublishedAt: &fixture.now}
	content, _ := json.Marshal(otherClinic)
	snapshot := string(content)
	otherClinic.PublishedJSON = &snapshot
	if err := fixture.db.Create(&otherClinic).Error; err != nil {
		t.Fatal(err)
	}
	otherDoctor := model.Doctor{ID: "doctor-other", ClinicID: otherClinic.ID, Name: "其他院区演示医生", Focus: []string{}}
	otherDoctor.DirectoryState = model.DirectoryState{Active: true, Version: 1, PublishedAt: &fixture.now}
	content, _ = json.Marshal(otherDoctor)
	doctorSnapshot := string(content)
	otherDoctor.PublishedJSON = &doctorSnapshot
	if err := fixture.db.Create(&otherDoctor).Error; err != nil {
		t.Fatal(err)
	}
	fixture.slot = model.Slot{ID: "slot-other", ClinicID: otherClinic.ID, DoctorID: otherDoctor.ID, StartsAt: fixture.now.Add(48 * time.Hour), EndsAt: fixture.now.Add(49 * time.Hour), Capacity: 2, Status: "open", Version: 1}
	if err := fixture.db.Create(&fixture.slot).Error; err != nil {
		t.Fatal(err)
	}
	remote := fixture.create(patient, "privacy-remote-appointment")
	manager := store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"})
	if allowed, err := fixture.repo.CanAccessAppointment(manager, remote.ID, false); err != nil || allowed {
		t.Fatalf("cross-clinic access: %v %v", allowed, err)
	}
	if allowed, err := fixture.repo.CanAccessAppointment(manager, local.ID, false); err != nil || !allowed {
		t.Fatalf("local access: %v %v", allowed, err)
	}
	page, err := fixture.repo.ListAppointments(manager, store.AppointmentFilter{Page: 1, PageSize: 20}, fixture.now)
	if err != nil || page.Total != 1 || len(page.Data) != 1 || page.Data[0].ID != local.ID {
		t.Fatalf("list leaks another clinic: %+v %v", page, err)
	}
	if _, err := fixture.repo.ReviewAppointment(manager, remote.ID, store.ReviewInput{Action: "confirm", Version: remote.Version}, fixture.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-clinic review: %v", err)
	}
	if _, err := fixture.repo.UpdateSlot(manager, fixture.slot.ID, store.SlotUpdate{Capacity: 3, Status: "open", Version: 1}, fixture.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-clinic slot write: %v", err)
	}
	overview, err := fixture.repo.Overview(manager)
	if err != nil || overview.AppointmentCount != 1 {
		t.Fatalf("overview leaks another clinic: %+v %v", overview, err)
	}
	unassigned := store.WithEmployeeScope(ctx, "manager", "")
	if allowed, err := fixture.repo.CanAccessAppointment(unassigned, local.ID, false); err != nil || allowed {
		t.Fatalf("unassigned manager allowed: %v %v", allowed, err)
	}
}
func TestEmployeeClinicAssignmentAndSessionRevocation(t *testing.T) {
	fixture := newBookingFixture(t)
	ctx := context.Background()
	password := "PrivacyRoleTest123!"
	roles := []string{"doctor", "nurse", "customer_service", "operations"}
	staffIDs := []string{"doctor-demo-1", "nurse-demo-1", "", ""}
	for index, role := range roles {
		account, err := fixture.repo.CreateEmployee(store.WithClinicAssignment(ctx, []string{"clinic-demo"}), "privacy-"+role, "演示权限账号", password, role, staffIDs[index], fixture.now)
		if err != nil {
			t.Fatalf("create %s: %v", role, err)
		}
		_, token, err := fixture.repo.LoginEmployee(ctx, account.Username, password, fixture.now)
		if err != nil {
			t.Fatal(err)
		}
		recovered, err := fixture.repo.AuthenticateEmployee(ctx, token, fixture.now)
		if err != nil || len(recovered.ClinicIDs) != 1 || recovered.ClinicIDs[0] != "clinic-demo" {
			t.Fatalf("live clinic grants: %+v %v", recovered, err)
		}
		if role == "customer_service" || role == "operations" {
			for _, path := range []string{"/api/v1/admin/patients/patient-other", "/api/v1/admin/photos/photo-other/content", "/api/v1/admin/followups"} {
				response := fixture.http("GET", path, "", token, "")
				if response.Code != 403 {
					t.Fatalf("%s accessed %s: %d %s", role, path, response.Code, response.Body)
				}
			}
		}
		if role == "operations" {
			response := fixture.http("GET", "/api/v1/admin/overview", "", token, "")
			if response.Code != 200 {
				t.Fatalf("overview: %d %s", response.Code, response.Body)
			}
		}
		updated, err := fixture.repo.SetEmployeeClinics(ctx, account.ID, account.Version, []string{"clinic-demo"}, fixture.now.Add(time.Second))
		if err != nil || updated.Version != 2 {
			t.Fatalf("clinic update: %+v %v", updated, err)
		}
		if _, err := fixture.repo.AuthenticateEmployee(ctx, token, fixture.now.Add(2*time.Second)); !errors.Is(err, store.ErrInvalidCredentials) {
			t.Fatalf("clinic change kept old session: %v", err)
		}
		history, err := fixture.repo.EmployeeEvents(ctx, account.ID)
		if err != nil || len(history) != 2 || history[0].Action != "clinics-updated" {
			t.Fatalf("clinic audit: %+v %v", history, err)
		}
	}
	if _, err := fixture.repo.CreateEmployee(store.WithClinicAssignment(ctx, []string{"clinic-demo"}), "privacy-invalid", "错误关联", password, "doctor", "nurse-demo-1", fixture.now); !errors.Is(err, store.ErrCareState) {
		t.Fatalf("doctor linked to nurse: %v", err)
	}
}

func TestPrivacyPolicyUpdateNotifiesPatient(t *testing.T) {
	fixture := newBookingFixture(t)
	ctx := context.Background()
	// 无同意记录的患者：打开收件箱即收到当前版变更通知，且只发一条
	fresh, err := fixture.repo.NewPatientSession(ctx, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	rows, total, err := fixture.repo.PatientMessages(ctx, fresh.Patient.ID, 1, 20)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].Kind != "privacy_policy" {
		t.Fatalf("privacy notice missing: %+v %d %v", rows, total, err)
	}
	if rows[0].Title != "隐私政策已更新（"+store.PrivacyPolicyVersion+"）" {
		t.Fatalf("unexpected title: %q", rows[0].Title)
	}
	unread, err := fixture.repo.UnreadPatientMessageCount(ctx, fresh.Patient.ID)
	if err != nil || unread != 1 {
		t.Fatalf("unread: %d %v", unread, err)
	}
	rows, total, err = fixture.repo.PatientMessages(ctx, fresh.Patient.ID, 1, 20)
	if err != nil || total != 1 {
		t.Fatalf("duplicate notice: %d %v", total, err)
	}
	// 已就当前版表态的患者不再收到
	consented := fixture.patient()
	rows, total, err = fixture.repo.PatientMessages(ctx, consented.Patient.ID, 1, 20)
	if err != nil || total != 0 {
		t.Fatalf("consented patient should get none: %d %v", total, err)
	}
	// 发布新版：已表态的患者也收到新版通知，且只发一条
	previous := store.PrivacyPolicyVersion
	store.PrivacyPolicyVersion = "2099-01-01.v9"
	t.Cleanup(func() { store.PrivacyPolicyVersion = previous })
	rows, total, err = fixture.repo.PatientMessages(ctx, consented.Patient.ID, 1, 20)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].Kind != "privacy_policy" {
		t.Fatalf("new version notice missing: %+v %d %v", rows, total, err)
	}
	rows, total, err = fixture.repo.PatientMessages(ctx, consented.Patient.ID, 1, 20)
	if err != nil || total != 1 {
		t.Fatalf("new version duplicate: %d %v", total, err)
	}
}
