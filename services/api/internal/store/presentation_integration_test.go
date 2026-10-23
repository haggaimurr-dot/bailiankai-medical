package store_test

import (
	"context"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"testing"
)

func TestPresentationDraftPublicationAndLegacyUpdate(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	manager := store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"})
	var clinic model.Clinic
	if err := f.db.First(&clinic, "id = ?", "clinic-demo").Error; err != nil {
		t.Fatal(err)
	}
	clinic.Presentation = &model.ClinicPresentation{IsDemo: true, BrandName: "Mock brand", Credentials: []model.Credential{{Name: "Mock license", Number: "MOCK-001"}}, Location: &model.ClinicLocation{Latitude: 31.23, Longitude: 121.47}}
	updated, err := f.repo.SaveDirectory(manager, "clinics", clinic.ID, clinic.Version, "updated", clinic, model.Doctor{}, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	clinic = *updated.(*model.Clinic)
	rows, err := f.repo.Clinics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Presentation != nil {
		t.Fatal("draft leaked")
	}
	published, err := f.repo.SaveDirectory(manager, "clinics", clinic.ID, clinic.Version, "published", clinic, model.Doctor{}, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	clinic = *published.(*model.Clinic)
	rows, err = f.repo.Clinics(ctx)
	if err != nil || rows[0].Presentation == nil || rows[0].Presentation.BrandName != "Mock brand" {
		t.Fatalf("published presentation missing: %v", err)
	}
	clinic.Presentation = nil // old clients omit the new field
	updated, err = f.repo.SaveDirectory(manager, "clinics", clinic.ID, clinic.Version, "updated", clinic, model.Doctor{}, false, f.now)
	if err != nil || updated.(*model.Clinic).Presentation == nil {
		t.Fatalf("legacy update cleared profile: %v", err)
	}
}
