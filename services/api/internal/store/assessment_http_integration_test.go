package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/httpapi"
	"example.com/bailiankai/api/internal/store"
)

func TestAssessmentMySQLHTTPSharedWorkflowAndStrictFields(t *testing.T) {
	a := newAIFixture(t)
	a.f.router = httpapi.NewWithAssessmentService(a.f.repo, a.media, a.service)
	body := fmt.Sprintf(`{"clinicId":%q,"requestKey":"http-create"}`, a.f.slot.ClinicID)
	response := a.f.http("POST", "/api/v1/me/assessments", body, a.session.Token, "")
	if response.Code != 201 {
		t.Fatal(response.Code, response.Body)
	}
	var payload struct{ Data store.AssessmentView }
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	view := payload.Data
	if repeat := a.f.http("POST", "/api/v1/me/assessments", body, a.session.Token, ""); repeat.Code != 200 {
		t.Fatal(repeat.Code, repeat.Body)
	}
	bad := fmt.Sprintf(`{"clinicId":%q,"requestKey":"spoof","patientId":"other"}`, a.f.slot.ClinicID)
	if got := a.f.http("POST", "/api/v1/me/assessments", bad, a.session.Token, ""); got.Code != 400 {
		t.Fatal(got.Code, got.Body)
	}
	path := "/api/v1/me/assessments/" + view.Task.ID
	consent := fmt.Sprintf(`{"version":%d,"policyVersion":"photo-ai-v1","granted":true}`, view.Task.Version)
	response = a.f.http("POST", path+"/consent", consent, a.session.Token, "http-consent")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body)
	}
	json.Unmarshal(response.Body.Bytes(), &payload)
	view = payload.Data
	photo, data := syntheticAssessmentPhoto(t, "front", 1)
	_ = photo
	upload := func(data []byte, extra bool) *httptest.ResponseRecorder {
		var input bytes.Buffer
		writer := multipart.NewWriter(&input)
		file, err := writer.CreateFormFile("file", "synthetic.png")
		if err != nil {
			t.Fatal(err)
		}
		file.Write(data)
		writer.WriteField("version", fmt.Sprint(view.Task.Version))
		if extra {
			writer.WriteField("patientId", "another")
		}
		writer.Close()
		req := httptest.NewRequest(http.MethodPost, path+"/photos/front", &input)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+a.session.Token)
		req.Header.Set("Idempotency-Key", "http-photo")
		w := httptest.NewRecorder()
		a.f.router.ServeHTTP(w, req)
		return w
	}
	if got := upload(data, true); got.Code != 400 {
		t.Fatal(got.Code, got.Body)
	}
	response = upload(data, false)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body)
	}
	json.Unmarshal(response.Body.Bytes(), &payload)
	view = payload.Data
	if got := a.f.http("GET", path+"/photos/"+view.Photos[0].ID+"/content", "", a.session.Token, ""); got.Code != 200 {
		t.Fatal(got.Code, got.Body)
	}
	other := a.f.patient()
	if got := a.f.http("GET", path, "", other.Token, ""); got.Code != 404 {
		t.Fatal(got.Code, got.Body)
	}
	if got := upload(bytes.Repeat([]byte("x"), 8<<20+1), false); got.Code != 413 {
		t.Fatal(got.Code, got.Body)
	}
	if got := a.f.http("POST", "/api/v1/admin/assessments/"+view.Task.ID+"/reviews", `{"version":1,"resultId":"test","decision":"approved","note":"test"}`, bookingAdminToken, "manager-review"); got.Code != 403 {
		t.Fatal(got.Code, got.Body)
	}
	if got := a.f.http("GET", "/api/v1/admin/assessments", "", bookingAdminToken, ""); got.Code != 200 {
		t.Fatal(got.Code, got.Body)
	}
}
func TestAssessmentMySQLSubmitLatencyAndResponseLossReplay(t *testing.T) {
	a := newAIFixture(t)
	view := a.prepared(t)
	a.f.router = httpapi.NewWithAssessmentService(a.f.repo, a.media, a.service)
	path := "/api/v1/me/assessments/" + view.Task.ID + "/submit"
	body := fmt.Sprintf(`{"version":%d}`, view.Task.Version)
	durations := []time.Duration{}
	for i := 0; i < 20; i++ {
		start := time.Now()
		response := a.f.http("POST", path, body, a.session.Token, "latency-submit")
		durations = append(durations, time.Since(start))
		if response.Code != 202 {
			t.Fatal(response.Code, response.Body)
		}
	}
	firstDuration := durations[0]
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[18]
	t.Logf("HTTP first submit=%s; p95=%s over 20 original/replay requests", firstDuration, p95)
	if p95 > time.Second || firstDuration > time.Second {
		t.Fatal("submit exceeded local latency target")
	}
	restored := store.NewAssessmentService(a.f.repo, a.media, assessmentProvider{}, assessmentConfig())
	fresh, err := restored.Get(context.Background(), view.Task.ID, a.patient)
	if err != nil || fresh.Task.Status != "queued" || fresh.Task.AttemptCount != 0 {
		t.Fatal(fresh, err)
	}
	var snapshots int64
	if err := a.f.db.Model(&store.AssessmentInputSnapshot{}).Where("assessment_id = ?", view.Task.ID).Count(&snapshots).Error; err != nil || snapshots != 1 {
		t.Fatal(snapshots, err)
	}
}
