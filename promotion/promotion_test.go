package promotion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func noop() (ValidateFunc, DeployFunc) {
	return nil, nil
}

func TestNewPipeline_DefaultEnvironments(t *testing.T) {
	p := NewPipeline(noop())

	envs := p.ListEnvironments()
	if len(envs) != 3 {
		t.Fatalf("expected 3 default environments, got %d", len(envs))
	}
	// Should be ordered: dev, staging, prod
	if envs[0].Name != EnvDev {
		t.Errorf("expected dev first, got %s", envs[0].Name)
	}
	if envs[1].Name != EnvStaging {
		t.Errorf("expected staging second, got %s", envs[1].Name)
	}
	if envs[2].Name != EnvProd {
		t.Errorf("expected prod third, got %s", envs[2].Name)
	}

	// Prod requires approval
	prod, _ := p.GetEnvironment(EnvProd)
	if !prod.RequiresApproval {
		t.Error("expected prod to require approval")
	}
}

func TestPipeline_Deploy(t *testing.T) {
	var deployed string
	deployFn := func(ctx context.Context, env EnvironmentName, wf, cfg string) error {
		deployed = string(env) + ":" + cfg
		return nil
	}

	p := NewPipeline(nil, deployFn)

	err := p.Deploy(context.Background(), "wf1", EnvDev, "config: dev")
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if deployed != "dev:config: dev" {
		t.Errorf("expected deploy call, got %q", deployed)
	}

	cfg, ok := p.GetConfig("wf1", EnvDev)
	if !ok || cfg != "config: dev" {
		t.Error("expected config to be tracked")
	}
}

func TestPipeline_DeployUnknownEnv(t *testing.T) {
	p := NewPipeline(noop())
	err := p.Deploy(context.Background(), "wf", "unknown", "cfg")
	if err == nil {
		t.Error("expected error for unknown env")
	}
}

func TestPipeline_DeployValidationFails(t *testing.T) {
	validateFn := func(ctx context.Context, env EnvironmentName, cfg string) error {
		return fmt.Errorf("invalid config")
	}
	p := NewPipeline(validateFn, nil)

	err := p.Deploy(context.Background(), "wf", EnvDev, "bad")
	if err == nil {
		t.Error("expected validation error")
	}
}

func TestPipeline_PromoteNoApproval(t *testing.T) {
	// Staging does not require approval by default
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvDev, "config-v1")

	record, err := p.Promote(context.Background(), "wf", EnvDev, EnvStaging, "alice")
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if record.Status != PromotionDeployed {
		t.Errorf("expected deployed, got %s", record.Status)
	}

	// Config should be in staging now
	cfg, ok := p.GetConfig("wf", EnvStaging)
	if !ok || cfg != "config-v1" {
		t.Error("expected config in staging")
	}
}

func TestPipeline_PromoteRequiresApproval(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvStaging, "config-staging")

	record, err := p.Promote(context.Background(), "wf", EnvStaging, EnvProd, "alice")
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if record.Status != PromotionPending {
		t.Errorf("expected pending_approval, got %s", record.Status)
	}
	if record.ApprovalStatus != ApprovalPending {
		t.Errorf("expected approval pending, got %s", record.ApprovalStatus)
	}

	// Config should NOT be in prod yet
	_, ok := p.GetConfig("wf", EnvProd)
	if ok {
		t.Error("expected no config in prod before approval")
	}
}

func TestPipeline_ApproveAndDeploy(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvStaging, "config-staging")

	record, _ := p.Promote(context.Background(), "wf", EnvStaging, EnvProd, "alice")

	approved, err := p.Approve(context.Background(), record.ID, "bob")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != PromotionDeployed {
		t.Errorf("expected deployed after approve, got %s", approved.Status)
	}
	if approved.ApprovedBy != "bob" {
		t.Errorf("expected approvedBy bob, got %s", approved.ApprovedBy)
	}
	if approved.ApprovedAt == nil {
		t.Error("expected approvedAt to be set")
	}

	// Config should now be in prod
	cfg, ok := p.GetConfig("wf", EnvProd)
	if !ok || cfg != "config-staging" {
		t.Error("expected config in prod after approval")
	}
}

func TestPipeline_Reject(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvStaging, "config")

	record, _ := p.Promote(context.Background(), "wf", EnvStaging, EnvProd, "alice")

	rejected, err := p.Reject(record.ID, "bob", "not ready")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Status != PromotionRejected {
		t.Errorf("expected rejected, got %s", rejected.Status)
	}
	if rejected.Error != "not ready" {
		t.Errorf("expected reason, got %q", rejected.Error)
	}
}

func TestPipeline_PromoteWrongDirection(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvProd, "config")

	_, err := p.Promote(context.Background(), "wf", EnvProd, EnvDev, "alice")
	if err == nil {
		t.Error("expected error for backward promotion")
	}
}

func TestPipeline_PromoteNoConfig(t *testing.T) {
	p := NewPipeline(nil, nil)
	_, err := p.Promote(context.Background(), "wf", EnvDev, EnvStaging, "alice")
	if err == nil {
		t.Error("expected error for missing config")
	}
}

func TestPipeline_ApproveNotPending(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvDev, "config")

	record, _ := p.Promote(context.Background(), "wf", EnvDev, EnvStaging, "alice")
	// This was auto-deployed (no approval needed)

	_, err := p.Approve(context.Background(), record.ID, "bob")
	if err == nil {
		t.Error("expected error for non-pending promotion")
	}
}

