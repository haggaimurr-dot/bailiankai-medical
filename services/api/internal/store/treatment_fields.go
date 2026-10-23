package store

import (
	"encoding/json"
	"example.com/bailiankai/api/internal/model"
	"strings"
	"unicode/utf8"
)

type TreatmentFieldPatch struct {
	StageIndex                              *int32
	Procedure, BodyArea, Device, Parameters *string
}

func (p TreatmentFieldPatch) Apply(f model.TreatmentFields) model.TreatmentFields {
	if p.StageIndex != nil {
		f.StageIndex = int(*p.StageIndex)
	}
	for _, field := range []struct {
		input  *string
		target *string
	}{{p.Procedure, &f.Procedure}, {p.BodyArea, &f.BodyArea}, {p.Device, &f.Device}, {p.Parameters, &f.Parameters}} {
		if field.input != nil {
			*field.target = strings.TrimSpace(*field.input)
		}
	}
	return f
}
func ValidTreatmentFields(f model.TreatmentFields) bool {
	return f.StageIndex >= 0 && f.StageIndex <= 8 && utf8.RuneCountInString(f.Procedure) <= 100 && utf8.RuneCountInString(f.BodyArea) <= 100 && utf8.RuneCountInString(f.Device) <= 100 && utf8.RuneCountInString(f.Parameters) <= 2000
}
func validPlanStage(content string, index int) bool {
	if index == 0 {
		return true
	}
	var plan struct {
		Stages []json.RawMessage `json:"stages"`
	}
	return json.Unmarshal([]byte(content), &plan) == nil && index > 0 && index <= len(plan.Stages)
}
