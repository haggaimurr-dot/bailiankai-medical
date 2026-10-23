package httpapi

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

type patientChartBody struct {
	DisplayName  string   `json:"displayName"`
	ClinicID     string   `json:"clinicId"`
	Gender       string   `json:"gender"`
	BirthDate    string   `json:"birthDate"`
	ContactPhone string   `json:"contactPhone"`
	Tags         []string `json:"tags"`
	Version      int      `json:"version"`
}

func (a *API) chartRepo(c *gin.Context) (store.PatientChartRepository, bool) {
	repo, ok := a.repo.(store.PatientChartRepository)
	if !ok {
		fail(c, 503, "SERVICE_UNAVAILABLE", "结构化档案服务暂不可用")
	}
	return repo, ok
}
func validStructuredFields(medical, allergy, treatment, skin, goals string) bool {
	for _, value := range []string{medical, allergy, treatment, goals} {
		if utf8.RuneCountInString(value) > 5000 {
			return false
		}
	}
	return utf8.RuneCountInString(skin) <= 32
}
func (a *API) decodeChart(c *gin.Context, update bool) (patientChartBody, bool) {
	var body patientChartBody
	required := []string{"displayName", "clinicId", "gender", "birthDate", "contactPhone", "tags"}
	if update {
		required = append(required, "version")
	}
	if !decodeObjectLimit(c, &body, required, nil, 32<<10) {
		return body, false
	}
	body.DisplayName, body.ContactPhone = strings.TrimSpace(body.DisplayName), strings.TrimSpace(body.ContactPhone)
	valid := body.DisplayName != "" && utf8.RuneCountInString(body.DisplayName) <= 64 && body.ClinicID != "" && validID(body.ClinicID) && body.Version >= 0 && (body.Gender == "unknown" || body.Gender == "male" || body.Gender == "female")
	if body.BirthDate != "" {
		day, err := time.Parse("2006-01-02", body.BirthDate)
		valid = valid && err == nil && day.Format("2006-01-02") <= a.now().In(time.FixedZone("Shanghai", 8*3600)).Format("2006-01-02") && day.Year() >= 1900
	}
	if body.ContactPhone != "" {
		valid = valid && regexp.MustCompile(`^1[3-9][0-9]{9}$`).MatchString(body.ContactPhone)
	}
	valid = valid && len(body.Tags) <= 20
	seen := map[string]bool{}
	for i, value := range body.Tags {
		value = strings.TrimSpace(value)
		body.Tags[i] = value
		if value == "" || utf8.RuneCountInString(value) > 24 || seen[value] {
			valid = false
		}
		seen[value] = true
	}
	if !valid {
		fail(c, 400, "INVALID_BODY", "姓名、院区、出生日期、手机号或标签无效；最多20个标签，每个不超过24字")
		return body, false
	}
	if body.Tags == nil {
		body.Tags = []string{}
	}
	return body, true
}
func (a *API) createPatientChart(c *gin.Context) {
	repo, ok := a.chartRepo(c)
	if !ok {
		return
	}
	body, ok := a.decodeChart(c, false)
	if !ok {
		return
	}
	row, err := repo.CreatePatientChart(c.Request.Context(), body.DisplayName, model.PatientChart{ClinicID: body.ClinicID, Gender: body.Gender, BirthDate: body.BirthDate, ContactPhone: body.ContactPhone, Tags: body.Tags}, a.careNow())
	if err != nil {
		a.patientRecordError(c, err)
		return
	}
	c.JSON(201, gin.H{"data": row})
}
func (a *API) savePatientChart(c *gin.Context) {
	repo, ok := a.chartRepo(c)
	if !ok {
		return
	}
	if !validID(c.Param("patientId")) {
		fail(c, 400, "INVALID_PATIENT_ID", "患者标识无效")
		return
	}
	body, ok := a.decodeChart(c, true)
	if !ok {
		return
	}
	row, err := repo.SavePatientChart(c.Request.Context(), body.DisplayName, model.PatientChart{PatientID: c.Param("patientId"), ClinicID: body.ClinicID, Gender: body.Gender, BirthDate: body.BirthDate, ContactPhone: body.ContactPhone, Tags: body.Tags, Version: body.Version})
	if err != nil {
		a.patientRecordError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": row})
}
func (a *API) consultationTemplates(c *gin.Context) {
	repo, ok := a.chartRepo(c)
	if !ok {
		return
	}
	rows, err := repo.ConsultationTemplates(c.Request.Context())
	if err != nil {
		a.patientRecordError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": rows})
}
func (a *API) createConsultationTemplate(c *gin.Context) {
	repo, ok := a.chartRepo(c)
	if !ok {
		return
	}
	var row model.ConsultationTemplate
	if !decodeObjectLimit(c, &row, []string{"name", "clinicId", "visitType", "chiefComplaint", "history", "assessment", "advice"}, []string{"medicalHistory", "allergyHistory", "treatmentHistory", "skinType", "goals"}, 64<<10) {
		return
	}
	row.Name = strings.TrimSpace(row.Name)
	if row.Name == "" || utf8.RuneCountInString(row.Name) > 64 || row.ClinicID == "" || !validID(row.ClinicID) || (row.VisitType != "initial" && row.VisitType != "revisit") || utf8.RuneCountInString(row.ChiefComplaint) > 1000 || utf8.RuneCountInString(row.History) > 5000 || utf8.RuneCountInString(row.Assessment) > 5000 || utf8.RuneCountInString(row.Advice) > 5000 || !validStructuredFields(row.MedicalHistory, row.AllergyHistory, row.TreatmentHistory, row.SkinType, row.Goals) {
		fail(c, 400, "INVALID_BODY", "模板名称、院区、类型或内容无效")
		return
	}
	row.RecordedBy, row.CreatedAt = currentEmployee(c).ID, a.careNow()
	saved, err := repo.CreateConsultationTemplate(c.Request.Context(), row)
	if err != nil {
		a.patientRecordError(c, err)
		return
	}
	c.JSON(201, gin.H{"data": saved})
}
func (a *API) deleteConsultationTemplate(c *gin.Context) {
	repo, ok := a.chartRepo(c)
	if !ok {
		return
	}
	if !validID(c.Param("templateId")) {
		fail(c, 400, "INVALID_BODY", "模板标识无效")
		return
	}
	if err := repo.DeleteConsultationTemplate(c.Request.Context(), c.Param("templateId"), currentEmployee(c).ID); err != nil {
		a.patientRecordError(c, err)
		return
	}
	c.Status(204)
}
