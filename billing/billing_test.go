package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Plan tests
// ---------------------------------------------------------------------------

func TestPlanLimits(t *testing.T) {
	tests := []struct {
		plan              Plan
		wantPrice         int
		wantExec          int64
		wantPipelines     int
		wantSteps         int
		wantRetention     int
		wantWorkers       int
		wantUnlimitedExec bool
	}{
		{PlanFree, 0, 1000, 5, 20, 7, 2, false},
		{PlanStarter, 4900, 50_000, 25, 50, 30, 8, false},
		{PlanProfessional, 19900, 500_000, 0, 0, 90, 32, false},
		{PlanEnterprise, 0, 0, 0, 0, 365, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.plan.Name, func(t *testing.T) {
			if tt.plan.PriceMonthly != tt.wantPrice {
				t.Errorf("price: got %d, want %d", tt.plan.PriceMonthly, tt.wantPrice)
			}
			if tt.plan.ExecutionsPerMonth != tt.wantExec {
				t.Errorf("executions: got %d, want %d", tt.plan.ExecutionsPerMonth, tt.wantExec)
			}
			if tt.plan.MaxPipelines != tt.wantPipelines {
				t.Errorf("pipelines: got %d, want %d", tt.plan.MaxPipelines, tt.wantPipelines)
			}
			if tt.plan.MaxStepsPerPipeline != tt.wantSteps {
				t.Errorf("steps: got %d, want %d", tt.plan.MaxStepsPerPipeline, tt.wantSteps)
			}
			if tt.plan.RetentionDays != tt.wantRetention {
				t.Errorf("retention: got %d, want %d", tt.plan.RetentionDays, tt.wantRetention)
			}
			if tt.plan.MaxWorkers != tt.wantWorkers {
				t.Errorf("workers: got %d, want %d", tt.plan.MaxWorkers, tt.wantWorkers)
			}
			if tt.plan.IsUnlimited() != tt.wantUnlimitedExec {
				t.Errorf("unlimited: got %v, want %v", tt.plan.IsUnlimited(), tt.wantUnlimitedExec)
			}
		})
	}
}

func TestPlanByID(t *testing.T) {
	for _, p := range AllPlans {
		got := PlanByID(p.ID)
		if got == nil {
			t.Fatalf("PlanByID(%q) returned nil", p.ID)
		}
		if got.Name != p.Name {
			t.Errorf("PlanByID(%q).Name = %q, want %q", p.ID, got.Name, p.Name)
		}
	}
	if PlanByID("nonexistent") != nil {
		t.Error("PlanByID for unknown ID should return nil")
	}
}

func TestAllPlansOrder(t *testing.T) {
	expected := []string{"free", "starter", "professional", "enterprise"}
	if len(AllPlans) != len(expected) {
		t.Fatalf("expected %d plans, got %d", len(expected), len(AllPlans))
	}
	for i, id := range expected {
		if AllPlans[i].ID != id {
			t.Errorf("AllPlans[%d].ID = %q, want %q", i, AllPlans[i].ID, id)
		}
	}
}

func TestEnterpriseFeaturesIncludeSSO(t *testing.T) {
	found := false
	for _, f := range PlanEnterprise.Features {
		if f == "sso" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Enterprise plan should include SSO feature")
	}
}

// ---------------------------------------------------------------------------
// InMemoryMeter tests
// ---------------------------------------------------------------------------

func TestInMemoryMeter_RecordAndGetUsage(t *testing.T) {
	ctx := context.Background()
	m := NewInMemoryMeter()
	m.SetPlan("tenant1", "starter")

	for i := range 10 {
		pipeline := fmt.Sprintf("pipeline-%d", i%3)
		if err := m.RecordExecution(ctx, "tenant1", pipeline); err != nil {
			t.Fatalf("RecordExecution: %v", err)
		}
	}

	report, err := m.GetUsage(ctx, "tenant1", time.Now())
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}
	if report.ExecutionCount != 10 {
		t.Errorf("ExecutionCount = %d, want 10", report.ExecutionCount)
	}
	if report.PipelineCount != 3 {
		t.Errorf("PipelineCount = %d, want 3", report.PipelineCount)
	}
}

