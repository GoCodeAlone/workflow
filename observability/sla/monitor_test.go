package sla

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.SLOs) != 3 {
		t.Fatalf("expected 3 default SLOs, got %d", len(cfg.SLOs))
	}
	if cfg.WindowDuration != 30*24*time.Hour {
		t.Errorf("expected 30d window, got %v", cfg.WindowDuration)
	}
}

func TestSLAMonitor_Uptime_AllSuccessful(t *testing.T) {
	cfg := SLAConfig{
		SLOs: []SLO{{Name: "uptime", Type: "uptime", Target: 0.999}},
	}
	m := NewSLAMonitor(cfg)

	for range 1000 {
		m.RecordUptime(true)
	}

	report := m.Status()
	if len(report.SLOs) != 1 {
		t.Fatalf("expected 1 SLO, got %d", len(report.SLOs))
	}
	if !report.SLOs[0].Met {
		t.Error("expected uptime SLO to be met at 100%")
	}
	if report.SLOs[0].Current != 1.0 {
		t.Errorf("expected current 1.0, got %f", report.SLOs[0].Current)
	}
	if !report.Overall {
		t.Error("expected overall compliant")
	}
}

func TestSLAMonitor_Uptime_BelowTarget(t *testing.T) {
	cfg := SLAConfig{
		SLOs: []SLO{{Name: "uptime", Type: "uptime", Target: 0.999}},
	}
	m := NewSLAMonitor(cfg)

	// 990 success, 10 failures = 99.0% uptime < 99.9%
	for range 990 {
		m.RecordUptime(true)
	}
	for range 10 {
		m.RecordUptime(false)
	}

	report := m.Status()
	if report.SLOs[0].Met {
		t.Error("expected uptime SLO to NOT be met at 99.0%")
	}
	if report.Overall {
		t.Error("expected overall non-compliant")
	}
}

func TestSLAMonitor_Uptime_NoData(t *testing.T) {
	cfg := SLAConfig{
		SLOs: []SLO{{Name: "uptime", Type: "uptime", Target: 0.999}},
	}
	m := NewSLAMonitor(cfg)

	report := m.Status()
	// No data should default to 1.0 (healthy assumption)
	if report.SLOs[0].Current != 1.0 {
		t.Errorf("expected 1.0 with no data, got %f", report.SLOs[0].Current)
	}
	if !report.SLOs[0].Met {
		t.Error("expected SLO met with no data")
	}
}

func TestSLAMonitor_Latency(t *testing.T) {
	cfg := SLAConfig{
		SLOs: []SLO{{Name: "p99_latency", Type: "latency", Target: 500}},
	}
	m := NewSLAMonitor(cfg)

	// 99 requests at 100ms, 1 request at 400ms => p99 should be around 400
	for range 99 {
		m.RecordLatency(100 * time.Millisecond)
	}
	m.RecordLatency(400 * time.Millisecond)

	report := m.Status()
	if !report.SLOs[0].Met {
		t.Errorf("expected latency SLO met, p99=%f", report.SLOs[0].Current)
	}
}

func TestSLAMonitor_Latency_Exceeded(t *testing.T) {
	cfg := SLAConfig{
		SLOs: []SLO{{Name: "p99_latency", Type: "latency", Target: 100}},
	}
	m := NewSLAMonitor(cfg)

	// All requests at 200ms, p99 = 200 > 100
	for range 100 {
		m.RecordLatency(200 * time.Millisecond)
	}

	report := m.Status()
	if report.SLOs[0].Met {
		t.Error("expected latency SLO to NOT be met")
	}
}

func TestSLAMonitor_Latency_NoData(t *testing.T) {
	cfg := SLAConfig{
		SLOs: []SLO{{Name: "p99_latency", Type: "latency", Target: 500}},
	}
	m := NewSLAMonitor(cfg)

	report := m.Status()
	if report.SLOs[0].Current != 0 {
		t.Errorf("expected 0 latency with no data, got %f", report.SLOs[0].Current)
	}
	if !report.SLOs[0].Met {
		t.Error("expected SLO met with no data (0 <= target)")
	}
}

func TestSLAMonitor_ErrorRate(t *testing.T) {
	cfg := SLAConfig{
		SLOs: []SLO{{Name: "error_rate", Type: "error_rate", Target: 0.01}},
	}
	m := NewSLAMonitor(cfg)

	// 999 success, 1 error = 0.1% error rate < 1%
	for range 999 {
		m.RecordRequest(false)
	}
	m.RecordRequest(true)

	report := m.Status()
	if !report.SLOs[0].Met {
		t.Errorf("expected error rate SLO met, current=%f", report.SLOs[0].Current)
	}
}

