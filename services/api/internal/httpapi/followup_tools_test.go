package httpapi

import (
	"example.com/bailiankai/api/internal/model"
	"strings"
	"testing"
)

func TestFollowupToolsPermissionsAndOffsets(t *testing.T) {
	for _, days := range [][]int{nil, {0}, {91}, {1, 1}, {1, 2, 3, 4, 5, 6, 7, 8, 9}} {
		if validFollowupOffsets(days) {
			t.Fatalf("invalid offsets accepted: %v", days)
		}
	}
	if !validFollowupOffsets([]int{90, 1, 7}) {
		t.Fatal("valid offsets rejected")
	}
	for _, role := range []string{"manager", "doctor", "clinician", "nurse", "customer_service", "operations"} {
		clinical := role == "manager" || role == "doctor" || role == "clinician" || role == "nurse"
		for _, path := range []string{"/api/v1/admin/followup-templates", "/api/v1/admin/appointments/:appointmentId/nursing-feedback"} {
			if allowedEmployeeOperation(role, "GET", path) != clinical {
				t.Fatalf("read permission %s %s", role, path)
			}
		}
		if allowedEmployeeOperation(role, "POST", "/api/v1/admin/appointments/:appointmentId/nursing-feedback") != clinical {
			t.Fatal("nursing permission", role)
		}
		if allowedEmployeeOperation(role, "PUT", "/api/v1/admin/followup-templates/:templateId") != (role == "manager") {
			t.Fatal("template write permission", role)
		}
	}
}

func TestFormalFollowupStandardValidation(t *testing.T) {
	complete := model.FollowupTemplate{StandardStatus: "formal", ApplicableTreatment: "治疗范围", NursingInstructions: "护理要求", FollowupChecklist: "检查要点", Response: "回评", Disposition: "异常上报", Active: true}
	if !validFollowupStandard(complete) {
		t.Fatal("complete formal standard rejected")
	}
	for _, field := range []string{"scope", "nursing", "checklist", "response", "disposition"} {
		in := complete
		switch field {
		case "scope":
			in.ApplicableTreatment = " "
		case "nursing":
			in.NursingInstructions = " "
		case "checklist":
			in.FollowupChecklist = " "
		case "response":
			in.Response = " "
		case "disposition":
			in.Disposition = " "
		}
		if validFollowupStandard(in) {
			t.Fatal("incomplete formal standard accepted", field)
		}
	}
	if !validFollowupStandard(model.FollowupTemplate{StandardStatus: "draft"}) {
		t.Fatal("draft rejected")
	}
	for _, in := range []model.FollowupTemplate{
		{StandardStatus: "draft", Active: true}, {StandardStatus: "unknown"},
		{StandardStatus: "draft", ApplicableTreatment: strings.Repeat("中", 201)},
		{StandardStatus: "draft", NursingInstructions: strings.Repeat("中", 2001)},
		{StandardStatus: "draft", FollowupChecklist: strings.Repeat("中", 2001)},
	} {
		if validFollowupStandard(in) {
			t.Fatal("invalid standard accepted")
		}
	}
}
