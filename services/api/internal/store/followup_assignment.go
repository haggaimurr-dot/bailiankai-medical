package store

import (
	"context"
	"sort"
	"time"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FollowupAssignmentInput struct {
	Version    int
	AssigneeID string
	Reason     string
	ActorID    string
}

type FollowupLeaveInput struct {
	Version   int
	ToStaffID string
	Reason    string
	ActorID   string
}

func (s *MySQL) FollowupStaff(ctx context.Context) ([]model.FollowupStaff, error) {
	staff := make([]model.FollowupStaff, 0)
	err := s.db.WithContext(ctx).Order("id").Find(&staff).Error
	return staff, err
}

func lockStaff(tx *gorm.DB, ids ...string) (map[string]model.FollowupStaff, error) {
	unique := make(map[string]bool, len(ids))
	ordered := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && !unique[id] {
			unique[id] = true
			ordered = append(ordered, id)
		}
	}
	sort.Strings(ordered)
	staff := make(map[string]model.FollowupStaff, len(ordered))
	for _, id := range ordered {
		var person model.FollowupStaff
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&person).Error; err != nil {
			return nil, followupDBError(err)
		}
		staff[id] = person
	}
	return staff, nil
}

func (s *MySQL) AssignFollowup(ctx context.Context, taskID string, input FollowupAssignmentInput, now time.Time) (model.FollowupTask, error) {
	var result model.FollowupTask
	actorID := input.ActorID
	if actorID == "" {
		actorID = "demo-clinician"
	}
	if input.Version < 1 || input.AssigneeID == "" || input.Reason == "" {
		return result, ErrCareState
	}
	initial, err := s.FollowupTask(ctx, taskID, "")
	if err != nil {
		return result, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if employeeScoped(ctx) {
			if err := requireClinicRecipient(tx, initial.AppointmentID, input.AssigneeID); err != nil {
				return err
			}
		}
		staff, err := lockStaff(tx, initial.AssigneeID, input.AssigneeID)
		if err != nil {
			return err
		}
		result, err = lockFollowup(tx, taskID, "")
		if err != nil {
			return err
		}
		if result.Version != input.Version {
			if result.Version == input.Version+1 && result.AssigneeID == input.AssigneeID {
				var latest model.FollowupAssignmentEvent
				if err := tx.Where("task_id = ? AND event_type <> ? AND task_version IN ?", taskID, "initial", []int{0, result.Version}).Order("task_version DESC, created_at DESC, id DESC").First(&latest).Error; err != nil {
					return err
				}
				if latest.EventType == "assign" && latest.Reason == input.Reason {
					return nil
				}
			}
			return ErrConflict
		}
		if result.AssigneeID != initial.AssigneeID {
			return ErrConflict
		}
		if result.Status != "pending" && result.Status != "submitted" && result.Status != "alert" {
			return ErrCareState
		}
		if result.AssigneeID == input.AssigneeID {
			return ErrCareState
		}
		target := staff[input.AssigneeID]
		if target.Status != "on_duty" {
			return ErrStaffOffDuty
		}
		if err := requireHandoffRecipient(tx, target.ID); err != nil {
			return err
		}
		previous := result.AssigneeID
		result.AssigneeID, result.AssigneeName, result.AssignedAt = target.ID, target.DisplayName, now
		result.Version++
		update := tx.Model(&model.FollowupTask{}).Where("id = ? AND version = ?", taskID, input.Version).Updates(map[string]any{"assignee_id": target.ID, "assigned_at": now, "version": result.Version})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrConflict
		}
		if err := supersedeHandoffReceipt(tx, taskID, now); err != nil {
			return err
		}
		if err := createHandoffReceipt(tx, taskID, previous, target.ID, input.Reason, actorID, now); err != nil {
			return err
		}
		if err := tx.Create(&model.FollowupAssignmentEvent{ID: randomID("assignment-"), TaskID: taskID, TaskVersion: result.Version, FromStaffID: &previous, ToStaffID: target.ID, EventType: "assign", Reason: input.Reason, ActorID: actorID, CreatedAt: now}).Error; err != nil {
			return err
		}
		return followupEvent(tx, result.AppointmentID, taskID, "clinician", actorID, "assignment-changed", map[string]string{"from": previous, "to": target.ID, "reason": input.Reason}, now)
	})
	return result, err
}