func TestSLAMonitor_ErrorRate_Exceeded(t *testing.T) {
	cfg := SLAConfig{
		SLOs: []SLO{{Name: "error_rate", Type: "error_rate", Target: 0.01}},
	}
	m := NewSLAMonitor(cfg)

	// 90 success, 10 errors = 10% > 1%
	for range 90 {
		m.RecordRequest(false)
	}
	for range 10 {
		m.RecordRequest(true)
	}

	report := m.Status()
	if report.SLOs[0].Met {
		t.Error("expected error rate SLO to NOT be met")
	}
}

func TestSLAMonitor_ErrorBudget(t *testing.T) {
	cfg := SLAConfig{
		SLOs: []SLO{{Name: "uptime", Type: "uptime", Target: 0.99}},
	}
	m := NewSLAMonitor(cfg)

	// 99 success, 1 failure = 99% uptime, exactly at target
	for range 99 {
		m.RecordUptime(true)
	}
	m.RecordUptime(false)

	report := m.Status()
	slo := report.SLOs[0]
	if slo.ErrorBudget < 0.0099 || slo.ErrorBudget > 0.0101 {
		t.Errorf("expected error budget ~0.01, got %f", slo.ErrorBudget)
	}
	// 1% downtime used, 1% budget = 100% consumed
	if slo.ErrorBudgetUsed != 1.0 {
		t.Errorf("expected 100%% error budget used, got %f", slo.ErrorBudgetUsed)
	}
}

func TestSLAMonitor_Reset(t *testing.T) {
	m := NewSLAMonitor(DefaultConfig())

	m.RecordUptime(true)
	m.RecordLatency(100 * time.Millisecond)
	m.RecordRequest(false)

	m.Reset()

	report := m.Status()
	// After reset, uptime should be 1.0 (no data)
	for _, slo := range report.SLOs {
		if slo.Type == "uptime" && slo.Current != 1.0 {
			t.Errorf("expected uptime 1.0 after reset, got %f", slo.Current)
		}
		if slo.Type == "latency" && slo.Current != 0 {
			t.Errorf("expected latency 0 after reset, got %f", slo.Current)
		}
		if slo.Type == "error_rate" && slo.Current != 0 {
			t.Errorf("expected error rate 0 after reset, got %f", slo.Current)
		}
	}
}

func TestSLAMonitor_Handler(t *testing.T) {
	m := NewSLAMonitor(DefaultConfig())
	m.RecordUptime(true)
	m.RecordRequest(false)

	handler := m.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/sla/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}

	var report Report
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(report.SLOs) != 3 {
		t.Errorf("expected 3 SLOs in report, got %d", len(report.SLOs))
	}
}

func TestSLAMonitor_Handler_MethodNotAllowed(t *testing.T) {
	m := NewSLAMonitor(DefaultConfig())
	handler := m.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/sla/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestPercentile(t *testing.T) {
	tests := []struct {
		name string
		data []float64
		p    float64
		want float64
	}{
		{"empty", nil, 0.99, 0},
		{"single", []float64{42}, 0.99, 42},
		{"two values p50", []float64{10, 20}, 0.5, 15},
		{"ten values p90", []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 0.9, 9.1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := percentile(tt.data, tt.p)
			if got != tt.want {
				t.Errorf("percentile(%v, %f) = %f, want %f", tt.data, tt.p, got, tt.want)
			}
		})
	}
}

func TestSortFloat64s(t *testing.T) {
	data := []float64{5, 3, 1, 4, 2}
	sortFloat64s(data)
	for i := 1; i < len(data); i++ {
		if data[i] < data[i-1] {
			t.Errorf("not sorted at index %d: %v", i, data)
		}
	}
}

func TestSLAMonitor_MultipleSLOs(t *testing.T) {
	cfg := SLAConfig{
		SLOs: []SLO{
			{Name: "uptime", Type: "uptime", Target: 0.999},
			{Name: "latency", Type: "latency", Target: 500},
			{Name: "errors", Type: "error_rate", Target: 0.01},
		},
	}
	m := NewSLAMonitor(cfg)

	// Good uptime
	for range 1000 {
		m.RecordUptime(true)
	}
	// Good latency
	for range 100 {
		m.RecordLatency(50 * time.Millisecond)
	}
	// Good error rate
	for range 1000 {
		m.RecordRequest(false)
	}

	report := m.Status()
	if !report.Overall {
		t.Error("expected all SLOs met")
	}
	for _, slo := range report.SLOs {
		if !slo.Met {
			t.Errorf("SLO %q should be met", slo.Name)
		}
	}
}