func TestPipeline_ApproveNotFound(t *testing.T) {
	p := NewPipeline(nil, nil)
	_, err := p.Approve(context.Background(), "nonexistent", "bob")
	if err == nil {
		t.Error("expected error")
	}
}

func TestPipeline_RejectNotFound(t *testing.T) {
	p := NewPipeline(nil, nil)
	_, err := p.Reject("nonexistent", "bob", "reason")
	if err == nil {
		t.Error("expected error")
	}
}

func TestPipeline_ListPromotions(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvDev, "config")
	_, _ = p.Promote(context.Background(), "wf", EnvDev, EnvStaging, "alice")

	records := p.ListPromotions()
	if len(records) != 1 {
		t.Fatalf("expected 1 promotion, got %d", len(records))
	}
}

func TestPipeline_GetPromotion(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvDev, "config")
	record, _ := p.Promote(context.Background(), "wf", EnvDev, EnvStaging, "alice")

	got, ok := p.GetPromotion(record.ID)
	if !ok {
		t.Fatal("expected to find promotion")
	}
	if got.WorkflowName != "wf" {
		t.Errorf("expected wf, got %s", got.WorkflowName)
	}
}

func TestPipeline_GetAllConfigs(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvDev, "dev-cfg")
	_ = p.Deploy(context.Background(), "wf", EnvStaging, "staging-cfg")

	configs := p.GetAllConfigs("wf")
	if len(configs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(configs))
	}
	if configs[EnvDev] != "dev-cfg" {
		t.Errorf("expected dev-cfg, got %s", configs[EnvDev])
	}
}

func TestPipeline_GetAllConfigs_Empty(t *testing.T) {
	p := NewPipeline(nil, nil)
	configs := p.GetAllConfigs("nonexistent")
	if configs != nil {
		t.Errorf("expected nil, got %v", configs)
	}
}

func TestPipeline_SetEnvironment(t *testing.T) {
	p := NewPipeline(nil, nil)
	p.SetEnvironment(&Environment{
		Name:             "qa",
		Description:      "QA env",
		RequiresApproval: true,
		Order:            1,
	})

	env, ok := p.GetEnvironment("qa")
	if !ok || env.Description != "QA env" {
		t.Error("expected custom env")
	}
}

func TestPipeline_PromoteValidationFails(t *testing.T) {
	validateFn := func(ctx context.Context, env EnvironmentName, cfg string) error {
		if env == EnvStaging {
			return fmt.Errorf("staging validation failed")
		}
		return nil
	}
	p := NewPipeline(validateFn, nil)
	_ = p.Deploy(context.Background(), "wf", EnvDev, "config")

	record, err := p.Promote(context.Background(), "wf", EnvDev, EnvStaging, "alice")
	if err == nil {
		t.Error("expected validation error")
	}
	if record == nil || record.Status != PromotionFailed {
		t.Error("expected failed status")
	}
}

// --- HTTP handler tests ---

func TestHandler_ListEnvironments(t *testing.T) {
	p := NewPipeline(nil, nil)
	h := NewHandler(p)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/environments", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	envs := resp["environments"].([]any)
	if len(envs) != 3 {
		t.Errorf("expected 3 envs, got %d", len(envs))
	}
}

func TestHandler_Promote(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvDev, "config")

	h := NewHandler(p)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"workflowName":"wf","fromEnv":"dev","toEnv":"staging","requestedBy":"alice"}`
	req := httptest.NewRequest("POST", "/api/promote", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_PromoteRequiresApproval(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvStaging, "config")

	h := NewHandler(p)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"workflowName":"wf","fromEnv":"staging","toEnv":"prod","requestedBy":"alice"}`
	req := httptest.NewRequest("POST", "/api/promote", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_ApproveReject(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvStaging, "config")
	record, _ := p.Promote(context.Background(), "wf", EnvStaging, EnvProd, "alice")

	h := NewHandler(p)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Approve
	body := `{"approvedBy":"bob"}`
	req := httptest.NewRequest("POST", "/api/promotions/"+record.ID+"/approve", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_Deploy(t *testing.T) {
	p := NewPipeline(nil, nil)
	h := NewHandler(p)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"workflowName":"wf","environment":"dev","configYaml":"modules: []"}`
	req := httptest.NewRequest("POST", "/api/deploy", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_GetConfigs(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvDev, "dev-cfg")

	h := NewHandler(p)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/configs/wf", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandler_ListPromotions(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvDev, "config")
	_, _ = p.Promote(context.Background(), "wf", EnvDev, EnvStaging, "alice")

	h := NewHandler(p)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/promotions", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandler_GetPromotion(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvDev, "config")
	record, _ := p.Promote(context.Background(), "wf", EnvDev, EnvStaging, "alice")

	h := NewHandler(p)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/promotions/"+record.ID, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandler_GetPromotionNotFound(t *testing.T) {
	p := NewPipeline(nil, nil)
	h := NewHandler(p)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/promotions/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandler_Reject(t *testing.T) {
	p := NewPipeline(nil, nil)
	_ = p.Deploy(context.Background(), "wf", EnvStaging, "config")
	record, _ := p.Promote(context.Background(), "wf", EnvStaging, EnvProd, "alice")

	h := NewHandler(p)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"rejectedBy":"bob","reason":"not ready"}`
	req := httptest.NewRequest("POST", "/api/promotions/"+record.ID+"/reject", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
