package alerts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/ai/classifier"
	"github.com/GoCodeAlone/workflow/ai/sentiment"
)

func TestNewAlertEngine(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if len(engine.rules) != 4 {
		t.Errorf("expected 4 default rules, got %d", len(engine.rules))
	}
}

func TestEvaluate_RiskEscalation_Critical(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())

	alerts := engine.EvaluateRiskEscalation(context.Background(), "conv-1", &classifier.Classification{
		Category:    classifier.CategoryCrisis,
		Priority:    1,
		Confidence:  0.95,
		Subcategory: "suicidal-ideation",
	})

	if len(alerts) == 0 {
		t.Fatal("expected at least one alert")
	}

	found := false
	for _, a := range alerts {
		if a.Type == AlertRiskEscalation {
			found = true
			if a.Severity != SeverityCritical {
				t.Errorf("expected critical severity for priority 1, got %s", a.Severity)
			}
			if a.ConversationID != "conv-1" {
				t.Errorf("expected conv-1, got %s", a.ConversationID)
			}
		}
	}
	if !found {
		t.Error("expected risk_escalation alert")
	}
}

func TestEvaluate_RiskEscalation_High(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())

	alerts := engine.EvaluateRiskEscalation(context.Background(), "conv-2", &classifier.Classification{
		Category:    classifier.CategoryCrisis,
		Priority:    2,
		Confidence:  0.8,
		Subcategory: "self-harm",
	})

	found := false
	for _, a := range alerts {
		if a.Type == AlertRiskEscalation {
			found = true
			if a.Severity != SeverityHigh {
				t.Errorf("expected high severity for priority 2, got %s", a.Severity)
			}
		}
	}
	if !found {
		t.Error("expected risk_escalation alert")
	}
}

func TestEvaluate_RiskEscalation_NonCrisis(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())

	alerts := engine.EvaluateRiskEscalation(context.Background(), "conv-3", &classifier.Classification{
		Category: classifier.CategoryGeneralSupport,
		Priority: 3,
	})

	for _, a := range alerts {
		if a.Type == AlertRiskEscalation {
			t.Error("did not expect risk_escalation for general-support")
		}
	}
}

func TestEvaluate_WorkloadImbalance(t *testing.T) {
	engine := NewAlertEngine(nil, Config{
		MaxConversationsPerAgent: 3,
		MaxWaitTime:              5 * time.Minute,
	})

	workloads := []CounselorWorkload{
		{CounselorID: "c1", ActiveConversations: 2},
		{CounselorID: "c2", ActiveConversations: 5},
		{CounselorID: "c3", ActiveConversations: 1},
	}

	alerts := engine.EvaluateWorkload(context.Background(), workloads)

	found := false
	for _, a := range alerts {
		if a.Type == AlertWorkloadImbalance {
			found = true
			if a.Severity != SeverityMedium {
				t.Errorf("expected medium severity, got %s", a.Severity)
			}
		}
	}
	if !found {
		t.Error("expected workload_imbalance alert")
	}
}

func TestEvaluate_WorkloadImbalance_AllUnderLimit(t *testing.T) {
	engine := NewAlertEngine(nil, Config{
		MaxConversationsPerAgent: 5,
		MaxWaitTime:              5 * time.Minute,
	})

	workloads := []CounselorWorkload{
		{CounselorID: "c1", ActiveConversations: 2},
		{CounselorID: "c2", ActiveConversations: 3},
	}

	alerts := engine.EvaluateWorkload(context.Background(), workloads)

	for _, a := range alerts {
		if a.Type == AlertWorkloadImbalance {
			t.Error("did not expect workload alert when under limit")
		}
	}
}

