package module

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPartitionedDatabase_PartitionKey(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		DSN:          "postgres://localhost/test",
		PartitionKey: "tenant_id",
		Tables:       []string{"forms", "submissions"},
	}
	pd := NewPartitionedDatabase("db", cfg)

	if pd.PartitionKey() != "tenant_id" {
		t.Errorf("expected tenant_id, got %q", pd.PartitionKey())
	}
	if pd.Name() != "db" {
		t.Errorf("expected name 'db', got %q", pd.Name())
	}
	tables := pd.Tables()
	if len(tables) != 2 {
		t.Errorf("expected 2 tables, got %d", len(tables))
	}
}

func TestPartitionedDatabase_Defaults(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		PartitionKey: "tenant_id",
	}
	pd := NewPartitionedDatabase("db", cfg)

	if pd.PartitionType() != PartitionTypeList {
		t.Errorf("expected default partition type %q, got %q", PartitionTypeList, pd.PartitionType())
	}
	if pd.PartitionNameFormat() != "{table}_{tenant}" {
		t.Errorf("expected default format, got %q", pd.PartitionNameFormat())
	}
}

func TestPartitionedDatabase_PartitionTableName(t *testing.T) {
	tests := []struct {
		format   string
		table    string
		tenant   string
		expected string
	}{
		{"{table}_{tenant}", "forms", "org-alpha", "forms_org_alpha"},
		{"{tenant}_{table}", "forms", "org-alpha", "org_alpha_forms"},
		{"{table}_{tenant}", "submissions", "org.beta", "submissions_org_beta"},
		{"", "forms", "org-alpha", "forms_org_alpha"}, // default format
	}

	for _, tc := range tests {
		cfg := PartitionedDatabaseConfig{
			Driver:              "pgx",
			PartitionKey:        "tenant_id",
			PartitionNameFormat: tc.format,
		}
		pd := NewPartitionedDatabase("db", cfg)
		got := pd.PartitionTableName(tc.table, tc.tenant)
		if got != tc.expected {
			t.Errorf("PartitionTableName(format=%q, table=%q, tenant=%q) = %q, want %q",
				tc.format, tc.table, tc.tenant, got, tc.expected)
		}
	}
}

func TestPartitionedDatabase_EnsurePartition_InvalidDriver(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver:       "sqlite3",
		PartitionKey: "tenant_id",
		Tables:       []string{"forms"},
	}
	pd := NewPartitionedDatabase("db", cfg)

	err := pd.EnsurePartition(context.Background(), "org-alpha")
	if err == nil {
		t.Fatal("expected error for non-postgres driver")
	}
}

func TestPartitionedDatabase_EnsurePartition_InvalidTenantValue(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		PartitionKey: "tenant_id",
		Tables:       []string{"forms"},
	}
	pd := NewPartitionedDatabase("db", cfg)

	err := pd.EnsurePartition(context.Background(), "org'; DROP TABLE forms; --")
	if err == nil {
		t.Fatal("expected error for invalid tenant value")
	}
}

func TestPartitionedDatabase_EnsurePartition_InvalidPartitionKey(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		PartitionKey: "bad column name!",
		Tables:       []string{"forms"},
	}
	pd := NewPartitionedDatabase("db", cfg)

	err := pd.EnsurePartition(context.Background(), "org-alpha")
	if err == nil {
		t.Fatal("expected error for invalid partition key")
	}
}

func TestPartitionedDatabase_EnsurePartition_UnsupportedType(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver:        "pgx",
		PartitionKey:  "tenant_id",
		Tables:        []string{"forms"},
		PartitionType: "hash",
	}
	pd := NewPartitionedDatabase("db", cfg)
	// DB is nil — but the partition type check should happen before the nil check
	err := pd.EnsurePartition(context.Background(), "org-alpha")
	if err == nil {
		t.Fatal("expected error for unsupported partition type")
	}
}