func TestInMemoryMeter_CheckLimit_UnderLimit(t *testing.T) {
	ctx := context.Background()
	m := NewInMemoryMeter()
	m.SetPlan("tenant1", "free") // 1000 exec/mo

	if err := m.RecordExecution(ctx, "tenant1", "pipe"); err != nil {
		t.Fatal(err)
	}

	allowed, remaining, err := m.CheckLimit(ctx, "tenant1")
	if err != nil {
		t.Fatalf("CheckLimit: %v", err)
	}
	if !allowed {
		t.Error("expected allowed=true")
	}
	if remaining != 999 {
		t.Errorf("remaining = %d, want 999", remaining)
	}
}

func TestInMemoryMeter_CheckLimit_AtLimit(t *testing.T) {
	ctx := context.Background()
	m := NewInMemoryMeter()
	m.SetPlan("tenant1", "free") // 1000 exec/mo

	for range PlanFree.ExecutionsPerMonth {
		if err := m.RecordExecution(ctx, "tenant1", "pipe"); err != nil {
			t.Fatal(err)
		}
	}

	allowed, remaining, err := m.CheckLimit(ctx, "tenant1")
	if err != nil {
		t.Fatalf("CheckLimit: %v", err)
	}
	if allowed {
		t.Error("expected allowed=false at limit")
	}
	if remaining != 0 {
		t.Errorf("remaining = %d, want 0", remaining)
	}
}

func TestInMemoryMeter_CheckLimit_OverLimit(t *testing.T) {
	ctx := context.Background()
	m := NewInMemoryMeter()
	m.SetPlan("tenant1", "free")

	for range PlanFree.ExecutionsPerMonth + 5 {
		_ = m.RecordExecution(ctx, "tenant1", "pipe")
	}

	allowed, remaining, err := m.CheckLimit(ctx, "tenant1")
	if err != nil {
		t.Fatalf("CheckLimit: %v", err)
	}
	if allowed {
		t.Error("expected allowed=false when over limit")
	}
	if remaining != 0 {
		t.Errorf("remaining = %d, want 0", remaining)
	}
}

func TestInMemoryMeter_CheckLimit_Unlimited(t *testing.T) {
	ctx := context.Background()
	m := NewInMemoryMeter()
	m.SetPlan("tenant1", "enterprise")

	for range 100 {
		_ = m.RecordExecution(ctx, "tenant1", "pipe")
	}

	allowed, remaining, err := m.CheckLimit(ctx, "tenant1")
	if err != nil {
		t.Fatalf("CheckLimit: %v", err)
	}
	if !allowed {
		t.Error("expected allowed=true for unlimited plan")
	}
	if remaining != -1 {
		t.Errorf("remaining = %d, want -1 (unlimited)", remaining)
	}
}

func TestInMemoryMeter_CheckLimit_DefaultPlan(t *testing.T) {
	ctx := context.Background()
	m := NewInMemoryMeter()
	// no explicit plan set, defaults to free

	allowed, remaining, err := m.CheckLimit(ctx, "new-tenant")
	if err != nil {
		t.Fatalf("CheckLimit: %v", err)
	}
	if !allowed {
		t.Error("expected allowed=true for new tenant with no usage")
	}
	if remaining != 1000 {
		t.Errorf("remaining = %d, want 1000", remaining)
	}
}

func TestInMemoryMeter_GetUsage_NoData(t *testing.T) {
	ctx := context.Background()
	m := NewInMemoryMeter()

	report, err := m.GetUsage(ctx, "ghost", time.Now())
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}
	if report.ExecutionCount != 0 {
		t.Errorf("ExecutionCount = %d, want 0", report.ExecutionCount)
	}
}

// ---------------------------------------------------------------------------
// SQLiteMeter tests
// ---------------------------------------------------------------------------

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSQLiteMeter_RecordAndGetUsage(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	m, err := NewSQLiteMeter(db)
	if err != nil {
		t.Fatalf("NewSQLiteMeter: %v", err)
	}
	m.SetPlan("t1", "starter")

	for i := range 5 {
		if err := m.RecordExecution(ctx, "t1", fmt.Sprintf("p%d", i%2)); err != nil {
			t.Fatalf("RecordExecution: %v", err)
		}
	}

	report, err := m.GetUsage(ctx, "t1", time.Now())
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}
	if report.ExecutionCount != 5 {
		t.Errorf("ExecutionCount = %d, want 5", report.ExecutionCount)
	}
	if report.PipelineCount != 2 {
		t.Errorf("PipelineCount = %d, want 2", report.PipelineCount)
	}
}

