package model

import (
	"strings"
	"time"
)

// Review is server-managed and retained in directory history and publication snapshots.
type DoctorQualificationReview struct {
	Status     string    `json:"status"`
	Note       string    `json:"note"`
	ReviewerID string    `json:"reviewerId"`
	ReviewedAt time.Time `json:"reviewedAt"`
}

func (d Doctor) FormalQualificationsComplete() bool {
	if d.Presentation == nil || len(d.Presentation.Credentials) == 0 {
		return false
	}
	for _, c := range d.Presentation.Credentials {
		if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Number) == "" || strings.TrimSpace(c.Issuer) == "" || strings.TrimSpace(c.ImageURL) == "" {
			return false
		}
	}
	return true
}

func (d Doctor) QualificationsPublishable() bool {
	// Legacy and explicitly marked demo profiles remain compatible.
	return d.Presentation == nil || d.Presentation.IsDemo ||
		(d.FormalQualificationsComplete() && d.QualificationReview != nil && d.QualificationReview.Status == "approved")
}
