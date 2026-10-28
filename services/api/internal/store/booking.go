package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrUnauthenticated   = errors.New("patient session invalid or expired")
	ErrIdempotency       = errors.New("idempotency key reused with different request")
	ErrActiveAppointment = errors.New("patient already has an active appointment for this slot")
	ErrSlotFull          = errors.New("slot full")
	ErrSlotClosed        = errors.New("slot closed")
	ErrTransition        = errors.New("appointment transition is not allowed")
)

type SessionResult struct {
	Token     string
	ExpiresAt time.Time
	Patient   model.Patient
}
type AppointmentInput struct {
	Waitlist     bool   `json:"waitlist,omitempty"`
	SlotID       string `json:"slotId"`
	ContactName  string `json:"contactName"`
	ContactPhone string `json:"contactPhone"`
	Note         string `json:"note"`
}
type AppointmentFilter struct {
	ClinicID        string
	Arrival         string
	NeedsReschedule bool
	Query           string
	PatientID       string
	Status          string
	Date            time.Time
	DoctorID        string
	Page            int
	PageSize        int
}
type AppointmentPage struct {
	Data     []model.Appointment
	Page     int
	PageSize int
	Total    int64
}
type ReviewInput struct {
	Action  string
	Reason  string
	Version int
}

type BookingRepository interface {
	NewPatientSession(context.Context, time.Time) (SessionResult, error)
	AuthenticatePatient(context.Context, string, time.Time) (model.Patient, error)
	CreateAppointment(context.Context, string, string, AppointmentInput, time.Time) (model.Appointment, bool, error)
	AppointmentByRequest(context.Context, string, string, time.Time) (model.Appointment, error)
	GetAppointment(context.Context, string, string, time.Time) (model.Appointment, error)
	ListAppointments(context.Context, AppointmentFilter, time.Time) (AppointmentPage, error)
	CancelAppointment(context.Context, string, string, int, time.Time) (model.Appointment, error)
	ReviewAppointment(context.Context, string, ReviewInput, time.Time) (model.Appointment, error)
	RemindAppointments(context.Context, []string, time.Time) ([]AppointmentRemindResult, error)
	ExpirePending(context.Context, time.Time) (int, error)
}

func randomID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func NormalizeAppointmentInput(in AppointmentInput) AppointmentInput {
	in.SlotID = strings.TrimSpace(in.SlotID)
	in.ContactName = strings.TrimSpace(in.ContactName)
	in.ContactPhone = strings.TrimSpace(in.ContactPhone)
	in.Note = strings.TrimSpace(in.Note)
	return in
}
func requestHash(in AppointmentInput) string {
	b, _ := json.Marshal(NormalizeAppointmentInput(in))
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *MySQL) NewPatientSession(ctx context.Context, now time.Time) (SessionResult, error) {
	now = now.In(database.Shanghai).Truncate(time.Millisecond)
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return SessionResult{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	patient := model.Patient{ID: randomID("patient-"), DisplayName: "演示访客", CreatedAt: now}
	session := model.PatientSession{TokenHash: tokenHash(token), PatientID: patient.ID, ExpiresAt: now.Add(7 * 24 * time.Hour), CreatedAt: now}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&patient).Error; err != nil {
			return err
		}
		return tx.Create(&session).Error
	})
	if err != nil {
		return SessionResult{}, err
	}
	return SessionResult{Token: token, ExpiresAt: session.ExpiresAt, Patient: patient}, nil
}

func (s *MySQL) AuthenticatePatient(ctx context.Context, token string, now time.Time) (model.Patient, error) {
	var patient model.Patient
	// Only generated 32-byte URL-safe tokens are accepted; admin bearer is separate.
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return patient, ErrUnauthenticated
	}
	err = s.db.WithContext(ctx).Table("patients").Select("patients.*").Joins("JOIN sessions ON sessions.patient_id = patients.id").Where("sessions.token_hash = ? AND sessions.expires_at > ?", tokenHash(token), now).First(&patient).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return patient, ErrUnauthenticated
	}
	return patient, err
}

