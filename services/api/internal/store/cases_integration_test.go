package store_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"example.com/bailiankai/api/internal/httpapi"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"fmt"
	"image"
	"image/jpeg"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCaseLifecycleMySQL(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	manager := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "manager", "", []string{"clinic-demo"}), "manager-case-test")
	doctor := store.WithEmployeeActor(store.WithEmployeeScope(ctx, "doctor", "doctor-demo-1", []string{"clinic-demo"}), "doctor-case-test")
	foreign := store.WithEmployeeScope(ctx, "manager", "", []string{"foreign-clinic"})
	patient, other := f.patient(), f.patient()
	appt := f.create(patient, "case-booking-test-001")
	if err := f.db.Model(&model.Appointment{}).Where("id = ?", appt.ID).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	plan := model.CarePlan{ID: "case-plan", AppointmentID: appt.ID, UpdatedAt: f.now}
	pv := model.CarePlanVersion{ID: "case-plan-version", CarePlanID: plan.ID, Version: 1, ContentJSON: "{}", PublishedBy: "test", PublishedAt: f.now}
	treatment := model.Treatment{ID: "case-treatment", AppointmentID: appt.ID, PatientID: patient.Patient.ID, PlanVersionID: pv.ID, Summary: "test", TreatedAt: f.now.Add(-time.Hour), RecordedAt: f.now, Version: 1, RequestHash: strings.Repeat("a", 64)}
	task := model.FollowupTask{ID: "case-task", AppointmentID: appt.ID, PatientID: patient.Patient.ID, TreatmentID: &treatment.ID, AssigneeID: "doctor-demo-1", AssignedAt: f.now, Title: "test", DueAt: f.now, Status: "submitted", Version: 1, SubmittedAt: &f.now}
	for _, row := range []any{&plan, &pv, &treatment, &task} {
		if err := f.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	media, err := store.NewPrivateMedia(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])
	before := model.CarePhoto{ID: "photo-case-source-before", AppointmentID: appt.ID, PatientID: patient.Patient.ID, Angle: "front", MediaType: "image/jpeg", SizeBytes: int64(len(data)), SHA256: checksum, UploadedAt: f.now.Add(-2 * time.Hour)}
	after := model.FollowupPhoto{ID: "photo-case-source-after", AppointmentID: appt.ID, PatientID: patient.Patient.ID, TaskID: task.ID, Angle: "front", SizeBytes: int64(len(data)), SHA256: checksum, UploadedAt: f.now}
	for _, row := range []any{&before, &after} {
		if err := f.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{before.ID, after.ID} {
		if err := media.Save(ctx, id, data); err != nil {
			t.Fatal(err)
		}
	}
	input := store.CaseChange{AppointmentID: appt.ID, Category: "瘢痕修复", Tags: []string{"正面", "随访"}, Title: "案例标题", Summary: "摘要", Body: "已授权正文", Pairs: []store.CasePair{{BeforeSourceID: before.ID, AfterSourceID: after.ID}}}
	if _, err := f.repo.ChangeCase(manager, "", "create", "", input, media, f.now); !errors.Is(err, store.ErrPhotoConsent) {
		t.Fatal("clinical consent missing", err)
	}
	if err := f.repo.SetPhotoConsent(ctx, appt.ID, patient.Patient.ID, true, f.now); err != nil {
		t.Fatal(err)
	}
	row, err := f.repo.ChangeCase(manager, "", "create", "", input, media, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if row.ConsentRevision != 0 {
		t.Fatal("clinical consent became display consent")
	}
	change := func(actor context.Context, action, pid string, in store.CaseChange) {
		t.Helper()
		in.Version = row.Version
		next, err := f.repo.ChangeCase(actor, row.ID, action, pid, in, media, f.now)
		if err != nil {
			t.Fatal(action, err)
		}
		row = next
	}
	expect := func(actor context.Context, action, pid string, in store.CaseChange, want error) {
		t.Helper()
		in.Version = row.Version
		_, err := f.repo.ChangeCase(actor, row.ID, action, pid, in, media, f.now)
		if !errors.Is(err, want) {
			t.Fatal(action, err, want)
		}
	}
	expect(manager, "publish", "", store.CaseChange{}, store.ErrCareState)
	expect(manager, "review", "", store.CaseChange{Decision: "approved"}, store.ErrForbidden)
	expect(foreign, "update", "", input, store.ErrNotFound)
	expect(ctx, "consent", other.Patient.ID, store.CaseChange{Granted: true, PolicyVersion: store.CasePolicyVersion}, store.ErrNotFound)
	if _, err := f.repo.CaseHistory(foreign, row.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("history scope", err)
	}
	change(doctor, "review", "", store.CaseChange{Decision: "approved"})
	expect(manager, "publish", "", store.CaseChange{}, store.ErrCareState)
	change(ctx, "consent", patient.Patient.ID, store.CaseChange{Granted: true, PolicyVersion: store.CasePolicyVersion})
	change(manager, "publish", "", store.CaseChange{})
	for _, tc := range []struct {
		category, tag string
		total         int64
	}{{"瘢痕修复", "正面", 1}, {"其他分类", "", 0}, {"", "正", 0}, {"", "随访", 1}} {
		matches, total, err := f.repo.Cases(ctx, "public", "", store.CaseFilter{Page: 1, PageSize: 1, Category: tc.category, Tag: tc.tag})
		if err != nil || total != tc.total || int64(len(matches)) != tc.total {
			t.Fatal("metadata filters", tc, total, err)
		}
	}
	for _, query := range []string{appt.ID, "%", "missing-appointment"} {
		candidates, err := f.repo.ListAppointments(manager, store.AppointmentFilter{Status: "completed", Query: query, Page: 1, PageSize: 1}, f.now)
		want := int64(0)
		if query == appt.ID {
			want = 1
		}
		if err != nil || candidates.Total != want {
			t.Fatal("candidate search", query, candidates.Total, err)
		}
	}
	candidates, err := f.repo.ListAppointments(foreign, store.AppointmentFilter{Status: "completed", Query: appt.ID, Page: 1, PageSize: 20}, f.now)
	if err != nil || candidates.Total != 0 {
		t.Fatal("candidate clinic scope", candidates.Total, err)
	}
	// Candidates beyond the old first-100 limit remain reachable through pagination.
	for i := 0; i < 105; i++ {
		candidate := appt
		candidate.ID = fmt.Sprintf("case-candidate-%03d", i)
		candidate.RequestKey = fmt.Sprintf("case-candidate-key-%03d", i)
		candidate.Status = "completed"
		if err := f.db.Create(&candidate).Error; err != nil {
			t.Fatal(err)
		}
	}
	candidatePage, err := f.repo.ListAppointments(manager, store.AppointmentFilter{Status: "completed", Query: "case-candidate-", Page: 6, PageSize: 20}, f.now)
	if err != nil || candidatePage.Total != 105 || len(candidatePage.Data) != 5 || candidatePage.Page != 6 {
		t.Fatal("candidate pagination beyond 100", candidatePage.Total, len(candidatePage.Data), err)
	}
	beyond, total, err := f.repo.Cases(ctx, "public", "", store.CaseFilter{Category: "瘢痕修复", Page: 2, PageSize: 1})
	if err != nil || total != 1 || len(beyond) != 0 {
		t.Fatal("case filter pagination", total, err)
	}
	router := httpapi.NewWithMedia(f.repo, media)
	get := func(path string, want int) string {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable response")
		}
		return w.Body.String()
	}
	body := get("/api/v1/cases/"+row.ID, 200)
	for _, secret := range []string{patient.Patient.ID, appt.ID, before.ID, after.ID, "doctor-case-test"} {
		if strings.Contains(body, secret) {
			t.Fatal("public leak", secret)
		}
	}
	imageID := row.Content.Pairs[0].BeforeImageID
	imagePath := "/api/v1/cases/" + row.ID + "/images/" + imageID + "/content"
	get(imagePath, 200)
	// Copies and their authorizations remain independent of clinical storage.
	if err := f.repo.SetPhotoConsent(ctx, appt.ID, patient.Patient.ID, false, f.now); err != nil {
		t.Fatal(err)
	}
	if err := media.Delete(ctx, before.ID); err != nil {
		t.Fatal(err)
	}
	get(imagePath, 200)
	referenced, err := f.repo.MediaReferenced(ctx, imageID)
	if err != nil || !referenced {
		t.Fatal("case image absent from catalog", err)
	}
	change(manager, "unpublish", "", store.CaseChange{})
	get(imagePath, 404)
	get("/api/v1/cases/"+row.ID, 404)
	change(manager, "publish", "", store.CaseChange{})
	// A stale withdrawal must still revoke, and grant never republishes.
	oldVersion := row.Version - 2
	row, err = f.repo.ChangeCase(ctx, row.ID, "consent", patient.Patient.ID, store.CaseChange{Version: oldVersion, Granted: false}, media, f.now)
	if err != nil {
		t.Fatal(err)
	}
	get(imagePath, 404)
	if granted, err := f.repo.PhotoConsent(ctx, appt.ID, patient.Patient.ID); err != nil || granted {
		t.Fatal("case revoke altered care consent", err)
	}
	change(ctx, "consent", patient.Patient.ID, store.CaseChange{Granted: true, PolicyVersion: store.CasePolicyVersion})
	get(imagePath, 404)
	change(manager, "publish", "", store.CaseChange{})
	// Concurrent publish and revoke serialize; neither order leaves public access.
	version := row.Version
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, e := f.repo.ChangeCase(manager, row.ID, "publish", "", store.CaseChange{Version: version}, media, f.now)
		outcomes <- e
	}()
	go func() {
		defer wg.Done()
		_, e := f.repo.ChangeCase(ctx, row.ID, "consent", patient.Patient.ID, store.CaseChange{Version: version, Granted: false}, media, f.now)
		outcomes <- e
	}()
	wg.Wait()
	close(outcomes)
	for e := range outcomes {
		if e != nil && !errors.Is(e, store.ErrConflict) {
			t.Fatal(e)
		}
	}
	get(imagePath, 404)
	row, err = f.repo.Case(manager, row.ID, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := media.Save(ctx, before.ID, data); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.SetPhotoConsent(ctx, appt.ID, patient.Patient.ID, true, f.now); err != nil {
		t.Fatal(err)
	}
	change(ctx, "consent", patient.Patient.ID, store.CaseChange{Granted: true, PolicyVersion: store.CasePolicyVersion})
	change(manager, "publish", "", store.CaseChange{})
	input.Category = "术后随访"
	input.Tags = []string{"复诊"}
	change(manager, "update", "", input)
	if row.ConsentRevision != 0 || row.ReviewStatus != "pending" || row.Revision != 2 {
		t.Fatal("edit retained authorization", row)
	}
	get(imagePath, 404)
	expect(manager, "publish", "", store.CaseChange{}, store.ErrCareState)
	// Wrong angle and unsubmitted after photos cannot be associated.
	if err := f.db.Model(&model.FollowupPhoto{}).Where("id = ?", after.ID).Update("angle", "left").Error; err != nil {
		t.Fatal(err)
	}
	expect(manager, "update", "", input, store.ErrCareState)
	if err := f.db.Model(&model.FollowupPhoto{}).Where("id = ?", after.ID).Update("angle", "front").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.FollowupTask{}).Where("id = ?", task.ID).Update("submitted_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	expect(manager, "update", "", input, store.ErrNotFound)
	events, err := f.repo.CaseHistory(manager, row.ID)
	if err != nil || len(events) < 10 {
		t.Fatal("audit history", err)
	}
	foundConsent := false
	for _, e := range events {
		if e.Action == "consent" {
			foundConsent = true
			if e.PolicyVersion != store.CasePolicyVersion || e.PolicyContent != store.CasePolicyContent || !strings.Contains(e.SnapshotJSON, "已授权正文") || !strings.Contains(e.SnapshotJSON, "瘢痕修复") || !strings.Contains(e.SnapshotJSON, "正面") {
				t.Fatal("consent snapshot absent")
			}
		}
	}
	if !foundConsent {
		t.Fatal("no consent audit")
	}
	// Backups include every retained case revision; restoring bytes cannot restore consent.
	backup, err := store.NewPrivateMedia(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	refs, err := f.repo.MediaReferences(ctx)
	if err != nil || len(refs) < 6 {
		t.Fatal("case revisions missing from backup catalog", err)
	}
	snapshot, key := store.NewMediaSnapshotID(), make([]byte, 32)
	if count, err := store.BackupMedia(ctx, media, backup, f.repo, snapshot, key); err != nil || count != len(refs) {
		t.Fatal("case backup", count, err)
	}
	currentImage := row.Content.Pairs[0].BeforeImageID
	if err := media.Delete(ctx, currentImage); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RestoreMedia(ctx, backup, media, f.repo, snapshot, key, true); err != nil {
		t.Fatal(err)
	}
	if _, err := media.Read(ctx, currentImage); err != nil {
		t.Fatal("case image was not restored", err)
	}
	get("/api/v1/cases/"+row.ID+"/images/"+currentImage+"/content", 404)
}
