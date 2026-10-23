package store_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/bailiankai/api/internal/httpapi"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func TestDirectoryPublicationDisableAndHistory(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	manager := store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"})
	patient := f.patient()
	appointment := f.create(patient, "directory-history-booking")
	var doctor model.Doctor
	if err := f.db.First(&doctor, "id = ?", f.slot.DoctorID).Error; err != nil {
		t.Fatal(err)
	}
	original := doctor.Name
	doctor.Name = "Renamed doctor"
	doctor.AvatarURL = "https://example.com/avatar.jpg"
	doctor.ServiceIntroduction = "Dedicated consultation service"
	save := func(action string, version int, active bool) *model.Doctor {
		t.Helper()
		row, err := f.repo.SaveDirectory(manager, "doctors", doctor.ID, version, action, model.Clinic{}, doctor, active, f.now)
		if err != nil {
			t.Fatal(err)
		}
		return row.(*model.Doctor)
	}
	doctor = *save("updated", doctor.Version, true)
	public, err := f.repo.Doctors(ctx, "clinic-demo")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range public {
		if row.ID == doctor.ID && row.Name != original {
			t.Fatal("draft leaked to public")
		}
	}
	if _, err := f.repo.SaveDirectory(manager, "doctors", doctor.ID, 1, "published", model.Clinic{}, doctor, true, f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale version: %v", err)
	}
	doctor = *save("published", doctor.Version, true)
	public, err = f.repo.Doctors(ctx, "clinic-demo")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range public {
		if row.ID == doctor.ID {
			found = true
			if row.Name != doctor.Name || row.AvatarURL != doctor.AvatarURL || row.ServiceIntroduction != doctor.ServiceIntroduction {
				t.Fatal("published profile mismatch")
			}
		}
	}
	if !found {
		t.Fatal("published doctor missing")
	}
	historical, err := f.repo.GetAppointment(ctx, appointment.ID, patient.Patient.ID, f.now)
	if err != nil || historical.DoctorName != original {
		t.Fatalf("historical identity overwritten: %+v %v", historical, err)
	}
	doctor = *save("status", doctor.Version, false)
	another := f.patient()
	if _, _, err := f.repo.CreateAppointment(ctx, another.Patient.ID, "disabled-doctor-booking", f.input(), f.now); !errors.Is(err, store.ErrSlotClosed) {
		t.Fatalf("disabled doctor booking: %v", err)
	}
	slots, err := f.repo.Slots(ctx, store.SlotFilter{DoctorID: doctor.ID, Now: f.now})
	if err != nil || len(slots) != 0 {
		t.Fatalf("disabled slots exposed: %v %v", slots, err)
	}
	history, err := f.repo.DirectoryHistory(manager, "doctors", doctor.ID)
	if err != nil || len(history) != 4 {
		t.Fatalf("history: %v %v", history, err)
	}
	if !strings.Contains(history[1].ContentJSON, doctor.ServiceIntroduction) {
		t.Fatal("missing published snapshot")
	}
	foreign := store.WithEmployeeScope(ctx, "manager", "", []string{"other-clinic"})
	if _, err := f.repo.DirectoryHistory(foreign, "doctors", doctor.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-clinic history: %v", err)
	}
	if _, err := f.repo.GetAppointment(ctx, appointment.ID, patient.Patient.ID, f.now); err != nil {
		t.Fatal("disabled profile broke old appointment", err)
	}
	doctor = *save("status", doctor.Version, true)
	var clinic model.Clinic
	if err := f.db.First(&clinic, "id = ?", "clinic-demo").Error; err != nil {
		t.Fatal(err)
	}
	row, err := f.repo.SaveDirectory(manager, "clinics", clinic.ID, clinic.Version, "status", clinic, model.Doctor{}, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	public, err = f.repo.Doctors(ctx, clinic.ID)
	if err != nil || len(public) != 0 {
		t.Fatalf("disabled clinic exposes doctors: %v %v", public, err)
	}
	if _, _, err := f.repo.CreateAppointment(ctx, another.Patient.ID, "disabled-clinic-booking", f.input(), f.now); !errors.Is(err, store.ErrSlotClosed) {
		t.Fatalf("disabled clinic booking: %v", err)
	}
	clinic = *row.(*model.Clinic)
	if _, err := f.repo.SaveDirectory(manager, "clinics", clinic.ID, clinic.Version, "status", clinic, model.Doctor{}, true, f.now); err != nil {
		t.Fatal(err)
	}
}

