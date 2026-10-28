package httpapi

import (
	"time"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) registerWaitlistRoutes(r *gin.Engine) {
	booking := r.Group("/api/v1", a.bookingAvailable)
	me := booking.Group("/me", a.authenticatePatient)
	me.GET("/waitlist", a.myWaitlist)
	me.POST("/appointments/:appointmentId/waitlist-confirm", a.confirmWaitlist)
	admin := booking.Group("/admin/waitlist", a.authenticate)
	admin.GET("", a.adminWaitlistQueue)
	admin.POST("", a.sweepWaitlist)
}

func waitlistOfferDTO(offer *model.WaitlistOffer) *apitypes.WaitlistOffer {
	if offer == nil {
		return nil
	}
	return &apitypes.WaitlistOffer{
		Id:            apitypes.EntityId(offer.ID),
		AppointmentId: apitypes.EntityId(offer.AppointmentID),
		SlotId:        apitypes.EntityId(offer.SlotID),
		PatientId:     apitypes.EntityId(offer.PatientID),
		Status:        apitypes.WaitlistOfferStatus(offer.Status),
		ExpiresAt:     offer.ExpiresAt,
		CreatedAt:     offer.CreatedAt,
		UpdatedAt:     offer.UpdatedAt,
	}
}

func waitlistEntriesDTO(entries []store.WaitlistEntry, now time.Time, admin bool) []apitypes.WaitlistEntry {
	data := make([]apitypes.WaitlistEntry, 0, len(entries))
	for _, entry := range entries {
		data = append(data, apitypes.WaitlistEntry{
			Appointment: appointmentDTO(entry.Appointment, now, admin),
			Offer:       waitlistOfferDTO(entry.Offer),
		})
	}
	return data
}

func (a *API) myWaitlist(c *gin.Context) {
	entries, err := a.waitlist.WaitlistEntries(c.Request.Context(), currentPatient(c).ID, a.now())
	if err != nil {
		a.bookingError(c, err, false)
		return
	}
	c.JSON(200, apitypes.WaitlistListResult{Data: waitlistEntriesDTO(entries, a.now(), false)})
}

func (a *API) adminWaitlistQueue(c *gin.Context) {
	slotID := c.Query("slotId")
	if slotID == "" {
		fail(c, 400, "INVALID_SLOT_ID", "需提供时段标识")
		return
	}
	entries, err := a.waitlist.WaitlistQueue(c.Request.Context(), slotID, a.now())
	if err != nil {
		a.bookingError(c, err, false)
		return
	}
	c.JSON(200, apitypes.WaitlistListResult{Data: waitlistEntriesDTO(entries, a.now(), true)})
}

func (a *API) confirmWaitlist(c *gin.Context) {
	if !validAppointmentID(c) {
		return
	}
	row, err := a.waitlist.ConfirmWaitlist(c.Request.Context(), c.Param("appointmentId"), currentPatient(c).ID, a.now())
	if err != nil {
		a.bookingError(c, err, false)
		return
	}
	c.JSON(200, apitypes.AppointmentResult{Data: appointmentDTO(row, a.now(), false)})
}

func (a *API) sweepWaitlist(c *gin.Context) {
	result, err := a.waitlist.SweepWaitlist(c.Request.Context(), a.now())
	if err != nil {
		a.bookingError(c, err, false)
		return
	}
	c.JSON(200, apitypes.WaitlistSweepResult{Data: struct {
		Expired int32 `json:"expired"`
		Offered int32 `json:"offered"`
	}{Expired: int32(result.Expired), Offered: int32(result.Offered)}})
}