func (s *MySQL) FollowupAssignmentHistory(ctx context.Context, taskID string) ([]model.FollowupAssignmentEvent, error) {
	if _, err := s.FollowupTask(ctx, taskID, ""); err != nil {
		return nil, err
	}
	events := make([]model.FollowupAssignmentEvent, 0)
	err := s.db.WithContext(ctx).Model(&model.FollowupAssignmentEvent{}).
		Select("followup_assignment_events.*, source.display_name AS from_name, target.display_name AS to_name, COALESCE(actor.display_name, followup_assignment_events.actor_id) AS actor_name").
		Joins("LEFT JOIN followup_staff source ON source.id = followup_assignment_events.from_staff_id").
		Joins("JOIN followup_staff target ON target.id = followup_assignment_events.to_staff_id").
		Joins("LEFT JOIN employee_accounts actor ON actor.id = followup_assignment_events.actor_id").
		Where("followup_assignment_events.task_id = ?", taskID).Order("followup_assignment_events.task_version, followup_assignment_events.created_at, CASE WHEN followup_assignment_events.event_type = 'initial' THEN 0 ELSE 1 END, followup_assignment_events.id").Find(&events).Error
	return events, err
}

func (s *MySQL) FollowupStaffEvents(ctx context.Context, staffID string) ([]model.FollowupStaffEvent, error) {
	if err := requireManagedStaff(ctx, s.db.WithContext(ctx), staffID); err != nil {
		return nil, err
	}
	var staff model.FollowupStaff
	if err := s.db.WithContext(ctx).Where("id = ?", staffID).First(&staff).Error; err != nil {
		return nil, followupDBError(err)
	}
	events := make([]model.FollowupStaffEvent, 0)
	err := s.db.WithContext(ctx).Model(&model.FollowupStaffEvent{}).
		Select("followup_staff_events.*, target.display_name AS to_name, COALESCE(actor.display_name, followup_staff_events.actor_id) AS actor_name").
		Joins("LEFT JOIN followup_staff target ON target.id = followup_staff_events.to_staff_id").
		Joins("LEFT JOIN employee_accounts actor ON actor.id = followup_staff_events.actor_id").
		Where("followup_staff_events.staff_id = ?", staffID).Order("followup_staff_events.staff_version DESC, followup_staff_events.created_at DESC, followup_staff_events.id DESC").Find(&events).Error
	return events, err
}