func TestSQLiteMeter_CheckLimit(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	m, err := NewSQLiteMeter(db)
	if err != nil {
		t.Fatal(err)
	}
	m.SetPlan("t1", "free")

	allowed, remaining, err := m.CheckLimit(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed || remaining != 1000 {
		t.Errorf("allowed=%v remaining=%d, want true/1000", allowed, remaining)
	}

	// Exhaust the free plan limit.
	for range PlanFree.ExecutionsPerMonth {
		_ = m.RecordExecution(ctx, "t1", "p")
	}

	allowed, remaining, err = m.CheckLimit(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Error("expected allowed=false at limit")
	}
	if remaining != 0 {
		t.Errorf("remaining = %d, want 0", remaining)
	}
}

// ---------------------------------------------------------------------------
// MockBillingProvider tests
// ---------------------------------------------------------------------------

func TestMockBillingProvider_FullLifecycle(t *testing.T) {
	ctx := context.Background()
	p := NewMockBillingProvider()

	cusID, err := p.CreateCustomer(ctx, "tenant1", "user@example.com")
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}
	if cusID == "" {
		t.Fatal("expected non-empty customer ID")
	}

	subID, err := p.CreateSubscription(ctx, cusID, "starter")
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if subID == "" {
		t.Fatal("expected non-empty subscription ID")
	}

	if err := p.ReportUsage(ctx, subID, 42); err != nil {
		t.Fatalf("ReportUsage: %v", err)
	}
	if len(p.UsageReports) != 1 || p.UsageReports[0].Quantity != 42 {
		t.Errorf("unexpected usage reports: %+v", p.UsageReports)
	}

	if err := p.HandleWebhook(ctx, []byte(`{"type":"test"}`), "sig"); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if len(p.WebhookPayloads) != 1 {
		t.Errorf("expected 1 webhook payload, got %d", len(p.WebhookPayloads))
	}

	if err := p.CancelSubscription(ctx, subID); err != nil {
		t.Fatalf("CancelSubscription: %v", err)
	}
	if len(p.Subscriptions) != 0 {
		t.Error("subscription should be removed after cancel")
	}
}

func TestMockBillingProvider_UnknownCustomer(t *testing.T) {
	ctx := context.Background()
	p := NewMockBillingProvider()

	_, err := p.CreateSubscription(ctx, "cus_missing", "starter")
	if err == nil {
		t.Error("expected error for unknown customer")
	}
}

func TestMockBillingProvider_CancelUnknownSubscription(t *testing.T) {
	ctx := context.Background()
	p := NewMockBillingProvider()

	err := p.CancelSubscription(ctx, "sub_missing")
	if err == nil {
		t.Error("expected error for unknown subscription")
	}
}

func TestMockBillingProvider_InjectedErrors(t *testing.T) {
	ctx := context.Background()
	p := NewMockBillingProvider()
	injected := fmt.Errorf("injected")

	p.CreateCustomerErr = injected
	if _, err := p.CreateCustomer(ctx, "t", "e"); err != injected {
		t.Errorf("CreateCustomer err = %v, want injected", err)
	}

	p.CreateCustomerErr = nil
	p.CreateSubscriptionErr = injected
	if _, err := p.CreateSubscription(ctx, "c", "p"); err != injected {
		t.Errorf("CreateSubscription err = %v, want injected", err)
	}

	p.CancelSubscriptionErr = injected
	if err := p.CancelSubscription(ctx, "s"); err != injected {
		t.Errorf("CancelSubscription err = %v, want injected", err)
	}

	p.ReportUsageErr = injected
	if err := p.ReportUsage(ctx, "s", 1); err != injected {
		t.Errorf("ReportUsage err = %v, want injected", err)
	}

	p.HandleWebhookErr = injected
	if err := p.HandleWebhook(ctx, nil, ""); err != injected {
		t.Errorf("HandleWebhook err = %v, want injected", err)
	}
}

