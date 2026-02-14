package sla

import (
	"encoding/json"
	"math"
	"net/http"
	"sync"
	"time"
)

// SLO defines a Service Level Objective.
type SLO struct {
	// Name is a human-readable name for this SLO.
	Name string `json:"name"`
	// Type is "uptime", "latency", or "error_rate".
	Type string `json:"type"`
	// Target is the target value (e.g., 0.999 for 99.9% uptime, 500 for 500ms latency).
	Target float64 `json:"target"`
}

// SLAConfig configures the SLA monitor.
type SLAConfig struct {
	// SLOs is the list of SLOs to track.
	SLOs []SLO `json:"slos"`
	// WindowDuration is the rolling window over which SLIs are calculated.
	WindowDuration time.Duration `json:"window_duration"`
}

// DefaultConfig returns a default SLA config with standard SLOs.
func DefaultConfig() SLAConfig {
	return SLAConfig{
		SLOs: []SLO{
			{Name: "uptime", Type: "uptime", Target: 0.999},
			{Name: "p99_latency_ms", Type: "latency", Target: 500},
			{Name: "error_rate", Type: "error_rate", Target: 0.01},
		},
		WindowDuration: 30 * 24 * time.Hour, // 30 days
	}
}

// SLAMonitor tracks SLIs and computes SLO compliance and error budgets.
type SLAMonitor struct {
	mu     sync.RWMutex
	config SLAConfig

	// Uptime tracking
	totalChecks   int64
	successChecks int64

	// Latency tracking (stored as milliseconds)
	latencies []float64

	// Error rate tracking
	totalRequests int64
	errorRequests int64

	// Window start time
	windowStart time.Time
}

// NewSLAMonitor creates a new SLAMonitor with the given config.
func NewSLAMonitor(cfg SLAConfig) *SLAMonitor {
	return &SLAMonitor{
		config:      cfg,
		latencies:   make([]float64, 0, 1024),
		windowStart: time.Now(),
	}
}

// RecordUptime records an uptime check result.
func (m *SLAMonitor) RecordUptime(success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalChecks++
	if success {
		m.successChecks++
	}
}

// RecordLatency records a request latency.
func (m *SLAMonitor) RecordLatency(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latencies = append(m.latencies, float64(d.Milliseconds()))
}

// RecordRequest records a request outcome.
func (m *SLAMonitor) RecordRequest(isError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalRequests++
	if isError {
		m.errorRequests++
	}
}

// SLOStatus represents the status of a single SLO.
type SLOStatus struct {
	Name            string  `json:"name"`
	Type            string  `json:"type"`
	Target          float64 `json:"target"`
	Current         float64 `json:"current"`
	Met             bool    `json:"met"`
	ErrorBudget     float64 `json:"error_budget"`
	ErrorBudgetUsed float64 `json:"error_budget_used"`
}

// Report represents a full SLA status report.
type Report struct {
	Timestamp   time.Time   `json:"timestamp"`
	WindowStart time.Time   `json:"window_start"`
	WindowEnd   time.Time   `json:"window_end"`
	SLOs        []SLOStatus `json:"slos"`
	Overall     bool        `json:"overall_compliant"`
}

// Status computes the current SLA status report.
func (m *SLAMonitor) Status() Report {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	report := Report{
		Timestamp:   now,
		WindowStart: m.windowStart,
		WindowEnd:   now,
		Overall:     true,
	}

	for _, slo := range m.config.SLOs {
		status := m.computeSLOStatus(slo)
		if !status.Met {
			report.Overall = false
		}
		report.SLOs = append(report.SLOs, status)
	}

	return report
}

func (m *SLAMonitor) computeSLOStatus(slo SLO) SLOStatus {
	status := SLOStatus{
		Name:   slo.Name,
		Type:   slo.Type,
		Target: slo.Target,
	}

	switch slo.Type {
	case "uptime":
		if m.totalChecks > 0 {
			status.Current = float64(m.successChecks) / float64(m.totalChecks)
		} else {
			status.Current = 1.0 // no data = assume healthy
		}
		status.Met = status.Current >= slo.Target
		// Error budget: fraction of allowed downtime remaining
		status.ErrorBudget = 1.0 - slo.Target
		if status.ErrorBudget > 0 {
			used := (1.0 - status.Current) / status.ErrorBudget
			status.ErrorBudgetUsed = math.Min(used, 1.0)
		}

	case "latency":
		p99 := percentile(m.latencies, 0.99)
		status.Current = p99
		status.Met = p99 <= slo.Target
		// Error budget: how much of the latency target is consumed
		if slo.Target > 0 {
			status.ErrorBudget = slo.Target
			status.ErrorBudgetUsed = math.Min(p99/slo.Target, 1.0)
		}

	case "error_rate":
		if m.totalRequests > 0 {
			status.Current = float64(m.errorRequests) / float64(m.totalRequests)
		}
		status.Met = status.Current <= slo.Target
		status.ErrorBudget = slo.Target
		if slo.Target > 0 {
			status.ErrorBudgetUsed = math.Min(status.Current/slo.Target, 1.0)
		}
	}

	return status
}

// percentile computes the p-th percentile of a sorted copy of data.
// p should be between 0.0 and 1.0.
func percentile(data []float64, p float64) float64 {
	n := len(data)
	if n == 0 {
		return 0
	}

	// Copy and sort
	sorted := make([]float64, n)
	copy(sorted, data)
	sortFloat64s(sorted)

	idx := p * float64(n-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper || upper >= n {
		return sorted[lower]
	}
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

// sortFloat64s sorts a slice of float64 in ascending order (insertion sort for simplicity).
func sortFloat64s(data []float64) {
	for i := 1; i < len(data); i++ {
		key := data[i]
		j := i - 1
		for j >= 0 && data[j] > key {
			data[j+1] = data[j]
			j--
		}
		data[j+1] = key
	}
}

// Handler returns an HTTP handler that serves the SLA status as JSON at GET /api/sla/status.
func (m *SLAMonitor) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		report := m.Status()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(report)
	}
}

// Reset clears all SLI data and resets the window start time.
func (m *SLAMonitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalChecks = 0
	m.successChecks = 0
	m.latencies = m.latencies[:0]
	m.totalRequests = 0
	m.errorRequests = 0
	m.windowStart = time.Now()
}
