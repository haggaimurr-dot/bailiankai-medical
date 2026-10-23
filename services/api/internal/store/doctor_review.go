package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/bailiankai/api/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrDoctorQualifications = errors.New("formal doctor qualifications require review")

type DoctorReviewRepository interface {
	ReviewDoctor(context.Context, string, int, string, string, time.Time) (model.Doctor, error)
}

func (s *MySQL) ReviewDoctor(ctx context.Context, id string, version int, status, note string, now time.Time) (model.Doctor, error) {
	var doctor model.Doctor
	note = strings.TrimSpace(note)
	actor, _ := ctx.Value(employeeActorKey{}).(string)
	if actor == "" || version < 1 || (status != "approved" && status != "rejected") || note == "" || utf8.RuneCountInString(note) > 2000 {
		return doctor, ErrCareState
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := clinicScope(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"}), "clinic_id").First(&doctor, "id = ?", id).Error; err != nil {
			return followupDBError(err)
		}
		if doctor.Version != version {
			return ErrConflict
		}
		if !doctor.Active || doctor.Presentation == nil {
			return ErrCareState
		}
		if status == "approved" && !doctor.FormalQualificationsComplete() {
			return ErrDoctorQualifications
		}
		doctor.QualificationReview = &model.DoctorQualificationReview{Status: status, Note: note, ReviewerID: actor, ReviewedAt: now}
		doctor.Version++
		if err := tx.Save(&doctor).Error; err != nil {
			return err
		}
		return directoryEvent(tx, ctx, "doctors", id, "qualification-"+status, doctor.Version, &doctor, now)
	})
	return doctor, err
}
