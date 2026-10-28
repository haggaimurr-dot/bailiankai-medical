package store

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 候补限时确认（新增-P1：改期与候补）。
//
// 已有模型：满员时段可登记候补，生成一条 status=waiting、不占名额的预约；
// 患者可通过改期把 waiting 预约 claim 成待确认。本文件补上缺失的一环：
// 空位释放时自动通知最早一位候补者，并给其一段限时独占窗口：
//   - offer 本身不占名额（reserved 只计真实预约，旧测试“waiting 不占号”
//     的语义不变）；独占靠准入拦截实现：offer 有效期内，非持有者的直接
//     预约与改期 claim 都返回 SLOT_FULL（见 CreateAppointment 与
//     RescheduleAppointment 内的检查），持有者用确认或原改期 claim 占座；
//   - 超时未确认或候补预约失效时，offer 过期，同一事务内按轮询顺序顺延
//     给下一位候补者（已 lapse 者排在未 lapse 者之后）；超时者收到一条
//     “已顺延”站内消息。
// 触发点：预约取消 / 工作人员拒绝 / 待确认过期（见 transition 与 expireOne
// 内的 settle 调用，失败只记日志，绝不反噬主流程）。

const waitlistHoldWindow = 2 * time.Hour

type WaitlistRepository interface {
	WaitlistEntries(context.Context, string, time.Time) ([]WaitlistEntry, error)
	WaitlistQueue(context.Context, string, time.Time) ([]WaitlistEntry, error)
	ConfirmWaitlist(context.Context, string, string, time.Time) (model.Appointment, error)
	SweepWaitlist(context.Context, time.Time) (WaitlistSweepResult, error)
}

// WaitlistEntry is one waiting appointment plus its active offer, if any.
type WaitlistEntry struct {
	Appointment model.Appointment
	Offer       *model.WaitlistOffer
}

type WaitlistSweepResult struct {
	Expired int
	Offered int
}

// settleWaitlistSlotTx expires stale offers for one slot (timed out, or whose
// appointment is no longer waiting) and — if the slot then has a vacancy and
// no active offer — offers it to the earliest waiting patient. Offers never
// touch the reserved count: exclusivity is enforced at booking/claim time,
// so "waiting consumes no reservation" keeps holding. The caller must hold no
// conflicting locks; the slot row lock serializes concurrent settles for the
// same slot. It reports how many offers it expired and whether it created a
// new offer.
func settleWaitlistSlotTx(tx *gorm.DB, slotID string, now time.Time) (offered bool, expired int, err error) {
	slot, err := lockSlot(tx, slotID)
	if err != nil {
		return false, 0, err
	}
	var offers []model.WaitlistOffer
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("slot_id = ? AND status = ?", slotID, "offered").
		Order("created_at, id").Find(&offers).Error; err != nil {
		return false, 0, err
	}
	for _, offer := range offers {
		var appt model.Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&appt, "id = ?", offer.AppointmentID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// Appointment gone: offer is stale, drop it below as expired.
				appt.Status = ""
			} else {
				return false, 0, err
			}
		}
		timedOut := !offer.ExpiresAt.After(now)
		stale := appt.Status != "waiting"
		if !timedOut && !stale {
			// A live offer already holds this slot's vacancy.
			return false, expired, nil
		}
		if err := tx.Model(&model.WaitlistOffer{}).
			Where("id = ? AND status = ?", offer.ID, "offered").
			Updates(map[string]any{"status": "expired", "updated_at": now}).Error; err != nil {
			return false, 0, err
		}
		expired++
		if timedOut && appt.Status == "waiting" {
			// The waiter is still waiting (only the hold lapsed): tell them
			// the offer moved on. No message for cancelled/expired waiters:
			// they already received their own status message.
			subject := appointmentSubject(tx, slotID)
			if err := writePatientMessage(tx, offer.PatientID, "waitlist_offer_expired",
				"候补确认已超时", "您的候补（"+subject+"）确认已超时，空位已顺延给下一位候补患者；您的候补登记仍有效，有新的空位时会再次通知您。", offer.AppointmentID, "", now); err != nil {
				return false, 0, err
			}
		}
	}
	if slot.Status != "open" || slot.Reserved >= slot.Capacity || !slot.StartsAt.After(now) {
		return false, expired, nil
	}
	// Offer order is round-robin, not pure registration order: a waiter who
	// already let an offer for this slot lapse goes behind waiters who have
	// not, so a lapse always forwards the seat to the next waiter instead of
	// handing the same vacancy straight back to the lapser. When every
	// remaining waiter has lapsed equally, the earliest registrant is offered
	// again (a new round) rather than leaving the seat idle.
	var waiters []model.Appointment
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("slot_id = ? AND status = ? AND confirmation_deadline > ?", slotID, "waiting", now).
		Order("created_at, id").Find(&waiters).Error; err != nil {
		return false, 0, err
	}
	if len(waiters) == 0 {
		return false, expired, nil
	}
	type lapseCount struct {
		AppointmentID string
		Count         int
	}
	var lapses []lapseCount
	if err := tx.Model(&model.WaitlistOffer{}).Select("appointment_id, COUNT(*) AS count").
		Where("slot_id = ? AND status = ?", slotID, "expired").
		Group("appointment_id").Scan(&lapses).Error; err != nil {
		return false, 0, err
	}
	lapseOf := map[string]int{}
	for _, l := range lapses {
		lapseOf[l.AppointmentID] = l.Count
	}
	waiter := waiters[0]
	for _, w := range waiters[1:] {
		if lapseOf[w.ID] < lapseOf[waiter.ID] {
			waiter = w
		}
	}
	expiresAt := now.Add(waitlistHoldWindow)
	if slot.StartsAt.Before(expiresAt) {
		expiresAt = slot.StartsAt
	}
	offer := model.WaitlistOffer{ID: randomID("waitoffer-"), AppointmentID: waiter.ID,
		SlotID: slotID, PatientID: waiter.PatientID, Status: "offered",
		ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now}
	if err := tx.Create(&offer).Error; err != nil {
		return false, 0, err
	}
	subject := appointmentSubject(tx, slotID)
	deadline := expiresAt.In(database.Shanghai).Format("01-02 15:04")
	if err := writePatientMessage(tx, waiter.PatientID, "waitlist_offer",
		"候补有空位了", "您的候补（"+subject+"）有了空位，名额已为您暂留，请在 "+deadline+"（北京时间）前确认，超时将自动顺延给下一位候补患者。", waiter.ID, "", now); err != nil {
		return false, 0, err
	}
	return true, expired, nil
}

