package httpapi

import (
	"example.com/bailiankai/api/internal/model"
	"math"
	"time"
)

func validCredentials(rows []model.Credential) bool {
	if len(rows) > 20 {
		return false
	}
	for _, row := range rows {
		if !textLength(row.Name, 1, 120) || !textLength(row.Number, 0, 120) || !textLength(row.Issuer, 0, 120) || !validAvatar(row.ImageURL) {
			return false
		}
		if row.ExpiresAt != "" {
			parsed, err := time.Parse("2006-01-02", row.ExpiresAt)
			if err != nil || parsed.Format("2006-01-02") != row.ExpiresAt {
				return false
			}
		}
	}
	return true
}

func validClinicPresentation(p *model.ClinicPresentation) bool {
	if p == nil {
		return true
	}
	if !textLength(p.BrandName, 1, 120) || !textLength(p.Tagline, 0, 120) || !textLength(p.Introduction, 0, 5000) || !validAvatar(p.LogoURL) || !textLength(p.Hours, 0, 120) || !textLength(p.Transport, 0, 2000) || !textLength(p.ArrivalGuide, 0, 2000) || !validCredentials(p.Credentials) {
		return false
	}
	if len(p.EnvironmentImages) > 10 || len(p.FeaturedReviews) > 10 {
		return false
	}
	for _, row := range p.EnvironmentImages {
		if row.ImageURL == "" || !validAvatar(row.ImageURL) || !textLength(row.Caption, 0, 200) {
			return false
		}
	}
	for _, row := range p.FeaturedReviews {
		if !textLength(row.DisplayName, 0, 50) || !textLength(row.Content, 1, 1000) || row.Rating < 0 || row.Rating > 5 {
			return false
		}
	}
	if p.Location != nil {
		lat, lng := p.Location.Latitude, p.Location.Longitude
		return !math.IsNaN(lat) && !math.IsInf(lat, 0) && !math.IsNaN(lng) && !math.IsInf(lng, 0) && lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
	}
	return true
}
