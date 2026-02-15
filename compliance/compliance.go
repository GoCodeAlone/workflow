package compliance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/GoCodeAlone/workflow/audit"
)

// Framework identifies a compliance framework.
type Framework string

const (
	FrameworkSOC2  Framework = "SOC2"
	FrameworkHIPAA Framework = "HIPAA"
)

// ControlStatus indicates whether a compliance control is satisfied.
type ControlStatus string

const (
	StatusPass    ControlStatus = "pass"
	StatusFail    ControlStatus = "fail"
	StatusWarning ControlStatus = "warning"
)

// Control represents a single compliance control check.
type Control struct {
	ID          string        `json:"id"`
	Framework   Framework     `json:"framework"`
	Category    string        `json:"category"`
	Description string        `json:"description"`
	Status      ControlStatus `json:"status"`
	Details     string        `json:"details,omitempty"`
}

// Report is a full compliance report covering one or more frameworks.
type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	Controls    []Control `json:"controls"`
	Summary     Summary   `json:"summary"`
}

// Summary aggregates counts across all controls.
type Summary struct {
	Total    int `json:"total"`
	Passed   int `json:"passed"`
	Failed   int `json:"failed"`
	Warnings int `json:"warnings"`
}

// AuditTrailSummary summarizes the audit trail for the reporting period.
type AuditTrailSummary struct {
	TotalEvents      int            `json:"total_events"`
	EventsByType     map[string]int `json:"events_by_type"`
	AuthFailures     int            `json:"auth_failures"`
	DataAccessEvents int            `json:"data_access_events"`
	ConfigChanges    int            `json:"config_changes"`
}

// AuditEventCollector reads audit events for the reporting period.
type AuditEventCollector interface {
	CollectEvents(ctx context.Context, start, end time.Time) ([]audit.Event, error)
}

// InMemoryCollector collects events from a slice (useful for testing).
type InMemoryCollector struct {
	Events []audit.Event
}

// CollectEvents filters events within the given time range.
func (c *InMemoryCollector) CollectEvents(_ context.Context, start, end time.Time) ([]audit.Event, error) {
	var filtered []audit.Event
	for i := range c.Events {
		if !c.Events[i].Timestamp.Before(start) && !c.Events[i].Timestamp.After(end) {
			filtered = append(filtered, c.Events[i])
		}
	}
	return filtered, nil
}

// CheckConfig holds the configuration state used for compliance checks.
type CheckConfig struct {
	EncryptionEnabled  bool `json:"encryption_enabled"`
	AuditLoggingActive bool `json:"audit_logging_active"`
	TLSEnabled         bool `json:"tls_enabled"`
	BackupEnabled      bool `json:"backup_enabled"`
	AccessControlled   bool `json:"access_controlled"`
	RetentionDays      int  `json:"retention_days"`
}

// Generator creates compliance reports.
type Generator struct {
	collector AuditEventCollector
	config    CheckConfig
}

// NewGenerator creates a new compliance report generator.
func NewGenerator(collector AuditEventCollector, cfg CheckConfig) *Generator {
	return &Generator{
		collector: collector,
		config:    cfg,
	}
}

// Generate produces a compliance report for the given time period.
func (g *Generator) Generate(ctx context.Context, start, end time.Time) (*Report, error) {
	events, err := g.collector.CollectEvents(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("compliance: failed to collect audit events: %w", err)
	}

	trail := summarizeAuditTrail(events)
	controls := g.evaluateControls(trail)

	summary := Summary{Total: len(controls)}
	for _, c := range controls {
		switch c.Status {
		case StatusPass:
			summary.Passed++
		case StatusFail:
			summary.Failed++
		case StatusWarning:
			summary.Warnings++
		}
	}

	return &Report{
		GeneratedAt: time.Now().UTC(),
		PeriodStart: start,
		PeriodEnd:   end,
		Controls:    controls,
		Summary:     summary,
	}, nil
}

func summarizeAuditTrail(events []audit.Event) AuditTrailSummary {
	s := AuditTrailSummary{
		EventsByType: make(map[string]int),
	}
	for i := range events {
		s.TotalEvents++
		s.EventsByType[string(events[i].Type)]++
		switch events[i].Type {
		case audit.EventAuthFailure:
			s.AuthFailures++
		case audit.EventDataAccess:
			s.DataAccessEvents++
		case audit.EventConfigChange:
			s.ConfigChanges++
		}
	}
	return s
}

func (g *Generator) evaluateControls(trail AuditTrailSummary) []Control {
	var controls []Control

	controls = append(controls,
		// SOC2 Controls
		g.checkEncryption(FrameworkSOC2, "SOC2-CC6.1"),
		g.checkAuditLogging(trail, FrameworkSOC2, "SOC2-CC7.2"),
		g.checkAccessControl(FrameworkSOC2, "SOC2-CC6.3"),
		g.checkAuthFailures(trail, FrameworkSOC2, "SOC2-CC6.6"),
		g.checkTLS(FrameworkSOC2, "SOC2-CC6.7"),
		// HIPAA Controls
		g.checkEncryption(FrameworkHIPAA, "HIPAA-164.312(a)(2)(iv)"),
		g.checkAuditLogging(trail, FrameworkHIPAA, "HIPAA-164.312(b)"),
		g.checkAccessControl(FrameworkHIPAA, "HIPAA-164.312(a)(1)"),
		g.checkDataAccess(trail, FrameworkHIPAA, "HIPAA-164.312(d)"),
		g.checkBackup(FrameworkHIPAA, "HIPAA-164.308(a)(7)(ii)(A)"),
		g.checkRetention(FrameworkHIPAA, "HIPAA-164.530(j)(2)"),
	)

	return controls
}

