package httpapi

import (
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
	"strings"
)

func (a *API) reviewDoctor(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	repo, ok := a.repo.(store.DoctorReviewRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "资质审核暂不可用")
		return
	}
	if !directoryID(c) {
		return
	}
	var body struct {
		Version int    `json:"version"`
		Status  string `json:"status"`
		Note    string `json:"note"`
	}
	if !decodeObject(c, &body, []string{"version", "status", "note"}, nil) {
		return
	}
	body.Note = strings.TrimSpace(body.Note)
	if body.Version < 1 || (body.Status != "approved" && body.Status != "rejected") || !textLength(body.Note, 1, 2000) {
		fail(c, 400, "INVALID_REVIEW", "请选择通过或退回，并填写核验依据或退回原因（最多 2000 字）")
		return
	}
	row, err := repo.ReviewDoctor(c.Request.Context(), c.Param("resourceId"), body.Version, body.Status, body.Note, a.now())
	if err != nil {
		directoryFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": row})
}