func lockPatient(tx *gorm.DB, id string) error {
	var patient model.Patient
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&patient, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUnauthenticated
		}
		return err
	}
	return nil
}
func lockSlot(tx *gorm.DB, id string) (model.Slot, error) {
	var slot model.Slot
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&slot, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrNotFound
	}
	return slot, err
}
func appointmentQuery(db *gorm.DB) *gorm.DB {
	return db.Model(&model.Appointment{}).Select("appointments.*, slots.clinic_id, COALESCE(appointments.clinic_name_snapshot, clinics.name) AS clinic_name, slots.doctor_id, COALESCE(appointments.doctor_name_snapshot, doctors.name) AS doctor_name, slots.starts_at, slots.ends_at, slots.status AS slot_status, COALESCE((SELECT reason FROM schedule_events WHERE slot_id = slots.id AND action = 'suspended' ORDER BY created_at DESC, id DESC LIMIT 1), '') AS suspension_reason, EXISTS(SELECT 1 FROM treatments WHERE treatments.appointment_id = appointments.id) AS treatment_recorded").Joins("JOIN slots ON slots.id = appointments.slot_id").Joins("JOIN clinics ON clinics.id = slots.clinic_id").Joins("JOIN doctors ON doctors.id = slots.doctor_id")
}
func loadAppointment(db *gorm.DB, id, patientID string) (model.Appointment, error) {
	var result model.Appointment
	q := appointmentQuery(db).Where("appointments.id = ?", id)
	if patientID != "" {
		q = q.Where("appointments.patient_id = ?", patientID)
	}
	err := q.First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrNotFound
	}
	return result, err
}
func writeEvent(tx *gorm.DB, a model.Appointment, actorType, actorID, action, from string, now time.Time) error {
	return tx.Create(&model.AppointmentEvent{ID: randomID("event-"), AppointmentID: a.ID, ActorType: actorType, ActorID: actorID, Action: action, FromStatus: from, ToStatus: a.Status, Detail: a.RejectionReason, CreatedAt: now}).Error
}

func (s *MySQL) CreateAppointment(ctx context.Context, patientID, key string, input AppointmentInput, now time.Time) (model.Appointment, bool, error) {
	requestStarted := time.Now()
	requestNow := now
	if _, err := s.ExpirePending(ctx, now); err != nil {
		return model.Appointment{}, false, err
	}
	in := NormalizeAppointmentInput(input)
	hash := requestHash(in)
	now = now.In(database.Shanghai).Truncate(time.Millisecond)
	var appointment model.Appointment
	replayed := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPatient(tx, patientID); err != nil {
			return err
		}
		// The patient lock serializes this patient's requests. A plain read avoids
		// gap locks on absent keys, which could deadlock different patients.
		err := tx.Where("patient_id = ? AND request_key = ?", patientID, key).First(&appointment).Error
		if err == nil {
			if appointment.RequestHash != hash {
				return ErrIdempotency
			}
			replayed = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		slot, err := lockSlot(tx, in.SlotID)
		if err != nil {
			return err
		}
		var active model.Appointment
		err = tx.Where("patient_id = ? AND active_slot_id = ?", patientID, in.SlotID).First(&active).Error
		if err == nil {
			return ErrActiveAppointment
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// Account for time spent waiting for row locks. The caller can supply a
		// test clock without permitting requests to cross a real deadline.
		now = requestNow.Add(time.Since(requestStarted)).In(database.Shanghai).Truncate(time.Millisecond)
		if !slot.StartsAt.After(now) {
			return ErrPast
		}
		// Lock profiles until commit so disable and booking have a defined order.
		var doctor model.Doctor
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&doctor, "id = ?", slot.DoctorID).Error; err != nil {
			return err
		}
		var clinic model.Clinic
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&clinic, "id = ?", slot.ClinicID).Error; err != nil {
			return err
		}
		if !doctor.Active || doctor.PublishedJSON == nil || !clinic.Active || clinic.PublishedJSON == nil {
			return ErrSlotClosed
		}
		var publishedDoctor model.Doctor
		var publishedClinic model.Clinic
		if err := json.Unmarshal([]byte(*doctor.PublishedJSON), &publishedDoctor); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(*clinic.PublishedJSON), &publishedClinic); err != nil {
			return err
		}
		now = requestNow.Add(time.Since(requestStarted)).In(database.Shanghai).Truncate(time.Millisecond)
		if !slot.StartsAt.After(now) {
			return ErrPast
		}
		if slot.Status != "open" {
			return ErrSlotClosed
		}
		// A live waitlist offer holds the vacancy for its patient: direct
		// bookings by anyone else see the slot as full. Waitlist
		// registrations themselves always queue and are never blocked here.
		if !in.Waitlist {
			if offer, err := activeWaitlistOffer(tx, slot.ID, now); err != nil {
				return err
			} else if offer != nil && offer.PatientID != patientID {
				return ErrSlotFull
			}
		}
		if !in.Waitlist && slot.Reserved >= slot.Capacity {
			return ErrSlotFull
		}
		if in.Waitlist && (slot.Capacity == 0 || slot.Reserved < slot.Capacity) {
			return ErrTransition
		}
		deadline := now.Add(s.pendingConfirmation)
		if slot.StartsAt.Before(deadline) {
			deadline = slot.StartsAt
		}
		appointment = model.Appointment{ID: randomID("appointment-"), PatientID: patientID, SlotID: slot.ID, ContactName: in.ContactName, ContactPhone: in.ContactPhone, Note: in.Note, Status: "pending", RejectionReason: "", ConfirmationDeadline: deadline, RequestKey: key, RequestHash: hash, CreatedAt: now, UpdatedAt: now, Version: 1}
		if in.Waitlist {
			appointment.Status = "waiting"
			appointment.ConfirmationDeadline = slot.StartsAt
		}
		appointment.DoctorNameSnapshot = &publishedDoctor.Name
		appointment.ClinicNameSnapshot = &publishedClinic.Name
		if err := tx.Create(&appointment).Error; err != nil {
			return err
		}
		if in.Waitlist {
			return writeEvent(tx, appointment, "patient", patientID, "waitlist", "", now)
		}
		result := tx.Model(&model.Slot{}).Where("id = ? AND reserved < capacity", slot.ID).Updates(map[string]any{"reserved": gorm.Expr("reserved + 1"), "version": gorm.Expr("version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrSlotFull
		}
		return writeEvent(tx, appointment, "patient", patientID, "submit", "", now)
	})
	if err != nil {
		return model.Appointment{}, false, err
	}
	if replayed {
		if _, err := s.ExpirePending(ctx, requestNow.Add(time.Since(requestStarted))); err != nil {
			return model.Appointment{}, false, err
		}
	}
	appointment, err = loadAppointment(s.db.WithContext(ctx), appointment.ID, patientID)
	return appointment, replayed, err
}

