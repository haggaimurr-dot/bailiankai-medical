package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *API) registerReviewRoutes(r *gin.Engine) {
	booking := r.Group("/api/v1", a.bookingAvailable)
	booking.GET("/doctors/:doctorId/reviews", a.listDoctorReviewsPublic)
	me := booking.Group("/me", a.authenticatePatient)
	me.POST("/doctors/:doctorId/reviews", a.createDoctorReview)
	me.GET("/doctors/:doctorId/reviewable", a.listReviewableAppointments)
	me.GET("/reviews", a.listMyReviews)
	admin := booking.Group("/admin/reviews", a.authenticate)
	admin.GET("", a.listAdminReviews)
	admin.POST("/:reviewId/review", a.decideDoctorReview)
}

func maskDisplayName(name string) string {
	runes := []rune(strings.TrimSpace(name))
	if len(runes) == 0 {
		return "匿名患者"
	}
	if len(runes) == 1 {
		return "*"
	}
	return string(runes[0]) + strings.Repeat("*", len(runes)-1)
}

func reviewDTO(row model.DoctorReview, displayName string) apitypes.DoctorReview {
	result := apitypes.DoctorReview{
		Id: apitypes.EntityId(row.ID), DoctorId: apitypes.EntityId(row.DoctorID),
		PatientId: apitypes.EntityId(row.PatientID), AppointmentId: apitypes.EntityId(row.AppointmentID),
		Rating: row.Rating, Content: row.Content, Status: apitypes.DoctorReviewStatus(row.Status),
		CreatedAt: row.CreatedAt.In(database.Shanghai),
	}
	if displayName != "" {
		name := displayName
		result.DisplayName = &name
	}
	if row.ReviewNote != "" {
		note := row.ReviewNote
		result.ReviewNote = &note
	}
	if row.ReviewedAt != nil {
		reviewed := row.ReviewedAt.In(database.Shanghai)
		result.ReviewedAt = &reviewed
	}
	return result
}

func reviewPublicDTO(row store.PatientReviewWithPatient) apitypes.DoctorReviewPublic {
	name := maskDisplayName(row.PatientDisplayName)
	return apitypes.DoctorReviewPublic{
		Id: apitypes.EntityId(row.ID), DoctorId: apitypes.EntityId(row.DoctorID),
		Rating: row.Rating, Content: row.Content, CreatedAt: row.CreatedAt.In(database.Shanghai),
		DisplayName: &name,
	}
}

func (a *API) reviewRepo(c *gin.Context) (store.PatientReviewRepository, bool) {
	repo, ok := a.repo.(store.PatientReviewRepository)
	if !ok {
		fail(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "评价服务暂不可用")
		return nil, false
	}
	return repo, true
}

