package compliance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/audit"
)

func baseEvents(start time.Time) []audit.Event {
	return []audit.Event{
		{Timestamp: start.Add(time.Hour), Type: audit.EventAuth, Action: "login", Actor: "user1", Success: true},
		{Timestamp: start.Add(2 * time.Hour), Type: audit.EventAuthFailure, Action: "login", Actor: "attacker", Success: false},
		{Timestamp: start.Add(3 * time.Hour), Type: audit.EventDataAccess, Action: "access", Actor: "user1", Resource: "records"},
		{Timestamp: start.Add(4 * time.Hour), Type: audit.EventConfigChange, Action: "config_change", Actor: "admin", Resource: "server"},
	}
}

func TestGenerator_Generate_AllPass(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	gen := NewGenerator(
		&InMemoryCollector{Events: baseEvents(start)},
		CheckConfig{
			EncryptionEnabled:  true,
			AuditLoggingActive: true,
			TLSEnabled:         true,
			BackupEnabled:      true,
			AccessControlled:   true,
			RetentionDays:      365 * 7,
		},
	)

	report, err := gen.Generate(context.Background(), start, end)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if report.Summary.Total == 0 {
		t.Fatal("expected non-zero total controls")
	}
	if report.Summary.Failed != 0 {
		t.Errorf("expected 0 failures, got %d", report.Summary.Failed)
		for _, c := range report.Controls {
			if c.Status == StatusFail {
				t.Logf("  FAIL: %s - %s: %s", c.ID, c.Description, c.Details)
			}
		}
	}
	if report.PeriodStart != start || report.PeriodEnd != end {
		t.Error("report period does not match request")
	}
}

func TestGenerator_Generate_Failures(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	gen := NewGenerator(
		&InMemoryCollector{Events: baseEvents(start)},
		CheckConfig{
			EncryptionEnabled:  false,
			AuditLoggingActive: false,
			TLSEnabled:         false,
			BackupEnabled:      false,
			AccessControlled:   false,
			RetentionDays:      0,
		},
	)

	report, err := gen.Generate(context.Background(), start, end)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if report.Summary.Failed == 0 {
		t.Error("expected failures when everything is disabled")
	}
}

func TestGenerator_Generate_NoEvents(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	gen := NewGenerator(
		&InMemoryCollector{Events: nil},
		CheckConfig{
			AuditLoggingActive: true,
			EncryptionEnabled:  true,
			TLSEnabled:         true,
			AccessControlled:   true,
			BackupEnabled:      true,
			RetentionDays:      365 * 7,
		},
	)

	report, err := gen.Generate(context.Background(), start, end)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if report.Summary.Warnings == 0 {
		t.Error("expected warnings when audit logging is active but no events")
	}
}

func TestGenerator_Generate_HighAuthFailures(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	var events []audit.Event
	for i := range 150 {
		events = append(events, audit.Event{
			Timestamp: start.Add(time.Duration(i) * time.Minute),
			Type:      audit.EventAuthFailure,
			Action:    "login",
			Actor:     "attacker",
			Success:   false,
		})
	}

	gen := NewGenerator(
		&InMemoryCollector{Events: events},
		CheckConfig{
			AuditLoggingActive: true,
			EncryptionEnabled:  true,
			TLSEnabled:         true,
			AccessControlled:   true,
			BackupEnabled:      true,
			RetentionDays:      365 * 7,
		},
	)

	report, err := gen.Generate(context.Background(), start, end)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if report.Summary.Warnings == 0 {
		t.Error("expected warnings for high auth failure count")
	}
}

func TestInMemoryCollector_FiltersByTime(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	events := []audit.Event{
		{Timestamp: start.Add(-time.Hour), Type: audit.EventAuth, Action: "login"},     // before
		{Timestamp: start.Add(time.Hour), Type: audit.EventAuth, Action: "login"},      // in range
		{Timestamp: start.Add(25 * time.Hour), Type: audit.EventAuth, Action: "login"}, // after
	}

	c := &InMemoryCollector{Events: events}
	end := start.Add(24 * time.Hour)
	got, err := c.CollectEvents(context.Background(), start, end)
	if err != nil {
		t.Fatalf("CollectEvents: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 event in range, got %d", len(got))
	}
}

