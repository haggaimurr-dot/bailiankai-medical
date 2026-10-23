package httpapi

import (
	"context"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"testing"
	"time"
)

type careTemplateFake struct {
	fakeRepository
	role  string
	owner string
}

func (r *careTemplateFake) AuthenticateEmployee(context.Context, string, time.Time) (model.EmployeeAccount, error) {
	return model.EmployeeAccount{ID: "template-owner", Role: r.role, ClinicIDs: []string{"clinic-demo"}}, nil
}
func (r *careTemplateFake) ListCareTemplates(_ context.Context, owner string) ([]store.CarePlanTemplate, error) {
	r.owner = owner
	return []store.CarePlanTemplate{}, nil
}
func (r *careTemplateFake) CreateCareTemplate(_ context.Context, owner, name string, _ store.CarePlanContent, now time.Time) (store.CarePlanTemplate, error) {
	r.owner = owner
	return store.CarePlanTemplate{ID: "template-1", Name: name, CreatedAt: now}, nil
}
func (r *careTemplateFake) DeleteCareTemplate(_ context.Context, owner, id string) error {
	r.owner = owner
	return store.ErrNotFound
}

func TestCareTemplateOwnershipAndValidation(t *testing.T) {
	for _, role := range []string{"manager", "clinician", "doctor", "nurse", "operations", "customer_service"} {
		repo := &careTemplateFake{role: role}
		result := serve(New(repo, ""), "GET", "/api/v1/admin/care-plan-templates", "", testToken, "")
		allowed := role != "operations" && role != "customer_service"
		if allowed && (result.Code != 200 || repo.owner != "template-owner") {
			t.Fatal(role, result.Code, result.Body)
		}
		if !allowed && (result.Code != 403 || repo.owner != "") {
			t.Fatal(role, result.Code, result.Body)
		}
	}
	for _, body := range []string{
		`{"name":"", "content":{"summary":"摘要","stages":[],"instructions":""}}`,
		`{"name":"模板", "ownerId":"forged", "content":{"summary":"摘要","stages":[{"title":"阶段","body":"内容"}],"instructions":""}}`,
	} {
		repo := &careTemplateFake{role: "manager"}
		result := serve(New(repo, ""), "POST", "/api/v1/admin/care-plan-templates", body, testToken, "")
		if result.Code != 400 || repo.owner != "" {
			t.Fatal(result.Code, result.Body)
		}
	}
	repo := &careTemplateFake{role: "manager"}
	body := `{"name":"模板", "content":{"summary":"摘要","stages":[{"title":"阶段","body":"内容"}],"instructions":""}}`
	result := serve(New(repo, ""), "POST", "/api/v1/admin/care-plan-templates", body, testToken, "")
	if result.Code != 201 || repo.owner != "template-owner" {
		t.Fatal(result.Code, result.Body)
	}
	result = serve(New(repo, ""), "DELETE", "/api/v1/admin/care-plan-templates/template-other", "", testToken, "")
	if result.Code != 404 || repo.owner != "template-owner" {
		t.Fatal(result.Code, result.Body)
	}
}
