package httpapi

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

const photoConsentVersion = "photo-care-v2"
const maxPhotoBytes = 8 << 20

func (a *API) mediaUnavailable(c *gin.Context) {
	// SDK errors may contain signed URLs. Keep credentials and object names out
	// of logs and responses; retry/repair work is handled by the media worker.
	slog.Error("private media operation failed", "method", c.Request.Method, "route", c.FullPath())
	c.Header("Retry-After", "5")
	fail(c, http.StatusServiceUnavailable, "MEDIA_UNAVAILABLE", "图片存储暂不可用，请稍后重试")
}

func (a *API) careError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(c, http.StatusNotFound, "CARE_RECORD_NOT_FOUND", "预约或资料不存在")
	case errors.Is(err, store.ErrForbidden):
		fail(c, http.StatusNotFound, "NOT_FOUND", "资料不存在或无权访问")
	case errors.Is(err, store.ErrConflict):
		fail(c, http.StatusConflict, "VERSION_CONFLICT", "资料已被其他操作修改，请刷新后重试")
	case errors.Is(err, store.ErrPhotoConsent):
		fail(c, http.StatusForbidden, "PHOTO_CONSENT_REQUIRED", "请先同意照片用于本次诊疗")
	case errors.Is(err, store.ErrTreatmentTimeLocked):
		fail(c, http.StatusConflict, "TREATMENT_TIME_LOCKED", "已有随访反馈或照片，实际治疗时间不能更正；其他内容仍可更正")
	case errors.Is(err, store.ErrStaffOffDuty):
		fail(c, http.StatusConflict, "FOLLOWUP_STAFF_OFF_DUTY", "负责人已离岗，请选择在岗医护")
	case errors.Is(err, store.ErrHandoffRecipientMissing):
		fail(c, http.StatusConflict, "HANDOFF_RECIPIENT_ACCOUNT_REQUIRED", "接手人没有启用中的关联员工账号，请先开通账号")
	case errors.Is(err, store.ErrHandoffNotOverdue):
		fail(c, http.StatusConflict, "HANDOFF_NOT_OVERDUE", "此交接尚未超时，或未达到可记录跟进的条件")
	case errors.Is(err, store.ErrCareState):
		fail(c, http.StatusConflict, "CARE_STATE_CONFLICT", "当前预约状态不允许此操作")
	default:
		a.unavailable(c, err)
	}
}

func validCareAppointment(c *gin.Context) bool { return validAppointmentID(c) }

func photoDTO(row model.CarePhoto) apitypes.CarePhoto {
	return apitypes.CarePhoto{Id: apitypes.EntityId(row.ID), AppointmentId: apitypes.EntityId(row.AppointmentID), Angle: apitypes.PhotoAngle(row.Angle), MediaType: apitypes.CarePhotoMediaType("image/jpeg"), SizeBytes: row.SizeBytes, UploadedAt: row.UploadedAt}
}

func (a *API) getPhotoConsent(c *gin.Context) {
	if !validCareAppointment(c) {
		return
	}
	patient := currentPatient(c)
	granted, err := a.care.PhotoConsent(c.Request.Context(), c.Param("appointmentId"), patient.ID)
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(http.StatusOK, apitypes.PhotoConsentResult{Data: apitypes.PhotoConsent{Granted: granted, Version: photoConsentVersion}})
}

func (a *API) setPhotoConsent(c *gin.Context) {
	if !validCareAppointment(c) {
		return
	}
	var body apitypes.SetPhotoConsentJSONRequestBody
	if !decodeObject(c, &body, []string{"granted"}, nil) {
		return
	}
	patient := currentPatient(c)
	appointmentID := c.Param("appointmentId")
	err := a.care.SetPhotoConsent(c.Request.Context(), appointmentID, patient.ID, body.Granted, a.now().In(database.Shanghai).Truncate(time.Millisecond))
	if err != nil {
		a.careError(c, err)
		return
	}
	c.JSON(http.StatusOK, apitypes.PhotoConsentResult{Data: apitypes.PhotoConsent{Granted: body.Granted, Version: photoConsentVersion}})
}

