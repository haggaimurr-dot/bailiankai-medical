package model

import (
	"encoding/json"
	"testing"
)

func TestPresentationRejectsIncompleteOrUnknownData(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"isDemo":false,"credentials":null}`, `{"isDemo":false,"credentials":[],"secret":"hidden"}`, `{"isDemo":false,"credentials":[{"name":"certificate"}]}`} {
		var p DoctorPresentation
		if json.Unmarshal([]byte(body), &p) == nil {
			t.Fatalf("accepted invalid presentation: %s", body)
		}
	}
	for _, body := range []string{`{"latitude":31}`, `{"latitude":null,"longitude":121}`, `{"latitude":31,"longitude":121,"type":"wgs84"}`} {
		var p ClinicLocation
		if json.Unmarshal([]byte(body), &p) == nil {
			t.Fatalf("accepted invalid location: %s", body)
		}
	}
}

func TestPresentationSnapshotRoundTrip(t *testing.T) {
	p := ClinicPresentation{IsDemo: true, BrandName: "Mock", Location: &ClinicLocation{Latitude: 31.23, Longitude: 121.47}, Credentials: []Credential{{Name: "Mock license", Number: "MOCK-001"}}}
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var restored ClinicPresentation
	if err := json.Unmarshal(body, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Location.Latitude != p.Location.Latitude || restored.Credentials[0].Number != "MOCK-001" || !restored.IsDemo {
		t.Fatal("snapshot lost presentation data")
	}
}