func TestDirectoryCreationAndEmployeeLink(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	manager, err := f.repo.CreateEmployee(ctx, "directory-manager", "Manager", "directory-password-123", "manager", "", f.now)
	if err != nil {
		t.Fatal(err)
	}
	managed := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", manager.ClinicIDs), manager.ID)
	row, err := f.repo.SaveDirectory(managed, "clinics", "", 0, "created", model.Clinic{Name: "New clinic", Address: "Address", Phone: "123"}, model.Doctor{}, true, f.now)
	if err != nil {
		t.Fatal(err)
	}
	clinic := *row.(*model.Clinic)
	var refreshed model.EmployeeAccount
	if err := f.db.First(&refreshed, "id = ?", manager.ID).Error; err != nil {
		t.Fatal(err)
	}
	managed = store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", refreshed.ClinicIDs), manager.ID)
	if _, err := f.repo.SaveDirectory(managed, "clinics", clinic.ID, clinic.Version, "published", clinic, model.Doctor{}, true, f.now); err != nil {
		t.Fatal(err)
	}
	row, err = f.repo.SaveDirectory(managed, "doctors", "", 0, "created", model.Clinic{}, model.Doctor{ClinicID: clinic.ID, Name: "New doctor", Title: "Doctor", Summary: "Profile", Focus: []string{}, Initials: "D"}, true, f.now)
	if err != nil {
		t.Fatal(err)
	}
	doctor := *row.(*model.Doctor)
	account, err := f.repo.CreateEmployee(store.WithClinicAssignment(managed, []string{clinic.ID}), "directory-doctor", "Doctor", "directory-password-123", "doctor", doctor.ID, f.now)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := f.repo.LoginEmployee(ctx, account.Username, "directory-password-123", f.now)
	if err != nil {
		t.Fatal(err)
	}
	linked, err := f.repo.LinkDoctorEmployee(managed, doctor.ID, account.ID, doctor.Version, account.Version, f.now)
	if err != nil || linked.StaffID == nil || *linked.StaffID != doctor.ID {
		t.Fatalf("link: %+v %v", linked, err)
	}
	if _, err := f.repo.AuthenticateEmployee(ctx, token, f.now); !errors.Is(err, store.ErrInvalidCredentials) {
		t.Fatalf("old session not revoked: %v", err)
	}
	if _, err := f.repo.LinkDoctorEmployee(managed, doctor.ID, account.ID, doctor.Version, linked.Version, f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale link: %v", err)
	}
	// Manager-only HTTP routes and strict body checks use the real repository.
	_, managerToken, err := f.repo.LoginEmployee(ctx, manager.Username, "directory-password-123", f.now)
	if err != nil {
		t.Fatal(err)
	}
	router := httpapi.New(f.repo, "")
	for _, body := range []string{`{"version":1,"active":null}`, `{"version":1,"active":false,"extra":1}`, `{"version":0,"active":false}`} {
		request := httptest.NewRequest("POST", "/api/v1/admin/clinics/"+clinic.ID+"/status", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+managerToken)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 400 {
			t.Fatalf("invalid body %s: %d %s", body, response.Code, response.Body.String())
		}
	}
	_, doctorToken, err := f.repo.LoginEmployee(ctx, account.Username, "directory-password-123", f.now)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/v1/admin/doctors", nil)
	request.Header.Set("Authorization", "Bearer "+doctorToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatalf("doctor manages directory: %d", response.Code)
	}
}