func (s *MySQL) GetAppointment(ctx context.Context, id, patientID string, now time.Time) (model.Appointment, error) {
	if patientID == "" && employeeScoped(ctx) {
		allowed, err := s.CanAccessAppointment(ctx, id, false)
		if err != nil {
			return model.Appointment{}, err
		}
		if !allowed {
			return model.Appointment{}, ErrNotFound
		}
	}
	if _, err := s.ExpirePending(ctx, now); err != nil {
		return model.Appointment{}, err
	}
	return loadAppointment(s.db.WithContext(ctx), id, patientID)
}

func (s *MySQL) AppointmentByRequest(ctx context.Context, patientID, key string, now time.Time) (model.Appointment, error) {
	if _, err := s.ExpirePending(ctx, now); err != nil {
		return model.Appointment{}, err
	}
	var result model.Appointment
	err := appointmentQuery(s.db.WithContext(ctx)).Where("appointments.patient_id = ? AND appointments.request_key = ?", patientID, key).First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrNotFound
	}
	return result, err
}

func (s *MySQL) ListAppointments(ctx context.Context, f AppointmentFilter, now time.Time) (AppointmentPage, error) {
	if _, err := s.ExpirePending(ctx, now); err != nil {
		return AppointmentPage{}, err
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 20
	}
	if f.PageSize > 100 {
		f.PageSize = 100
	}
	result := AppointmentPage{Data: []model.Appointment{}, Page: f.Page, PageSize: f.PageSize}
	q := clinicScope(ctx, appointmentQuery(s.db.WithContext(ctx)), "slots.clinic_id")
	if f.PatientID != "" {
		q = q.Where("appointments.patient_id = ?", f.PatientID)
	}
	if staffID := clinicianStaff(ctx); staffID != "" {
		q = q.Where("(slots.doctor_id = ? OR EXISTS (SELECT 1 FROM followup_tasks access_task WHERE access_task.appointment_id = appointments.id AND access_task.assignee_id = ?))", staffID, staffID)
	}
	if f.ClinicID != "" {
		q = q.Where("slots.clinic_id = ?", f.ClinicID)
	}
	if f.Arrival == "arrived" {
		q = q.Where("appointments.arrived_at IS NOT NULL")
	}
	if f.Arrival == "expected" {
		q = q.Where("appointments.arrived_at IS NULL AND appointments.status = ?", "confirmed")
	}
	if f.NeedsReschedule {
		q = q.Where("slots.status = ? AND appointments.status IN ? AND appointments.arrived_at IS NULL", "closed", []string{"pending", "confirmed"})
	}
	if f.Status != "" {
		q = q.Where("appointments.status = ?", f.Status)
	}
	if f.Query != "" {
		q = q.Where("(LOCATE(?, appointments.id) > 0 OR LOCATE(?, appointments.contact_name) > 0 OR LOCATE(?, appointments.contact_phone) > 0)", f.Query, f.Query, f.Query)
	}
	if f.DoctorID != "" {
		q = q.Where("slots.doctor_id = ?", f.DoctorID)
	}
	if !f.Date.IsZero() {
		q = q.Where("slots.starts_at >= ? AND slots.starts_at < ?", f.Date, f.Date.AddDate(0, 0, 1))
	}
	if err := q.Count(&result.Total).Error; err != nil {
		return result, err
	}
	err := q.Order("appointments.created_at DESC, appointments.id DESC").Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).Find(&result.Data).Error
	return result, err
}

