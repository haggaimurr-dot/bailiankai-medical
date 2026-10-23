package store

import "context"

type Overview struct {
	AppointmentCount        int64 `json:"appointmentCount"`
	PendingAppointmentCount int64 `json:"pendingAppointmentCount"`
	FollowupCount           int64 `json:"followupCount"`
}

func (s *MySQL) Overview(ctx context.Context) (Overview, error) {
	var data Overview
	query := s.patientAppointmentsQuery(ctx)
	if err := query.Count(&data.AppointmentCount).Error; err != nil {
		return data, err
	}
	if err := s.patientAppointmentsQuery(ctx).Where("appointments.status = ?", "pending").Count(&data.PendingAppointmentCount).Error; err != nil {
		return data, err
	}
	err := s.taskClinicScope(ctx, s.db.WithContext(ctx).Table("followup_tasks"), "followup_tasks.id").Count(&data.FollowupCount).Error
	return data, err
}
