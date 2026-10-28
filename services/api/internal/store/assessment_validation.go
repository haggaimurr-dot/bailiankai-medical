package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"time"
)

func assessmentOneOf(value string, values ...string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func validAssessmentContent(c AssessmentContent, photos []AssessmentPhoto) bool {
	if !assessmentText(c.Summary, 1000) || len(c.Observations) > 30 || len(c.Limitations) < 1 || len(c.Limitations) > 20 || !assessmentText(c.ConsultationAdvice.Reason, 1000) || c.ConsultationAdvice.Preparation == nil || len(c.ConsultationAdvice.Preparation) > 10 || c.Observations == nil || !assessmentText(c.PlainSummary, 1000) || len(c.CareAdvice) < 1 || len(c.CareAdvice) > 10 {
		return false
	}
	for _, item := range c.CareAdvice {
		if !assessmentText(item, 500) {
			return false
		}
	}
	for _, l := range c.Limitations {
		if !assessmentText(l, 500) {
			return false
		}
	}
	for _, p := range c.ConsultationAdvice.Preparation {
		if !assessmentText(p, 500) {
			return false
		}
	}
	evidence := map[string]string{}
	for _, p := range photos {
		evidence[p.ID] = p.Angle
	}
	ids := map[string]bool{}
	for _, o := range c.Observations {
		if !assessmentID(o.ID) || ids[o.ID] || !assessmentText(o.Description, 500) || !assessmentOneOf(o.Code, "surface_irregularity", "color_variation", "other_visible_change") || !assessmentOneOf(o.Region, "forehead", "left_cheek", "right_cheek", "nose", "chin", "multiple", "unknown") || !assessmentOneOf(o.Certainty, "low", "medium", "high", "unknown") || len(o.Evidence) < 1 || len(o.Evidence) > 3 {
			return false
		}
		ids[o.ID] = true
		seen := map[string]bool{}
		for _, e := range o.Evidence {
			if !assessmentOneOf(e.Angle, "front", "left", "right") || evidence[e.PhotoID] != e.Angle || seen[e.PhotoID] {
				return false
			}
			seen[e.PhotoID] = true
		}
	}
	return true
}
func validAssessmentAnalysis(a AssessmentAnalysis, input AssessmentFrozenInput) bool {
	return a.SchemaVersion == "assessment-result-v1" && a.RequiresHumanReview && a.Model == input.Configuration.Model && assessmentOneOf(a.Outcome, "observations_available", "inconclusive") && (a.Outcome != "inconclusive" || len(a.Observations) == 0) && validAssessmentContent(a.AssessmentContent, input.Photos)
}
func validAssessmentQuality(q AssessmentQuality, cfg AssessmentPipelineConfig) bool {
	if !assessmentOneOf(q.Status, "passed", "rejected", "unavailable") || q.CheckerVersion != cfg.CheckerVersion || q.ThresholdProfileVersion != cfg.ThresholdProfileVersion || (q.CheckedAt == nil || q.CheckedAt.IsZero()) || q.Checks == nil || q.Issues == nil || q.RetakeAdvice == nil || len(q.Issues) > 20 || len(q.RetakeAdvice) > 20 {
		return false
	}
	for _, v := range q.Issues {
		if !assessmentText(v, 200) {
			return false
		}
	}
	for _, v := range q.RetakeAdvice {
		if !assessmentText(v, 500) {
			return false
		}
	}
	if q.Status == "unavailable" {
		return len(q.Checks) <= 7 && len(q.Issues) > 0
	}
	if len(q.Checks) != 7 {
		return false
	}
	seen := map[string]bool{}
	allPassed := true
	for _, c := range q.Checks {
		if !assessmentOneOf(c.Code, "encoding", "dimensions", "face", "clarity", "exposure", "occlusion", "angle") || seen[c.Code] || !assessmentOneOf(c.Status, "pass", "fail", "unknown") || !assessmentText(c.Message, 500) {
			return false
		}
		seen[c.Code] = true
		for _, n := range []*float64{c.Score, c.Threshold} {
			if n != nil && (math.IsNaN(*n) || math.IsInf(*n, 0)) {
				return false
			}
		}
		allPassed = allPassed && c.Status == "pass"
	}
	return (q.Status == "passed" && allPassed && len(q.Issues) == 0) || (q.Status == "rejected" && !allPassed && len(q.Issues) > 0)
}

// Reject duplicate object keys as well as unknown fields and trailing JSON.
// Standard encoding/json alone accepts duplicate keys with last-value wins.
func DecodeAssessmentJSON(data []byte, target any) error {
	if len(data) > 1<<20 {
		return ErrAssessmentInput
	}
	tokens := json.NewDecoder(bytes.NewReader(data))
	var visit func() error
	visit = func() error {
		token, err := tokens.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for tokens.More() {
				key, err := tokens.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return ErrAssessmentInput
				}
				seen[name] = true
				if err := visit(); err != nil {
					return err
				}
			}
		case '[':
			for tokens.More() {
				if err := visit(); err != nil {
					return err
				}
			}
		default:
			return ErrAssessmentInput
		}
		_, err = tokens.Token()
		return err
	}
	if err := visit(); err != nil {
		return err
	}
	if _, err := tokens.Token(); !errors.Is(err, io.EOF) {
		return ErrAssessmentInput
	}
	var object any
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if !assessmentRequiredJSON(reflect.TypeOf(target).Elem(), object) {
		return ErrAssessmentInput
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrAssessmentInput
	}
	return nil
}

func assessmentRequiredJSON(t reflect.Type, value any) bool {
	if t.Kind() == reflect.Pointer {
		if value == nil {
			return true
		}
		return assessmentRequiredJSON(t.Elem(), value)
	}
	if value == nil {
		return false
	}
	if t == reflect.TypeOf(time.Time{}) {
		_, ok := value.(string)
		return ok
	}
	switch t.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			if f.Anonymous && tag == "" {
				if !assessmentRequiredJSON(f.Type, value) {
					return false
				}
				continue
			}
			name := strings.Split(tag, ",")[0]
			if name == "" {
				name = f.Name
			}
			child, found := object[name]
			if !found {
				if !strings.Contains(tag, ",omitempty") {
					return false
				}
				continue
			}
			if !assessmentRequiredJSON(f.Type, child) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		array, ok := value.([]any)
		if !ok {
			return false
		}
		for _, child := range array {
			if !assessmentRequiredJSON(t.Elem(), child) {
				return false
			}
		}
	case reflect.Map:
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for _, child := range object {
			if !assessmentRequiredJSON(t.Elem(), child) {
				return false
			}
		}
	case reflect.Bool:
		_, ok := value.(bool)
		return ok
	case reflect.String:
		_, ok := value.(string)
		return ok
	case reflect.Int, reflect.Int32, reflect.Int64, reflect.Float64:
		_, ok := value.(float64)
		return ok
	}
	return true
}
