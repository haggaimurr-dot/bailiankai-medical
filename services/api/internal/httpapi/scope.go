package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (a *API) authorize(c *gin.Context, check func() (bool, error)) {
	allowed, err := check()
	if err != nil {
		a.unavailable(c, err)
		return
	}
	if !allowed {
		fail(c, http.StatusNotFound, "NOT_FOUND", "资料不存在或无权访问")
		return
	}
	c.Next()
}
func (a *API) requireAppointmentScope(c *gin.Context) {
	a.authorize(c, func() (bool, error) {
		return a.employees.CanAccessAppointment(c.Request.Context(), c.Param("appointmentId"), false)
	})
}
func (a *API) requireDoctorScope(c *gin.Context) {
	a.authorize(c, func() (bool, error) {
		return a.employees.CanAccessAppointment(c.Request.Context(), c.Param("appointmentId"), true)
	})
}
func (a *API) requireTaskScope(c *gin.Context) {
	a.authorize(c, func() (bool, error) { return a.employees.CanAccessTask(c.Request.Context(), c.Param("taskId")) })
}
func (a *API) requireCarePhotoScope(c *gin.Context) {
	a.authorize(c, func() (bool, error) { return a.employees.CanAccessCarePhoto(c.Request.Context(), c.Param("photoId")) })
}
func (a *API) requireFollowupPhotoScope(c *gin.Context) {
	a.authorize(c, func() (bool, error) {
		return a.employees.CanAccessFollowupPhoto(c.Request.Context(), c.Param("photoId"))
	})
}
