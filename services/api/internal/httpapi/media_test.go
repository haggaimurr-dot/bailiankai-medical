package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"github.com/gin-gonic/gin"
)

type mediaTestStore struct {
	store.CareRepository
	store.FollowupRepository
	row                   model.CarePhoto
	revoked               bool
	checks, audits, saves int
	saveErr               error
}

func (s *mediaTestStore) CarePhotoByID(_ context.Context, _, patient string, consent bool) (model.CarePhoto, error) {
	s.checks++
	if consent && s.revoked {
		return s.row, store.ErrPhotoConsent
	}
	if patient != "" && patient != s.row.PatientID {
		return s.row, store.ErrNotFound
	}
	return s.row, nil
}
func (s *mediaTestStore) FollowupPhoto(ctx context.Context, id, patient string, consent bool) (model.FollowupPhoto, error) {
	row, err := s.CarePhotoByID(ctx, id, patient, consent)
	return model.FollowupPhoto{ID: row.ID, PatientID: row.PatientID, AppointmentID: row.AppointmentID, TaskID: "task-test", SHA256: row.SHA256, SizeBytes: row.SizeBytes}, err
}
func (s *mediaTestStore) RecordCareAccess(context.Context, string, string, string, string, string, string, time.Time) error {
	s.audits++
	return nil
}
func (s *mediaTestStore) RecordFollowupAccess(context.Context, string, string, string, string, string, time.Time) error {
	s.audits++
	return nil
}
func (s *mediaTestStore) SaveCarePhoto(_ context.Context, row model.CarePhoto, _ time.Time) (string, error) {
	s.saves++
	s.row = row
	return "", s.saveErr
}
func (s *mediaTestStore) SaveFollowupPhoto(_ context.Context, _, _ string, row model.FollowupPhoto, _ time.Time) (model.FollowupPhoto, string, error) {
	s.saves++
	return row, "", s.saveErr
}

type mediaTestBackend struct {
	store.Media
	data    []byte
	onRead  func()
	err     error
	deletes int
}

func (m *mediaTestBackend) Read(context.Context, string) ([]byte, error) {
	if m.onRead != nil {
		m.onRead()
	}
	return m.data, m.err
}
func (m *mediaTestBackend) Save(_ context.Context, _ string, data []byte) error {
	m.data = data
	return m.err
}
func (m *mediaTestBackend) Delete(context.Context, string) error { m.deletes++; return nil }

func TestPhotoAccessRecheckedAfterStorageRead(t *testing.T) {
	for _, source := range []string{"care", "followup"} {
		for _, revoked := range []bool{false, true} {
			t.Run(source+map[bool]string{true: "-revoked-during-read", false: "-allowed"}[revoked], func(t *testing.T) {
				data := []byte("photograph")
				hash := sha256.Sum256(data)
				repo := &mediaTestStore{row: model.CarePhoto{ID: "photo-test", AppointmentID: "appointment-test", PatientID: "patient-test", SHA256: hex.EncodeToString(hash[:]), SizeBytes: int64(len(data))}}
				media := &mediaTestBackend{data: data, onRead: func() { repo.revoked = revoked }}
				a := &API{care: repo, followup: repo, media: media, now: time.Now}
				r := gin.New()
				r.GET("/photos/:photoId", func(c *gin.Context) {
					c.Set("employee", model.EmployeeAccount{ID: "employee-test"})
					c.Header("Cache-Control", "no-store")
					if source == "care" {
						a.adminCarePhotoContent(c)
					} else {
						a.followupPhotoContent(c, true)
					}
				})
				response := serve(r, "GET", "/photos/photo-test", "", "", "")
				if repo.checks != 2 {
					t.Fatalf("authorization checks=%d", repo.checks)
				}
				if revoked {
					if response.Code != 403 || strings.Contains(response.Body.String(), string(data)) || repo.audits != 0 {
						t.Fatalf("revocation exposed photo %d %s audits=%d", response.Code, response.Body, repo.audits)
					}
				} else if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), data) || repo.audits != 1 || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
					t.Fatalf("authorized read %d %s", response.Code, response.Body)
				}
			})
		}
	}
}

func TestMediaFailuresAndAmbiguousDatabaseCommit(t *testing.T) {
	for _, source := range []string{"care", "followup"} {
		for _, storageFails := range []bool{false, true} {
			t.Run(source+map[bool]string{true: "-storage", false: "-commit"}[storageFails], func(t *testing.T) {
				repo := &mediaTestStore{saveErr: errors.New("commit acknowledgement lost")}
				media := &mediaTestBackend{}
				if storageFails {
					media.err = errors.New("signed private URL must not leak")
				}
				a := &API{care: repo, followup: repo, media: media, now: time.Now}
				r := gin.New()
				r.POST("/appointments/:appointmentId/photos/:angle", func(c *gin.Context) {
					c.Set("patient", model.Patient{ID: "patient-test"})
					if source == "care" {
						a.uploadCarePhoto(c)
					} else {
						c.Params = append(c.Params, gin.Param{Key: "taskId", Value: "task-test"})
						a.uploadFollowupPhoto(c)
					}
				})
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				file, _ := writer.CreateFormFile("file", "test.jpg")
				if err := jpeg.Encode(file, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
					t.Fatal(err)
				}
				writer.Close()
				request := httptest.NewRequest(http.MethodPost, "/appointments/appointment-test/photos/front", &body)
				request.Header.Set("Content-Type", writer.FormDataContentType())
				response := httptest.NewRecorder()
				r.ServeHTTP(response, request)
				if response.Code != 503 || media.deletes != 0 {
					t.Fatalf("ambiguous failure deleted bytes or wrong status %d deletes=%d", response.Code, media.deletes)
				}
				if storageFails && (repo.saves != 0 || !strings.Contains(response.Body.String(), "MEDIA_UNAVAILABLE")) {
					t.Fatalf("storage failure reached database %s", response.Body)
				}
				if !storageFails && repo.saves != 1 {
					t.Fatalf("database was not invoked")
				}
				if strings.Contains(response.Body.String(), "private URL") {
					t.Fatal("private storage detail leaked")
				}
			})
		}
	}
}

func TestCorruptPhotoNeverReturned(t *testing.T) {
	repo := &mediaTestStore{row: model.CarePhoto{ID: "photo-test", PatientID: "patient-test", SizeBytes: 99, SHA256: "bad-hash"}}
	a := &API{care: repo, media: &mediaTestBackend{data: []byte("corrupt-private-bytes")}, now: time.Now}
	r := gin.New()
	r.GET("/photos/:photoId", func(c *gin.Context) {
		c.Set("patient", model.Patient{ID: "patient-test"})
		a.patientCarePhotoContent(c)
	})
	response := serve(r, "GET", "/photos/photo-test", "", "", "")
	if response.Code != 503 || strings.Contains(response.Body.String(), "corrupt-private-bytes") || repo.audits != 0 {
		t.Fatalf("corrupt photo exposed: %d %s", response.Code, response.Body)
	}
}