func TestPartitionedDatabase_SyncPartitionsFromSource_NoSourceTable(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		PartitionKey: "tenant_id",
		Tables:       []string{"forms"},
	}
	pd := NewPartitionedDatabase("db", cfg)

	// No source table => no-op
	err := pd.SyncPartitionsFromSource(context.Background())
	if err != nil {
		t.Fatalf("expected no-op when sourceTable is empty, got: %v", err)
	}
}

func TestPartitionedDatabase_SyncPartitionsFromSource_InvalidSourceTable(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		PartitionKey: "tenant_id",
		Tables:       []string{"forms"},
		SourceTable:  "invalid table!",
	}
	pd := NewPartitionedDatabase("db", cfg)

	err := pd.SyncPartitionsFromSource(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid source table name")
	}
}

func TestSanitizePartitionSuffix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"org-alpha", "org_alpha"},
		{"org.beta", "org_beta"},
		{"tenant_1", "tenant_1"},
		{"org-my.tenant", "org_my_tenant"},
	}

	for _, tc := range tests {
		got := sanitizePartitionSuffix(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizePartitionSuffix(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestIsSupportedPartitionDriver(t *testing.T) {
	supported := []string{"pgx", "pgx/v5", "postgres"}
	for _, d := range supported {
		if !isSupportedPartitionDriver(d) {
			t.Errorf("expected %q to be supported", d)
		}
	}

	unsupported := []string{"sqlite3", "sqlite", "mysql", "", "postgresql"}
	for _, d := range unsupported {
		if isSupportedPartitionDriver(d) {
			t.Errorf("expected %q to be unsupported", d)
		}
	}
}

// testMultiPartitionManager extends testPartitionManager with MultiPartitionManager support.
type testMultiPartitionManager struct {
	testPartitionManager
	configs                []PartitionConfig
	ensureForKeyCalledWith []struct{ key, value string }
	syncForKeyCalledWith   []string
	ensureForKeyErr        error
	syncForKeyErr          error
}

func (m *testMultiPartitionManager) PartitionConfigs() []PartitionConfig { return m.configs }

func (m *testMultiPartitionManager) EnsurePartitionForKey(_ context.Context, partitionKey, value string) error {
	m.ensureForKeyCalledWith = append(m.ensureForKeyCalledWith, struct{ key, value string }{partitionKey, value})
	return m.ensureForKeyErr
}

func (m *testMultiPartitionManager) SyncPartitionsForKey(_ context.Context, partitionKey string) error {
	m.syncForKeyCalledWith = append(m.syncForKeyCalledWith, partitionKey)
	return m.syncForKeyErr
}

// ─── Multi-partition config tests ────────────────────────────────────────────

func TestPartitionedDatabase_MultiPartition_NormalizesDefaults(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver: "pgx",
		Partitions: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms"}},
			{PartitionKey: "api_version", Tables: []string{"contracts"}, PartitionType: PartitionTypeRange},
		},
	}
	pd := NewPartitionedDatabase("db", cfg)

	cfgs := pd.PartitionConfigs()
	if len(cfgs) != 2 {
		t.Fatalf("expected 2 partition configs, got %d", len(cfgs))
	}

	// First config gets default type and format
	if cfgs[0].PartitionType != PartitionTypeList {
		t.Errorf("expected first partition type %q, got %q", PartitionTypeList, cfgs[0].PartitionType)
	}
	if cfgs[0].PartitionNameFormat != "{table}_{tenant}" {
		t.Errorf("expected first partition format %q, got %q", "{table}_{tenant}", cfgs[0].PartitionNameFormat)
	}

	// Second config keeps explicit type
	if cfgs[1].PartitionType != PartitionTypeRange {
		t.Errorf("expected second partition type %q, got %q", PartitionTypeRange, cfgs[1].PartitionType)
	}
}

func TestPartitionedDatabase_MultiPartition_PrimaryKeyIsFirst(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver: "pgx",
		Partitions: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms"}},
			{PartitionKey: "api_version", Tables: []string{"contracts"}},
		},
	}
	pd := NewPartitionedDatabase("db", cfg)

	if pd.PartitionKey() != "tenant_id" {
		t.Errorf("expected PartitionKey() = %q, got %q", "tenant_id", pd.PartitionKey())
	}
}

