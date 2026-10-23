package store_test

import (
	"context"
	"errors"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"testing"
)

func TestDoctorQualificationReviewLifecycle(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	manager := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"}), "mock-reviewer")
	var d model.Doctor
	if err := f.db.First(&d, "id = ?", "doctor-demo-1").Error; err != nil {
		t.Fatal(err)
	}
	d.Presentation = &model.DoctorPresentation{Credentials: []model.Credential{{Name: "模拟医师执业证书", Number: "MOCK-001", Issuer: "模拟机构", ImageURL: "https://example.com/mock-license.png"}}}
	row, err := f.repo.SaveDirectory(manager, "doctors", d.ID, d.Version, "updated", model.Clinic{}, d, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	d = *row.(*model.Doctor)
	if _, err = f.repo.SaveDirectory(manager, "doctors", d.ID, d.Version, "published", model.Clinic{}, d, false, f.now); !errors.Is(err, store.ErrDoctorQualifications) {
		t.Fatalf("unreviewed publish = %v", err)
	}
	d, err = f.repo.ReviewDoctor(manager, d.ID, d.Version, "rejected", "mock: 请重新核对附件", f.now)
	if err != nil || d.QualificationReview.Status != "rejected" {
		t.Fatalf("reject = %v", err)
	}
	if _, err = f.repo.SaveDirectory(manager, "doctors", d.ID, d.Version, "published", model.Clinic{}, d, false, f.now); !errors.Is(err, store.ErrDoctorQualifications) {
		t.Fatalf("rejected publish = %v", err)
	}
	if _, err = f.repo.ReviewDoctor(manager, d.ID, d.Version-1, "approved", "mock verification", f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale review = %v", err)
	}
	outside := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-other"}), "outside-reviewer")
	if _, err = f.repo.ReviewDoctor(outside, d.ID, d.Version, "approved", "mock verification", f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("outside review = %v", err)
	}
	d, err = f.repo.ReviewDoctor(manager, d.ID, d.Version, "approved", "mock: 核对身份、职称及所属院区", f.now)
	if err != nil {
		t.Fatal(err)
	}
	beforePublish, err := f.repo.Doctors(ctx, d.ClinicID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range beforePublish {
		if p.ID == d.ID && p.QualificationReview != nil {
			t.Fatal("unpublished review leaked into legacy snapshot")
		}
	}
	row, err = f.repo.SaveDirectory(manager, "doctors", d.ID, d.Version, "published", model.Clinic{}, d, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	d = *row.(*model.Doctor)
	d.Summary = "modified mock biography"
	row, err = f.repo.SaveDirectory(manager, "doctors", d.ID, d.Version, "updated", model.Clinic{}, d, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	d = *row.(*model.Doctor)
	if d.QualificationReview != nil {
		t.Fatal("edit retained approval")
	}
	public, err := f.repo.Doctors(ctx, d.ClinicID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range public {
		if p.ID == d.ID {
			found = true
			if p.QualificationReview == nil || p.QualificationReview.Status != "approved" || p.Summary == d.Summary {
				t.Fatal("draft replaced approved public snapshot")
			}
		}
	}
	if !found {
		t.Fatal("published doctor disappeared after draft update")
	}
	events, err := f.repo.DirectoryHistory(manager, "doctors", d.ID)
	if err != nil {
		t.Fatal(err)
	}
	approved, rejected := false, false
	for _, e := range events {
		if e.Action == "qualification-approved" {
			approved = e.ActorID == "mock-reviewer"
		}
		if e.Action == "qualification-rejected" {
			rejected = true
		}
	}
	if !approved || !rejected {
		t.Fatal("review audit events missing")
	}
}