// ---------------------------------------------------------------------------
// HTTP handler tests
// ---------------------------------------------------------------------------

func setupHandler(t *testing.T) (*http.ServeMux, *InMemoryMeter, *MockBillingProvider) {
	t.Helper()
	meter := NewInMemoryMeter()
	provider := NewMockBillingProvider()
	h := NewHandler(meter, provider)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux, meter, provider
}

func TestHandler_ListPlans(t *testing.T) {
	mux, _, _ := setupHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/plans", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var plans []Plan
	if err := json.NewDecoder(w.Body).Decode(&plans); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(plans) != len(AllPlans) {
		t.Errorf("got %d plans, want %d", len(plans), len(AllPlans))
	}
}

func TestHandler_GetUsage(t *testing.T) {
	mux, meter, _ := setupHandler(t)
	ctx := context.Background()
	meter.SetPlan("tenant1", "starter")
	_ = meter.RecordExecution(ctx, "tenant1", "pipeline-a")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/usage?tenant_id=tenant1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		ExecutionCount int64 `json:"execution_count"`
		Allowed        bool  `json:"allowed"`
		Remaining      int64 `json:"remaining"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ExecutionCount != 1 {
		t.Errorf("execution_count = %d, want 1", resp.ExecutionCount)
	}
	if !resp.Allowed {
		t.Error("expected allowed=true")
	}
}

func TestHandler_GetUsage_MissingTenantID(t *testing.T) {
	mux, _, _ := setupHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/usage", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandler_GetUsage_InvalidPeriod(t *testing.T) {
	mux, _, _ := setupHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/usage?tenant_id=t1&period=bad", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandler_Subscribe(t *testing.T) {
	mux, _, _ := setupHandler(t)

	body := `{"tenant_id":"t1","email":"a@b.com","plan_id":"starter"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/billing/subscribe", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp subscribeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.CustomerID == "" || resp.SubscriptionID == "" {
		t.Errorf("expected non-empty IDs, got %+v", resp)
	}
}

func TestHandler_Subscribe_UnknownPlan(t *testing.T) {
	mux, _, _ := setupHandler(t)

	body := `{"tenant_id":"t1","email":"a@b.com","plan_id":"nonexistent"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/billing/subscribe", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown plan, got %d", w.Code)
	}
}

func TestHandler_Subscribe_MissingFields(t *testing.T) {
	mux, _, _ := setupHandler(t)

	body := `{"tenant_id":"t1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/billing/subscribe", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing plan_id, got %d", w.Code)
	}
}

func TestHandler_Subscribe_WithExistingCustomer(t *testing.T) {
	mux, _, provider := setupHandler(t)
	ctx := context.Background()

	cusID, _ := provider.CreateCustomer(ctx, "t1", "a@b.com")

	body := fmt.Sprintf(`{"tenant_id":"t1","plan_id":"starter","customer_id":"%s"}`, cusID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/billing/subscribe", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_CancelSubscription(t *testing.T) {
	mux, _, provider := setupHandler(t)
	ctx := context.Background()

	cusID, _ := provider.CreateCustomer(ctx, "t1", "a@b.com")
	subID, _ := provider.CreateSubscription(ctx, cusID, "starter")

	body := fmt.Sprintf(`{"subscription_id":"%s"}`, subID)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/billing/subscribe", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_CancelSubscription_MissingID(t *testing.T) {
	mux, _, _ := setupHandler(t)

	body := `{}`
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/billing/subscribe", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandler_Webhook(t *testing.T) {
	mux, _, _ := setupHandler(t)

	body := `{"type":"invoice.paid","data":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/billing/webhook", strings.NewReader(body))
	req.Header.Set("Stripe-Signature", "t=123,v1=abc")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_Webhook_ProviderError(t *testing.T) {
	meter := NewInMemoryMeter()
	provider := NewMockBillingProvider()
	provider.HandleWebhookErr = fmt.Errorf("bad signature")

	h := NewHandler(meter, provider)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"type":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/billing/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for webhook error, got %d", w.Code)
	}
}