func TestPartitionedDatabase_MultiPartition_TablesReturnsPrimary(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver: "pgx",
		Partitions: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms", "submissions"}},
			{PartitionKey: "api_version", Tables: []string{"contracts"}},
		},
	}
	pd := NewPartitionedDatabase("db", cfg)

	tables := pd.Tables()
	if len(tables) != 2 || tables[0] != "forms" || tables[1] != "submissions" {
		t.Errorf("unexpected Tables() result: %v", tables)
	}
}

func TestPartitionedDatabase_MultiPartition_SinglePartitionFieldsIgnored(t *testing.T) {
	// When Partitions is set, top-level single-partition fields must be ignored.
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		PartitionKey: "should_be_ignored",
		Tables:       []string{"ignored_table"},
		Partitions: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms"}},
		},
	}
	pd := NewPartitionedDatabase("db", cfg)

	if pd.PartitionKey() != "tenant_id" {
		t.Errorf("expected PartitionKey() = %q, got %q", "tenant_id", pd.PartitionKey())
	}
	if len(pd.Tables()) != 1 || pd.Tables()[0] != "forms" {
		t.Errorf("unexpected Tables(): %v", pd.Tables())
	}
}

func TestPartitionedDatabase_EnsurePartitionForKey_InvalidKey(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver: "pgx",
		Partitions: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms"}},
		},
	}
	pd := NewPartitionedDatabase("db", cfg)

	err := pd.EnsurePartitionForKey(context.Background(), "unknown_key", "val")
	if err == nil {
		t.Fatal("expected error for unknown partition key")
	}
	if !strings.Contains(err.Error(), "no partition config found for key") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPartitionedDatabase_SyncPartitionsForKey_InvalidKey(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver: "pgx",
		Partitions: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms"}},
		},
	}
	pd := NewPartitionedDatabase("db", cfg)

	err := pd.SyncPartitionsForKey(context.Background(), "unknown_key")
	if err == nil {
		t.Fatal("expected error for unknown partition key")
	}
	if !strings.Contains(err.Error(), "no partition config found for key") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPartitionedDatabase_SyncPartitionsForKey_NoSourceTable(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver: "pgx",
		Partitions: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms"}},
		},
	}
	pd := NewPartitionedDatabase("db", cfg)

	// No sourceTable on the config → no-op
	err := pd.SyncPartitionsForKey(context.Background(), "tenant_id")
	if err != nil {
		t.Fatalf("expected no-op, got: %v", err)
	}
}

func TestPartitionedDatabase_SyncPartitionsForKey_InvalidSourceTable(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver: "pgx",
		Partitions: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms"}, SourceTable: "invalid table!"},
		},
	}
	pd := NewPartitionedDatabase("db", cfg)

	err := pd.SyncPartitionsForKey(context.Background(), "tenant_id")
	if err == nil {
		t.Fatal("expected error for invalid source table")
	}
}

func TestPartitionedDatabase_PartitionConfigs_ReturnsCopy(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver: "pgx",
		Partitions: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms"}},
			{PartitionKey: "api_version", Tables: []string{"contracts"}},
		},
	}
	pd := NewPartitionedDatabase("db", cfg)

	cfgs1 := pd.PartitionConfigs()
	cfgs1[0].PartitionKey = "mutated" // mutate the returned struct field
	cfgs2 := pd.PartitionConfigs()
	if cfgs2[0].PartitionKey == "mutated" {
		t.Error("PartitionConfigs returned a reference instead of a copy (PartitionKey)")
	}
}