func (a *API) patientCarePhotos(c *gin.Context) {
	if !validCareAppointment(c) {
		return
	}
	rows, err := a.care.ListCarePhotos(c.Request.Context(), c.Param("appointmentId"), currentPatient(c).ID, false)
	if err != nil {
		a.careError(c, err)
		return
	}
	patient := currentPatient(c)
	if err := a.care.RecordCareAccess(c.Request.Context(), c.Param("appointmentId"), "photo-set", c.Param("appointmentId"), "listed", "patient", patient.ID, a.now().In(database.Shanghai).Truncate(time.Millisecond)); err != nil {
		a.careError(c, err)
		return
	}
	data := make([]apitypes.CarePhoto, 0, len(rows))
	for _, row := range rows {
		data = append(data, photoDTO(row))
	}
	c.JSON(http.StatusOK, apitypes.CarePhotoList{Data: data})
}

func normalizePhoto(data []byte) ([]byte, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 {
		return nil, errors.New("invalid image")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	canvas := image.NewRGBA(source.Bounds())
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(canvas, canvas.Bounds(), source, source.Bounds().Min, draw.Over)
	var output bytes.Buffer
	if err := jpeg.Encode(&output, canvas, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	if output.Len() == 0 || output.Len() > maxPhotoBytes {
		return nil, errors.New("image too large")
	}
	return output.Bytes(), nil
}

func (a *API) uploadCarePhoto(c *gin.Context) {
	if !validCareAppointment(c) {
		return
	}
	angle := c.Param("angle")
	if angle != "front" && angle != "left" && angle != "right" {
		fail(c, http.StatusBadRequest, "INVALID_PHOTO_ANGLE", "照片角度无效")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPhotoBytes+(1<<20))
	file, header, err := c.Request.FormFile("file")
	if err != nil || header.Size < 1 || header.Size > maxPhotoBytes {
		fail(c, http.StatusBadRequest, "INVALID_PHOTO", "请选择不超过 8 MB 的 JPG 或 PNG 图片")
		return
	}
	defer file.Close()
	input, err := io.ReadAll(io.LimitReader(file, maxPhotoBytes+1))
	if err != nil || len(input) > maxPhotoBytes {
		fail(c, http.StatusBadRequest, "INVALID_PHOTO", "图片大小超过 8 MB")
		return
	}
	data, err := normalizePhoto(input)
	if err != nil {
		fail(c, http.StatusBadRequest, "INVALID_PHOTO", "图片无法读取，请选择有效的 JPG 或 PNG 图片")
		return
	}
	patient := currentPatient(c)
	appointmentID := c.Param("appointmentId")
	hash := sha256.Sum256(data)
	row := model.CarePhoto{ID: randomCareID(), AppointmentID: appointmentID, PatientID: patient.ID, Angle: angle, MediaType: "image/jpeg", SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(hash[:]), UploadedAt: a.now().In(database.Shanghai).Truncate(time.Millisecond)}
	if err := store.SavePhotoWithVariant(c.Request.Context(), a.media, row.ID, data); err != nil {
		a.mediaUnavailable(c)
		return
	}
	_, err = a.care.SaveCarePhoto(c.Request.Context(), row, row.UploadedAt)
	if err != nil {
		// A commit acknowledgement can be lost. Never delete bytes on an
		// ambiguous database error; the age-gated worker checks references.
		a.careError(c, err)
		return
	}
	c.JSON(http.StatusOK, apitypes.CarePhotoResult{Data: photoDTO(row)})
}

func randomCareID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "photo-" + hex.EncodeToString(b[:])
}

func (a *API) carePhotoContent(c *gin.Context, patientID string, requireConsent bool) {
	id := c.Param("photoId")
	if !validID(id) {
		fail(c, http.StatusBadRequest, "INVALID_PHOTO_ID", "照片编号格式无效")
		return
	}
	row, err := a.care.CarePhotoByID(c.Request.Context(), id, patientID, requireConsent)
	if err != nil {
		a.careError(c, err)
		return
	}
	actorType, actorID := "clinician", "demo-clinician"
	if patientID != "" {
		actorType, actorID = "patient", patientID
	} else {
		actorID = currentEmployee(c).ID
	}
	data, err := a.media.Read(c.Request.Context(), row.ID)
	if err != nil {
		a.mediaUnavailable(c)
		return
	}
	if err := store.VerifyMedia(data, store.MediaReference{ID: row.ID, SHA256: row.SHA256, SizeBytes: row.SizeBytes}); err != nil {
		a.mediaUnavailable(c)
		return
	}
	// Object reads can take seconds. Re-evaluate consent and employee scope
	// after I/O so revocation during a slow download does not expose bytes.
	if _, err := a.care.CarePhotoByID(c.Request.Context(), id, patientID, requireConsent); err != nil {
		a.careError(c, err)
		return
	}
	if err := a.care.RecordCareAccess(c.Request.Context(), row.AppointmentID, "photo", row.ID, "viewed", actorType, actorID, a.now().In(database.Shanghai).Truncate(time.Millisecond)); err != nil {
		a.careError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", "inline")
	c.Data(http.StatusOK, "image/jpeg", data)
}

func (a *API) patientCarePhotoContent(c *gin.Context) {
	a.carePhotoContent(c, currentPatient(c).ID, false)
}
func (a *API) adminCarePhotoContent(c *gin.Context) { a.carePhotoContent(c, "", true) }

func (a *API) adminCarePhotos(c *gin.Context) {
	if !validCareAppointment(c) {
		return
	}
	rows, err := a.care.ListCarePhotos(c.Request.Context(), c.Param("appointmentId"), "", true)
	if err != nil {
		a.careError(c, err)
		return
	}
	if err := a.care.RecordCareAccess(c.Request.Context(), c.Param("appointmentId"), "photo-set", c.Param("appointmentId"), "listed", "clinician", currentEmployee(c).ID, a.now().In(database.Shanghai).Truncate(time.Millisecond)); err != nil {
		a.careError(c, err)
		return
	}
	data := make([]apitypes.CarePhoto, 0, len(rows))
	for _, row := range rows {
		data = append(data, photoDTO(row))
	}
	c.JSON(http.StatusOK, apitypes.CarePhotoList{Data: data})
}

func contentDTO(value string) (apitypes.CarePlanContent, error) {
	var content apitypes.CarePlanContent
	err := json.Unmarshal([]byte(value), &content)
	return content, err
}

func planVersionDTO(row model.CarePlanVersion) (apitypes.CarePlanVersion, error) {
	content, err := contentDTO(row.ContentJSON)
	if err != nil {
		return apitypes.CarePlanVersion{}, err
	}
	return apitypes.CarePlanVersion{Id: apitypes.EntityId(row.ID), Version: int32(row.Version), Content: content, ChangeReason: row.ChangeReason, PublishedBy: "负责医生", PublishedAt: row.PublishedAt}, nil
}

func planWorkspaceDTO(plan model.CarePlan, versions []model.CarePlanVersion) (apitypes.CarePlanWorkspace, error) {
	result := apitypes.CarePlanWorkspace{DraftVersion: int32(plan.DraftVersion), Versions: make([]apitypes.CarePlanVersion, 0, len(versions))}
	if plan.DraftJSON != nil {
		content, err := contentDTO(*plan.DraftJSON)
		if err != nil {
			return result, err
		}
		result.Draft = &content
	}
	for _, row := range versions {
		version, err := planVersionDTO(row)
		if err != nil {
			return result, err
		}
		result.Versions = append(result.Versions, version)
	}
	return result, nil
}

func (a *API) patientCarePlan(c *gin.Context) {
	if !validCareAppointment(c) {
		return
	}
	rows, err := a.care.PatientCarePlan(c.Request.Context(), c.Param("appointmentId"), currentPatient(c).ID)
	if err != nil {
		a.careError(c, err)
		return
	}
	patient := currentPatient(c)
	if err := a.care.RecordCareAccess(c.Request.Context(), c.Param("appointmentId"), "care-plan", c.Param("appointmentId"), "viewed", "patient", patient.ID, a.now().In(database.Shanghai).Truncate(time.Millisecond)); err != nil {
		a.careError(c, err)
		return
	}
	data := make([]apitypes.CarePlanVersion, 0, len(rows))
	for _, row := range rows {
		version, err := planVersionDTO(row)
		if err != nil {
			a.unavailable(c, err)
			return
		}
		data = append(data, version)
	}
	c.JSON(http.StatusOK, apitypes.CarePlanVersionList{Data: data})
}

func (a *API) adminCarePlan(c *gin.Context) {
	if !validCareAppointment(c) {
		return
	}
	plan, versions, err := a.care.AdminCarePlan(c.Request.Context(), c.Param("appointmentId"))
	if err != nil {
		a.careError(c, err)
		return
	}
	if err := a.care.RecordCareAccess(c.Request.Context(), c.Param("appointmentId"), "care-plan", c.Param("appointmentId"), "viewed", "clinician", currentEmployee(c).ID, a.now().In(database.Shanghai).Truncate(time.Millisecond)); err != nil {
		a.careError(c, err)
		return
	}
	workspace, err := planWorkspaceDTO(plan, versions)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(http.StatusOK, apitypes.CarePlanWorkspaceResult{Data: workspace})
}

func careContent(body apitypes.CarePlanContent) (store.CarePlanContent, bool) {
	content := store.CarePlanContent{Summary: strings.TrimSpace(body.Summary), Instructions: strings.TrimSpace(body.Instructions), Stages: make([]store.CarePlanStage, 0, len(body.Stages))}
	if utf8.RuneCountInString(content.Summary) < 1 || utf8.RuneCountInString(content.Summary) > 1000 || utf8.RuneCountInString(content.Instructions) > 2000 || len(body.Stages) < 1 || len(body.Stages) > 8 {
		return content, false
	}
	for _, stage := range body.Stages {
		title, text := strings.TrimSpace(stage.Title), strings.TrimSpace(stage.Body)
		if utf8.RuneCountInString(title) < 1 || utf8.RuneCountInString(title) > 40 || utf8.RuneCountInString(text) < 1 || utf8.RuneCountInString(text) > 1000 {
			return content, false
		}
		content.Stages = append(content.Stages, store.CarePlanStage{Title: title, Body: text})
	}
	return content, true
}

func (a *API) saveCarePlanDraft(c *gin.Context) {
	if !validCareAppointment(c) {
		return
	}
	var body apitypes.SaveCarePlanDraftJSONRequestBody
	if !decodeObjectLimit(c, &body, []string{"version", "content"}, nil, 64<<10) {
		return
	}
	content, ok := careContent(body.Content)
	if !ok || body.Version < 0 {
		fail(c, http.StatusBadRequest, "INVALID_CARE_PLAN", "请检查方案摘要、阶段和注意事项的长度")
		return
	}
	plan, err := a.care.SaveCarePlanDraft(c.Request.Context(), c.Param("appointmentId"), int(body.Version), content, a.now().In(database.Shanghai).Truncate(time.Millisecond))
	if err != nil {
		a.careError(c, err)
		return
	}
	workspace, versions, err := a.care.AdminCarePlan(c.Request.Context(), c.Param("appointmentId"))
	if err != nil {
		a.careError(c, err)
		return
	}
	workspace.DraftVersion = plan.DraftVersion
	result, err := planWorkspaceDTO(workspace, versions)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(http.StatusOK, apitypes.CarePlanWorkspaceResult{Data: result})
}

func (a *API) publishCarePlan(c *gin.Context) {
	if !validCareAppointment(c) {
		return
	}
	var body apitypes.PublishCarePlanJSONRequestBody
	if !decodeObject(c, &body, []string{"version", "changeReason"}, nil) {
		return
	}
	reason := strings.TrimSpace(body.ChangeReason)
	if body.Version < 1 || utf8.RuneCountInString(reason) < 1 || utf8.RuneCountInString(reason) > 300 {
		fail(c, http.StatusBadRequest, "INVALID_CARE_PLAN", "请提供有效草稿版本和发布说明（最多 300 字）")
		return
	}
	row, err := a.care.PublishCarePlan(c.Request.Context(), c.Param("appointmentId"), int(body.Version), reason, a.now().In(database.Shanghai).Truncate(time.Millisecond))
	if err != nil {
		a.careError(c, err)
		return
	}
	version, err := planVersionDTO(row)
	if err != nil {
		a.unavailable(c, err)
		return
	}
	c.JSON(http.StatusOK, apitypes.CarePlanVersionResult{Data: version})
}