func (a *API) listDoctorReviewsPublic(c *gin.Context) {
	doctorID := c.Param("doctorId")
	if !validID(doctorID) {
		fail(c, http.StatusBadRequest, "INVALID_ID", "医生编号无效")
		return
	}
	repo, ok := a.reviewRepo(c)
	if !ok {
		return
	}
	rows, err := repo.PublicPatientReviews(c.Request.Context(), doctorID)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	data := make([]apitypes.DoctorReviewPublic, 0, len(rows))
	for _, row := range rows {
		data = append(data, reviewPublicDTO(row))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func (a *API) createDoctorReview(c *gin.Context) {
	doctorID := c.Param("doctorId")
	if !validID(doctorID) {
		fail(c, http.StatusBadRequest, "INVALID_ID", "医生编号无效")
		return
	}
	var body apitypes.PatientReviewInput
	if !decodeObject(c, &body, []string{"appointmentId", "rating", "content"}, nil) {
		return
	}
	if !validID(string(body.AppointmentId)) {
		fail(c, http.StatusBadRequest, "INVALID_ID", "预约编号无效")
		return
	}
	repo, ok := a.reviewRepo(c)
	if !ok {
		return
	}
	row, err := repo.CreatePatientReview(c.Request.Context(), currentPatient(c).ID, doctorID, string(body.AppointmentId), body.Rating, body.Content, a.now())
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			fail(c, http.StatusNotFound, "NOT_FOUND", "预约不存在")
		case errors.Is(err, store.ErrReviewNotEligible):
			fail(c, http.StatusConflict, "REVIEW_NOT_ELIGIBLE", "该就诊尚未完成治疗，暂不能评价")
		case errors.Is(err, store.ErrReviewDuplicate):
			fail(c, http.StatusConflict, "REVIEW_ALREADY_EXISTS", "该就诊已评价")
		default:
			fail(c, http.StatusBadRequest, "INVALID_REVIEW", "评分须为 1–5 分，评价内容 1–1000 字")
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": reviewDTO(row, "")})
}

func (a *API) listReviewableAppointments(c *gin.Context) {
	doctorID := c.Param("doctorId")
	if !validID(doctorID) {
		fail(c, http.StatusBadRequest, "INVALID_ID", "医生编号无效")
		return
	}
	repo, ok := a.reviewRepo(c)
	if !ok {
		return
	}
	rows, err := repo.ReviewableAppointments(c.Request.Context(), currentPatient(c).ID, doctorID)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	data := make([]apitypes.ReviewableAppointment, 0, len(rows))
	for _, row := range rows {
		data = append(data, apitypes.ReviewableAppointment{
			AppointmentId: apitypes.EntityId(row.AppointmentID), DoctorId: apitypes.EntityId(row.DoctorID),
			StartsAt: row.StartsAt.In(database.Shanghai),
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func (a *API) listMyReviews(c *gin.Context) {
	doctorID := c.Query("doctorId")
	if doctorID != "" && !validID(doctorID) {
		fail(c, http.StatusBadRequest, "INVALID_ID", "医生编号无效")
		return
	}
	repo, ok := a.reviewRepo(c)
	if !ok {
		return
	}
	rows, err := repo.MyPatientReviews(c.Request.Context(), currentPatient(c).ID, doctorID)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	data := make([]apitypes.DoctorReview, 0, len(rows))
	for _, row := range rows {
		data = append(data, reviewDTO(row, ""))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func (a *API) listAdminReviews(c *gin.Context) {
	status := c.Query("status")
	if status != "" && status != "pending" && status != "approved" && status != "rejected" {
		fail(c, http.StatusBadRequest, "INVALID_QUERY", "评价状态无效")
		return
	}
	repo, ok := a.reviewRepo(c)
	if !ok {
		return
	}
	rows, err := repo.AdminPatientReviews(c.Request.Context(), status)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	data := make([]apitypes.DoctorReview, 0, len(rows))
	for _, row := range rows {
		data = append(data, reviewDTO(row.DoctorReview, row.PatientDisplayName))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func (a *API) decideDoctorReview(c *gin.Context) {
	reviewID := c.Param("reviewId")
	if !validID(reviewID) {
		fail(c, http.StatusBadRequest, "INVALID_ID", "评价编号无效")
		return
	}
	var body apitypes.DoctorReviewDecision
	if !decodeObject(c, &body, []string{"decision"}, []string{"note"}) {
		return
	}
	note := ""
	if body.Note != nil {
		note = *body.Note
	}
	if utf8.RuneCountInString(note) > 500 {
		fail(c, http.StatusBadRequest, "INVALID_REVIEW", "审核备注最多 500 字")
		return
	}
	repo, ok := a.reviewRepo(c)
	if !ok {
		return
	}
	row, err := repo.DecidePatientReview(c.Request.Context(), reviewID, string(body.Decision), note, a.now())
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			fail(c, http.StatusNotFound, "NOT_FOUND", "评价不存在")
		case errors.Is(err, store.ErrConflict):
			fail(c, http.StatusConflict, "REVIEW_ALREADY_DECIDED", "该评价已审核")
		default:
			fail(c, http.StatusBadRequest, "INVALID_REVIEW", "审核意见无效")
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": reviewDTO(row, "")})
}
