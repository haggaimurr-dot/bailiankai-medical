package httpapi

import (
	"time"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	openapi_types "github.com/oapi-codegen/runtime/types"
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

// credentialExpiry converts a validated YYYY-MM-DD expiry string to the
// contract date type; empty or unparseable values stay absent.
func credentialExpiry(value string) *openapi_types.Date {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return nil
	}
	return &openapi_types.Date{Time: parsed}
}

func credentialDTOs(rows []model.Credential) []apitypes.Credential {
	result := make([]apitypes.Credential, 0, len(rows))
	for _, row := range rows {
		result = append(result, apitypes.Credential{Name: row.Name, Number: row.Number, Issuer: row.Issuer, ImageUrl: row.ImageURL, ExpiresAt: credentialExpiry(row.ExpiresAt)})
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
	if len(p.EnvironmentImages) > 0 {
		rows := make([]apitypes.EnvironmentImage, 0, len(p.EnvironmentImages))
		for _, row := range p.EnvironmentImages {
			rows = append(rows, apitypes.EnvironmentImage{ImageUrl: row.ImageURL, Caption: row.Caption})
		}
		result.EnvironmentImages = &rows
	}
	if len(p.FeaturedReviews) > 0 {
		rows := make([]apitypes.FeaturedReview, 0, len(p.FeaturedReviews))
		for _, row := range p.FeaturedReviews {
			rating := row.Rating
			review := apitypes.FeaturedReview{DisplayName: row.DisplayName, Content: row.Content}
			if rating != 0 {
				review.Rating = &rating
			}
			rows = append(rows, review)
		}
		result.FeaturedReviews = &rows
	}
	return result
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func patientMessageDTO(row model.PatientMessage) apitypes.PatientMessage {
	return apitypes.PatientMessage{
		Id: row.ID, Kind: apitypes.PatientMessageKind(row.Kind), Title: row.Title, Body: row.Body,
		AppointmentId: row.AppointmentID, TicketId: strPtrOrNil(row.TicketID), Read: row.ReadAt != nil, CreatedAt: row.CreatedAt.In(database.Shanghai),
	}
}

func slotDTO(row model.Slot) apitypes.Slot {
	remaining := row.Capacity - row.Reserved
	if remaining < 0 {
		remaining = 0
	}
	return apitypes.Slot{Id: row.ID, ClinicId: row.ClinicID, DoctorId: row.DoctorID, StartsAt: row.StartsAt.In(database.Shanghai), EndsAt: row.EndsAt.In(database.Shanghai), Capacity: int32(row.Capacity), Reserved: int32(row.Reserved), Remaining: int32(remaining), Status: apitypes.SlotStatus(row.Status), Version: int32(row.Version)}
}

func slotDraftDTO(row store.SlotDraft) apitypes.SlotDraft {
	day, _ := time.Parse("2006-01-02", row.Date)
	return apitypes.SlotDraft{Date: openapi_types.Date{Time: day}, StartsAt: row.StartsAt, EndsAt: row.EndsAt}
}

func slotDraftDTOs(rows []store.SlotDraft) []apitypes.SlotDraft {
	data := make([]apitypes.SlotDraft, 0, len(rows))
	for _, row := range rows {
		data = append(data, slotDraftDTO(row))
	}
	return data
}

func slotConflictDTO(row store.SlotConflict) apitypes.SlotConflict {
	day, _ := time.Parse("2006-01-02", row.Date)
	item := apitypes.SlotConflict{Date: openapi_types.Date{Time: day}, StartsAt: row.StartsAt, EndsAt: row.EndsAt, Reason: apitypes.SlotConflictReason(row.Reason)}
	if row.ExistingSlotID != "" {
		id := row.ExistingSlotID
		capacity := int32(row.ExistingCapacity)
		reserved := int32(row.ExistingReserved)
		item.ExistingSlotId = &id
		item.ExistingCapacity = &capacity
		item.ExistingReserved = &reserved
	}
	return item
}

func slotConflictDTOs(rows []store.SlotConflict) []apitypes.SlotConflict {
	data := make([]apitypes.SlotConflict, 0, len(rows))
	for _, row := range rows {
		data = append(data, slotConflictDTO(row))
	}
	return data
}
