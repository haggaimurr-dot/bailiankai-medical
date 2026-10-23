package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/apitypes"
	"example.com/bailiankai/api/internal/httpapi"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

func newFollowupFixture(t *testing.T) (*bookingFixture, store.SessionResult, model.Appointment, store.TreatmentInput) {
	t.Helper()
	f := newBookingFixture(t)
	patient := f.patient()
	appointment := f.create(patient, "followup-test-request-0001")
	var err error
	appointment, err = f.repo.ReviewAppointment(context.Background(), appointment.ID, store.ReviewInput{Version: appointment.Version, Action: "confirm"}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	content := store.CarePlanContent{Summary: "虚构发布方案", Stages: []store.CarePlanStage{{Title: "阶段", Body: "演示内容"}}, Instructions: "演示"}
	draft, err := f.repo.SaveCarePlanDraft(context.Background(), appointment.ID, 0, content, f.now)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := f.repo.PublishCarePlan(context.Background(), appointment.ID, draft.DraftVersion, "测试发布", f.now)
	if err != nil {
		t.Fatal(err)
	}
	router, err := httpapi.NewWithMediaRoot(f.repo, bookingAdminToken, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.router = router
	return f, patient, appointment, store.TreatmentInput{Version: appointment.Version, PlanVersionID: plan.ID, Summary: "虚构实际治疗", Notes: "测试记录", TreatedAt: f.now.Add(-48 * time.Hour), Offsets: []int{1, 3, 7}}
}
func followupTasks(t *testing.T, f *bookingFixture, appointment model.Appointment, input store.TreatmentInput) []model.FollowupTask {
	t.Helper()
	if _, err := f.repo.RecordTreatment(context.Background(), appointment.ID, input, f.now); err != nil {
		t.Fatal(err)
	}
	_, tasks, err := f.repo.FollowupWorkspace(context.Background(), appointment.ID, "")
	if err != nil || len(tasks) != 3 {
		t.Fatalf("tasks=%d err=%v", len(tasks), err)
	}
	return tasks
}
func uploadFollowup(t *testing.T, f *bookingFixture, taskID, token, angle string, status int) apitypes.CarePhoto {
	t.Helper()
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 48, 48))); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "synthetic.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(pngData.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/followups/"+taskID+"/photos/"+angle, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	requireHTTP(t, w, status, "")
	var result apitypes.CarePhotoResult
	if status == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
	}
	return result.Data
}
func TestTreatmentCorrectionMySQLIntegration(t *testing.T) {
	ctx := context.Background()
	f, patient, appointment, input := newFollowupFixture(t)
	initial, err := f.repo.RecordTreatment(ctx, appointment.ID, input, f.now)
	if err != nil || initial.Version != 1 {
		t.Fatalf("initial treatment=%+v err=%v", initial, err)
	}
	_, originalTasks, err := f.repo.FollowupWorkspace(ctx, appointment.ID, patient.Patient.ID)
	if err != nil || len(originalTasks) != 3 {
		t.Fatalf("initial tasks=%d err=%v", len(originalTasks), err)
	}
	correction := store.TreatmentCorrectionInput{Version: 1, PlanVersionID: input.PlanVersionID, Summary: "更正后的治疗摘要", Notes: input.Notes, TreatedAt: input.TreatedAt.Add(-24 * time.Hour), CorrectionReason: "实际治疗日期录入错误"}
	current, err := f.repo.CorrectTreatment(ctx, appointment.ID, correction, f.now)
	if err != nil || current.Version != 2 || !current.TreatedAt.Equal(correction.TreatedAt) {
		t.Fatalf("corrected treatment=%+v err=%v", current, err)
	}
	_, tasks, err := f.repo.FollowupWorkspace(ctx, appointment.ID, patient.Patient.ID)
	if err != nil || len(tasks) != len(originalTasks) {
		t.Fatalf("corrected tasks=%d err=%v", len(tasks), err)
	}
	for i := range tasks {
		if tasks[i].ID != originalTasks[i].ID || tasks[i].Version != originalTasks[i].Version+1 || !tasks[i].DueAt.Equal(originalTasks[i].DueAt.Add(-24*time.Hour)) {
			t.Fatalf("task schedule not shifted with treatment: old=%+v new=%+v", originalTasks[i], tasks[i])
		}
	}
	revisions, err := f.repo.TreatmentRevisions(ctx, appointment.ID, patient.Patient.ID)
	if err != nil || len(revisions) != 2 || revisions[0].Version != 2 || revisions[1].Version != 1 || revisions[1].Summary != input.Summary || revisions[0].CorrectionReason != correction.CorrectionReason {
		t.Fatalf("revision history=%+v err=%v", revisions, err)
	}
	if _, err := f.repo.CorrectTreatment(ctx, appointment.ID, correction, f.now); err != nil {
		t.Fatalf("identical retry should recover existing correction: %v", err)
	}
	if _, err := f.repo.TreatmentRevisions(ctx, appointment.ID, "another-patient"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("other patient history access: %v", err)
	}
	stale := correction
	stale.Summary = "冲突修改"
	if _, err := f.repo.CorrectTreatment(ctx, appointment.ID, stale, f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale correction: %v", err)
	}
	if _, err := f.repo.SubmitFollowup(ctx, tasks[0].ID, patient.Patient.ID, store.FollowupInput{Version: tasks[0].Version, Symptoms: "已恢复", Severity: 0}, f.now); err != nil {
		t.Fatal(err)
	}
	next := correction
	next.Version = 2
	next.TreatedAt = correction.TreatedAt.Add(-time.Hour)
	next.CorrectionReason = "再次核对时间"
	if _, err := f.repo.CorrectTreatment(ctx, appointment.ID, next, f.now); !errors.Is(err, store.ErrTreatmentTimeLocked) {
		t.Fatalf("treatment time should be locked after feedback: %v", err)
	}
	next.TreatedAt = correction.TreatedAt
	next.Summary = "仅更正治疗摘要"
	updated, err := f.repo.CorrectTreatment(ctx, appointment.ID, next, f.now)
	if err != nil || updated.Version != 3 {
		t.Fatalf("summary correction after feedback=%+v err=%v", updated, err)
	}
	revisions, err = f.repo.TreatmentRevisions(ctx, appointment.ID, patient.Patient.ID)
	if err != nil || len(revisions) != 3 || revisions[2].Summary != input.Summary {
		t.Fatalf("immutable original revision=%+v err=%v", revisions, err)
	}
	endpoint := "/api/v1/admin/appointments/" + appointment.ID + "/treatment/corrections"
	requestBody, _ := json.Marshal(map[string]any{"version": 3, "planVersionId": input.PlanVersionID, "summary": "接口再次更正", "notes": input.Notes, "treatedAt": correction.TreatedAt, "correctionReason": "复核录入文字"})
	requireHTTP(t, f.http("POST", endpoint, string(requestBody), patient.Token, ""), 401, "")
	w := f.http("POST", endpoint, string(requestBody), bookingAdminToken, "")
	requireHTTP(t, w, 200, "")
	var response apitypes.FollowupWorkspaceResult
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Data.Treatment == nil || response.Data.Treatment.Version != 4 || len(response.Data.TreatmentRevisions) != 4 {
		t.Fatalf("correction HTTP response=%+v err=%v", response, err)
	}
	w = f.http("GET", "/api/v1/me/appointments/"+appointment.ID+"/followups", "", patient.Token, "")
	requireHTTP(t, w, 200, "")
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || len(response.Data.TreatmentRevisions) != 4 || response.Data.TreatmentRevisions[3].Summary != input.Summary {
		t.Fatalf("patient history response=%+v err=%v", response, err)
	}
}
func TestFollowupAssignmentMySQLIntegration(t *testing.T) {
	ctx := context.Background()
	f, patient, appointment, input := newFollowupFixture(t)
	for _, staffID := range []string{"nurse-demo-1", "doctor-demo-2"} {
		role := "doctor"
		if staffID == "nurse-demo-1" {
			role = "nurse"
		}
		if _, err := f.repo.CreateEmployee(store.WithClinicAssignment(ctx, []string{f.slot.ClinicID}), "assignment-"+staffID, "交接验收", "AssignmentTest123!", role, staffID, f.now); err != nil {
			t.Fatal(err)
		}
	}
	tasks := followupTasks(t, f, appointment, input)
	staff, err := f.repo.FollowupStaff(ctx)
	if err != nil || len(staff) != 3 || tasks[0].AssigneeID != appointment.DoctorID {
		t.Fatalf("initial staff/tasks=%+v %+v err=%v", staff, tasks, err)
	}
	assign := store.FollowupAssignmentInput{Version: tasks[0].Version, AssigneeID: "nurse-demo-1", Reason: "演示护士接手随访"}
	assigned, err := f.repo.AssignFollowup(ctx, tasks[0].ID, assign, f.now)
	if err != nil || assigned.AssigneeID != assign.AssigneeID || assigned.Version != tasks[0].Version+1 {
		t.Fatalf("assigned=%+v err=%v", assigned, err)
	}
	if _, err := f.repo.AssignFollowup(ctx, tasks[0].ID, assign, f.now); err != nil {
		t.Fatalf("identical assignment retry: %v", err)
	}
	history, err := f.repo.FollowupAssignmentHistory(ctx, tasks[0].ID)
	if err != nil || len(history) != 2 || history[0].EventType != "initial" || history[1].EventType != "assign" || history[1].ToName == "" {
		t.Fatalf("assignment history=%+v err=%v", history, err)
	}
	stale := assign
	stale.AssigneeID = "doctor-demo-2"
	if _, err := f.repo.AssignFollowup(ctx, tasks[0].ID, stale, f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale assignment: %v", err)
	}
	source := appointment.DoctorID
	leave := store.FollowupLeaveInput{Version: 1, ToStaffID: "nurse-demo-1", Reason: "测试离岗交接"}
	if _, _, err := f.repo.LeaveFollowupStaff(ctx, source, store.FollowupLeaveInput{Version: 1, ToStaffID: "missing-staff", Reason: "无效目标"}, f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing handoff target: %v", err)
	}
	left, count, err := f.repo.LeaveFollowupStaff(ctx, source, leave, f.now)
	if err != nil || left.Status != "off_duty" || count != 2 {
		t.Fatalf("leave=%+v transferred=%d err=%v", left, count, err)
	}
	_, count, err = f.repo.LeaveFollowupStaff(ctx, source, leave, f.now)
	if err != nil || count != 2 {
		t.Fatalf("leave retry transferred=%d err=%v", count, err)
	}
	staffEvents, err := f.repo.FollowupStaffEvents(ctx, source)
	if err != nil || len(staffEvents) != 1 || staffEvents[0].TransferredCount != 2 {
		t.Fatalf("staff leave history=%+v err=%v", staffEvents, err)
	}
	workspace, rows, err := f.repo.FollowupWorkspace(ctx, appointment.ID, patient.Patient.ID)
	if err != nil || workspace == nil || len(rows) != 3 {
		t.Fatalf("workspace after leave=%+v rows=%+v err=%v", workspace, rows, err)
	}
	for _, task := range rows {
		if task.AssigneeID != "nurse-demo-1" {
			t.Fatalf("active task left on off-duty staff: %+v", task)
		}
	}
	if _, err := f.repo.AssignFollowup(ctx, tasks[0].ID, store.FollowupAssignmentInput{Version: assigned.Version, AssigneeID: source, Reason: "尝试交给离岗者"}, f.now); !errors.Is(err, store.ErrStaffOffDuty) {
		t.Fatalf("off-duty assignment: %v", err)
	}
	returned, err := f.repo.ReturnFollowupStaff(ctx, source, left.Version, f.now)
	if err != nil || returned.Status != "on_duty" || returned.Version != left.Version+1 {
		t.Fatalf("return=%+v err=%v", returned, err)
	}
	staffEvents, err = f.repo.FollowupStaffEvents(ctx, source)
	if err != nil || len(staffEvents) != 2 || staffEvents[0].EventType != "return" {
		t.Fatalf("staff return history=%+v err=%v", staffEvents, err)
	}
}
func TestFollowupMySQLIntegration(t *testing.T) {
	ctx := context.Background()
	t.Run("atomic treatment concurrent replay plan snapshot and cancellation", func(t *testing.T) {
		f, patient, appointment, input := newFollowupFixture(t)
		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make(chan model.Treatment, 8)
		failures := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Go(func() {
				<-start
				row, err := f.repo.RecordTreatment(ctx, appointment.ID, input, f.now)
				results <- row
				failures <- err
			})
		}
		close(start)
		wg.Wait()
		close(results)
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		id := ""
		for row := range results {
			if id != "" && id != row.ID {
				t.Fatal("duplicate treatment")
			}
			id = row.ID
		}
		treatment, tasks, err := f.repo.FollowupWorkspace(ctx, appointment.ID, patient.Patient.ID)
		if err != nil || len(tasks) != 3 || treatment.PlanVersionID != input.PlanVersionID {
			t.Fatal(treatment, tasks, err)
		}
		var count int64
		f.db.Model(&model.FollowupEvent{}).Where("action = 'treatment-recorded'").Count(&count)
		if count != 1 {
			t.Fatalf("audits=%d", count)
		}
		altered := input
		altered.Summary = "不同记录"
		if _, err := f.repo.RecordTreatment(ctx, appointment.ID, altered, f.now); !errors.Is(err, store.ErrConflict) {
			t.Fatal("different payload must conflict", err)
		}
		if _, err := f.repo.CancelAppointment(ctx, appointment.ID, patient.Patient.ID, appointment.Version, f.now); !errors.Is(err, store.ErrTransition) && !errors.Is(err, store.ErrConflict) {
			t.Fatal("treated appointment cancellable", err)
		}
		loaded, err := f.repo.GetAppointment(ctx, appointment.ID, patient.Patient.ID, f.now)
		if err != nil {
			t.Fatal(err)
		}
		f.assertReserved(0)
		if loaded.Status != "completed" {
			t.Fatal("appointment not completed")
		}
		if !loaded.TreatmentRecorded {
			t.Fatal("treatment flag missing")
		}
		w := f.http("GET", "/api/v1/me/appointments/"+appointment.ID, "", patient.Token, "")
		requireHTTP(t, w, 200, "")
		var result apitypes.AppointmentResult
		json.Unmarshal(w.Body.Bytes(), &result)
		if result.Data.CanCancel {
			t.Fatal("UI offers cancellation after treatment")
		}
		queue, total, err := f.repo.FollowupQueue(ctx, store.FollowupFilter{PatientID: patient.Patient.ID, Status: "overdue", Page: 1, PageSize: 20, Now: f.now})
		if err != nil || total != 1 || len(queue) != 1 {
			t.Fatal(queue, total, err)
		}
		queue, total, err = f.repo.FollowupQueue(ctx, store.FollowupFilter{Page: 2, PageSize: 2, Now: f.now})
		if err != nil || total != 3 || len(queue) != 1 {
			t.Fatal(queue, total, err)
		}
	})
	t.Run("ownership published plan association and strict HTTP validation", func(t *testing.T) {
		f, patient, appointment, input := newFollowupFixture(t)
		other := f.patient()
		otherAppointment := f.create(other, "other-followup-request-0001")
		otherAppointment, err := f.repo.ReviewAppointment(ctx, otherAppointment.ID, store.ReviewInput{Version: otherAppointment.Version, Action: "confirm"}, f.now)
		if err != nil {
			t.Fatal(err)
		}
		otherDraft, err := f.repo.SaveCarePlanDraft(ctx, otherAppointment.ID, 0, store.CarePlanContent{Summary: "other", Stages: []store.CarePlanStage{{Title: "stage", Body: "body"}}}, f.now)
		if err != nil {
			t.Fatal(err)
		}
		otherPlan, err := f.repo.PublishCarePlan(ctx, otherAppointment.ID, otherDraft.DraftVersion, "other", f.now)
		if err != nil {
			t.Fatal(err)
		}
		input.PlanVersionID = otherPlan.ID
		if _, err := f.repo.RecordTreatment(ctx, appointment.ID, input, f.now); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("accepted another appointment's plan", err)
		}
		var count int64
		if err := f.db.Model(&model.Treatment{}).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("failed record left treatment", count, err)
		}
		f.assertReserved(2)
		input.PlanVersionID = "draft-not-published"
		if _, err := f.repo.RecordTreatment(ctx, appointment.ID, input, f.now); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
		input.PlanVersionID = ""
		if _, err := f.repo.RecordTreatment(ctx, appointment.ID, input, f.now); err == nil {
			t.Fatal("missing plan accepted")
		}
		requireHTTP(t, f.http("GET", "/api/v1/me/appointments/"+appointment.ID+"/followups", "", other.Token, ""), 404, "")
		requireHTTP(t, f.http("GET", "/api/v1/admin/followups", "", patient.Token, ""), 401, "")
		requireHTTP(t, f.http("GET", "/api/v1/me/followups", "", bookingAdminToken, ""), 401, "")
		requireHTTP(t, f.http("GET", "/api/v1/admin/followups?status=invalid", "", bookingAdminToken, ""), 400, "")
		requireHTTP(t, f.http("GET", "/api/v1/admin/followups?pageSize=101", "", bookingAdminToken, ""), 400, "")
		body := `{"version":2,"planVersionId":"valid","summary":"demo","notes":"","treatedAt":"2099-01-01T00:00:00+08:00","offsets":[1]}`
		requireHTTP(t, f.http("POST", "/api/v1/admin/appointments/"+appointment.ID+"/treatment", body, bookingAdminToken, ""), 400, "")
		requireHTTP(t, f.http("POST", "/api/v1/admin/appointments/"+appointment.ID+"/treatment", `{}`, bookingAdminToken, ""), 400, "")
	})
	t.Run("submission recovery alerts explicit handling and concurrent review", func(t *testing.T) {
		f, patient, appointment, input := newFollowupFixture(t)
		tasks := followupTasks(t, f, appointment, input)
		other := f.patient()
		normal := store.FollowupInput{Version: 1, Symptoms: "没有不适", Severity: 0}
		if _, err := f.repo.SubmitFollowup(ctx, tasks[0].ID, other.Patient.ID, normal, f.now); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
		row, err := f.repo.SubmitFollowup(ctx, tasks[0].ID, patient.Patient.ID, normal, f.now)
		if err != nil || row.Status != "submitted" || row.Version != 2 {
			t.Fatal(row, err)
		}
		replay, err := f.repo.SubmitFollowup(ctx, tasks[0].ID, patient.Patient.ID, normal, f.now)
		if err != nil || replay.Version != 2 {
			t.Fatal(replay, err)
		}
		edited := normal
		edited.Symptoms = "修改反馈"
		if _, err := f.repo.SubmitFollowup(ctx, tasks[0].ID, patient.Patient.ID, edited, f.now); !errors.Is(err, store.ErrConflict) {
			t.Fatal(err)
		}
		if _, err := f.repo.ReviewFollowup(ctx, tasks[1].ID, store.FollowupReviewInput{Version: 1, Action: "review", Response: "尚未提交"}, f.now); !errors.Is(err, store.ErrCareState) {
			t.Fatal(err)
		}
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, text := range []string{"回评甲", "回评乙"} {
			wg.Go(func() {
				<-start
				_, err := f.repo.ReviewFollowup(ctx, tasks[0].ID, store.FollowupReviewInput{Version: 2, Action: "review", Response: text}, f.now)
				errs <- err
			})
		}
		close(start)
		wg.Wait()
		close(errs)
		success, conflict := 0, 0
		for err := range errs {
			if err == nil {
				success++
			} else if errors.Is(err, store.ErrConflict) {
				conflict++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatal(success, conflict)
		}
		row, err = f.repo.SubmitFollowup(ctx, tasks[1].ID, patient.Patient.ID, store.FollowupInput{Version: 1, Symptoms: "明显不适（虚构）", Severity: 2}, f.now)
		if err != nil || row.Status != "alert" {
			t.Fatal(row, err)
		}
		if _, err := f.repo.ReviewFollowup(ctx, row.ID, store.FollowupReviewInput{Version: 2, Action: "review", Response: "直接回评"}, f.now); !errors.Is(err, store.ErrCareState) {
			t.Fatal(err)
		}
		if _, err := f.repo.ReviewFollowup(ctx, row.ID, store.FollowupReviewInput{Version: 2, Action: "handle", Response: "无处理记录"}, f.now); !errors.Is(err, store.ErrCareState) {
			t.Fatal(err)
		}
		review := store.FollowupReviewInput{Version: 2, Action: "handle", Response: "虚构医护回评", Disposition: "已联系并安排演示复诊"}
		handled, err := f.repo.ReviewFollowup(ctx, row.ID, review, f.now)
		if err != nil || handled.Status != "handled" {
			t.Fatal(handled, err)
		}
		handled, err = f.repo.ReviewFollowup(ctx, row.ID, review, f.now)
		if err != nil || handled.Version != 3 {
			t.Fatal(handled, err)
		}
		concerned, err := f.repo.SubmitFollowup(ctx, tasks[2].ID, patient.Patient.ID, store.FollowupInput{Version: 1, Symptoms: "请求关注", Severity: 0, Concern: true}, f.now)
		if err != nil || concerned.Status != "alert" {
			t.Fatal(concerned, err)
		}
		w := f.http("GET", "/api/v1/me/appointments/"+appointment.ID+"/followups", "", patient.Token, "")
		requireHTTP(t, w, 200, "虚构医护回评")
		w = f.http("GET", "/api/v1/me/followups", "", other.Token, "")
		requireHTTP(t, w, 200, "")
		var list apitypes.FollowupTaskList
		json.Unmarshal(w.Body.Bytes(), &list)
		if len(list.Data) != 0 {
			t.Fatal("other patient sees tasks")
		}
	})
	t.Run("task photos frozen on submit consent revocation and prior authorization version", func(t *testing.T) {
		f, patient, appointment, input := newFollowupFixture(t)
		tasks := followupTasks(t, f, appointment, input)
		other := f.patient()
		uploadFollowup(t, f, tasks[0].ID, patient.Token, "front", 403)
		if err := f.repo.SetPhotoConsent(ctx, appointment.ID, patient.Patient.ID, true, f.now); err != nil {
			t.Fatal(err)
		}
		// Old M2 authorization cannot silently expand to the new followup purpose.
		if err := f.db.Model(&model.PhotoConsentEvent{}).Where("appointment_id = ?", appointment.ID).Update("version", "photo-care-v1").Error; err != nil {
			t.Fatal(err)
		}
		uploadFollowup(t, f, tasks[0].ID, patient.Token, "front", 403)
		if err := f.repo.SetPhotoConsent(ctx, appointment.ID, patient.Patient.ID, true, f.now.Add(time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		photo := uploadFollowup(t, f, tasks[0].ID, patient.Token, "front", 200)
		requireHTTP(t, f.http("GET", "/api/v1/admin/followup-photos/"+photo.Id+"/content", "", bookingAdminToken, ""), 200, "")
		requireHTTP(t, f.http("GET", "/api/v1/me/followup-photos/"+photo.Id+"/content", "", other.Token, ""), 404, "")
		uploadFollowup(t, f, tasks[0].ID, other.Token, "left", 404)
		photo = uploadFollowup(t, f, tasks[0].ID, patient.Token, "front", 200)
		if _, err := f.repo.SubmitFollowup(ctx, tasks[0].ID, patient.Patient.ID, store.FollowupInput{Version: 1, Symptoms: "虚构情况", Severity: 0}, f.now); err != nil {
			t.Fatal(err)
		}
		uploadFollowup(t, f, tasks[0].ID, patient.Token, "front", 409)
		second := uploadFollowup(t, f, tasks[1].ID, patient.Token, "front", 200)
		if second.Id == photo.Id {
			t.Fatal("tasks share photo")
		}
		if err := f.repo.SetPhotoConsent(ctx, appointment.ID, patient.Patient.ID, false, f.now.Add(2*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		requireHTTP(t, f.http("GET", "/api/v1/admin/followup-photos/"+photo.Id+"/content", "", bookingAdminToken, ""), 403, "")
		requireHTTP(t, f.http("GET", "/api/v1/me/followup-photos/"+photo.Id+"/content", "", patient.Token, ""), 200, "")
		photos, granted, err := f.repo.FollowupPhotos(ctx, tasks[0].ID, "", true)
		if err != nil || granted || len(photos) != 0 {
			t.Fatal(photos, granted, err)
		}
		photos, _, err = f.repo.FollowupPhotos(ctx, tasks[0].ID, patient.Patient.ID, false)
		if err != nil || len(photos) != 1 || photos[0].ID != photo.Id {
			t.Fatal(photos, err)
		}
	})
}
