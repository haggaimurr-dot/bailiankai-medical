package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPatientTimelineAndAdviceReceipts(t *testing.T) {
	f, patient, appointment, input := newFollowupFixture(t)
	ctx := context.Background()
	plan, _, publishErr := f.repo.AdminCarePlan(ctx, appointment.ID)
	if publishErr != nil {
		t.Fatal(publishErr)
	}
	draft, err := f.repo.SaveCarePlanDraft(ctx, appointment.ID, plan.DraftVersion, store.CarePlanContent{Summary: "新版方案", Stages: []store.CarePlanStage{{Title: "阶段", Body: "演示内容"}}, Instructions: "演示"}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	published, err := f.repo.PublishCarePlan(ctx, appointment.ID, draft.DraftVersion, "新版本", f.now)
	if err != nil {
		t.Fatal(err)
	}
	tasks := followupTasks(t, f, appointment, input)
	submitted, reviewed := f.now.Add(-2*time.Hour), f.now.Add(-time.Hour)
	if err := f.db.Model(&model.FollowupTask{}).Where("id = ?", tasks[0].ID).Updates(map[string]any{"submitted_at": submitted, "reviewed_at": reviewed, "symptoms": "反馈", "response": "回评", "disposition": "处理"}).Error; err != nil {
		t.Fatal(err)
	}
	manager := store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"})
	initial, err := f.repo.CreateConsultation(manager, model.Consultation{PatientID: patient.Patient.ID, ChiefComplaint: "独立问诊", History: "既往诊疗情况", Assessment: "问诊结论", Advice: "初诊医嘱", VisitedAt: f.now.Add(-4 * time.Hour), RecordedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	next := f.now.Add(7 * 24 * time.Hour)
	revisit, err := f.repo.CreateConsultation(manager, model.Consultation{PatientID: patient.Patient.ID, ChiefComplaint: "复诊", VisitType: "revisit", ParentConsultationID: &initial.ID, Advice: "复诊医嘱", VisitedAt: f.now.Add(-3 * time.Hour), NextVisitAt: &next, StageIndex: 1, StageStatus: "in_progress", RecordedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	rows, total, err := f.repo.PatientTimeline(ctx, patient.Patient.ID, 1, 100)
	if err != nil || total != 11 || len(rows) != 11 {
		t.Fatalf("timeline count=%d len=%d err=%v", total, len(rows), err)
	}
	kinds := map[string]int{}
	for i, row := range rows {
		kinds[row.Kind]++
		if i > 0 && rows[i-1].OccurredAt.Before(row.OccurredAt) {
			t.Fatal("timeline is not globally ordered")
		}
		if row.Kind == "initial" && (row.AppointmentID != "" || row.Title != initial.ChiefComplaint || row.History != initial.History || row.Summary != initial.Assessment) {
			t.Fatal("independent consultation missing")
		}
		if row.Kind == "revisit" && (row.ParentConsultationID != initial.ID || row.NextVisitAt == nil || !row.NextVisitAt.Equal(next) || row.StageIndex != 1) {
			t.Fatal("revisit linkage lost")
		}
	}
	if kinds["followup"] != 3 || len(kinds) != 8 {
		t.Fatalf("event kinds: %+v", kinds)
	}
	seen := map[string]bool{}
	for page := 1; page <= 4; page++ {
		batch, count, err := f.repo.PatientTimeline(ctx, patient.Patient.ID, page, 3)
		if err != nil || count != total {
			t.Fatal("pagination failed", err)
		}
		for _, row := range batch {
			if seen[row.ID] {
				t.Fatal("duplicate event across pages")
			}
			seen[row.ID] = true
		}
	}
	if len(seen) != int(total) {
		t.Fatal("pagination skipped events")
	}
	other := f.patient()
	empty, count, err := f.repo.PatientTimeline(ctx, other.Patient.ID, 1, 20)
	if err != nil || count != 0 || len(empty) != 0 {
		t.Fatal("other patient read records", err)
	}
	state, err := f.repo.PatientAdvice(ctx, patient.Patient.ID, "initial", initial.ID)
	if err != nil || state.AdviceVersion == "" || state.ReadAt != nil {
		t.Fatal("initial read state", err)
	}
	if _, err := f.repo.ConfirmAdviceRead(ctx, other.Patient.ID, "initial", initial.ID, state.AdviceVersion, f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("cross patient acknowledgement", err)
	}
	if _, err := f.repo.ConfirmAdviceRead(ctx, patient.Patient.ID, "initial", initial.ID, strings.Repeat("0", 64), f.now); !errors.Is(err, store.ErrConflict) {
		t.Fatal("stale version accepted", err)
	}
	receipt, err := f.repo.ConfirmAdviceRead(ctx, patient.Patient.ID, "initial", initial.ID, state.AdviceVersion, f.now)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			duplicate, err := f.repo.ConfirmAdviceRead(ctx, patient.Patient.ID, "initial", initial.ID, state.AdviceVersion, f.now.Add(time.Hour))
			if err != nil || !duplicate.ReadAt.Equal(receipt.ReadAt) {
				t.Error("duplicate changed first read time", err)
			}
		}()
	}
	wg.Wait()
	state, err = f.repo.PatientAdvice(ctx, patient.Patient.ID, "initial", initial.ID)
	if err != nil || state.ReadAt == nil || !state.ReadAt.Equal(receipt.ReadAt) {
		t.Fatal("read state did not persist", err)
	}
	// A newly published version needs its own receipt, even with identical instructions.
	planState, err := f.repo.PatientAdvice(ctx, patient.Patient.ID, "plan", input.PlanVersionID)
	if err != nil {
		t.Fatal(err)
	}
	firstAdviceReceipt, err := f.repo.ConfirmAdviceRead(ctx, patient.Patient.ID, "plan", input.PlanVersionID, planState.AdviceVersion, f.now)
	if err != nil || firstAdviceReceipt.TodosCreated != 1 {
		t.Fatalf("first plan confirmation todosCreated=%d err=%v", firstAdviceReceipt.TodosCreated, err)
	}
	// Confirming the plan advice splits its instructions into one follow-up
	// todo per line, due at the end of the confirmation day; a repeated
	// confirmation must not create duplicates.
	adviceReceipt, err := f.repo.ConfirmAdviceRead(ctx, patient.Patient.ID, "plan", input.PlanVersionID, planState.AdviceVersion, f.now)
	if err != nil || adviceReceipt.TodosCreated != 0 {
		t.Fatalf("duplicate plan confirmation created todos: %+v err=%v", adviceReceipt, err)
	}
	_, adviceTasks, err := f.repo.FollowupWorkspace(ctx, appointment.ID, patient.Patient.ID)
	if err != nil {
		t.Fatal(err)
	}
	adviceTodos := []model.FollowupTask{}
	for _, task := range adviceTasks {
		if strings.HasPrefix(task.Title, "【护理】") && task.Title == "【护理】演示" {
			adviceTodos = append(adviceTodos, task)
		}
	}
	if len(adviceTodos) != 1 {
		t.Fatalf("advice todos=%d, want 1 (tasks=%d)", len(adviceTodos), len(adviceTasks))
	}
	wantDue := time.Date(f.now.Year(), f.now.Month(), f.now.Day(), 23, 59, 59, 0, f.now.Location())
	if !adviceTodos[0].DueAt.Equal(wantDue) || adviceTodos[0].Status != "pending" {
		t.Fatalf("advice todo due=%v status=%s, want due=%v pending", adviceTodos[0].DueAt, adviceTodos[0].Status, wantDue)
	}
	newState, err := f.repo.PatientAdvice(ctx, patient.Patient.ID, "plan", published.ID)
	if err != nil || newState.ReadAt != nil {
		t.Fatal("new version inherited acknowledgement", err)
	}
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		return w
	}
	w := request("GET", "/api/v1/me/records?page=1&pageSize=100", patient.Token, "")
	requireHTTP(t, w, 200, "")
	var response struct {
		Data []store.TimelineEvent `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range response.Data {
		if row.ResourceID == initial.ID && row.ReadAt != nil {
			found = true
		}
	}
	if !found {
		t.Fatal("timeline did not include persisted receipt")
	}
	requireHTTP(t, request("GET", "/api/v1/me/records?pageSize=0", patient.Token, ""), 400, "")
	requireHTTP(t, request("GET", "/api/v1/me/records?patientId="+patient.Patient.ID, other.Token, ""), 400, "")
	path := "/api/v1/me/records/revisit/" + revisit.ID + "/read"
	requireHTTP(t, request("GET", path, other.Token, ""), 404, "")
	revisitState, err := f.repo.PatientAdvice(ctx, patient.Patient.ID, "revisit", revisit.ID)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"adviceVersion":"` + revisitState.AdviceVersion + `"}`
	requireHTTP(t, request("POST", path, other.Token, body), 404, "")
	requireHTTP(t, request("POST", path, patient.Token, body), 200, "")
}