func releaseReservation(tx *gorm.DB, slotID string) error {
	result := tx.Model(&model.Slot{}).Where("id = ? AND reserved > 0", slotID).Updates(map[string]any{"reserved": gorm.Expr("reserved - 1"), "version": gorm.Expr("version + 1")})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("reservation counter invariant violated")
	}
	return nil
}

func persistTransition(tx *gorm.DB, a *model.Appointment, to, reason, actorType, actorID, action string, release bool, now time.Time) error {
	from := a.Status
	if release {
		if err := releaseReservation(tx, a.SlotID); err != nil {
			return err
		}
	}
	result := tx.Model(&model.Appointment{}).Where("id = ? AND version = ?", a.ID, a.Version).Updates(map[string]any{"status": to, "rejection_reason": reason, "version": a.Version + 1, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrConflict
	}
	a.Status, a.RejectionReason, a.Version, a.UpdatedAt = to, reason, a.Version+1, now
	if err := writeEvent(tx, *a, actorType, actorID, action, from, now); err != nil {
		return err
	}
	return appointmentStatusMessage(tx, a, to, reason, "", now)
}

// Mutations use the same patient -> slot -> appointment locking order.
// Reading immutable ownership first does not lock another row or disclose it.
func (s *MySQL) transition(ctx context.Context, id, patientID string, version int, action, reason string, now time.Time) (model.Appointment, error) {
	requestStarted := time.Now()
	requestNow := now
	if _, err := s.ExpirePending(ctx, now); err != nil {
		return model.Appointment{}, err
	}
	initial, err := loadAppointment(s.db.WithContext(ctx), id, patientID)
	if err != nil {
		return model.Appointment{}, err
	}
	var row model.Appointment
	var businessErr error
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPatient(tx, initial.PatientID); err != nil {
			return err
		}
		slot, err := lockSlot(tx, initial.SlotID)
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		now = requestNow.Add(time.Since(requestStarted)).In(database.Shanghai).Truncate(time.Millisecond)
		if now.Before(row.UpdatedAt) {
			now = row.UpdatedAt
		}
		if row.SlotID != slot.ID {
			return ErrConflict
		}
		if row.Status == "pending" && !row.ConfirmationDeadline.After(now) {
			if err := persistTransition(tx, &row, "expired", "", "system", "expiry-worker", "expire", true, now); err != nil {
				return err
			}
			businessErr = ErrTransition
			return nil // Commit the expiry even though the requested action failed.
		}
		if row.Version != version {
			return ErrConflict
		}
		if !slot.StartsAt.After(now) {
			return ErrTransition
		}
		switch action {
		case "cancel":
			if row.ArrivedAt != nil {
				return ErrTransition
			}
			var treatmentCount int64
			if err := tx.Model(&model.Treatment{}).Where("appointment_id = ?", row.ID).Count(&treatmentCount).Error; err != nil {
				return err
			}
			if treatmentCount > 0 {
				return ErrTransition
			}
			if row.Status != "waiting" && row.Status != "pending" && row.Status != "confirmed" {
				return ErrTransition
			}
			if err := persistTransition(tx, &row, "cancelled", "", "patient", patientID, "cancel", row.Status != "waiting", now); err != nil {
				return err
			}
			// A cancellation frees (or un-holds) the seat: offer it to the
			// earliest waiter in this same transaction so no direct booking
			// can take it first. Settle failures only log; they must never
			// fail the cancellation.
			if _, _, err := settleWaitlistSlotTx(tx, row.SlotID, now); err != nil {
				slog.Error("waitlist settle after cancel failed", "slot", row.SlotID, "error", err)
			}
			return nil
		case "confirm", "reject":
			if (action == "confirm" && slot.Status != "open") || row.Status != "pending" || !row.ConfirmationDeadline.After(now) {
				return ErrTransition
			}
			to := "confirmed"
			if action == "reject" {
				to = "rejected"
			}
			if err := persistTransition(tx, &row, to, reason, "operator", employeeActor(ctx), action, action == "reject", now); err != nil {
				return err
			}
			// A rejection frees the seat: offer it to the earliest waiter in
			// this same transaction so no direct booking can take it first.
			// Settle failures only log; they must never fail the review.
			if action == "reject" {
				if _, _, err := settleWaitlistSlotTx(tx, row.SlotID, now); err != nil {
					slog.Error("waitlist settle after reject failed", "slot", row.SlotID, "error", err)
				}
			}
			return nil
		default:
			return ErrTransition
		}
	})
	if err != nil {
		return model.Appointment{}, err
	}
	if businessErr != nil {
		return model.Appointment{}, businessErr
	}
	return loadAppointment(s.db.WithContext(ctx), id, patientID)
}

