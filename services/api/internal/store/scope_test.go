package store

import (
	"context"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"strings"
	"testing"
)

func TestClinicScopeDefaultsToDeny(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{DSN: "unused:unused@tcp(127.0.0.1:1)/unused", SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, ctx := range []context.Context{WithEmployeeScope(context.Background(), "manager", ""), WithEmployeeScope(context.Background(), "doctor", "doctor-demo", nil)} {
		query := clinicScope(ctx, db.Table("slots"), "clinic_id").Find(&[]map[string]any{})
		if !strings.Contains(query.Statement.SQL.String(), "1 = 0") {
			t.Fatalf("unassigned employee query: %s", query.Statement.SQL.String())
		}
	}
	ctx := WithEmployeeScope(context.Background(), "manager", "", []string{"clinic-a"})
	query := clinicScope(ctx, db.Table("slots"), "clinic_id").Find(&[]map[string]any{})
	if !strings.Contains(query.Statement.SQL.String(), "clinic_id IN") || len(query.Statement.Vars) != 1 || query.Statement.Vars[0] != "clinic-a" {
		t.Fatalf("missing scope: %s %v", query.Statement.SQL.String(), query.Statement.Vars)
	}
}
func TestClinicalRoleMapping(t *testing.T) {
	for _, role := range []string{"doctor", "nurse", "clinician"} {
		if !ValidEmployeeRole(role) || !ClinicalRole(role) {
			t.Fatalf("invalid clinical role %s", role)
		}
		if clinicianStaff(WithEmployeeScope(context.Background(), role, "staff-self", []string{"clinic-a"})) != "staff-self" {
			t.Fatal("clinical staff restriction missing")
		}
	}
	for _, role := range []string{"manager", "customer_service", "operations"} {
		if !ValidEmployeeRole(role) || ClinicalRole(role) {
			t.Fatalf("invalid nonclinical role %s", role)
		}
	}
	if ValidEmployeeRole("unknown") {
		t.Fatal("unknown role allowed")
	}
}
