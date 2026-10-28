package store

import "testing"

func TestAssessmentContentDiff(t *testing.T) {
	original := AssessmentContent{
		Summary: "AI 概要",
		Observations: []AssessmentObservation{
			{ID: "obs-1", Code: "surface_irregularity", Description: "表面不平", Region: "forehead", Certainty: "high"},
			{ID: "obs-2", Code: "color_variation", Description: "颜色变化", Region: "nose", Certainty: "medium"},
		},
		Limitations:        []string{"光线不足"},
		ConsultationAdvice: AssessmentAdvice{Recommended: true, Reason: "建议面诊", Preparation: []string{"带病历"}},
	}
	unchanged := original
	if got := assessmentContentDiff(original, unchanged); got != "" {
		t.Errorf("unchanged diff = %q, want empty", got)
	}
	amended := AssessmentContent{
		Summary: "医生修订后的概要",
		Observations: []AssessmentObservation{
			{ID: "obs-1", Code: "surface_irregularity", Description: "表面明显不平", Region: "forehead", Certainty: "high"},
			{ID: "obs-3", Code: "other_visible_change", Description: "新增观察", Region: "chin", Certainty: "low"},
		},
		Limitations:        []string{"光线不足", "角度偏差"},
		ConsultationAdvice: AssessmentAdvice{Recommended: true, Reason: "建议面诊", Preparation: []string{"带病历"}},
	}
	want := "修订：观察概要已改；观察新增1条、删除1条、修改1条；局限新增1条、删除0条"
	if got := assessmentContentDiff(original, amended); got != want {
		t.Errorf("amended diff = %q, want %q", got, want)
	}
	adviceOnly := original
	adviceOnly.ConsultationAdvice = AssessmentAdvice{Recommended: false, Reason: "暂不需面诊", Preparation: []string{}}
	if got := assessmentContentDiff(original, adviceOnly); got != "修订：面诊建议已改" {
		t.Errorf("advice diff = %q, want %q", got, "修订：面诊建议已改")
	}
}
