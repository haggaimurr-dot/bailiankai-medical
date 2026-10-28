package httpapi

import (
	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
)

func clinicDTO(row model.Clinic) apitypes.Clinic {
	return apitypes.Clinic{Id: row.ID, Name: row.Name, Address: row.Address, Phone: row.Phone, Presentation: clinicPresentationDTO(row.Presentation)}
}

func doctorDTO(row model.Doctor) apitypes.Doctor {
	focus := row.Focus
	if focus == nil {
		focus = []string{}
	}
	var presentation *apitypes.DoctorPresentation
	if row.Presentation != nil {
		presentation = &apitypes.DoctorPresentation{IsDemo: row.Presentation.IsDemo, Credentials: credentialDTOs(row.Presentation.Credentials)}
	}
	result := apitypes.Doctor{Id: row.ID, ClinicId: row.ClinicID, Name: row.Name, Title: row.Title, Summary: row.Summary, Focus: focus, Initials: row.Initials, AvatarUrl: &row.AvatarURL, ServiceIntroduction: &row.ServiceIntroduction, Presentation: presentation}
	if row.QualificationReview != nil && row.QualificationReview.Status == "approved" {
		result.QualificationReviewedAt = &row.QualificationReview.ReviewedAt
	}
	return result
}

func credentialDTOs(rows []model.Credential) []apitypes.Credential {
	result := make([]apitypes.Credential, 0, len(rows))
	for _, row := range rows {
		result = append(result, apitypes.Credential{Name: row.Name, Number: row.Number, Issuer: row.Issuer, ImageUrl: row.ImageURL})
	}
	return result
}

func clinicPresentationDTO(p *model.ClinicPresentation) *apitypes.ClinicPresentation {
	if p == nil {
		return nil
	}
	result := &apitypes.ClinicPresentation{IsDemo: p.IsDemo, BrandName: p.BrandName, Tagline: p.Tagline, Introduction: p.Introduction, LogoUrl: p.LogoURL, Hours: p.Hours, Transport: p.Transport, ArrivalGuide: p.ArrivalGuide, Credentials: credentialDTOs(p.Credentials)}
	if p.Location != nil {
		result.Location = &apitypes.ClinicLocation{Latitude: p.Location.Latitude, Longitude: p.Location.Longitude}
	}
	return result
}

func slotDTO(row model.Slot) apitypes.Slot {
	remaining := row.Capacity - row.Reserved
	if remaining < 0 {
		remaining = 0
	}
	return apitypes.Slot{Id: row.ID, ClinicId: row.ClinicID, DoctorId: row.DoctorID, StartsAt: row.StartsAt.In(database.Shanghai), EndsAt: row.EndsAt.In(database.Shanghai), Capacity: int32(row.Capacity), Reserved: int32(row.Reserved), Remaining: int32(remaining), Status: apitypes.SlotStatus(row.Status), Version: int32(row.Version)}
}