func (s *MySQL) LeaveFollowupStaff(ctx context.Context, staffID string, input FollowupLeaveInput, now time.Time) (model.FollowupStaff, int, error) {
	var source model.FollowupStaff
	if err := requireManagedStaff(ctx, s.db.WithContext(ctx), staffID); err != nil {
		return source, 0, err
	}
	actorID := input.ActorID
	if actorID == "" {
		actorID = "demo-clinician"
	}
	transferred := 0
	if input.Version < 1 || staffID == "" || staffID == input.ToStaffID || input.ToStaffID == "" || input.Reason == "" {
		return source, 0, ErrCareState
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		staff, err := lockStaff(tx, staffID, input.ToStaffID)
		if err != nil {
			return err
		}
		source = staff[staffID]
		target := staff[input.ToStaffID]
		if source.Version == input.Version+1 && source.Status == "off_duty" && source.LastHandoffTo != nil && *source.LastHandoffTo == input.ToStaffID && source.LastLeaveReason == input.Reason {
			var latest model.FollowupStaffEvent
			if err := tx.Where("staff_id = ? AND event_type = ?", staffID, "leave").Order("staff_version DESC, created_at DESC, id DESC").First(&latest).Error; err != nil {
				return err
			}
			transferred = latest.TransferredCount
			return nil
		}
		if source.Version != input.Version {
			return ErrConflict
		}
		if source.Status != "on_duty" || target.Status != "on_duty" {
			return ErrStaffOffDuty
		}
		var appointmentIDs []string
		if err := tx.Model(&model.FollowupTask{}).Where("assignee_id = ? AND status IN ?", staffID, []string{"pending", "submitted", "alert"}).Distinct().Pluck("appointment_id", &appointmentIDs).Error; err != nil {
			return err
		}
		sort.Strings(appointmentIDs)
		for _, id := range appointmentIDs {
			allowed, err := s.CanAccessAppointment(ctx, id, false)
			if err != nil {
				return err
			}
			if !allowed {
				return ErrForbidden
			}
			var appointment model.Appointment
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&appointment).Error; err != nil {
				return followupDBError(err)
			}
		}
		var tasks []model.FollowupTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("assignee_id = ? AND status IN ?", staffID, []string{"pending", "submitted", "alert"}).Order("id").Find(&tasks).Error; err != nil {
			return err
		}
		if len(tasks) > 0 {
			if err := requireHandoffRecipient(tx, target.ID); err != nil {
				return err
			}
		}
		for _, task := range tasks {
			if employeeScoped(ctx) {
				if err := requireClinicRecipient(tx, task.AppointmentID, target.ID); err != nil {
					return err
				}
			}
			update := tx.Model(&model.FollowupTask{}).Where("id = ? AND version = ?", task.ID, task.Version).Updates(map[string]any{"assignee_id": target.ID, "assigned_at": now, "version": task.Version + 1})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return ErrConflict
			}
			if err := supersedeHandoffReceipt(tx, task.ID, now); err != nil {
				return err
			}
			if err := createHandoffReceipt(tx, task.ID, staffID, target.ID, input.Reason, actorID, now); err != nil {
				return err
			}
			from := staffID
			if err := tx.Create(&model.FollowupAssignmentEvent{ID: randomID("assignment-"), TaskID: task.ID, TaskVersion: task.Version + 1, FromStaffID: &from, ToStaffID: target.ID, EventType: "leave", Reason: input.Reason, ActorID: actorID, CreatedAt: now}).Error; err != nil {
				return err
			}
			if err := followupEvent(tx, task.AppointmentID, task.ID, "clinician", actorID, "assignment-leave", map[string]string{"from": staffID, "to": target.ID, "reason": input.Reason}, now); err != nil {
				return err
			}
			transferred++
		}
		source.Status = "off_duty"
		source.Version++
		source.LastHandoffTo = &target.ID
		source.LastLeaveReason = input.Reason
		update := tx.Model(&model.FollowupStaff{}).Where("id = ? AND version = ?", staffID, input.Version).Updates(map[string]any{"status": source.Status, "version": source.Version, "last_handoff_to": target.ID, "last_leave_reason": input.Reason})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrConflict
		}
		return tx.Create(&model.FollowupStaffEvent{ID: randomID("staff-event-"), StaffID: staffID, StaffVersion: source.Version, EventType: "leave", ToStaffID: &target.ID, Reason: input.Reason, TransferredCount: transferred, ActorID: actorID, CreatedAt: now}).Error
	})
	return source, transferred, err
}

func (s *MySQL) ReturnFollowupStaff(ctx context.Context, staffID string, version int, now time.Time, actorIDs ...string) (model.FollowupStaff, error) {
	if err := requireManagedStaff(ctx, s.db.WithContext(ctx), staffID); err != nil {
		return model.FollowupStaff{}, err
	}
	var result model.FollowupStaff
	actorID := "demo-clinician"
	if len(actorIDs) > 0 && actorIDs[0] != "" {
		actorID = actorIDs[0]
	}
	if version < 1 || staffID == "" {
		return result, ErrCareState
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", staffID).First(&result).Error; err != nil {
			return followupDBError(err)
		}
		if result.Status == "on_duty" && result.Version == version+1 {
			return nil
		}
		if result.Version != version {
			return ErrConflict
		}
		if result.Status != "off_duty" {
			return ErrCareState
		}
		result.Status = "on_duty"
		result.Version++
		if err := tx.Model(&model.FollowupStaff{}).Where("id = ?", staffID).Updates(map[string]any{"status": result.Status, "version": result.Version}).Error; err != nil {
			return err
		}
		return tx.Create(&model.FollowupStaffEvent{ID: randomID("staff-event-"), StaffID: staffID, StaffVersion: result.Version, EventType: "return", Reason: "重新在岗", ActorID: actorID, CreatedAt: now}).Error
	})
	return result, err
}
