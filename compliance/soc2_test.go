package compliance

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Audit Log Tests
// ---------------------------------------------------------------------------

func TestAuditLogRecord(t *testing.T) {
	log := NewInMemoryAuditLog()
	ctx := context.Background()

	entry := &AuditEntry{
		ActorID:    "user-1",
		ActorType:  "user",
		Action:     "create",
		Resource:   "workflow",
		ResourceID: "wf-123",
		TenantID:   "tenant-abc",
		Success:    true,
	}

	if err := log.Record(ctx, entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Verify ID and timestamp were assigned
	if entry.ID == "" {
		t.Error("expected ID to be assigned")
	}
	if entry.Timestamp.IsZero() {
		t.Error("expected Timestamp to be assigned")
	}

	// Query back
	entries, err := log.Query(ctx, AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ActorID != "user-1" {
		t.Errorf("expected actor_id user-1, got %s", entries[0].ActorID)
	}
	if entries[0].Resource != "workflow" {
		t.Errorf("expected resource workflow, got %s", entries[0].Resource)
	}
}

func TestAuditLogRecord_NilEntry(t *testing.T) {
	log := NewInMemoryAuditLog()
	err := log.Record(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil entry")
	}
}

func TestAuditLogQuery(t *testing.T) {
	log := NewInMemoryAuditLog()
	ctx := context.Background()

	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	entries := []*AuditEntry{
		{ActorID: "user-1", Action: "create", Resource: "workflow", TenantID: "tenant-a", Success: true, Timestamp: now},
		{ActorID: "user-2", Action: "delete", Resource: "workflow", TenantID: "tenant-a", Success: true, Timestamp: now.Add(time.Hour)},
		{ActorID: "user-1", Action: "login", Resource: "session", TenantID: "tenant-b", Success: false, Timestamp: now.Add(2 * time.Hour)},
		{ActorID: "system", Action: "execute", Resource: "workflow", TenantID: "tenant-a", Success: true, Timestamp: now.Add(3 * time.Hour)},
		{ActorID: "user-1", Action: "update", Resource: "project", TenantID: "tenant-a", Success: true, Timestamp: now.Add(4 * time.Hour)},
	}
	for _, e := range entries {
		if err := log.Record(ctx, e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	tests := []struct {
		name     string
		filter   AuditFilter
		expected int
	}{
		{"filter by actor", AuditFilter{ActorID: "user-1"}, 3},
		{"filter by action", AuditFilter{Action: "create"}, 1},
		{"filter by resource", AuditFilter{Resource: "workflow"}, 3},
		{"filter by tenant", AuditFilter{TenantID: "tenant-b"}, 1},
		{"filter by success=true", AuditFilter{Success: boolPtr(true)}, 4},
		{"filter by success=false", AuditFilter{Success: boolPtr(false)}, 1},
		{
			"filter by time range",
			AuditFilter{
				StartTime: timePtr(now.Add(30 * time.Minute)),
				EndTime:   timePtr(now.Add(3*time.Hour + 30*time.Minute)),
			},
			3,
		},
		{"filter by actor + resource", AuditFilter{ActorID: "user-1", Resource: "workflow"}, 1},
		{"no filter returns all", AuditFilter{}, 5},
		{"limit", AuditFilter{Limit: 2}, 2},
		{"offset", AuditFilter{Offset: 3}, 2},
		{"limit + offset", AuditFilter{Limit: 2, Offset: 1}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := log.Query(ctx, tt.filter)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(results) != tt.expected {
				t.Errorf("expected %d results, got %d", tt.expected, len(results))
			}
		})
	}
}

func TestAuditLogCount(t *testing.T) {
	log := NewInMemoryAuditLog()
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		success := i%2 == 0
		_ = log.Record(ctx, &AuditEntry{
			ActorID:  "user-1",
			Action:   "read",
			Resource: "workflow",
			TenantID: "t1",
			Success:  success,
		})
	}

	// Count all
	total, err := log.Count(ctx, AuditFilter{})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if total != 10 {
		t.Errorf("expected 10 total, got %d", total)
	}

	// Count successful
	successCount, err := log.Count(ctx, AuditFilter{Success: boolPtr(true)})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if successCount != 5 {
		t.Errorf("expected 5 successful, got %d", successCount)
	}
}

func TestAuditLogExportJSON(t *testing.T) {
	log := NewInMemoryAuditLog()
	ctx := context.Background()

	_ = log.Record(ctx, &AuditEntry{
		ActorID:  "user-1",
		Action:   "create",
		Resource: "workflow",
		Success:  true,
	})
	_ = log.Record(ctx, &AuditEntry{
		ActorID:  "user-2",
		Action:   "delete",
		Resource: "project",
		Success:  false,
		ErrorMsg: "permission denied",
	})

	data, err := log.Export(ctx, AuditFilter{}, "json")
	if err != nil {
		t.Fatalf("Export JSON: %v", err)
	}

	var exported []*AuditEntry
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(exported) != 2 {
		t.Errorf("expected 2 entries in export, got %d", len(exported))
	}
	if exported[1].ErrorMsg != "permission denied" {
		t.Errorf("expected error_message 'permission denied', got %q", exported[1].ErrorMsg)
	}
}

func TestAuditLogExportCSV(t *testing.T) {
	log := NewInMemoryAuditLog()
	ctx := context.Background()

	_ = log.Record(ctx, &AuditEntry{
		ActorID:   "user-1",
		ActorType: "user",
		Action:    "login",
		Resource:  "session",
		TenantID:  "tenant-a",
		IPAddress: "10.0.0.1",
		Success:   true,
	})

	data, err := log.Export(ctx, AuditFilter{}, "csv")
	if err != nil {
		t.Fatalf("Export CSV: %v", err)
	}

	r := csv.NewReader(strings.NewReader(string(data)))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("Read CSV: %v", err)
	}

	// Header + 1 data row
	if len(records) != 2 {
		t.Fatalf("expected 2 CSV rows (header + data), got %d", len(records))
	}

	header := records[0]
	if header[0] != "id" || header[4] != "action" {
		t.Errorf("unexpected CSV header: %v", header)
	}

	row := records[1]
	if row[2] != "user-1" { // actor_id
		t.Errorf("expected actor_id user-1, got %s", row[2])
	}
	if row[10] != "true" { // success
		t.Errorf("expected success true, got %s", row[10])
	}
}

func TestAuditLogExportUnsupportedFormat(t *testing.T) {
	log := NewInMemoryAuditLog()
	_, err := log.Export(context.Background(), AuditFilter{}, "xml")
	if err == nil {
		t.Error("expected error for unsupported format")
	}
}

// ---------------------------------------------------------------------------
// SOC2 Control Registry Tests
// ---------------------------------------------------------------------------

func TestControlRegistry(t *testing.T) {
	reg := NewControlRegistry()

	// Register a control
	ctrl := &SOC2Control{
		ID:          "CC6.1",
		Category:    "Security",
		Title:       "Logical Access Controls",
		Description: "Test control",
		Status:      ControlStatusPlanned,
	}
	reg.Register(ctrl)

	// Get it back
	got, ok := reg.Get("CC6.1")
	if !ok {
		t.Fatal("expected to find CC6.1")
	}
	if got.Title != "Logical Access Controls" {
		t.Errorf("expected title 'Logical Access Controls', got %q", got.Title)
	}

	// Get non-existent
	_, ok = reg.Get("NONEXISTENT")
	if ok {
		t.Error("expected not to find NONEXISTENT")
	}

	// List all
	all := reg.List("")
	if len(all) != 1 {
		t.Errorf("expected 1 control, got %d", len(all))
	}

	// List by category
	security := reg.List("Security")
	if len(security) != 1 {
		t.Errorf("expected 1 Security control, got %d", len(security))
	}
	privacy := reg.List("Privacy")
	if len(privacy) != 0 {
		t.Errorf("expected 0 Privacy controls, got %d", len(privacy))
	}

	// Update status
	if err := reg.UpdateStatus("CC6.1", ControlStatusImplemented); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ = reg.Get("CC6.1")
	if got.Status != ControlStatusImplemented {
		t.Errorf("expected status implemented, got %s", got.Status)
	}
	if got.LastReview.IsZero() {
		t.Error("expected LastReview to be set after UpdateStatus")
	}

	// Update non-existent
	if err := reg.UpdateStatus("NONEXISTENT", ControlStatusImplemented); err == nil {
		t.Error("expected error for non-existent control")
	}
}

func TestControlRegistryDefaults(t *testing.T) {
	reg := NewControlRegistry()
	reg.RegisterDefaults()

	all := reg.List("")
	if len(all) < 15 {
		t.Errorf("expected at least 15 default controls, got %d", len(all))
	}

	// Verify all 5 categories are present
	categories := make(map[string]bool)
	for _, c := range all {
		categories[c.Category] = true
	}
	expected := []string{"Security", "Availability", "Processing Integrity", "Confidentiality", "Privacy"}
	for _, cat := range expected {
		if !categories[cat] {
			t.Errorf("expected category %q in defaults", cat)
		}
	}

	// Verify some specific controls exist
	for _, id := range []string{"CC6.1", "CC6.7", "A1.1", "PI1.1", "C1.1", "P1.1", "CC7.2"} {
		if _, ok := reg.Get(id); !ok {
			t.Errorf("expected default control %s to be registered", id)
		}
	}
}

func TestAddEvidence(t *testing.T) {
	reg := NewControlRegistry()
	reg.RegisterDefaults()

	evidence := EvidenceItem{
		Type:        "automated_test",
		Description: "RBAC enforcement test passes",
		Source:      "TestRBACEnforcement",
		CollectedAt: time.Now().UTC(),
		Valid:       true,
	}

	err := reg.AddEvidence("CC6.3", evidence)
	if err != nil {
		t.Fatalf("AddEvidence: %v", err)
	}

	ctrl, _ := reg.Get("CC6.3")
	if len(ctrl.Evidence) != 1 {
		t.Fatalf("expected 1 evidence item, got %d", len(ctrl.Evidence))
	}
	if ctrl.Evidence[0].Type != "automated_test" {
		t.Errorf("expected evidence type 'automated_test', got %q", ctrl.Evidence[0].Type)
	}

	// Add to non-existent control
	err = reg.AddEvidence("NONEXISTENT", evidence)
	if err == nil {
		t.Error("expected error for non-existent control")
	}
}

func TestComplianceScore(t *testing.T) {
	reg := NewControlRegistry()

	// Empty registry
	if score := reg.ComplianceScore(); score != 0 {
		t.Errorf("expected 0 score for empty registry, got %f", score)
	}

	// Register 4 controls
	reg.Register(&SOC2Control{ID: "T1", Category: "Security", Status: ControlStatusImplemented})
	reg.Register(&SOC2Control{ID: "T2", Category: "Security", Status: ControlStatusImplemented})
	reg.Register(&SOC2Control{ID: "T3", Category: "Security", Status: ControlStatusPlanned})
	reg.Register(&SOC2Control{ID: "T4", Category: "Security", Status: ControlStatusNotApplicable})

	// 2 implemented out of 3 applicable = 66.67%
	score := reg.ComplianceScore()
	if score < 66.0 || score > 67.0 {
		t.Errorf("expected ~66.67%% score, got %f", score)
	}

	// Mark remaining as implemented
	_ = reg.UpdateStatus("T3", ControlStatusImplemented)
	score = reg.ComplianceScore()
	if score != 100 {
		t.Errorf("expected 100%% score, got %f", score)
	}
}

func TestGenerateReport(t *testing.T) {
	reg := NewControlRegistry()
	reg.RegisterDefaults()

	// Mark some as implemented
	_ = reg.UpdateStatus("CC6.1", ControlStatusImplemented)
	_ = reg.UpdateStatus("CC6.7", ControlStatusImplemented)
	_ = reg.UpdateStatus("A1.1", ControlStatusPartial)

	report := reg.GenerateReport()

	if report.TotalControls < 15 {
		t.Errorf("expected at least 15 total controls, got %d", report.TotalControls)
	}
	if report.Implemented != 2 {
		t.Errorf("expected 2 implemented, got %d", report.Implemented)
	}
	if report.Partial != 1 {
		t.Errorf("expected 1 partial, got %d", report.Partial)
	}
	if report.GeneratedAt.IsZero() {
		t.Error("expected GeneratedAt to be set")
	}
	if report.Score <= 0 {
		t.Error("expected positive score with some implemented controls")
	}
	if len(report.ByCategory) == 0 {
		t.Error("expected ByCategory to be populated")
	}
	if report.ByCategory["Security"] == 0 {
		t.Error("expected Security category count > 0")
	}
}

// ---------------------------------------------------------------------------
// Retention Policy Tests
// ---------------------------------------------------------------------------

func TestRetentionPolicy(t *testing.T) {
	mgr := NewRetentionManager(nil)

	policy := &DataRetentionPolicy{
		Name:           "Test Audit Logs",
		DataType:       "audit_logs",
		RetentionDays:  365,
		ArchiveEnabled: true,
		ArchiveFormat:  "json",
	}

	mgr.AddPolicy(policy)

	got, ok := mgr.GetPolicy("audit_logs")
	if !ok {
		t.Fatal("expected to find audit_logs policy")
	}
	if got.RetentionDays != 365 {
		t.Errorf("expected 365 days, got %d", got.RetentionDays)
	}

	// Non-existent
	_, ok = mgr.GetPolicy("unknown")
	if ok {
		t.Error("expected not to find unknown policy")
	}

	// List
	policies := mgr.ListPolicies()
	if len(policies) != 1 {
		t.Errorf("expected 1 policy, got %d", len(policies))
	}

	// Add nil (should be no-op)
	mgr.AddPolicy(nil)
	policies = mgr.ListPolicies()
	if len(policies) != 1 {
		t.Errorf("expected 1 policy after nil add, got %d", len(policies))
	}
}

func TestRetentionShouldRetain(t *testing.T) {
	mgr := NewRetentionManager(nil)
	mgr.AddPolicy(&DataRetentionPolicy{
		Name:          "Audit Logs",
		DataType:      "audit_logs",
		RetentionDays: 90,
	})

	// Data from 30 days ago: should retain
	recent := time.Now().UTC().AddDate(0, 0, -30)
	if !mgr.ShouldRetain("audit_logs", recent) {
		t.Error("expected to retain data from 30 days ago (policy: 90 days)")
	}

	// Data from 180 days ago: should NOT retain
	old := time.Now().UTC().AddDate(0, 0, -180)
	if mgr.ShouldRetain("audit_logs", old) {
		t.Error("expected NOT to retain data from 180 days ago (policy: 90 days)")
	}

	// Data from exactly the boundary
	boundary := time.Now().UTC().AddDate(0, 0, -90)
	// At the boundary, the data may or may not be retained depending on time precision,
	// but data from before the boundary should not be retained
	veryOld := boundary.Add(-time.Hour)
	if mgr.ShouldRetain("audit_logs", veryOld) {
		t.Error("expected NOT to retain data from beyond the retention boundary")
	}

	// No policy means retain
	if !mgr.ShouldRetain("unknown_type", old) {
		t.Error("expected to retain data when no policy exists (conservative default)")
	}
}

func TestRetentionDefaultPolicies(t *testing.T) {
	defaults := DefaultPolicies()

	if len(defaults) < 4 {
		t.Errorf("expected at least 4 default policies, got %d", len(defaults))
	}

	types := make(map[string]bool)
	for _, p := range defaults {
		types[p.DataType] = true
		if p.RetentionDays <= 0 {
			t.Errorf("policy %q has invalid retention days: %d", p.DataType, p.RetentionDays)
		}
		if p.Name == "" {
			t.Errorf("policy %q has empty name", p.DataType)
		}
	}

	// Verify key data types are covered
	for _, dt := range []string{"audit_logs", "executions", "events", "dlq_entries"} {
		if !types[dt] {
			t.Errorf("expected default policy for %q", dt)
		}
	}
}

// ---------------------------------------------------------------------------
// Concurrent Audit Log Test
// ---------------------------------------------------------------------------

func TestConcurrentAuditLog(t *testing.T) {
	log := NewInMemoryAuditLog()
	ctx := context.Background()

	const goroutines = 20
	const entriesPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(gID int) {
			defer wg.Done()
			for i := 0; i < entriesPerGoroutine; i++ {
				_ = log.Record(ctx, &AuditEntry{
					ActorID:  "user-concurrent",
					Action:   "create",
					Resource: "workflow",
					TenantID: "tenant-stress",
					Success:  true,
				})
			}
		}(g)
	}

	wg.Wait()

	count, err := log.Count(ctx, AuditFilter{})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	expected := int64(goroutines * entriesPerGoroutine)
	if count != expected {
		t.Errorf("expected %d entries after concurrent writes, got %d", expected, count)
	}

	// Also verify queries work correctly after concurrent writes
	entries, err := log.Query(ctx, AuditFilter{Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 10 {
		t.Errorf("expected 10 entries with limit, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// SQLite Audit Log Tests
// ---------------------------------------------------------------------------

func TestSQLiteAuditLog(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit_test.db")
	slog, err := NewSQLiteAuditLog(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteAuditLog: %v", err)
	}
	defer slog.Close()

	ctx := context.Background()

	// Record
	entry := &AuditEntry{
		ActorID:    "user-1",
		ActorType:  "user",
		Action:     "create",
		Resource:   "workflow",
		ResourceID: "wf-456",
		TenantID:   "tenant-x",
		IPAddress:  "192.168.1.1",
		UserAgent:  "test-agent",
		Success:    true,
		Details:    map[string]any{"key": "value"},
	}
	if err := slog.Record(ctx, entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Record a second entry
	entry2 := &AuditEntry{
		ActorID:  "user-2",
		Action:   "delete",
		Resource: "project",
		TenantID: "tenant-x",
		Success:  false,
		ErrorMsg: "forbidden",
	}
	if err := slog.Record(ctx, entry2); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Query all
	entries, err := slog.Query(ctx, AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Query by actor
	entries, err = slog.Query(ctx, AuditFilter{ActorID: "user-1"})
	if err != nil {
		t.Fatalf("Query by actor: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry for user-1, got %d", len(entries))
	}
	if entries[0].Details["key"] != "value" {
		t.Errorf("expected details key=value, got %v", entries[0].Details)
	}

	// Query by success
	entries, err = slog.Query(ctx, AuditFilter{Success: boolPtr(false)})
	if err != nil {
		t.Fatalf("Query by success: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 failed entry, got %d", len(entries))
	}
	if entries[0].ErrorMsg != "forbidden" {
		t.Errorf("expected error_message 'forbidden', got %q", entries[0].ErrorMsg)
	}

	// Count
	count, err := slog.Count(ctx, AuditFilter{TenantID: "tenant-x"})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}

	// Export JSON
	data, err := slog.Export(ctx, AuditFilter{}, "json")
	if err != nil {
		t.Fatalf("Export JSON: %v", err)
	}
	var exported []*AuditEntry
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(exported) != 2 {
		t.Errorf("expected 2 exported entries, got %d", len(exported))
	}

	// Export CSV
	csvData, err := slog.Export(ctx, AuditFilter{}, "csv")
	if err != nil {
		t.Fatalf("Export CSV: %v", err)
	}
	if !strings.Contains(string(csvData), "user-1") {
		t.Error("expected CSV to contain user-1")
	}
}

func TestSQLiteAuditLog_NilEntry(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit_nil.db")
	slog, err := NewSQLiteAuditLog(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteAuditLog: %v", err)
	}
	defer slog.Close()

	if err := slog.Record(context.Background(), nil); err == nil {
		t.Error("expected error for nil entry")
	}
}

func TestSQLiteAuditLog_InvalidPath(t *testing.T) {
	// Attempt to open in a directory that does not exist
	_, err := NewSQLiteAuditLog("/nonexistent/path/audit.db")
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

// ---------------------------------------------------------------------------
// HTTP Handler Tests
// ---------------------------------------------------------------------------

func newTestComplianceHandler() (*ComplianceHandler, *InMemoryAuditLog) {
	auditLog := NewInMemoryAuditLog()
	registry := NewControlRegistry()
	registry.RegisterDefaults()
	_ = registry.UpdateStatus("CC6.1", ControlStatusImplemented)
	_ = registry.UpdateStatus("CC6.7", ControlStatusImplemented)

	retention := NewRetentionManager(nil)
	for _, p := range DefaultPolicies() {
		retention.AddPolicy(p)
	}

	handler := NewComplianceHandler(auditLog, registry, retention)
	return handler, auditLog
}

func TestHandlerQueryAudit(t *testing.T) {
	handler, auditLog := newTestComplianceHandler()
	ctx := context.Background()

	_ = auditLog.Record(ctx, &AuditEntry{ActorID: "user-1", Action: "create", Resource: "workflow", Success: true})
	_ = auditLog.Record(ctx, &AuditEntry{ActorID: "user-2", Action: "delete", Resource: "project", Success: true})

	mux := http.NewServeMux()
	handler.RegisterComplianceRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/audit?actor_id=user-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	items, ok := resp["items"].([]any)
	if !ok {
		t.Fatal("expected items array in response")
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item for user-1, got %d", len(items))
	}
}

func TestHandlerExportAudit(t *testing.T) {
	handler, auditLog := newTestComplianceHandler()
	ctx := context.Background()

	_ = auditLog.Record(ctx, &AuditEntry{ActorID: "user-1", Action: "login", Success: true})

	mux := http.NewServeMux()
	handler.RegisterComplianceRoutes(mux)

	// JSON export
	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/audit/export?format=json", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	// CSV export
	req = httptest.NewRequest(http.MethodGet, "/api/v1/compliance/audit/export?format=csv", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv" {
		t.Errorf("expected Content-Type text/csv, got %s", ct)
	}
}

func TestHandlerListControls(t *testing.T) {
	handler, _ := newTestComplianceHandler()

	mux := http.NewServeMux()
	handler.RegisterComplianceRoutes(mux)

	// All controls
	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/controls", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	total := int(resp["total"].(float64))
	if total < 15 {
		t.Errorf("expected at least 15 controls, got %d", total)
	}

	// Filter by category
	req = httptest.NewRequest(http.MethodGet, "/api/v1/compliance/controls?category=Security", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandlerGetControl(t *testing.T) {
	handler, _ := newTestComplianceHandler()

	mux := http.NewServeMux()
	handler.RegisterComplianceRoutes(mux)

	// Existing control
	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/controls/CC6.1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var ctrl SOC2Control
	if err := json.NewDecoder(w.Body).Decode(&ctrl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ctrl.ID != "CC6.1" {
		t.Errorf("expected ID CC6.1, got %s", ctrl.ID)
	}

	// Non-existent control
	req = httptest.NewRequest(http.MethodGet, "/api/v1/compliance/controls/NONEXISTENT", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandlerGenerateReport(t *testing.T) {
	handler, _ := newTestComplianceHandler()

	mux := http.NewServeMux()
	handler.RegisterComplianceRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/report", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var report ComplianceReport
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if report.TotalControls < 15 {
		t.Errorf("expected at least 15 controls, got %d", report.TotalControls)
	}
	if report.Score <= 0 {
		t.Error("expected positive score")
	}
}

func TestHandlerComplianceScore(t *testing.T) {
	handler, _ := newTestComplianceHandler()

	mux := http.NewServeMux()
	handler.RegisterComplianceRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/score", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	score, ok := resp["score"].(float64)
	if !ok {
		t.Fatal("expected score field")
	}
	if score <= 0 {
		t.Error("expected positive score")
	}
}

func TestHandlerListRetention(t *testing.T) {
	handler, _ := newTestComplianceHandler()

	mux := http.NewServeMux()
	handler.RegisterComplianceRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/retention", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	total := int(resp["total"].(float64))
	if total < 4 {
		t.Errorf("expected at least 4 retention policies, got %d", total)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func boolPtr(b bool) *bool {
	return &b
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// Ensure file was read for os import
var _ = os.TempDir
