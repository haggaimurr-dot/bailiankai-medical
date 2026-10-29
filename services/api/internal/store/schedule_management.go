package store

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ScheduleTemplate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	DoctorID string `json:"doctorId"`
	Weekdays []int  `json:"weekdays"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Duration int    `json:"duration"`
	Capacity int    `json:"capacity"`
	Open     bool   `json:"open"`
}
type HolidayRule struct {
	Date string `json:"date"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}
type ScheduleSetting struct {
	ClinicID  string             `json:"clinicId" gorm:"primaryKey"`
	Templates []ScheduleTemplate `json:"templates" gorm:"serializer:json"`
	Holidays  []HolidayRule      `json:"holidays" gorm:"serializer:json"`
	Version   int                `json:"version"`
	UpdatedBy string             `json:"-"`
	UpdatedAt time.Time          `json:"-"`
}
type SchedulePatientTask struct {
	ID                 string    `json:"id" gorm:"primaryKey"`
	AppointmentID      string    `json:"appointmentId"`
	SlotID             string    `json:"slotId"`
	Reason             string    `json:"reason"`
	ContactStatus      string    `json:"contactStatus"`
	Note               string    `json:"note"`
	Version            int       `json:"version"`
	UpdatedBy          string    `json:"updatedBy"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
	ContactName        string    `json:"contactName" gorm:"->"`
	ContactPhone       string    `json:"contactPhone" gorm:"->"`
	AppointmentStatus  string    `json:"appointmentStatus" gorm:"->"`
	AppointmentVersion int       `json:"appointmentVersion" gorm:"->"`
	CurrentSlotID      string    `json:"currentSlotId" gorm:"->"`
	SlotStatus         string    `json:"slotStatus" gorm:"->"`
	DoctorID           string    `json:"doctorId" gorm:"->"`
	StartsAt           time.Time `json:"startsAt" gorm:"->"`
	EndsAt             time.Time `json:"endsAt" gorm:"->"`
}
type ScheduleContactInput struct {
	Version       int    `json:"version"`
	ContactStatus string `json:"contactStatus"`
	Note          string `json:"note"`
}
type ScheduleManagementRepository interface {
	GetScheduleSetting(context.Context, string) (ScheduleSetting, error)
	SaveScheduleSetting(context.Context, ScheduleSetting, time.Time) (ScheduleSetting, error)
	SchedulePatientTasks(context.Context, string, time.Time) ([]SchedulePatientTask, error)
	UpdateScheduleContact(context.Context, string, ScheduleContactInput, time.Time) error
	RescheduleAffectedPatient(context.Context, string, string, RescheduleInput, time.Time) (model.Appointment, error)
}

func createSchedulePatientTasks(tx *gorm.DB, ctx context.Context, slotID, reason string, now time.Time) error {
	var appointments []model.Appointment
	if err := tx.Where("slot_id = ? AND status IN ?", slotID, []string{"pending", "confirmed", "waiting"}).Find(&appointments).Error; err != nil {
		return err
	}
	for _, appointment := range appointments {
		if err := tx.Clauses(clause.OnConflict{DoUpdates: clause.Assignments(map[string]any{"reason": reason, "contact_status": "todo", "note": "", "version": gorm.Expr("version + 1"), "updated_by": employeeActor(ctx), "updated_at": now})}).Create(&SchedulePatientTask{ID: randomID("schedule-task-"), AppointmentID: appointment.ID, SlotID: slotID, Reason: reason, ContactStatus: "todo", Version: 1, UpdatedBy: employeeActor(ctx), CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *MySQL) GetScheduleSetting(ctx context.Context, clinicID string) (ScheduleSetting, error) {
	var clinic model.Clinic
	if err := clinicScope(ctx, s.db.WithContext(ctx), "id").First(&clinic, "id = ?", clinicID).Error; err != nil {
		return ScheduleSetting{}, followupDBError(err)
	}
	row := ScheduleSetting{ClinicID: clinicID, Templates: []ScheduleTemplate{}, Holidays: []HolidayRule{}}
	err := s.db.WithContext(ctx).First(&row, "clinic_id = ?", clinicID).Error
	if err == gorm.ErrRecordNotFound {
		return row, nil
	}
	return row, err
}
func validScheduleSetting(in ScheduleSetting) bool {
	if len(in.Templates) > 100 || len(in.Holidays) > 366 || in.Version < 0 {
		return false
	}
	ids, dates := map[string]bool{}, map[string]bool{}
	for _, t := range in.Templates {
		from, e1 := time.Parse("15:04", t.Start)
		to, e2 := time.Parse("15:04", t.End)
		if t.ID == "" || len(t.ID) > 64 || ids[t.ID] || strings.TrimSpace(t.Name) == "" || utf8.RuneCountInString(t.Name) > 50 || t.DoctorID == "" || e1 != nil || e2 != nil || !to.After(from) || t.Duration < 1 || t.Duration > 1440 || int(to.Sub(from).Minutes())%t.Duration != 0 || t.Capacity < 0 || t.Capacity > 100 || len(t.Weekdays) == 0 || len(t.Weekdays) > 7 {
			return false
		}
		ids[t.ID] = true
		seen := map[int]bool{}
		for _, day := range t.Weekdays {
			if day < 0 || day > 6 || seen[day] {
				return false
			}
			seen[day] = true
		}
	}
	for _, h := range in.Holidays {
		date, err := time.Parse("2006-01-02", h.Date)
		if err != nil || date.Format("2006-01-02") != h.Date || dates[h.Date] || strings.TrimSpace(h.Name) == "" || utf8.RuneCountInString(h.Name) > 50 || h.Kind != "rest" && h.Kind != "work" {
			return false
		}
		dates[h.Date] = true
	}
	return true
}
func (s *MySQL) SaveScheduleSetting(ctx context.Context, in ScheduleSetting, now time.Time) (ScheduleSetting, error) {
	if !validScheduleSetting(in) {
		return in, ErrScheduleInput
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var clinic model.Clinic
		if err := clinicScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"}), "id").First(&clinic, "id = ?", in.ClinicID).Error; err != nil {
			return followupDBError(err)
		}
		var previous ScheduleSetting
		err := tx.First(&previous, "clinic_id = ?", in.ClinicID).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		if previous.Version != in.Version {
			return ErrConflict
		}
		for _, t := range in.Templates {
			var count int64
			if err := tx.Model(&model.Doctor{}).Where("id = ? AND clinic_id = ? AND active = TRUE AND published_json IS NOT NULL", t.DoctorID, in.ClinicID).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return ErrNotFound
			}
		}
		in.Version++
		in.UpdatedAt = now
		in.UpdatedBy = employeeActor(ctx)
		if previous.Version == 0 {
			return tx.Create(&in).Error
		}
		return tx.Save(&in).Error
	})
	return in, err
}
func (s *MySQL) SchedulePatientTasks(ctx context.Context, clinicID string, now time.Time) ([]SchedulePatientTask, error) {
	if _, err := s.ExpirePending(ctx, now); err != nil {
		return nil, err
	}
	rows := []SchedulePatientTask{}
	q := clinicScope(ctx, s.db.WithContext(ctx).Table("schedule_patient_tasks AS tasks").Select("tasks.*, appointments.contact_name, appointments.contact_phone, appointments.status AS appointment_status, appointments.version AS appointment_version, appointments.slot_id AS current_slot_id, slots.status AS slot_status, slots.doctor_id, slots.starts_at, slots.ends_at").Joins("JOIN appointments ON appointments.id = tasks.appointment_id").Joins("JOIN slots ON slots.id = tasks.slot_id"), "slots.clinic_id")
	err := q.Where("slots.clinic_id = ?", clinicID).Order("tasks.created_at DESC, tasks.id").Scan(&rows).Error
	return rows, err
}
func (s *MySQL) UpdateScheduleContact(ctx context.Context, id string, in ScheduleContactInput, now time.Time) error {
	if in.Version < 1 || utf8.RuneCountInString(in.Note) > 500 || in.ContactStatus != "todo" && in.ContactStatus != "contacted" && in.ContactStatus != "unreachable" {
		return ErrScheduleInput
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task SchedulePatientTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", id).Error; err != nil {
			return followupDBError(err)
		}
		var slot model.Slot
		if err := clinicScope(ctx, tx, "clinic_id").First(&slot, "id = ?", task.SlotID).Error; err != nil {
			return followupDBError(err)
		}
		if task.Version != in.Version {
			return ErrConflict
		}
		return tx.Model(&task).Updates(map[string]any{"contact_status": in.ContactStatus, "note": strings.TrimSpace(in.Note), "version": task.Version + 1, "updated_at": now, "updated_by": employeeActor(ctx)}).Error
	})
}
func (s *MySQL) RescheduleAffectedPatient(ctx context.Context, id, key string, in RescheduleInput, now time.Time) (model.Appointment, error) {
	var task SchedulePatientTask
	if err := s.db.WithContext(ctx).First(&task, "id = ?", id).Error; err != nil {
		return model.Appointment{}, followupDBError(err)
	}
	var slot model.Slot
	if err := clinicScope(ctx, s.db.WithContext(ctx), "clinic_id").First(&slot, "id = ?", task.SlotID).Error; err != nil {
		return model.Appointment{}, followupDBError(err)
	}
	row, err := s.GetAppointment(ctx, task.AppointmentID, "", now)
	if err != nil {
		return row, err
	}
	return s.RescheduleAppointment(ctx, row.ID, row.PatientID, key, in, now)
}

// Rest-day rules apply to new open slots; existing schedules require explicit suspension.
func scheduleHolidayCheck(tx *gorm.DB, row model.Slot) error {
	if row.Status == "open" {
		var setting ScheduleSetting
		err := tx.First(&setting, "clinic_id = ?", row.ClinicID).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		if err == nil {
			for _, h := range setting.Holidays {
				day, err := time.ParseInLocation("2006-01-02", h.Date, database.Shanghai)
				if err != nil {
					return ErrScheduleInput
				}
				if h.Kind == "rest" && row.StartsAt.Before(day.AddDate(0, 0, 1)) && row.EndsAt.After(day) {
					return ErrScheduleInput
				}
			}
		}
		// 节假日表（全局）：节假日当天不允许新建开放时段。
		var count int64
		dayStart := time.Date(row.StartsAt.In(database.Shanghai).Year(), row.StartsAt.In(database.Shanghai).Month(), row.StartsAt.In(database.Shanghai).Day(), 0, 0, 0, 0, database.Shanghai)
		if err := tx.Model(&model.Holiday{}).Where("holiday_date = ?", dayStart.Format("2006-01-02")).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrScheduleInput
		}
	}
	return nil
}