func TestEvaluate_SentimentDrop(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())

	now := time.Now()
	trend := &sentiment.Trend{
		ConversationID: "conv-drop",
		CurrentScore:   -0.8,
		AverageScore:   -0.3,
		Direction:      "declining",
		SharpDrop:      true,
		SharpDropAt:    &now,
	}

	alerts := engine.EvaluateSentimentDrop(context.Background(), "conv-drop", trend)

	found := false
	for _, a := range alerts {
		if a.Type == AlertSentimentDrop {
			found = true
			if a.Severity != SeverityHigh {
				t.Errorf("expected high severity for score -0.8, got %s", a.Severity)
			}
		}
	}
	if !found {
		t.Error("expected sentiment_drop alert")
	}
}

func TestEvaluate_SentimentDrop_NoSharpDrop(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())

	trend := &sentiment.Trend{
		ConversationID: "conv-stable",
		CurrentScore:   -0.2,
		AverageScore:   -0.1,
		Direction:      "stable",
		SharpDrop:      false,
	}

	alerts := engine.EvaluateSentimentDrop(context.Background(), "conv-stable", trend)

	for _, a := range alerts {
		if a.Type == AlertSentimentDrop {
			t.Error("did not expect sentiment_drop alert without sharp drop")
		}
	}
}

func TestEvaluate_LongWaitTime(t *testing.T) {
	engine := NewAlertEngine(nil, Config{
		MaxWaitTime: 5 * time.Minute,
	})

	alerts := engine.EvaluateWaitTime(context.Background(), "conv-wait", 8*time.Minute)

	found := false
	for _, a := range alerts {
		if a.Type == AlertLongWaitTime {
			found = true
			if a.Severity != SeverityMedium {
				t.Errorf("expected medium severity for 8m wait, got %s", a.Severity)
			}
		}
	}
	if !found {
		t.Error("expected long_wait_time alert")
	}
}

func TestEvaluate_LongWaitTime_UnderThreshold(t *testing.T) {
	engine := NewAlertEngine(nil, Config{
		MaxWaitTime: 5 * time.Minute,
	})

	alerts := engine.EvaluateWaitTime(context.Background(), "conv-ok", 3*time.Minute)

	for _, a := range alerts {
		if a.Type == AlertLongWaitTime {
			t.Error("did not expect long_wait_time alert under threshold")
		}
	}
}

func TestEvaluate_LongWaitTime_Critical(t *testing.T) {
	engine := NewAlertEngine(nil, Config{
		MaxWaitTime: 5 * time.Minute,
	})

	alerts := engine.EvaluateWaitTime(context.Background(), "conv-critical", 25*time.Minute)

	found := false
	for _, a := range alerts {
		if a.Type == AlertLongWaitTime {
			found = true
			if a.Severity != SeverityCritical {
				t.Errorf("expected critical severity for 25m wait, got %s", a.Severity)
			}
		}
	}
	if !found {
		t.Error("expected long_wait_time alert")
	}
}

func TestGetAlerts_FilterByType(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())

	// Generate a risk escalation alert
	engine.EvaluateRiskEscalation(context.Background(), "conv-1", &classifier.Classification{
		Category: classifier.CategoryCrisis,
		Priority: 1,
	})

	// Generate a wait time alert
	engine.EvaluateWaitTime(context.Background(), "conv-2", 10*time.Minute)

	alertType := AlertRiskEscalation
	alerts := engine.GetAlerts(AlertFilter{Type: &alertType})

	for _, a := range alerts {
		if a.Type != AlertRiskEscalation {
			t.Errorf("expected only risk_escalation alerts, got %s", a.Type)
		}
	}
}

func TestGetAlerts_FilterUnresolved(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())

	engine.EvaluateWaitTime(context.Background(), "conv-1", 10*time.Minute)
	engine.EvaluateWaitTime(context.Background(), "conv-2", 10*time.Minute)

	// Resolve first alert
	all := engine.GetAlerts(AlertFilter{})
	if len(all) < 2 {
		t.Fatalf("expected at least 2 alerts, got %d", len(all))
	}
	_ = engine.ResolveAlert(all[0].ID)

	unresolved := engine.GetAlerts(AlertFilter{Unresolved: true})
	for _, a := range unresolved {
		if a.ResolvedAt != nil {
			t.Error("expected only unresolved alerts")
		}
	}
}