func (s *MySQL) CancelAppointment(ctx context.Context, id, patientID string, version int, now time.Time) (model.Appointment, error) {
	return s.transition(ctx, id, patientID, version, "cancel", "", now)
}
func (s *MySQL) ReviewAppointment(ctx context.Context, id string, in ReviewInput, now time.Time) (model.Appointment, error) {
	if scope, ok := scopeFrom(ctx); ok && scope.Role != "manager" && scope.Role != "customer_service" {
		return model.Appointment{}, ErrForbidden
	}
	if employeeScoped(ctx) {
		allowed, err := s.CanAccessAppointment(ctx, id, false)
		if err != nil {
			return model.Appointment{}, err
		}
		if !allowed {
			return model.Appointment{}, ErrNotFound
		}
	}
	if in.Action == "confirm" {
		in.Reason = ""
	}
	return s.transition(ctx, id, "", in.Version, in.Action, strings.TrimSpace(in.Reason), now)
}

func (s *MySQL) expireOne(ctx context.Context, initial model.Appointment, now time.Time) (bool, error) {
	expired := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPatient(tx, initial.PatientID); err != nil {
			return err
		}
		if _, err := lockSlot(tx, initial.SlotID); err != nil {
			return err
		}
		var row model.Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", initial.ID).Error; err != nil {
			return err
		}
		if row.SlotID != initial.SlotID || (row.Status != "pending" && row.Status != "waiting") || row.ConfirmationDeadline.After(now) {
			return nil
		}
		if err := persistTransition(tx, &row, "expired", "", "system", "expiry-worker", "expire", row.Status == "pending", now); err != nil {
			return err
		}
		// A pending expiry frees the seat; a waiting expiry drops its offer
		// hold. Either way the slot's waitlist needs settling in this same
		// transaction. Failures only log.
		if _, _, err := settleWaitlistSlotTx(tx, row.SlotID, now); err != nil {
			slog.Error("waitlist settle after expiry failed", "slot", row.SlotID, "error", err)
		}
		expired = true
		return nil
	})
	return expired, err
}

func (s *MySQL) ExpirePending(ctx context.Context, now time.Time) (int, error) {
	total := 0
	for {
		var candidates []model.Appointment
		if err := s.db.WithContext(ctx).Select("id", "patient_id", "slot_id").Where("status IN ? AND confirmation_deadline <= ?", []string{"pending", "waiting"}, now).Order("confirmation_deadline, id").Limit(100).Find(&candidates).Error; err != nil {
			return total, err
		}
		if len(candidates) == 0 {
			return total, nil
		}
		for _, candidate := range candidates {
			expired, err := s.expireOne(ctx, candidate, now)
			if err != nil {
				return total, err
			}
			if expired {
				total++
			}
		}
	}
}

// Expiry state lives in MySQL. Startup and every tick recover deadlines missed
// while the process was stopped. Request paths perform the same cleanup.
func (s *MySQL) RunExpiryWorker(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		batchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		count, err := s.ExpirePending(batchCtx, time.Now().In(database.Shanghai))
		cancel()
		if err != nil && ctx.Err() == nil {
			slog.Error("appointment expiry failed", "error", err)
		}
		if count > 0 {
			slog.Info("pending appointments expired", "count", count)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *MySQL) SetPendingConfirmation(d time.Duration) error {
	if d < time.Minute || d > 24*time.Hour {
		return fmt.Errorf("pending confirmation must be between 1 and 1440 minutes")
	}
	s.pendingConfirmation = d
	return nil
}
