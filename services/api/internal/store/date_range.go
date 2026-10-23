package store

import "time"

// DateRange uses inclusive Shanghai dates translated to a half-open time interval.
type DateRange struct{ From, Until time.Time }

func (r DateRange) Active() bool { return !r.From.IsZero() && !r.Until.IsZero() }
