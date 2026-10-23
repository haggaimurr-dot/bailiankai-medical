package model

import "testing"

func TestDoctorQualificationsPublishable(t *testing.T) {
	complete := Credential{Name: "模拟医师执业证", Number: "MOCK-001", Issuer: "模拟发证机构", ImageURL: "https://example.com/mock.png"}
	for _, tc := range []struct {
		name string
		p    *DoctorPresentation
		r    *DoctorQualificationReview
		want bool
	}{
		{"legacy", nil, nil, true},
		{"demo", &DoctorPresentation{IsDemo: true}, nil, true},
		{"unreviewed", &DoctorPresentation{Credentials: []Credential{complete}}, nil, false},
		{"rejected", &DoctorPresentation{Credentials: []Credential{complete}}, &DoctorQualificationReview{Status: "rejected"}, false},
		{"approved", &DoctorPresentation{Credentials: []Credential{complete}}, &DoctorQualificationReview{Status: "approved"}, true},
		{"missing evidence", &DoctorPresentation{Credentials: []Credential{{Name: "证照"}}}, &DoctorQualificationReview{Status: "approved"}, false},
		{"no credentials", &DoctorPresentation{}, &DoctorQualificationReview{Status: "approved"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Doctor{Presentation: tc.p, QualificationReview: tc.r}).QualificationsPublishable(); got != tc.want {
				t.Fatalf("publishable = %v, want %v", got, tc.want)
			}
		})
	}
}