func TestReport_SOC2Controls(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	gen := NewGenerator(
		&InMemoryCollector{Events: baseEvents(start)},
		CheckConfig{
			EncryptionEnabled:  true,
			AuditLoggingActive: true,
			TLSEnabled:         true,
			AccessControlled:   true,
		},
	)

	report, err := gen.Generate(context.Background(), start, end)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	soc2Count := 0
	for _, c := range report.Controls {
		if c.Framework == FrameworkSOC2 {
			soc2Count++
		}
	}
	if soc2Count == 0 {
		t.Error("expected SOC2 controls in report")
	}
}

func TestReport_HIPAAControls(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	gen := NewGenerator(
		&InMemoryCollector{Events: baseEvents(start)},
		CheckConfig{
			EncryptionEnabled:  true,
			AuditLoggingActive: true,
			AccessControlled:   true,
			BackupEnabled:      true,
			RetentionDays:      365 * 7,
		},
	)

	report, err := gen.Generate(context.Background(), start, end)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	hipaaCount := 0
	for _, c := range report.Controls {
		if c.Framework == FrameworkHIPAA {
			hipaaCount++
		}
	}
	if hipaaCount == 0 {
		t.Error("expected HIPAA controls in report")
	}
}

// --- HTTP Handler Tests ---

func TestHandler_Report_Default(t *testing.T) {
	gen := NewGenerator(
		&InMemoryCollector{Events: nil},
		CheckConfig{
			EncryptionEnabled:  true,
			AuditLoggingActive: true,
			TLSEnabled:         true,
			AccessControlled:   true,
			BackupEnabled:      true,
			RetentionDays:      365 * 7,
		},
	)

	mux := http.NewServeMux()
	NewHandler(gen).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/compliance/report", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var report Report
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("failed to decode report: %v", err)
	}
	if report.Summary.Total == 0 {
		t.Error("expected non-zero controls in report")
	}
}

func TestHandler_Report_WithDateParams(t *testing.T) {
	gen := NewGenerator(
		&InMemoryCollector{Events: nil},
		CheckConfig{AuditLoggingActive: true, EncryptionEnabled: true, TLSEnabled: true, AccessControlled: true, BackupEnabled: true, RetentionDays: 365 * 7},
	)

	mux := http.NewServeMux()
	NewHandler(gen).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/compliance/report?start=2025-01-01T00:00:00Z&end=2025-02-01T00:00:00Z", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var report Report
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("failed to decode report: %v", err)
	}

	expectedStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if !report.PeriodStart.Equal(expectedStart) {
		t.Errorf("expected start %v, got %v", expectedStart, report.PeriodStart)
	}
}

func TestHandler_Report_InvalidStartParam(t *testing.T) {
	gen := NewGenerator(&InMemoryCollector{}, CheckConfig{})
	mux := http.NewServeMux()
	NewHandler(gen).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/compliance/report?start=not-a-date", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandler_Report_InvalidEndParam(t *testing.T) {
	gen := NewGenerator(&InMemoryCollector{}, CheckConfig{})
	mux := http.NewServeMux()
	NewHandler(gen).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/compliance/report?end=not-a-date", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestSummary_Counts(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	gen := NewGenerator(
		&InMemoryCollector{Events: baseEvents(start)},
		CheckConfig{
			EncryptionEnabled:  true,
			AuditLoggingActive: true,
			TLSEnabled:         true,
			AccessControlled:   true,
			BackupEnabled:      true,
			RetentionDays:      365 * 7,
		},
	)

	report, err := gen.Generate(context.Background(), start, end)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	total := report.Summary.Passed + report.Summary.Failed + report.Summary.Warnings
	if total != report.Summary.Total {
		t.Errorf("summary counts don't add up: %d + %d + %d != %d",
			report.Summary.Passed, report.Summary.Failed, report.Summary.Warnings, report.Summary.Total)
	}
}
