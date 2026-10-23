package store_test

import (
	"context"
	"errors"
	"example.com/bailiankai/api/internal/store"
	"testing"
)

func TestEmployeePasswordAndAccessRevokeSessions(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	manager, err := f.repo.CreateEmployee(ctx, "access-manager", "Manager", "OldPassword123!", "manager", "", f.now)
	if err != nil {
		t.Fatal(err)
	}
	scoped := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", manager.ClinicIDs), manager.ID)
	_, token, err := f.repo.LoginEmployee(ctx, manager.Username, "OldPassword123!", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.repo.ChangeEmployeePassword(scoped, manager.ID, manager.Version, "wrong", "NewPassword123!", f.now); !errors.Is(err, store.ErrInvalidCredentials) {
		t.Fatalf("wrong original: %v", err)
	}
	if _, err = f.repo.AuthenticateEmployee(ctx, token, f.now); err != nil {
		t.Fatalf("failure revoked session: %v", err)
	}
	manager, err = f.repo.ChangeEmployeePassword(scoped, manager.ID, manager.Version, "OldPassword123!", "NewPassword123!", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.repo.AuthenticateEmployee(ctx, token, f.now); !errors.Is(err, store.ErrInvalidCredentials) {
		t.Fatalf("password did not revoke: %v", err)
	}
	if _, _, err = f.repo.LoginEmployee(ctx, manager.Username, "OldPassword123!", f.now); !errors.Is(err, store.ErrInvalidCredentials) {
		t.Fatal("old password still works")
	}
	if _, _, err = f.repo.LoginEmployee(ctx, manager.Username, "NewPassword123!", f.now); err != nil {
		t.Fatal(err)
	}
	if _, err = f.repo.SetEmployeeAccess(scoped, manager.ID, manager.Version, "operations", "", f.now); !errors.Is(err, store.ErrCareState) {
		t.Fatalf("self demotion: %v", err)
	}
	account, err := f.repo.CreateEmployee(store.WithClinicAssignment(scoped, manager.ClinicIDs), "access-worker", "Worker", "OldPassword123!", "operations", "", f.now)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err = f.repo.LoginEmployee(ctx, account.Username, "OldPassword123!", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.repo.ChangeEmployeePassword(scoped, account.ID, account.Version, "OldPassword123!", "NewPassword123!", f.now); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("other password: %v", err)
	}
	if _, err = f.repo.SetEmployeeAccess(scoped, account.ID, account.Version, "doctor", "nurse-demo-1", f.now); !errors.Is(err, store.ErrCareState) {
		t.Fatalf("staff mismatch: %v", err)
	}
	updated, err := f.repo.SetEmployeeAccess(scoped, account.ID, account.Version, "customer_service", "", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != account.Version+1 || updated.Role != "customer_service" {
		t.Fatalf("access result: %+v", updated)
	}
	if _, err = f.repo.AuthenticateEmployee(ctx, token, f.now); !errors.Is(err, store.ErrInvalidCredentials) {
		t.Fatalf("access did not revoke: %v", err)
	}
	if _, err = f.repo.SetEmployeeAccess(scoped, account.ID, account.Version, "operations", "", f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale version: %v", err)
	}

	linked, err := f.repo.SetEmployeeAccess(scoped, account.ID, updated.Version, "doctor", "doctor-demo-1", f.now)
	if err != nil || linked.StaffID == nil || *linked.StaffID != "doctor-demo-1" {
		t.Fatalf("valid staff link: %+v %v", linked, err)
	}
	other, err := f.repo.CreateEmployee(store.WithClinicAssignment(scoped, manager.ClinicIDs), "access-other", "Other", "OldPassword123!", "operations", "", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.repo.SetEmployeeAccess(scoped, other.ID, other.Version, "doctor", "doctor-demo-1", f.now); !errors.Is(err, store.ErrAccountExists) {
		t.Fatalf("duplicate staff: %v", err)
	}
	outside := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-outside"}), manager.ID)
	if _, err = f.repo.SetEmployeeAccess(outside, other.ID, other.Version, "customer_service", "", f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("clinic boundary: %v", err)
	}
	if _, err = f.repo.SetEmployeeAccess(store.WithEmployeeActor(ctx, "another-manager"), manager.ID, manager.Version, "operations", "", f.now); !errors.Is(err, store.ErrCareState) {
		t.Fatalf("last manager: %v", err)
	}
	managerEvents, err := f.repo.EmployeeEvents(scoped, manager.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range managerEvents {
		if event.Action == "password-changed" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing self password audit")
	}
	events, err := f.repo.EmployeeEvents(scoped, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Action != "access-updated" && events[1].Action != "access-updated" {
		t.Fatalf("access audit: %+v", events)
	}
}
