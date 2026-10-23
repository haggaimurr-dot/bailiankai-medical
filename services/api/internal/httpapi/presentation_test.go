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
	doctor := doctorDTO(model.Doctor{Presentation: &model.DoctorPresentation{IsDemo: true}})
	if doctor.Presentation == nil || doctor.Presentation.Credentials == nil {
		t.Fatal("empty credentials must serialize as array")
	}
}