// activeWaitlistOffer returns the slot's live offer, if any. Offers are
// advisory rows: expiry is by time, so a row past expires_at is not active
// even before any settle runs.
func activeWaitlistOffer(tx *gorm.DB, slotID string, now time.Time) (*model.WaitlistOffer, error) {
	var offer model.WaitlistOffer
	if err := tx.Where("slot_id = ? AND status = ? AND expires_at > ?", slotID, "offered", now).
		Order("created_at, id").First(&offer).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &offer, nil
}

// settleWaitlistSlot runs the settle in its own transaction. Best-effort:
// failures are logged, never returned, so callers in request paths and hooks
// can invoke it without risking their own operation.
func (s *MySQL) settleWaitlistSlot(ctx context.Context, slotID string, now time.Time) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, _, err := settleWaitlistSlotTx(tx, slotID, now)
		return err
	})
	if err != nil {
		slog.Error("waitlist settle failed", "slot", slotID, "error", err)
	}
}

func (s *MySQL) WaitlistEntries(ctx context.Context, patientID string, now time.Time) ([]WaitlistEntry, error) {
	now = now.In(database.Shanghai).Truncate(time.Millisecond)
	// Settle this patient's waiting slots first so the UI never offers a
	// confirm button for an already-lapsed hold.
	var slots []string
	if err := s.db.WithContext(ctx).Model(&model.Appointment{}).
		Where("patient_id = ? AND status = ?", patientID, "waiting").
		Distinct("slot_id").Pluck("slot_id", &slots).Error; err != nil {
		return nil, err
	}
	for _, slotID := range slots {
		s.settleWaitlistSlot(ctx, slotID, now)
	}
	var rows []model.Appointment
	if err := appointmentQuery(s.db.WithContext(ctx)).
		Where("appointments.patient_id = ? AND appointments.status = ?", patientID, "waiting").
		Order("appointments.created_at, appointments.id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return attachOffers(s.db.WithContext(ctx), rows, now)
}

func (s *MySQL) WaitlistQueue(ctx context.Context, slotID string, now time.Time) ([]WaitlistEntry, error) {
	now = now.In(database.Shanghai).Truncate(time.Millisecond)
	s.settleWaitlistSlot(ctx, slotID, now)
	var rows []model.Appointment
	if err := appointmentQuery(s.db.WithContext(ctx)).
		Where("appointments.slot_id = ? AND appointments.status = ?", slotID, "waiting").
		Order("appointments.created_at, appointments.id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return attachOffers(s.db.WithContext(ctx), rows, now)
}

func attachOffers(db *gorm.DB, rows []model.Appointment, now time.Time) ([]WaitlistEntry, error) {
	entries := make([]WaitlistEntry, 0, len(rows))
	for _, row := range rows {
		entry := WaitlistEntry{Appointment: row}
		var offer model.WaitlistOffer
		if err := db.Where("appointment_id = ? AND status = ? AND expires_at > ?", row.ID, "offered", now).
			Order("created_at DESC, id DESC").First(&offer).Error; err == nil {
			entry.Offer = &offer
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *MySQL) ConfirmWaitlist(ctx context.Context, appointmentID, patientID string, now time.Time) (model.Appointment, error) {
	requestNow := now
	now = now.In(database.Shanghai).Truncate(time.Millisecond)
	initial, err := loadAppointment(s.db.WithContext(ctx), appointmentID, patientID)
	if err != nil {
		return model.Appointment{}, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPatient(tx, patientID); err != nil {
			return err
		}
		slot, err := lockSlot(tx, initial.SlotID)
		if err != nil {
			return err
		}
		var row model.Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&row, "id = ? AND patient_id = ?", appointmentID, patientID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if row.Status != "waiting" {
			return ErrTransition
		}
		var offer model.WaitlistOffer
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("appointment_id = ? AND status = ?", appointmentID, "offered").
			Order("created_at DESC, id DESC").First(&offer).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTransition
			}
			return err
		}
		if !offer.ExpiresAt.After(now) {
			// Hold lapsed: expire it and offer onward in this same
			// transaction, then report the lapse to the caller.
			if _, _, err := settleWaitlistSlotTx(tx, initial.SlotID, now); err != nil {
				return err
			}
			return ErrTransition
		}
		if slot.Status != "open" || !slot.StartsAt.After(now) {
			return ErrTransition
		}
		// The offer holds no seat (reserved counts real reservations only),
		// so confirming takes the seat here — guarded, so a seat taken by
		// another path in the meantime fails instead of overbooking. The
		// offer itself lapses on that path; there is no vacancy to forward.
		if slot.Reserved >= slot.Capacity {
			if err := tx.Model(&model.WaitlistOffer{}).Where("id = ? AND status = ?", offer.ID, "offered").
				Updates(map[string]any{"status": "expired", "updated_at": now}).Error; err != nil {
				return err
			}
			return ErrSlotFull
		}
		result := tx.Model(&model.Slot{}).Where("id = ? AND reserved < capacity", initial.SlotID).
			Updates(map[string]any{"reserved": gorm.Expr("reserved + 1"), "version": gorm.Expr("version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrSlotFull
		}
		// Seat taken above; now flip the appointment to pending.
		deadline := requestNow.Add(s.pendingConfirmation)
		if slot.StartsAt.Before(deadline) {
			deadline = slot.StartsAt
		}
		result = tx.Model(&model.Appointment{}).Where("id = ? AND version = ?", row.ID, row.Version).
			Updates(map[string]any{"status": "pending", "confirmation_deadline": deadline, "version": row.Version + 1, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		if err := tx.Model(&model.WaitlistOffer{}).Where("id = ? AND status = ?", offer.ID, "offered").
			Updates(map[string]any{"status": "accepted", "updated_at": now}).Error; err != nil {
			return err
		}
		row.Status = "pending"
		row.ConfirmationDeadline = deadline
		row.Version++
		if err := writeEvent(tx, row, "patient", patientID, "claim", "waiting", now); err != nil {
			return err
		}
		return appointmentStatusMessage(tx, &row, "pending", "", "claim", now)
	})
	if err != nil {
		return model.Appointment{}, err
	}
	return loadAppointment(s.db.WithContext(ctx), appointmentID, patientID)
}

func (s *MySQL) SweepWaitlist(ctx context.Context, now time.Time) (WaitlistSweepResult, error) {
	now = now.In(database.Shanghai).Truncate(time.Millisecond)
	var result WaitlistSweepResult
	// Two queries instead of a UNION: the two tables can carry different
	// column collations across environments (MariaDB vs MySQL defaults) and
	// UNION would fail with an illegal collation mix.
	slotSet := map[string]bool{}
	var slots []string
	addSlots := func(query string, args ...any) error {
		var ids []string
		if err := s.db.WithContext(ctx).Raw(query, args...).Pluck("slot_id", &ids).Error; err != nil {
			return err
		}
		for _, id := range ids {
			if !slotSet[id] {
				slotSet[id] = true
				slots = append(slots, id)
			}
		}
		return nil
	}
	if err := addSlots(`SELECT slot_id FROM waitlist_offers WHERE status = 'offered' AND expires_at <= ?`, now); err != nil {
		return result, err
	}
	if err := addSlots(`SELECT DISTINCT slot_id FROM appointments WHERE status = 'waiting' AND confirmation_deadline > ?`, now); err != nil {
		return result, err
	}
	for _, slotID := range slots {
		var offered bool
		var expired int
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var err error
			offered, expired, err = settleWaitlistSlotTx(tx, slotID, now)
			return err
		})
		if err != nil {
			slog.Error("waitlist sweep slot failed", "slot", slotID, "error", err)
			continue
		}
		result.Expired += expired
		if offered {
			result.Offered++
		}
	}
	return result, nil
}