func (g *Generator) checkEncryption(fw Framework, id string) Control {
	c := Control{
		ID:          id,
		Framework:   fw,
		Category:    "Encryption",
		Description: "Data encryption at rest and in transit",
	}
	if g.config.EncryptionEnabled {
		c.Status = StatusPass
		c.Details = "Encryption is enabled"
	} else {
		c.Status = StatusFail
		c.Details = "Encryption is not enabled"
	}
	return c
}

func (g *Generator) checkAuditLogging(trail AuditTrailSummary, fw Framework, id string) Control {
	c := Control{
		ID:          id,
		Framework:   fw,
		Category:    "Audit Logging",
		Description: "Audit logging is active and recording events",
	}
	if !g.config.AuditLoggingActive {
		c.Status = StatusFail
		c.Details = "Audit logging is not active"
		return c
	}
	if trail.TotalEvents == 0 {
		c.Status = StatusWarning
		c.Details = "Audit logging is active but no events found in period"
	} else {
		c.Status = StatusPass
		c.Details = fmt.Sprintf("%d events recorded in period", trail.TotalEvents)
	}
	return c
}

func (g *Generator) checkAccessControl(fw Framework, id string) Control {
	c := Control{
		ID:          id,
		Framework:   fw,
		Category:    "Access Control",
		Description: "Access control mechanisms are in place",
	}
	if g.config.AccessControlled {
		c.Status = StatusPass
		c.Details = "Access control is enabled"
	} else {
		c.Status = StatusFail
		c.Details = "Access control is not configured"
	}
	return c
}

func (g *Generator) checkAuthFailures(trail AuditTrailSummary, fw Framework, id string) Control {
	c := Control{
		ID:          id,
		Framework:   fw,
		Category:    "Authentication",
		Description: "Authentication failure monitoring",
	}
	if trail.AuthFailures > 100 {
		c.Status = StatusWarning
		c.Details = fmt.Sprintf("High number of auth failures: %d", trail.AuthFailures)
	} else {
		c.Status = StatusPass
		c.Details = fmt.Sprintf("%d auth failures in period", trail.AuthFailures)
	}
	return c
}

func (g *Generator) checkTLS(fw Framework, id string) Control {
	c := Control{
		ID:          id,
		Framework:   fw,
		Category:    "Transport Security",
		Description: "TLS encryption for data in transit",
	}
	if g.config.TLSEnabled {
		c.Status = StatusPass
		c.Details = "TLS is enabled"
	} else {
		c.Status = StatusFail
		c.Details = "TLS is not enabled"
	}
	return c
}

func (g *Generator) checkDataAccess(trail AuditTrailSummary, fw Framework, id string) Control {
	c := Control{
		ID:          id,
		Framework:   fw,
		Category:    "Data Access",
		Description: "Data access is tracked and auditable",
	}
	if !g.config.AuditLoggingActive {
		c.Status = StatusFail
		c.Details = "Audit logging is not active; data access cannot be tracked"
		return c
	}
	c.Status = StatusPass
	c.Details = fmt.Sprintf("%d data access events recorded", trail.DataAccessEvents)
	return c
}

func (g *Generator) checkBackup(fw Framework, id string) Control {
	c := Control{
		ID:          id,
		Framework:   fw,
		Category:    "Backup",
		Description: "Regular backups for disaster recovery",
	}
	if g.config.BackupEnabled {
		c.Status = StatusPass
		c.Details = "Backups are enabled"
	} else {
		c.Status = StatusFail
		c.Details = "Backups are not configured"
	}
	return c
}

func (g *Generator) checkRetention(fw Framework, id string) Control {
	c := Control{
		ID:          id,
		Framework:   fw,
		Category:    "Data Retention",
		Description: "Data retention policy meets minimum requirements (6 years for HIPAA)",
	}
	minDays := 365 * 6 // HIPAA requires 6-year retention
	switch {
	case g.config.RetentionDays >= minDays:
		c.Status = StatusPass
		c.Details = fmt.Sprintf("Retention set to %d days (%d years)", g.config.RetentionDays, g.config.RetentionDays/365)
	case g.config.RetentionDays > 0:
		c.Status = StatusWarning
		c.Details = fmt.Sprintf("Retention set to %d days, minimum recommended is %d days", g.config.RetentionDays, minDays)
	default:
		c.Status = StatusFail
		c.Details = "No retention policy configured"
	}
	return c
}

// Handler serves compliance reports over HTTP.
type Handler struct {
	generator *Generator
}

// NewHandler creates a new compliance HTTP handler.
func NewHandler(gen *Generator) *Handler {
	return &Handler{generator: gen}
}

// RegisterRoutes registers compliance endpoints on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/compliance/report", h.handleReport)
}

func (h *Handler) handleReport(w http.ResponseWriter, r *http.Request) {
	end := time.Now().UTC()
	start := end.AddDate(0, -1, 0) // default to last 30 days

	if s := r.URL.Query().Get("start"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			http.Error(w, "invalid start parameter: "+err.Error(), http.StatusBadRequest)
			return
		}
		start = t
	}
	if e := r.URL.Query().Get("end"); e != "" {
		t, err := time.Parse(time.RFC3339, e)
		if err != nil {
			http.Error(w, "invalid end parameter: "+err.Error(), http.StatusBadRequest)
			return
		}
		end = t
	}

	report, err := h.generator.Generate(r.Context(), start, end)
	if err != nil {
		http.Error(w, "failed to generate report: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(report); err != nil {
		http.Error(w, "failed to encode report", http.StatusInternalServerError)
	}
}
