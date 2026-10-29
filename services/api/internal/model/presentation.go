package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Presentation is part of the directory draft and its published snapshot.
type Credential struct {
	Name      string `json:"name"`
	Number    string `json:"number"`
	Issuer    string `json:"issuer"`
	ImageURL  string `json:"imageUrl"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

type ClinicLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type EnvironmentImage struct {
	ImageURL string `json:"imageUrl"`
	Caption  string `json:"caption"`
}

type FeaturedReview struct {
	DisplayName string `json:"displayName"`
	Content     string `json:"content"`
	Rating      int    `json:"rating,omitempty"`
}

type ClinicPresentation struct {
	IsDemo            bool               `json:"isDemo"`
	BrandName         string             `json:"brandName"`
	Tagline           string             `json:"tagline"`
	Introduction      string             `json:"introduction"`
	LogoURL           string             `json:"logoUrl"`
	Hours             string             `json:"hours"`
	Transport         string             `json:"transport"`
	ArrivalGuide      string             `json:"arrivalGuide"`
	Location          *ClinicLocation    `json:"location,omitempty"`
	Credentials       []Credential       `json:"credentials"`
	EnvironmentImages []EnvironmentImage `json:"environmentImages,omitempty"`
	FeaturedReviews   []FeaturedReview   `json:"featuredReviews,omitempty"`
}

type DoctorPresentation struct {
	IsDemo      bool         `json:"isDemo"`
	Credentials []Credential `json:"credentials"`
}

func decodePresentation(data []byte, target any, required ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return fmt.Errorf("presentation must be an object")
	}
	for _, name := range required {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("presentation field %s is required", name)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func (p *ClinicPresentation) UnmarshalJSON(data []byte) error {
	type plain ClinicPresentation
	return decodePresentation(data, (*plain)(p), "isDemo", "brandName", "tagline", "introduction", "logoUrl", "hours", "transport", "arrivalGuide", "credentials")
}
func (p *DoctorPresentation) UnmarshalJSON(data []byte) error {
	type plain DoctorPresentation
	return decodePresentation(data, (*plain)(p), "isDemo", "credentials")
}
func (p *ClinicLocation) UnmarshalJSON(data []byte) error {
	type plain ClinicLocation
	return decodePresentation(data, (*plain)(p), "latitude", "longitude")
}
func (p *Credential) UnmarshalJSON(data []byte) error {
	type plain Credential
	return decodePresentation(data, (*plain)(p), "name", "number", "issuer", "imageUrl")
}