func TestPartitionedDatabase_PartitionConfigs_DeepCopiesTables(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver: "pgx",
		Partitions: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms", "submissions"}},
		},
	}
	pd := NewPartitionedDatabase("db", cfg)

	cfgs1 := pd.PartitionConfigs()
	cfgs1[0].Tables[0] = "mutated_table" // mutate element of the returned Tables slice
	cfgs2 := pd.PartitionConfigs()
	if cfgs2[0].Tables[0] == "mutated_table" {
		t.Error("PartitionConfigs returned a shallow copy: Tables slice element was mutated in internal state")
	}
}

func TestPartitionedDatabase_BackwardCompat_SinglePartition(t *testing.T) {
	// Old-style config without Partitions field must behave exactly as before.
	cfg := PartitionedDatabaseConfig{
		Driver:              "pgx",
		PartitionKey:        "tenant_id",
		Tables:              []string{"forms", "submissions"},
		PartitionType:       PartitionTypeList,
		PartitionNameFormat: "{table}_{tenant}",
	}
	pd := NewPartitionedDatabase("db", cfg)

	cfgs := pd.PartitionConfigs()
	if len(cfgs) != 1 {
		t.Fatalf("expected 1 partition config for backward compat, got %d", len(cfgs))
	}
	if pd.PartitionKey() != "tenant_id" {
		t.Errorf("expected PartitionKey = 'tenant_id', got %q", pd.PartitionKey())
	}
	if pd.PartitionTableName("forms", "org-alpha") != "forms_org_alpha" {
		t.Errorf("unexpected PartitionTableName: %q", pd.PartitionTableName("forms", "org-alpha"))
	}
}

func TestPartitionedDatabase_MultiPartition_EnsurePartitionForKey_InvalidDriver(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver: "sqlite3",
		Partitions: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms"}},
		},
	}
	pd := NewPartitionedDatabase("db", cfg)

	err := pd.EnsurePartitionForKey(context.Background(), "tenant_id", "org-alpha")
	if err == nil {
		t.Fatal("expected error for non-postgres driver")
	}
}

func TestPartitionedDatabase_MultiPartition_EnsurePartitionForKey_InvalidValue(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver: "pgx",
		Partitions: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms"}},
		},
	}
	pd := NewPartitionedDatabase("db", cfg)

	err := pd.EnsurePartitionForKey(context.Background(), "tenant_id", "org'; DROP TABLE forms;--")
	if err == nil {
		t.Fatal("expected error for invalid partition value")
	}
}

func TestPartitionedDatabase_MultiPartition_EnsurePartitionForKey_UnsupportedType(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver: "pgx",
		Partitions: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms"}, PartitionType: "hash"},
		},
	}
	pd := NewPartitionedDatabase("db", cfg)

	// Unsupported type should error — partition type check comes before nil-db check
	err := pd.EnsurePartitionForKey(context.Background(), "tenant_id", "org-alpha")
	if err == nil {
		t.Fatal("expected error for unsupported partition type")
	}
}

func TestPartitionedDatabase_MultiPartition_SyncPartitionsFromSource_AllConfigs(t *testing.T) {
	// SyncPartitionsFromSource with multiple configs that have no sourceTable is a no-op for each.
	cfg := PartitionedDatabaseConfig{
		Driver: "pgx",
		Partitions: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms"}},
			{PartitionKey: "api_version", Tables: []string{"contracts"}},
		},
	}
	pd := NewPartitionedDatabase("db", cfg)

	err := pd.SyncPartitionsFromSource(context.Background())
	if err != nil {
		t.Fatalf("expected no-op with no source tables, got: %v", err)
	}
}

// ─── Step tests for partitionKey field ───────────────────────────────────────

