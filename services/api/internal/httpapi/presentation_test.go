package httpapi

import (
	"example.com/bailiankai/api/internal/model"
	"math"
	"testing"
)

func TestPresentationValidationAndPublicDTO(t *testing.T) {
	p := &model.ClinicPresentation{BrandName: "Mock brand", Credentials: []model.Credential{{Name: "License", ImageURL: "https://example.com/license.jpg"}}, Location: &model.ClinicLocation{Latitude: 31, Longitude: 121}}
	if !validClinicPresentation(p) {
		t.Fatal("valid profile rejected")
	}
	public := clinicDTO(model.Clinic{Presentation: p})
	if public.Presentation == nil || public.Presentation.Location.Latitude != 31 || public.Presentation.Credentials[0].ImageUrl != p.Credentials[0].ImageURL {
		t.Fatal("public presentation missing")
	}
	for _, location := range []model.ClinicLocation{{Latitude: 91}, {Longitude: 181}, {Latitude: math.NaN()}, {Longitude: math.Inf(1)}} {
		p.Location = &location
		if validClinicPresentation(p) {
			t.Fatal("invalid coordinate accepted")
		}
	}
	p.Location = nil
	p.Credentials[0].ImageURL = "http://example.com/license.jpg"
	if validClinicPresentation(p) {
		t.Fatal("insecure credential URL accepted")
	}
	p.Credentials[0].ImageURL = ""
	if !validClinicPresentation(p) {
		t.Fatal("text-only credential rejected")
	}
	if clinicDTO(model.Clinic{}).Presentation != nil {
		t.Fatal("legacy clinic invented profile")
	}
	p.Credentials[0].ImageURL = "https://example.com/license.jpg"
	p.EnvironmentImages = []model.EnvironmentImage{{ImageURL: "https://example.com/env-1.jpg", Caption: "大厅"}}
	p.FeaturedReviews = []model.FeaturedReview{{DisplayName: "王女士", Content: "沟通很耐心", Rating: 5}}
	if !validClinicPresentation(p) {
		t.Fatal("valid environment images and featured reviews rejected")
	}
	public = clinicDTO(model.Clinic{Presentation: p})
	if public.Presentation == nil || public.Presentation.EnvironmentImages == nil || len(*public.Presentation.EnvironmentImages) != 1 || (*public.Presentation.EnvironmentImages)[0].ImageUrl != "https://example.com/env-1.jpg" || public.Presentation.FeaturedReviews == nil || len(*public.Presentation.FeaturedReviews) != 1 || (*public.Presentation.FeaturedReviews)[0].Content != "沟通很耐心" || (*public.Presentation.FeaturedReviews)[0].Rating == nil || *(*public.Presentation.FeaturedReviews)[0].Rating != 5 {
		t.Fatal("public presentation missing environment images or featured reviews")
	}
	p.EnvironmentImages[0].ImageURL = "http://example.com/env-1.jpg"
	if validClinicPresentation(p) {
		t.Fatal("insecure environment image URL accepted")
	}
	p.EnvironmentImages[0].ImageURL = "https://example.com/env-1.jpg"
	p.FeaturedReviews[0].Rating = 6
	if validClinicPresentation(p) {
		t.Fatal("out-of-range review rating accepted")
	}
	doctor := doctorDTO(model.Doctor{Presentation: &model.DoctorPresentation{IsDemo: true}})
	if doctor.Presentation == nil || doctor.Presentation.Credentials == nil {
		t.Fatal("empty credentials must serialize as array")
	}
	// 新增-P1–P2：资质到期提醒。有效期为空表示长期有效；填了则必须是 YYYY-MM-DD。
	p.FeaturedReviews[0].Rating = 5
	p.Credentials[0].ExpiresAt = "2026-11-05"
	if !validClinicPresentation(p) {
		t.Fatal("valid credential expiry rejected")
	}
	public = clinicDTO(model.Clinic{Presentation: p})
	if public.Presentation == nil || public.Presentation.Credentials[0].ExpiresAt == nil || public.Presentation.Credentials[0].ExpiresAt.Format("2006-01-02") != "2026-11-05" {
		t.Fatal("credential expiry not passed through to public DTO")
	}
	for _, bad := range []string{"2026-13-01", "2026-11-5", "11/05/2026", "2026-11-05T00:00:00Z"} {
		p.Credentials[0].ExpiresAt = bad
		if validClinicPresentation(p) {
			t.Fatalf("invalid credential expiry accepted: %s", bad)
		}
	}
	p.Credentials[0].ExpiresAt = ""
	if credentialExpiry("") != nil || credentialExpiry("not-a-date") != nil || credentialExpiry("2027-01-31") == nil {
		t.Fatal("credentialExpiry conversion wrong")
	}
}