func TestAcknowledgeAlert(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())

	engine.EvaluateWaitTime(context.Background(), "conv-ack", 10*time.Minute)

	all := engine.GetAlerts(AlertFilter{})
	if len(all) == 0 {
		t.Fatal("expected at least one alert")
	}

	err := engine.AcknowledgeAlert(all[0].ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify acknowledged
	updated := engine.GetAlerts(AlertFilter{})
	for _, a := range updated {
		if a.ID == all[0].ID && a.AcknowledgedAt == nil {
			t.Error("expected alert to be acknowledged")
		}
	}
}

func TestAcknowledgeAlert_NotFound(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())
	err := engine.AcknowledgeAlert("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent alert")
	}
}

func TestResolveAlert_NotFound(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())
	err := engine.ResolveAlert("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent alert")
	}
}

func TestEvaluate_NilContext(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())
	alerts := engine.Evaluate(context.Background(), nil)
	if len(alerts) != 0 {
		t.Error("expected no alerts for nil context")
	}
}

func TestOnAlertCallback(t *testing.T) {
	var callbackAlerts []Alert
	engine := NewAlertEngine(nil, Config{
		MaxWaitTime: 5 * time.Minute,
		OnAlert: func(a Alert) {
			callbackAlerts = append(callbackAlerts, a)
		},
	})

	engine.EvaluateWaitTime(context.Background(), "conv-cb", 10*time.Minute)

	if len(callbackAlerts) == 0 {
		t.Error("expected callback to fire")
	}
}

// --- HTTP Handler Tests ---

func TestHandleGetAlerts(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())
	engine.EvaluateWaitTime(context.Background(), "conv-http", 10*time.Minute)

	handler := NewHandler(engine)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/alerts", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	count, _ := resp["count"].(float64)
	if count < 1 {
		t.Error("expected at least one alert")
	}
}

func TestHandleGetAlerts_WithFilter(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())
	engine.EvaluateWaitTime(context.Background(), "conv-f1", 10*time.Minute)
	engine.EvaluateRiskEscalation(context.Background(), "conv-f2", &classifier.Classification{
		Category: classifier.CategoryCrisis,
		Priority: 1,
	})

	handler := NewHandler(engine)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/alerts?type=long_wait_time&unresolved=true", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	alerts, ok := resp["alerts"].([]any)
	if !ok {
		t.Fatal("expected alerts array")
	}
	for _, a := range alerts {
		alertMap := a.(map[string]any)
		if alertMap["type"] != "long_wait_time" {
			t.Errorf("expected only long_wait_time alerts, got %v", alertMap["type"])
		}
	}
}

func TestHandleAcknowledge(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())
	engine.EvaluateWaitTime(context.Background(), "conv-ack-http", 10*time.Minute)

	all := engine.GetAlerts(AlertFilter{})
	if len(all) == 0 {
		t.Fatal("expected at least one alert")
	}

	handler := NewHandler(engine)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/alerts/"+all[0].ID+"/acknowledge", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAcknowledge_NotFound(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())
	handler := NewHandler(engine)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/alerts/nonexistent/acknowledge", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleResolve(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())
	engine.EvaluateWaitTime(context.Background(), "conv-res-http", 10*time.Minute)

	all := engine.GetAlerts(AlertFilter{})
	if len(all) == 0 {
		t.Fatal("expected at least one alert")
	}

	handler := NewHandler(engine)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/alerts/"+all[0].ID+"/resolve", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleResolve_NotFound(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())
	handler := NewHandler(engine)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/alerts/nonexistent/resolve", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleGetAlerts_Empty(t *testing.T) {
	engine := NewAlertEngine(nil, DefaultConfig())
	handler := NewHandler(engine)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/alerts", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	count, _ := resp["count"].(float64)
	if count != 0 {
		t.Errorf("expected 0 alerts, got %v", count)
	}
}