func TestDBCreatePartitionStep_WithPartitionKey(t *testing.T) {
	mgr := &testMultiPartitionManager{
		testPartitionManager: testPartitionManager{partitionKey: "tenant_id"},
		configs: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms"}},
			{PartitionKey: "api_version", Tables: []string{"contracts"}},
		},
	}
	app := NewMockApplication()
	app.Services["multi-db"] = mgr

	factory := NewDBCreatePartitionStepFactory()
	step, err := factory("create-part", map[string]any{
		"database":     "multi-db",
		"tenantKey":    "steps.body.tenant_id",
		"partitionKey": "api_version",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("body", map[string]any{"tenant_id": "v2"})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["tenant"] != "v2" {
		t.Errorf("expected tenant='v2', got %v", result.Output["tenant"])
	}
	if len(mgr.ensureForKeyCalledWith) != 1 ||
		mgr.ensureForKeyCalledWith[0].key != "api_version" ||
		mgr.ensureForKeyCalledWith[0].value != "v2" {
		t.Errorf("unexpected EnsurePartitionForKey calls: %v", mgr.ensureForKeyCalledWith)
	}
}

func TestDBCreatePartitionStep_WithPartitionKey_NotMultiManager(t *testing.T) {
	// Service is a PartitionManager but not MultiPartitionManager; using partitionKey should fail.
	mgr := &testPartitionManager{partitionKey: "tenant_id"}
	app := NewMockApplication()
	app.Services["part-db"] = mgr

	factory := NewDBCreatePartitionStepFactory()
	step, err := factory("create-part", map[string]any{
		"database":     "part-db",
		"tenantKey":    "steps.body.tenant_id",
		"partitionKey": "api_version",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("body", map[string]any{"tenant_id": "v2"})

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when service does not implement MultiPartitionManager")
	}
}

func TestDBSyncPartitionsStep_WithPartitionKey(t *testing.T) {
	mgr := &testMultiPartitionManager{
		testPartitionManager: testPartitionManager{partitionKey: "tenant_id"},
		configs: []PartitionConfig{
			{PartitionKey: "tenant_id", Tables: []string{"forms"}},
			{PartitionKey: "api_version", Tables: []string{"contracts"}},
		},
	}
	app := NewMockApplication()
	app.Services["multi-db"] = mgr

	factory := NewDBSyncPartitionsStepFactory()
	step, err := factory("sync-parts", map[string]any{
		"database":     "multi-db",
		"partitionKey": "api_version",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["synced"] != true {
		t.Errorf("expected synced=true, got %v", result.Output["synced"])
	}
	if len(mgr.syncForKeyCalledWith) != 1 || mgr.syncForKeyCalledWith[0] != "api_version" {
		t.Errorf("unexpected SyncPartitionsForKey calls: %v", mgr.syncForKeyCalledWith)
	}
}

func TestDBSyncPartitionsStep_WithPartitionKey_NotMultiManager(t *testing.T) {
	mgr := &testPartitionManager{partitionKey: "tenant_id"}
	app := NewMockApplication()
	app.Services["part-db"] = mgr

	factory := NewDBSyncPartitionsStepFactory()
	step, err := factory("sync-parts", map[string]any{
		"database":     "part-db",
		"partitionKey": "api_version",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when service does not implement MultiPartitionManager")
	}
}

func TestDBCreatePartitionStep_WithPartitionKey_Error(t *testing.T) {
	mgr := &testMultiPartitionManager{
		testPartitionManager: testPartitionManager{partitionKey: "tenant_id"},
		configs:              []PartitionConfig{{PartitionKey: "api_version", Tables: []string{"contracts"}}},
		ensureForKeyErr:      fmt.Errorf("injected error"),
	}
	app := NewMockApplication()
	app.Services["multi-db"] = mgr

	factory := NewDBCreatePartitionStepFactory()
	step, err := factory("create-part", map[string]any{
		"database":     "multi-db",
		"tenantKey":    "steps.body.val",
		"partitionKey": "api_version",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("body", map[string]any{"val": "v1"})

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error propagated from EnsurePartitionForKey")
	}
}

func TestDBSyncPartitionsStep_WithPartitionKey_Error(t *testing.T) {
	mgr := &testMultiPartitionManager{
		testPartitionManager: testPartitionManager{partitionKey: "tenant_id"},
		configs:              []PartitionConfig{{PartitionKey: "api_version", Tables: []string{"contracts"}}},
		syncForKeyErr:        fmt.Errorf("injected sync error"),
	}
	app := NewMockApplication()
	app.Services["multi-db"] = mgr

	factory := NewDBSyncPartitionsStepFactory()
	step, err := factory("sync-parts", map[string]any{
		"database":     "multi-db",
		"partitionKey": "api_version",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error propagated from SyncPartitionsForKey")
	}
}

// testPartitionManager is a mock PartitionManager for testing step.db_create_partition.
type testPartitionManager struct {
	partitionKey        string
	partitionNameFormat string
	partitions          map[string]bool
	syncCalled          bool
}

func (p *testPartitionManager) DB() *sql.DB          { return nil }
func (p *testPartitionManager) PartitionKey() string { return p.partitionKey }
func (p *testPartitionManager) PartitionTableName(parentTable, tenantValue string) string {
	format := p.partitionNameFormat
	if format == "" {
		format = "{table}_{tenant}"
	}
	suffix := sanitizePartitionSuffix(tenantValue)
	name := strings.ReplaceAll(format, "{table}", parentTable)
	name = strings.ReplaceAll(name, "{tenant}", suffix)
	return name
}
func (p *testPartitionManager) EnsurePartition(_ context.Context, tenantValue string) error {
	if p.partitions == nil {
		p.partitions = make(map[string]bool)
	}
	p.partitions[tenantValue] = true
	return nil
}
func (p *testPartitionManager) SyncPartitionsFromSource(_ context.Context) error {
	p.syncCalled = true
	return nil
}

func TestDBCreatePartitionStep_Execute(t *testing.T) {
	mgr := &testPartitionManager{partitionKey: "tenant_id"}
	app := NewMockApplication()
	app.Services["part-db"] = mgr

	factory := NewDBCreatePartitionStepFactory()
	step, err := factory("create-part", map[string]any{
		"database":  "part-db",
		"tenantKey": "steps.body.tenant_id",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("body", map[string]any{"tenant_id": "new-org"})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["tenant"] != "new-org" {
		t.Errorf("expected tenant='new-org', got %v", result.Output["tenant"])
	}
	if !mgr.partitions["new-org"] {
		t.Error("expected partition to be created for new-org")
	}
}

func TestDBCreatePartitionStep_MissingDatabase(t *testing.T) {
	factory := NewDBCreatePartitionStepFactory()
	_, err := factory("create-part", map[string]any{
		"tenantKey": "steps.body.tenant_id",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing database")
	}
}

func TestDBCreatePartitionStep_MissingTenantKey(t *testing.T) {
	factory := NewDBCreatePartitionStepFactory()
	_, err := factory("create-part", map[string]any{
		"database": "part-db",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing tenantKey")
	}
}

func TestDBCreatePartitionStep_NotPartitionManager(t *testing.T) {
	db := setupTenantTestDB(t)
	app := mockAppWithDB("plain-db", db)

	factory := NewDBCreatePartitionStepFactory()
	step, err := factory("create-part", map[string]any{
		"database":  "plain-db",
		"tenantKey": "steps.body.tenant_id",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("body", map[string]any{"tenant_id": "new-org"})

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when service does not implement PartitionManager")
	}
}

func TestDBCreatePartitionStep_NilTenantValue(t *testing.T) {
	mgr := &testPartitionManager{partitionKey: "tenant_id"}
	app := NewMockApplication()
	app.Services["part-db"] = mgr

	factory := NewDBCreatePartitionStepFactory()
	step, err := factory("create-part", map[string]any{
		"database":  "part-db",
		"tenantKey": "steps.body.tenant_id",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// No tenant_id in context
	pc := NewPipelineContext(nil, nil)

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when tenant value is nil")
	}
}

func TestDBSyncPartitionsStep_Execute(t *testing.T) {
	mgr := &testPartitionManager{partitionKey: "tenant_id"}
	app := NewMockApplication()
	app.Services["part-db"] = mgr

	factory := NewDBSyncPartitionsStepFactory()
	step, err := factory("sync-parts", map[string]any{
		"database": "part-db",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if !mgr.syncCalled {
		t.Error("expected SyncPartitionsFromSource to be called")
	}
	if result.Output["synced"] != true {
		t.Errorf("expected synced=true, got %v", result.Output["synced"])
	}
}

func TestDBSyncPartitionsStep_MissingDatabase(t *testing.T) {
	factory := NewDBSyncPartitionsStepFactory()
	_, err := factory("sync-parts", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing database")
	}
}

func TestDBSyncPartitionsStep_NotPartitionManager(t *testing.T) {
	db := setupTenantTestDB(t)
	app := mockAppWithDB("plain-db", db)

	factory := NewDBSyncPartitionsStepFactory()
	step, err := factory("sync-parts", map[string]any{
		"database": "plain-db",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when service does not implement PartitionManager")
	}
}

// ─── Auto-sync and periodic sync tests ───────────────────────────────────────

// boolPtr is a test helper that returns a pointer to a bool value.
func boolPtr(v bool) *bool { return &v }

func TestPartitionedDatabase_Start_NoSourceTable_NoSync(t *testing.T) {
	// When no sourceTable is configured, Start should succeed without attempting sync.
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		PartitionKey: "tenant_id",
		Tables:       []string{"forms"},
		// No DSN: base.Start is a no-op; no sourceTable: no sync attempted.
	}
	pd := NewPartitionedDatabase("db", cfg)

	app := NewMockApplication()
	if err := pd.Init(app); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	if err := pd.Start(context.Background()); err != nil {
		t.Fatalf("unexpected Start error: %v", err)
	}
	_ = pd.Stop(context.Background())
}

func TestPartitionedDatabase_Start_AutoSyncDisabled_NoSync(t *testing.T) {
	// When autoSync is explicitly false, Start should not call SyncPartitionsFromSource
	// even when sourceTable is configured.
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		PartitionKey: "tenant_id",
		Tables:       []string{"forms"},
		SourceTable:  "tenants",
		AutoSync:     boolPtr(false),
		// No DSN: base.Start is a no-op; sourceTable set but autoSync=false.
	}
	pd := NewPartitionedDatabase("db", cfg)

	app := NewMockApplication()
	if err := pd.Init(app); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	if err := pd.Start(context.Background()); err != nil {
		t.Fatalf("unexpected Start error: %v", err)
	}
	_ = pd.Stop(context.Background())
}

func TestPartitionedDatabase_Start_AutoSyncEnabled_NilDB(t *testing.T) {
	// When autoSync defaults to true and sourceTable is configured, Start must
	// attempt SyncPartitionsFromSource. With no DB connection the sync returns
	// "database connection is nil", which Start wraps and returns.
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		PartitionKey: "tenant_id",
		Tables:       []string{"forms"},
		SourceTable:  "tenants",
		// No DSN: base.Start is a no-op so DB stays nil.
		// AutoSync not set: defaults to true when sourceTable is present.
	}
	pd := NewPartitionedDatabase("db", cfg)

	app := NewMockApplication()
	if err := pd.Init(app); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	err := pd.Start(context.Background())
	if err == nil {
		t.Fatal("expected Start to return an error when DB connection is nil")
	}
	if !strings.Contains(err.Error(), "auto-sync on startup failed") {
		t.Errorf("expected auto-sync error message, got: %v", err)
	}
}

func TestPartitionedDatabase_Start_InvalidSyncInterval(t *testing.T) {
	// An invalid syncInterval string must cause Start to return a parse error.
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		PartitionKey: "tenant_id",
		Tables:       []string{"forms"},
		SourceTable:  "tenants",
		AutoSync:     boolPtr(false), // skip startup sync so we reach interval parsing
		SyncInterval: "not-a-duration",
	}
	pd := NewPartitionedDatabase("db", cfg)

	app := NewMockApplication()
	if err := pd.Init(app); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	err := pd.Start(context.Background())
	if err == nil {
		t.Fatal("expected Start to return an error for invalid syncInterval")
	}
	if !strings.Contains(err.Error(), "invalid syncInterval") {
		t.Errorf("expected syncInterval parse error, got: %v", err)
	}
}

func TestPartitionedDatabase_SyncInterval_NoSourceTable_NoGoroutine(t *testing.T) {
	// When syncInterval is set but no sourceTable is configured, no background
	// goroutine is started (hasSourceTable=false gates the goroutine launch).
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		PartitionKey: "tenant_id",
		Tables:       []string{"forms"},
		SyncInterval: "100ms",
		// No sourceTable: no goroutine should be started.
	}
	pd := NewPartitionedDatabase("db", cfg)

	app := NewMockApplication()
	if err := pd.Init(app); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	if err := pd.Start(context.Background()); err != nil {
		t.Fatalf("unexpected Start error: %v", err)
	}

	if pd.syncStop != nil {
		t.Error("expected syncStop channel to be nil when no sourceTable is configured")
	}

	if err := pd.Stop(context.Background()); err != nil {
		t.Fatalf("unexpected Stop error: %v", err)
	}
}

func TestPartitionedDatabase_PeriodicSync_GoroutineLifecycle(t *testing.T) {
	// When sourceTable is configured, autoSync is false, and syncInterval is set,
	// a background goroutine must be launched. Stop must cleanly terminate it.
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		PartitionKey: "tenant_id",
		Tables:       []string{"forms"},
		SourceTable:  "tenants",
		AutoSync:     boolPtr(false), // skip startup sync
		SyncInterval: "100ms",
	}
	pd := NewPartitionedDatabase("db", cfg)

	app := NewMockApplication()
	if err := pd.Init(app); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	if err := pd.Start(context.Background()); err != nil {
		t.Fatalf("unexpected Start error: %v", err)
	}

	if pd.syncStop == nil {
		t.Fatal("expected syncStop channel to be set after Start with syncInterval")
	}

	// Allow at least one tick; the goroutine will log nil-DB error but must
	// not panic or deadlock.
	time.Sleep(150 * time.Millisecond)

	done := make(chan error, 1)
	go func() { done <- pd.Stop(context.Background()) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("unexpected Stop error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2 seconds")
	}
}

func TestPartitionedDatabase_AutoSync_DefaultTrueWhenSourceTableSet(t *testing.T) {
	// Confirm that AutoSync==nil is treated as "true" when sourceTable is
	// configured: Start must attempt sync (and fail with nil DB error).
	cfg := PartitionedDatabaseConfig{
		Driver:      "pgx",
		SourceTable: "tenants",
		// AutoSync is nil: should behave as true when sourceTable is present.
	}
	if cfg.AutoSync != nil {
		t.Fatal("AutoSync must be nil for this test to be meaningful")
	}

	pd := NewPartitionedDatabase("db", cfg)
	app := NewMockApplication()
	if err := pd.Init(app); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	err := pd.Start(context.Background())
	if err == nil {
		t.Fatal("expected Start to fail when autoSync defaults to true and DB is nil")
	}
	if !strings.Contains(err.Error(), "auto-sync on startup failed") {
		t.Errorf("expected auto-sync startup error, got: %v", err)
	}
}
