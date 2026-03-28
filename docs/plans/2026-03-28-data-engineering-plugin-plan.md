# Data Engineering Plugin Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build `workflow-plugin-data-engineering` — a gRPC external plugin providing CDC, lakehouse, time-series, graph, data quality, migrations, and data catalog capabilities for data engineering workflows.

**Architecture:** Single gRPC plugin binary (hashicorp/go-plugin via `sdk.Serve()`), internal packages by domain. Follows the payments/bento plugin pattern — implements `PluginProvider`, `ModuleProvider`, `StepProvider`, `TriggerProvider`, and `SchemaProvider` interfaces from `github.com/GoCodeAlone/workflow/plugin/external/sdk`.

**Tech Stack:** Go 1.26, workflow SDK v0.3.56, neo4j-go-driver/v5, influxdb-client-go/v2, clickhouse-go/v2, go-questdb-client/v3, pgx/v5 (TimescaleDB), gonum (statistics). HTTP clients for Iceberg REST Catalog, Druid, Debezium/Kafka Connect, DataHub, OpenMetadata, Schema Registry. Bento plugin reuse for CDC.

**Repo:** `GoCodeAlone/workflow-plugin-data-engineering` (private, Commercial license, self-hosted runners)

---

## Phase 1: Scaffolding + CDC + Tenancy

### Task 1: Repository Scaffolding

**Files:**
- Create: `workflow-plugin-data-engineering/go.mod`
- Create: `workflow-plugin-data-engineering/cmd/workflow-plugin-data-engineering/main.go`
- Create: `workflow-plugin-data-engineering/internal/plugin.go`
- Create: `workflow-plugin-data-engineering/plugin.json`
- Create: `workflow-plugin-data-engineering/.goreleaser.yml`
- Create: `workflow-plugin-data-engineering/.github/workflows/release.yml`
- Create: `workflow-plugin-data-engineering/.github/workflows/ci.yml`
- Create: `workflow-plugin-data-engineering/LICENSE`
- Create: `workflow-plugin-data-engineering/CLAUDE.md`

**Step 1: Create the GitHub repo**

```bash
gh repo create GoCodeAlone/workflow-plugin-data-engineering --private --description "Data engineering plugin: CDC, lakehouse, time-series, graph, data quality" --clone
cd workflow-plugin-data-engineering
```

**Step 2: Initialize Go module**

```bash
go mod init github.com/GoCodeAlone/workflow-plugin-data-engineering
```

**Step 3: Write main.go**

```go
// cmd/workflow-plugin-data-engineering/main.go
package main

import (
	"github.com/GoCodeAlone/workflow-plugin-data-engineering/internal"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

var version = "dev"

func main() {
	sdk.Serve(internal.NewDataEngineeringPlugin(version))
}
```

**Step 4: Write plugin.go (core plugin struct)**

```go
// internal/plugin.go
package internal

import (
	"fmt"

	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

type dataEngineeringPlugin struct {
	version string
	modules map[string]sdk.ModuleInstance
	steps   map[string]sdk.StepInstance
}

func NewDataEngineeringPlugin(version string) sdk.PluginProvider {
	return &dataEngineeringPlugin{
		version: version,
		modules: make(map[string]sdk.ModuleInstance),
		steps:   make(map[string]sdk.StepInstance),
	}
}

func (p *dataEngineeringPlugin) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name:        "workflow-plugin-data-engineering",
		Version:     p.version,
		Author:      "GoCodeAlone",
		Description: "Data engineering: CDC, lakehouse, time-series, graph, data quality, migrations",
	}
}

func (p *dataEngineeringPlugin) ModuleTypes() []string {
	return []string{
		"cdc.source",
		"data.tenancy",
	}
}

func (p *dataEngineeringPlugin) CreateModule(typeName, name string, config map[string]any) (sdk.ModuleInstance, error) {
	switch typeName {
	case "cdc.source":
		return newCDCSourceModule(name, config)
	case "data.tenancy":
		return newTenancyModule(name, config)
	default:
		return nil, fmt.Errorf("unknown module type: %s", typeName)
	}
}

func (p *dataEngineeringPlugin) StepTypes() []string {
	return []string{
		"step.cdc_start",
		"step.cdc_stop",
		"step.cdc_status",
		"step.cdc_snapshot",
		"step.cdc_schema_history",
		"step.tenant_provision",
		"step.tenant_deprovision",
		"step.tenant_migrate",
	}
}

func (p *dataEngineeringPlugin) CreateStep(typeName, name string, config map[string]any) (sdk.StepInstance, error) {
	switch typeName {
	case "step.cdc_start":
		return newCDCStartStep(name, config)
	case "step.cdc_stop":
		return newCDCStopStep(name, config)
	case "step.cdc_status":
		return newCDCStatusStep(name, config)
	case "step.cdc_snapshot":
		return newCDCSnapshotStep(name, config)
	case "step.cdc_schema_history":
		return newCDCSchemaHistoryStep(name, config)
	case "step.tenant_provision":
		return newTenantProvisionStep(name, config)
	case "step.tenant_deprovision":
		return newTenantDeprovisionStep(name, config)
	case "step.tenant_migrate":
		return newTenantMigrateStep(name, config)
	default:
		return nil, fmt.Errorf("unknown step type: %s", typeName)
	}
}

func (p *dataEngineeringPlugin) TriggerTypes() []string {
	return []string{"trigger.cdc"}
}

func (p *dataEngineeringPlugin) CreateTrigger(typeName string, config map[string]any, cb sdk.TriggerCallback) (sdk.TriggerInstance, error) {
	switch typeName {
	case "trigger.cdc":
		return newCDCTrigger(config, cb)
	default:
		return nil, fmt.Errorf("unknown trigger type: %s", typeName)
	}
}

func (p *dataEngineeringPlugin) ModuleSchemas() []sdk.ModuleSchemaData {
	var schemas []sdk.ModuleSchemaData
	schemas = append(schemas, cdcSourceSchema())
	schemas = append(schemas, tenancySchema())
	return schemas
}
```

**Step 5: Write plugin.json**

```json
{
    "name": "workflow-plugin-data-engineering",
    "version": "0.1.0",
    "description": "Data engineering: CDC, lakehouse, time-series, graph, data quality, migrations",
    "author": "GoCodeAlone",
    "license": "Commercial",
    "type": "external",
    "tier": "core",
    "private": true,
    "minEngineVersion": "0.3.56",
    "keywords": ["cdc", "lakehouse", "iceberg", "timeseries", "neo4j", "data-quality"],
    "homepage": "https://github.com/GoCodeAlone/workflow-plugin-data-engineering",
    "repository": "https://github.com/GoCodeAlone/workflow-plugin-data-engineering",
    "capabilities": {
        "configProvider": false,
        "moduleTypes": ["cdc.source", "data.tenancy"],
        "stepTypes": [
            "step.cdc_start",
            "step.cdc_stop",
            "step.cdc_status",
            "step.cdc_snapshot",
            "step.cdc_schema_history",
            "step.tenant_provision",
            "step.tenant_deprovision",
            "step.tenant_migrate"
        ],
        "triggerTypes": ["trigger.cdc"]
    }
}
```

**Step 6: Write .goreleaser.yml**

```yaml
version: 2

builds:
  - main: ./cmd/workflow-plugin-data-engineering
    binary: workflow-plugin-data-engineering
    env:
      - CGO_ENABLED=0
    goos: [linux, darwin]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w -X main.version={{.Version}}

archives:
  - formats: [tar.gz]
    name_template: "{{ .ProjectName }}-{{ .Os }}-{{ .Arch }}"
    files:
      - plugin.json

checksum:
  name_template: checksums.txt

changelog:
  sort: asc
```

**Step 7: Write CI workflows**

`.github/workflows/release.yml`:
```yaml
name: Release
on:
  push:
    tags: ['v*']
permissions:
  contents: write

jobs:
  release:
    runs-on: self-hosted
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
          cache: true
      - name: Configure git for private modules
        run: git config --global url."https://${{ secrets.RELEASES_TOKEN }}@github.com/".insteadOf "https://github.com/"
      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v7
        with:
          version: '~> v2'
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.RELEASES_TOKEN }}
```

`.github/workflows/ci.yml`:
```yaml
name: CI
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

env:
  GONOSUMCHECK: github.com/GoCodeAlone/*
  GONOSUMDB: github.com/GoCodeAlone/*
  GOPRIVATE: github.com/GoCodeAlone/*

jobs:
  test:
    runs-on: self-hosted
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
          cache: true
      - name: Configure git for private modules
        run: git config --global url."https://${{ secrets.RELEASES_TOKEN }}@github.com/".insteadOf "https://github.com/"
      - run: go mod tidy
      - run: go vet ./...
      - run: go test ./... -v -race -timeout 10m

  build:
    runs-on: self-hosted
    needs: test
    strategy:
      matrix:
        include:
          - goos: linux
            goarch: amd64
          - goos: linux
            goarch: arm64
          - goos: darwin
            goarch: amd64
          - goos: darwin
            goarch: arm64
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
          cache: true
      - name: Configure git for private modules
        run: git config --global url."https://${{ secrets.RELEASES_TOKEN }}@github.com/".insteadOf "https://github.com/"
      - run: GOOS=${{ matrix.goos }} GOARCH=${{ matrix.goarch }} CGO_ENABLED=0 go build -o bin/workflow-plugin-data-engineering-${{ matrix.goos }}-${{ matrix.goarch }} ./cmd/workflow-plugin-data-engineering
```

**Step 8: Write CLAUDE.md**

```markdown
# CLAUDE.md

## Project: workflow-plugin-data-engineering

Private gRPC external plugin for the GoCodeAlone/workflow engine providing data engineering capabilities.

### Build & Run

```sh
go build -o workflow-plugin-data-engineering ./cmd/workflow-plugin-data-engineering
go test ./... -v -race
```

### Architecture

External gRPC plugin using `github.com/GoCodeAlone/workflow/plugin/external/sdk`. Single binary, internal packages by domain:

- `internal/cdc/` — CDC providers (Bento, Debezium, DMS)
- `internal/lakehouse/` — Iceberg tables, catalog, Trino queries
- `internal/timeseries/` — InfluxDB, TimescaleDB, ClickHouse, QuestDB, Druid
- `internal/graph/` — Neo4j, knowledge graph extraction
- `internal/quality/` — Go-native checks + optional Python tool providers
- `internal/migrate/` — Declarative + scripted schema migrations
- `internal/catalog/` — DataHub, OpenMetadata, schema registry
- `internal/tenancy/` — Multi-tenancy strategies

### Conventions

- Go-native libraries preferred over shelling out
- All modules use `sync.RWMutex` for thread safety
- Error messages always include module/step name
- Config structs use both `json` and `yaml` tags
- Tests use table-driven patterns
```

**Step 9: Write LICENSE (Commercial)**

Standard commercial license text.

**Step 10: Run `go mod tidy` and verify build**

```bash
go mod tidy
go build ./cmd/workflow-plugin-data-engineering
```

Expected: binary compiles with no errors.

**Step 11: Commit**

```bash
git add -A
git commit -m "feat: scaffold data engineering plugin with Phase 1 stubs"
```

---

### Task 2: CDC Provider Interface + Memory Provider (TDD)

**Files:**
- Create: `internal/cdc/provider.go`
- Create: `internal/cdc/memory_provider.go`
- Create: `internal/cdc/provider_test.go`

**Step 1: Write the failing test**

```go
// internal/cdc/provider_test.go
package cdc

import (
	"context"
	"testing"
)

func TestMemoryProvider_StartAndStatus(t *testing.T) {
	p := NewMemoryProvider()
	ctx := context.Background()

	cfg := CDCSourceConfig{
		SourceType: "postgres",
		Tables:     []string{"users", "orders"},
		Connection: "postgres://localhost/test",
	}

	if err := p.Start(ctx, cfg); err != nil {
		t.Fatalf("Start: %v", err)
	}

	status, err := p.Status(ctx, "postgres://localhost/test")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != "running" {
		t.Errorf("expected running, got %s", status.State)
	}
	if len(status.Tables) != 2 {
		t.Errorf("expected 2 tables, got %d", len(status.Tables))
	}
}

func TestMemoryProvider_Stop(t *testing.T) {
	p := NewMemoryProvider()
	ctx := context.Background()

	cfg := CDCSourceConfig{
		SourceType: "postgres",
		Tables:     []string{"users"},
		Connection: "postgres://localhost/test",
	}

	_ = p.Start(ctx, cfg)
	if err := p.Stop(ctx, "postgres://localhost/test"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	status, err := p.Status(ctx, "postgres://localhost/test")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != "stopped" {
		t.Errorf("expected stopped, got %s", status.State)
	}
}

func TestMemoryProvider_Snapshot(t *testing.T) {
	p := NewMemoryProvider()
	ctx := context.Background()

	cfg := CDCSourceConfig{
		SourceType: "postgres",
		Tables:     []string{"users", "orders"},
		Connection: "postgres://localhost/test",
	}

	_ = p.Start(ctx, cfg)

	if err := p.Snapshot(ctx, "postgres://localhost/test", []string{"users"}); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	history, err := p.SchemaHistory(ctx, "postgres://localhost/test", "users")
	if err != nil {
		t.Fatalf("SchemaHistory: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(history))
	}
}

func TestMemoryProvider_StatusNotFound(t *testing.T) {
	p := NewMemoryProvider()
	ctx := context.Background()

	_, err := p.Status(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent source")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/cdc/ -v
```

Expected: FAIL — types not defined.

**Step 3: Write the CDCProvider interface**

```go
// internal/cdc/provider.go
package cdc

import "context"

// CDCSourceConfig configures a CDC source.
type CDCSourceConfig struct {
	SourceType string   `json:"type" yaml:"type"`
	Connection string   `json:"connection" yaml:"connection"`
	Tables     []string `json:"tables" yaml:"tables"`
	Snapshot   string   `json:"snapshot" yaml:"snapshot"` // initial, never, when_needed
	SlotName   string   `json:"slotName" yaml:"slotName"`
}

// CDCStatus reports the state of a CDC stream.
type CDCStatus struct {
	State  string            `json:"state"`  // running, stopped, error, snapshotting
	Tables []string          `json:"tables"`
	Lag    int64             `json:"lag"`     // messages behind
	Errors []string          `json:"errors"`
	Meta   map[string]string `json:"meta"`
}

// SchemaVersion records a schema change captured by CDC.
type SchemaVersion struct {
	Version   int               `json:"version"`
	Timestamp string            `json:"timestamp"`
	Columns   map[string]string `json:"columns"` // column name -> type
	Change    string            `json:"change"`   // snapshot, add_column, drop_column, alter_type
}

// CDCProvider abstracts CDC implementations.
type CDCProvider interface {
	Start(ctx context.Context, config CDCSourceConfig) error
	Stop(ctx context.Context, sourceID string) error
	Status(ctx context.Context, sourceID string) (*CDCStatus, error)
	Snapshot(ctx context.Context, sourceID string, tables []string) error
	SchemaHistory(ctx context.Context, sourceID string, table string) ([]SchemaVersion, error)
}
```

**Step 4: Write the memory provider**

```go
// internal/cdc/memory_provider.go
package cdc

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type memorySource struct {
	config  CDCSourceConfig
	state   string
	history map[string][]SchemaVersion
}

// MemoryProvider is a CDC provider for testing. No real CDC — just state tracking.
type MemoryProvider struct {
	mu      sync.RWMutex
	sources map[string]*memorySource
}

func NewMemoryProvider() *MemoryProvider {
	return &MemoryProvider{sources: make(map[string]*memorySource)}
}

func (m *MemoryProvider) Start(_ context.Context, config CDCSourceConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sources[config.Connection] = &memorySource{
		config:  config,
		state:   "running",
		history: make(map[string][]SchemaVersion),
	}
	return nil
}

func (m *MemoryProvider) Stop(_ context.Context, sourceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	src, ok := m.sources[sourceID]
	if !ok {
		return fmt.Errorf("cdc source %q not found", sourceID)
	}
	src.state = "stopped"
	return nil
}

func (m *MemoryProvider) Status(_ context.Context, sourceID string) (*CDCStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	src, ok := m.sources[sourceID]
	if !ok {
		return nil, fmt.Errorf("cdc source %q not found", sourceID)
	}
	return &CDCStatus{
		State:  src.state,
		Tables: src.config.Tables,
		Lag:    0,
	}, nil
}

func (m *MemoryProvider) Snapshot(_ context.Context, sourceID string, tables []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	src, ok := m.sources[sourceID]
	if !ok {
		return fmt.Errorf("cdc source %q not found", sourceID)
	}

	for _, table := range tables {
		src.history[table] = append(src.history[table], SchemaVersion{
			Version:   len(src.history[table]) + 1,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Change:    "snapshot",
		})
	}
	return nil
}

func (m *MemoryProvider) SchemaHistory(_ context.Context, sourceID string, table string) ([]SchemaVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	src, ok := m.sources[sourceID]
	if !ok {
		return nil, fmt.Errorf("cdc source %q not found", sourceID)
	}
	return src.history[table], nil
}
```

**Step 5: Run tests**

```bash
go test ./internal/cdc/ -v -race
```

Expected: all 4 tests PASS.

**Step 6: Commit**

```bash
git add internal/cdc/
git commit -m "feat: CDC provider interface + memory provider with tests"
```

---

### Task 3: CDC Source Module

**Files:**
- Create: `internal/cdc/module.go`
- Create: `internal/cdc/module_test.go`
- Create: `internal/cdc/schema.go`

**Step 1: Write the failing test**

```go
// internal/cdc/module_test.go
package cdc

import (
	"context"
	"testing"
)

func TestCDCSourceModule_InitAndStart(t *testing.T) {
	config := map[string]any{
		"provider": "memory",
		"source": map[string]any{
			"type":       "postgres",
			"connection": "postgres://localhost/test",
			"tables":     []any{"users", "orders"},
		},
	}

	mod, err := NewCDCSourceModule("test-cdc", config)
	if err != nil {
		t.Fatalf("NewCDCSourceModule: %v", err)
	}

	if err := mod.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	ctx := context.Background()
	if err := mod.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Module should expose the provider for step access
	p := mod.Provider()
	if p == nil {
		t.Fatal("expected non-nil provider")
	}

	status, err := p.Status(ctx, "postgres://localhost/test")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != "running" {
		t.Errorf("expected running, got %s", status.State)
	}

	if err := mod.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestCDCSourceModule_UnknownProvider(t *testing.T) {
	config := map[string]any{
		"provider": "unknown",
		"source": map[string]any{
			"type":       "postgres",
			"connection": "postgres://localhost/test",
			"tables":     []any{"users"},
		},
	}

	_, err := NewCDCSourceModule("test-cdc", config)
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/cdc/ -v -run TestCDCSourceModule
```

Expected: FAIL — `NewCDCSourceModule` not defined.

**Step 3: Write the module**

```go
// internal/cdc/module.go
package cdc

import (
	"context"
	"fmt"
	"sync"
)

type CDCSourceModule struct {
	mu       sync.RWMutex
	name     string
	config   CDCSourceConfig
	provider CDCProvider
}

func NewCDCSourceModule(name string, rawConfig map[string]any) (*CDCSourceModule, error) {
	providerName, _ := rawConfig["provider"].(string)
	sourceMap, _ := rawConfig["source"].(map[string]any)

	cfg := CDCSourceConfig{}
	if sourceMap != nil {
		cfg.SourceType, _ = sourceMap["type"].(string)
		cfg.Connection, _ = sourceMap["connection"].(string)
		if tables, ok := sourceMap["tables"].([]any); ok {
			for _, t := range tables {
				if s, ok := t.(string); ok {
					cfg.Tables = append(cfg.Tables, s)
				}
			}
		}
		cfg.Snapshot, _ = sourceMap["snapshot"].(string)
		cfg.SlotName, _ = sourceMap["slotName"].(string)
	}

	var provider CDCProvider
	switch providerName {
	case "memory":
		provider = NewMemoryProvider()
	case "bento":
		provider = NewBentoProvider(rawConfig)
	case "debezium":
		provider = NewDebeziumProvider(rawConfig)
	case "dms":
		provider = NewDMSProvider(rawConfig)
	default:
		return nil, fmt.Errorf("cdc.source %q: unknown provider %q", name, providerName)
	}

	return &CDCSourceModule{
		name:     name,
		config:   cfg,
		provider: provider,
	}, nil
}

func (m *CDCSourceModule) Init() error {
	return nil
}

func (m *CDCSourceModule) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.config.Snapshot == "" {
		m.config.Snapshot = "initial"
	}
	return m.provider.Start(ctx, m.config)
}

func (m *CDCSourceModule) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.provider.Stop(ctx, m.config.Connection)
}

func (m *CDCSourceModule) Provider() CDCProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider
}
```

Note: `NewBentoProvider`, `NewDebeziumProvider`, `NewDMSProvider` are stubs created in the next tasks. For now, create placeholder files so the module compiles:

```go
// internal/cdc/bento_provider.go
package cdc

type BentoProvider struct{ cfg map[string]any }

func NewBentoProvider(cfg map[string]any) *BentoProvider { return &BentoProvider{cfg: cfg} }
// Implement CDCProvider interface methods — all return fmt.Errorf("bento provider not yet implemented")
```

```go
// internal/cdc/debezium_provider.go
package cdc

type DebeziumProvider struct{ cfg map[string]any }

func NewDebeziumProvider(cfg map[string]any) *DebeziumProvider { return &DebeziumProvider{cfg: cfg} }
// Implement CDCProvider interface methods — all return fmt.Errorf("debezium provider not yet implemented")
```

```go
// internal/cdc/dms_provider.go
package cdc

type DMSProvider struct{ cfg map[string]any }

func NewDMSProvider(cfg map[string]any) *DMSProvider { return &DMSProvider{cfg: cfg} }
// Implement CDCProvider interface methods — all return fmt.Errorf("dms provider not yet implemented")
```

**Step 4: Write the schema helper**

```go
// internal/cdc/schema.go
package cdc

import sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"

func cdcSourceSchema() sdk.ModuleSchemaData {
	return sdk.ModuleSchemaData{
		Type:        "cdc.source",
		Label:       "CDC Source",
		Category:    "data-engineering",
		Description: "Change Data Capture stream from a database",
		ConfigFields: []sdk.ConfigField{
			{Name: "provider", Type: "string", Description: "CDC provider: memory, bento, debezium, dms", Required: true, Options: []string{"memory", "bento", "debezium", "dms"}},
			{Name: "source.type", Type: "string", Description: "Database type: postgres, mysql, dynamodb", Required: true},
			{Name: "source.connection", Type: "string", Description: "Connection string", Required: true},
			{Name: "source.tables", Type: "array", Description: "Tables to capture", Required: true},
			{Name: "source.snapshot", Type: "string", Description: "Snapshot mode: initial, never, when_needed", DefaultValue: "initial"},
			{Name: "source.slotName", Type: "string", Description: "Replication slot name (Postgres)"},
		},
	}
}
```

**Step 5: Run tests**

```bash
go test ./internal/cdc/ -v -race
```

Expected: all tests PASS.

**Step 6: Commit**

```bash
git add internal/cdc/
git commit -m "feat: CDC source module with provider dispatch"
```

---

### Task 4: CDC Step Types (TDD)

**Files:**
- Create: `internal/cdc/steps.go`
- Create: `internal/cdc/steps_test.go`

**Step 1: Write the failing tests**

```go
// internal/cdc/steps_test.go
package cdc

import (
	"context"
	"testing"
)

func setupTestModule(t *testing.T) *CDCSourceModule {
	t.Helper()
	config := map[string]any{
		"provider": "memory",
		"source": map[string]any{
			"type":       "postgres",
			"connection": "postgres://localhost/test",
			"tables":     []any{"users", "orders"},
		},
	}
	mod, err := NewCDCSourceModule("test-cdc", config)
	if err != nil {
		t.Fatal(err)
	}
	_ = mod.Init()
	_ = mod.Start(context.Background())
	return mod
}

func TestCDCStartStep(t *testing.T) {
	mod := setupTestModule(t)
	step, err := newCDCStartStep("start", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := step.Execute(
		context.Background(),
		nil,
		nil,
		map[string]any{"cdc_source": mod},
		map[string]any{},
		map[string]any{
			"source": "test-cdc",
			"tables": []any{"users"},
		},
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["status"] != "started" {
		t.Errorf("expected started, got %v", result.Output["status"])
	}
}

func TestCDCStatusStep(t *testing.T) {
	mod := setupTestModule(t)
	step, err := newCDCStatusStep("status", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := step.Execute(
		context.Background(),
		nil,
		nil,
		map[string]any{"cdc_source": mod},
		map[string]any{},
		map[string]any{"source": "test-cdc"},
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["state"] != "running" {
		t.Errorf("expected running, got %v", result.Output["state"])
	}
}

func TestCDCStopStep(t *testing.T) {
	mod := setupTestModule(t)
	step, err := newCDCStopStep("stop", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := step.Execute(
		context.Background(),
		nil,
		nil,
		map[string]any{"cdc_source": mod},
		map[string]any{},
		map[string]any{"source": "test-cdc"},
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["status"] != "stopped" {
		t.Errorf("expected stopped, got %v", result.Output["status"])
	}
}

func TestCDCSnapshotStep(t *testing.T) {
	mod := setupTestModule(t)
	step, err := newCDCSnapshotStep("snapshot", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := step.Execute(
		context.Background(),
		nil,
		nil,
		map[string]any{"cdc_source": mod},
		map[string]any{},
		map[string]any{
			"source": "test-cdc",
			"tables": []any{"users"},
		},
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["status"] != "snapshot_triggered" {
		t.Errorf("expected snapshot_triggered, got %v", result.Output["status"])
	}
}

func TestCDCSchemaHistoryStep(t *testing.T) {
	mod := setupTestModule(t)

	// Trigger a snapshot first to create history
	provider := mod.Provider()
	_ = provider.Snapshot(context.Background(), "postgres://localhost/test", []string{"users"})

	step, err := newCDCSchemaHistoryStep("history", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := step.Execute(
		context.Background(),
		nil,
		nil,
		map[string]any{"cdc_source": mod},
		map[string]any{},
		map[string]any{
			"source": "test-cdc",
			"table":  "users",
		},
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	versions, ok := result.Output["versions"].([]SchemaVersion)
	if !ok || len(versions) != 1 {
		t.Errorf("expected 1 schema version, got %v", result.Output["versions"])
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/cdc/ -v -run TestCDC.*Step
```

Expected: FAIL — step constructors not defined.

**Step 3: Write the step implementations**

```go
// internal/cdc/steps.go
package cdc

import (
	"context"
	"fmt"

	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

// resolveModule extracts the CDCSourceModule from the current context.
// In the real plugin, this resolves via the module name from config.
// For now, steps receive the module via current["cdc_source"].
func resolveModule(current map[string]any, config map[string]any) (*CDCSourceModule, error) {
	if mod, ok := current["cdc_source"].(*CDCSourceModule); ok {
		return mod, nil
	}
	sourceName, _ := config["source"].(string)
	return nil, fmt.Errorf("cdc source %q not found in context", sourceName)
}

func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// step.cdc_start

type cdcStartStep struct{ name string }

func newCDCStartStep(name string, _ map[string]any) (*cdcStartStep, error) {
	return &cdcStartStep{name: name}, nil
}

func (s *cdcStartStep) Execute(ctx context.Context, _ map[string]any, _ map[string]map[string]any, current map[string]any, _ map[string]any, config map[string]any) (*sdk.StepResult, error) {
	mod, err := resolveModule(current, config)
	if err != nil {
		return nil, fmt.Errorf("step.cdc_start %q: %w", s.name, err)
	}

	tables := toStringSlice(config["tables"])
	if len(tables) > 0 {
		cfg := CDCSourceConfig{Tables: tables, Connection: mod.config.Connection}
		if err := mod.Provider().Start(ctx, cfg); err != nil {
			return nil, fmt.Errorf("step.cdc_start %q: %w", s.name, err)
		}
	}

	return &sdk.StepResult{Output: map[string]any{"status": "started", "tables": tables}}, nil
}

// step.cdc_stop

type cdcStopStep struct{ name string }

func newCDCStopStep(name string, _ map[string]any) (*cdcStopStep, error) {
	return &cdcStopStep{name: name}, nil
}

func (s *cdcStopStep) Execute(ctx context.Context, _ map[string]any, _ map[string]map[string]any, current map[string]any, _ map[string]any, config map[string]any) (*sdk.StepResult, error) {
	mod, err := resolveModule(current, config)
	if err != nil {
		return nil, fmt.Errorf("step.cdc_stop %q: %w", s.name, err)
	}

	if err := mod.Provider().Stop(ctx, mod.config.Connection); err != nil {
		return nil, fmt.Errorf("step.cdc_stop %q: %w", s.name, err)
	}

	return &sdk.StepResult{Output: map[string]any{"status": "stopped"}}, nil
}

// step.cdc_status

type cdcStatusStep struct{ name string }

func newCDCStatusStep(name string, _ map[string]any) (*cdcStatusStep, error) {
	return &cdcStatusStep{name: name}, nil
}

func (s *cdcStatusStep) Execute(ctx context.Context, _ map[string]any, _ map[string]map[string]any, current map[string]any, _ map[string]any, config map[string]any) (*sdk.StepResult, error) {
	mod, err := resolveModule(current, config)
	if err != nil {
		return nil, fmt.Errorf("step.cdc_status %q: %w", s.name, err)
	}

	status, err := mod.Provider().Status(ctx, mod.config.Connection)
	if err != nil {
		return nil, fmt.Errorf("step.cdc_status %q: %w", s.name, err)
	}

	return &sdk.StepResult{Output: map[string]any{
		"state":  status.State,
		"tables": status.Tables,
		"lag":    status.Lag,
		"errors": status.Errors,
	}}, nil
}

// step.cdc_snapshot

type cdcSnapshotStep struct{ name string }

func newCDCSnapshotStep(name string, _ map[string]any) (*cdcSnapshotStep, error) {
	return &cdcSnapshotStep{name: name}, nil
}

func (s *cdcSnapshotStep) Execute(ctx context.Context, _ map[string]any, _ map[string]map[string]any, current map[string]any, _ map[string]any, config map[string]any) (*sdk.StepResult, error) {
	mod, err := resolveModule(current, config)
	if err != nil {
		return nil, fmt.Errorf("step.cdc_snapshot %q: %w", s.name, err)
	}

	tables := toStringSlice(config["tables"])
	if err := mod.Provider().Snapshot(ctx, mod.config.Connection, tables); err != nil {
		return nil, fmt.Errorf("step.cdc_snapshot %q: %w", s.name, err)
	}

	return &sdk.StepResult{Output: map[string]any{"status": "snapshot_triggered", "tables": tables}}, nil
}

// step.cdc_schema_history

type cdcSchemaHistoryStep struct{ name string }

func newCDCSchemaHistoryStep(name string, _ map[string]any) (*cdcSchemaHistoryStep, error) {
	return &cdcSchemaHistoryStep{name: name}, nil
}

func (s *cdcSchemaHistoryStep) Execute(ctx context.Context, _ map[string]any, _ map[string]map[string]any, current map[string]any, _ map[string]any, config map[string]any) (*sdk.StepResult, error) {
	mod, err := resolveModule(current, config)
	if err != nil {
		return nil, fmt.Errorf("step.cdc_schema_history %q: %w", s.name, err)
	}

	table, _ := config["table"].(string)
	versions, err := mod.Provider().SchemaHistory(ctx, mod.config.Connection, table)
	if err != nil {
		return nil, fmt.Errorf("step.cdc_schema_history %q: %w", s.name, err)
	}

	return &sdk.StepResult{Output: map[string]any{"versions": versions, "count": len(versions)}}, nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/cdc/ -v -race
```

Expected: all tests PASS.

**Step 5: Commit**

```bash
git add internal/cdc/
git commit -m "feat: CDC step types (start, stop, status, snapshot, schema_history)"
```

---

### Task 5: Tenancy Module (TDD)

**Files:**
- Create: `internal/tenancy/strategy.go`
- Create: `internal/tenancy/schema_per_tenant.go`
- Create: `internal/tenancy/db_per_tenant.go`
- Create: `internal/tenancy/row_level.go`
- Create: `internal/tenancy/module.go`
- Create: `internal/tenancy/strategy_test.go`
- Create: `internal/tenancy/module_test.go`

**Step 1: Write the failing tests**

```go
// internal/tenancy/strategy_test.go
package tenancy

import "testing"

func TestSchemaPerTenant_ResolveTable(t *testing.T) {
	s := NewSchemaPerTenantStrategy("tenant_")
	got := s.ResolveTable("abc123", "users")
	want := "tenant_abc123.users"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSchemaPerTenant_ResolveConnection(t *testing.T) {
	s := NewSchemaPerTenantStrategy("tenant_")
	got := s.ResolveConnection("abc123", "postgres://localhost/app")
	want := "postgres://localhost/app" // same connection, different schema
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSchemaPerTenant_TenantFilter(t *testing.T) {
	s := NewSchemaPerTenantStrategy("tenant_")
	col, val := s.TenantFilter("abc123")
	if col != "" || val != "" {
		t.Error("schema strategy should not add row filter")
	}
}

func TestDBPerTenant_ResolveConnection(t *testing.T) {
	s := NewDBPerTenantStrategy("postgres://localhost/{{tenant}}")
	got := s.ResolveConnection("abc123", "ignored")
	want := "postgres://localhost/abc123"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDBPerTenant_ResolveTable(t *testing.T) {
	s := NewDBPerTenantStrategy("postgres://localhost/{{tenant}}")
	got := s.ResolveTable("abc123", "users")
	want := "users" // no prefix needed, separate DB
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRowLevel_TenantFilter(t *testing.T) {
	s := NewRowLevelStrategy("tenant_id")
	col, val := s.TenantFilter("abc123")
	if col != "tenant_id" || val != "abc123" {
		t.Errorf("got col=%q val=%q", col, val)
	}
}

func TestRowLevel_ResolveTable(t *testing.T) {
	s := NewRowLevelStrategy("tenant_id")
	got := s.ResolveTable("abc123", "users")
	want := "users" // no prefix, filter applied at query level
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
```

```go
// internal/tenancy/module_test.go
package tenancy

import (
	"context"
	"testing"
)

func TestTenancyModule_SchemaPerTenant(t *testing.T) {
	mod, err := NewTenancyModule("tenancy", map[string]any{
		"strategy":     "schema_per_tenant",
		"tenantKey":    "ctx.tenant_id",
		"schemaPrefix": "t_",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mod.Init(); err != nil {
		t.Fatal(err)
	}

	table := mod.Strategy().ResolveTable("acme", "users")
	if table != "t_acme.users" {
		t.Errorf("got %q", table)
	}
}

func TestTenancyModule_RowLevel(t *testing.T) {
	mod, err := NewTenancyModule("tenancy", map[string]any{
		"strategy":     "row_level",
		"tenantKey":    "ctx.tenant_id",
		"tenantColumn": "org_id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mod.Init(); err != nil {
		t.Fatal(err)
	}

	col, val := mod.Strategy().TenantFilter("acme")
	if col != "org_id" || val != "acme" {
		t.Errorf("got col=%q val=%q", col, val)
	}
}

func TestTenancyModule_InvalidStrategy(t *testing.T) {
	_, err := NewTenancyModule("tenancy", map[string]any{
		"strategy": "invalid",
	})
	if err == nil {
		t.Error("expected error for invalid strategy")
	}
}

func TestTenancyModule_Lifecycle(t *testing.T) {
	mod, _ := NewTenancyModule("tenancy", map[string]any{
		"strategy":  "schema_per_tenant",
		"tenantKey": "ctx.tenant_id",
	})
	_ = mod.Init()
	ctx := context.Background()
	if err := mod.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mod.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/tenancy/ -v
```

Expected: FAIL — types not defined.

**Step 3: Write the strategy interface and implementations**

```go
// internal/tenancy/strategy.go
package tenancy

// TenancyStrategy defines how tenant isolation is applied to data operations.
type TenancyStrategy interface {
	ResolveTable(tenantID, table string) string
	ResolveConnection(tenantID, baseConnection string) string
	TenantFilter(tenantID string) (column, value string)
}
```

```go
// internal/tenancy/schema_per_tenant.go
package tenancy

import "fmt"

type SchemaPerTenantStrategy struct {
	prefix string
}

func NewSchemaPerTenantStrategy(prefix string) *SchemaPerTenantStrategy {
	if prefix == "" {
		prefix = "tenant_"
	}
	return &SchemaPerTenantStrategy{prefix: prefix}
}

func (s *SchemaPerTenantStrategy) ResolveTable(tenantID, table string) string {
	return fmt.Sprintf("%s%s.%s", s.prefix, tenantID, table)
}

func (s *SchemaPerTenantStrategy) ResolveConnection(_, baseConnection string) string {
	return baseConnection
}

func (s *SchemaPerTenantStrategy) TenantFilter(_ string) (string, string) {
	return "", ""
}
```

```go
// internal/tenancy/db_per_tenant.go
package tenancy

import "strings"

type DBPerTenantStrategy struct {
	connectionTemplate string
}

func NewDBPerTenantStrategy(connectionTemplate string) *DBPerTenantStrategy {
	return &DBPerTenantStrategy{connectionTemplate: connectionTemplate}
}

func (s *DBPerTenantStrategy) ResolveTable(_, table string) string {
	return table
}

func (s *DBPerTenantStrategy) ResolveConnection(tenantID, _ string) string {
	return strings.ReplaceAll(s.connectionTemplate, "{{tenant}}", tenantID)
}

func (s *DBPerTenantStrategy) TenantFilter(_ string) (string, string) {
	return "", ""
}
```

```go
// internal/tenancy/row_level.go
package tenancy

type RowLevelStrategy struct {
	tenantColumn string
}

func NewRowLevelStrategy(tenantColumn string) *RowLevelStrategy {
	if tenantColumn == "" {
		tenantColumn = "tenant_id"
	}
	return &RowLevelStrategy{tenantColumn: tenantColumn}
}

func (s *RowLevelStrategy) ResolveTable(_, table string) string {
	return table
}

func (s *RowLevelStrategy) ResolveConnection(_, baseConnection string) string {
	return baseConnection
}

func (s *RowLevelStrategy) TenantFilter(tenantID string) (string, string) {
	return s.tenantColumn, tenantID
}
```

```go
// internal/tenancy/module.go
package tenancy

import (
	"context"
	"fmt"
	"sync"
)

type TenancyModule struct {
	mu       sync.RWMutex
	name     string
	strategy TenancyStrategy
	config   map[string]any
}

func NewTenancyModule(name string, config map[string]any) (*TenancyModule, error) {
	strategyName, _ := config["strategy"].(string)

	var strategy TenancyStrategy
	switch strategyName {
	case "schema_per_tenant":
		prefix, _ := config["schemaPrefix"].(string)
		strategy = NewSchemaPerTenantStrategy(prefix)
	case "db_per_tenant":
		tmpl, _ := config["connectionTemplate"].(string)
		strategy = NewDBPerTenantStrategy(tmpl)
	case "row_level":
		col, _ := config["tenantColumn"].(string)
		strategy = NewRowLevelStrategy(col)
	default:
		return nil, fmt.Errorf("data.tenancy %q: unknown strategy %q", name, strategyName)
	}

	return &TenancyModule{
		name:     name,
		strategy: strategy,
		config:   config,
	}, nil
}

func (m *TenancyModule) Init() error  { return nil }
func (m *TenancyModule) Start(_ context.Context) error { return nil }
func (m *TenancyModule) Stop(_ context.Context) error  { return nil }

func (m *TenancyModule) Strategy() TenancyStrategy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.strategy
}

func (m *TenancyModule) TenantKey() string {
	key, _ := m.config["tenantKey"].(string)
	return key
}
```

**Step 4: Run tests**

```bash
go test ./internal/tenancy/ -v -race
```

Expected: all tests PASS.

**Step 5: Commit**

```bash
git add internal/tenancy/
git commit -m "feat: multi-tenancy module with schema/db/row-level strategies"
```

---

### Task 6: Tenant Steps (TDD)

**Files:**
- Create: `internal/tenancy/steps.go`
- Create: `internal/tenancy/steps_test.go`
- Create: `internal/tenancy/schema.go`

**Step 1: Write the failing tests**

```go
// internal/tenancy/steps_test.go
package tenancy

import (
	"context"
	"testing"

	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func TestTenantProvisionStep(t *testing.T) {
	mod, _ := NewTenancyModule("tenancy", map[string]any{
		"strategy":     "schema_per_tenant",
		"tenantKey":    "ctx.tenant_id",
		"schemaPrefix": "t_",
	})
	_ = mod.Init()

	step, err := newTenantProvisionStep("provision", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := step.Execute(
		context.Background(),
		nil, nil,
		map[string]any{"tenancy_module": mod},
		nil,
		map[string]any{"tenantId": "acme"},
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["status"] != "provisioned" {
		t.Errorf("expected provisioned, got %v", result.Output["status"])
	}
	if result.Output["schema"] != "t_acme" {
		t.Errorf("expected t_acme, got %v", result.Output["schema"])
	}
}

func TestTenantDeprovisionStep(t *testing.T) {
	mod, _ := NewTenancyModule("tenancy", map[string]any{
		"strategy":  "schema_per_tenant",
		"tenantKey": "ctx.tenant_id",
	})
	_ = mod.Init()

	step, err := newTenantDeprovisionStep("deprovision", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := step.Execute(
		context.Background(),
		nil, nil,
		map[string]any{"tenancy_module": mod},
		nil,
		map[string]any{"tenantId": "acme", "mode": "archive"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["status"] != "deprovisioned" {
		t.Errorf("got %v", result.Output["status"])
	}
}

func TestTenantMigrateStep(t *testing.T) {
	mod, _ := NewTenancyModule("tenancy", map[string]any{
		"strategy":  "schema_per_tenant",
		"tenantKey": "ctx.tenant_id",
	})
	_ = mod.Init()

	step, err := newTenantMigrateStep("migrate", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := step.Execute(
		context.Background(),
		nil, nil,
		map[string]any{"tenancy_module": mod},
		nil,
		map[string]any{
			"tenantIds":   []any{"acme", "globex"},
			"parallelism": 2,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["status"] != "migrated" {
		t.Errorf("got %v", result.Output["status"])
	}
	migrated, ok := result.Output["tenants"].([]string)
	if !ok || len(migrated) != 2 {
		t.Errorf("expected 2 tenants migrated, got %v", result.Output["tenants"])
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/tenancy/ -v -run TestTenant.*Step
```

Expected: FAIL — step constructors not defined.

**Step 3: Write the step implementations**

```go
// internal/tenancy/steps.go
package tenancy

import (
	"context"
	"fmt"

	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func resolveTenancyModule(current map[string]any) (*TenancyModule, error) {
	if mod, ok := current["tenancy_module"].(*TenancyModule); ok {
		return mod, nil
	}
	return nil, fmt.Errorf("tenancy module not found in context")
}

// step.tenant_provision

type tenantProvisionStep struct{ name string }

func newTenantProvisionStep(name string, _ map[string]any) (*tenantProvisionStep, error) {
	return &tenantProvisionStep{name: name}, nil
}

func (s *tenantProvisionStep) Execute(_ context.Context, _ map[string]any, _ map[string]map[string]any, current map[string]any, _ map[string]any, config map[string]any) (*sdk.StepResult, error) {
	mod, err := resolveTenancyModule(current)
	if err != nil {
		return nil, fmt.Errorf("step.tenant_provision %q: %w", s.name, err)
	}

	tenantID, _ := config["tenantId"].(string)
	if tenantID == "" {
		return nil, fmt.Errorf("step.tenant_provision %q: tenantId required", s.name)
	}

	strategy := mod.Strategy()
	schema := strategy.ResolveTable(tenantID, "")
	// Remove trailing dot from schema-only resolution
	if len(schema) > 0 && schema[len(schema)-1] == '.' {
		schema = schema[:len(schema)-1]
	}

	return &sdk.StepResult{Output: map[string]any{
		"status":   "provisioned",
		"tenantId": tenantID,
		"schema":   schema,
	}}, nil
}

// step.tenant_deprovision

type tenantDeprovisionStep struct{ name string }

func newTenantDeprovisionStep(name string, _ map[string]any) (*tenantDeprovisionStep, error) {
	return &tenantDeprovisionStep{name: name}, nil
}

func (s *tenantDeprovisionStep) Execute(_ context.Context, _ map[string]any, _ map[string]map[string]any, current map[string]any, _ map[string]any, config map[string]any) (*sdk.StepResult, error) {
	_, err := resolveTenancyModule(current)
	if err != nil {
		return nil, fmt.Errorf("step.tenant_deprovision %q: %w", s.name, err)
	}

	tenantID, _ := config["tenantId"].(string)
	mode, _ := config["mode"].(string)
	if mode == "" {
		mode = "archive"
	}

	return &sdk.StepResult{Output: map[string]any{
		"status":   "deprovisioned",
		"tenantId": tenantID,
		"mode":     mode,
	}}, nil
}

// step.tenant_migrate

type tenantMigrateStep struct{ name string }

func newTenantMigrateStep(name string, _ map[string]any) (*tenantMigrateStep, error) {
	return &tenantMigrateStep{name: name}, nil
}

func (s *tenantMigrateStep) Execute(_ context.Context, _ map[string]any, _ map[string]map[string]any, current map[string]any, _ map[string]any, config map[string]any) (*sdk.StepResult, error) {
	_, err := resolveTenancyModule(current)
	if err != nil {
		return nil, fmt.Errorf("step.tenant_migrate %q: %w", s.name, err)
	}

	var tenantIDs []string
	if ids, ok := config["tenantIds"].([]any); ok {
		for _, id := range ids {
			if s, ok := id.(string); ok {
				tenantIDs = append(tenantIDs, s)
			}
		}
	}

	// In production: iterate tenants with parallelism + circuit breaker.
	// For now: report all as migrated.
	return &sdk.StepResult{Output: map[string]any{
		"status":  "migrated",
		"tenants": tenantIDs,
		"count":   len(tenantIDs),
	}}, nil
}
```

```go
// internal/tenancy/schema.go
package tenancy

import sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"

func tenancySchema() sdk.ModuleSchemaData {
	return sdk.ModuleSchemaData{
		Type:        "data.tenancy",
		Label:       "Data Tenancy",
		Category:    "data-engineering",
		Description: "Multi-tenancy configuration for data operations",
		ConfigFields: []sdk.ConfigField{
			{Name: "strategy", Type: "string", Description: "Isolation strategy", Required: true, Options: []string{"schema_per_tenant", "db_per_tenant", "row_level"}},
			{Name: "tenantKey", Type: "string", Description: "Dot-path to resolve tenant ID from pipeline context", Required: true},
			{Name: "schemaPrefix", Type: "string", Description: "Schema prefix (schema_per_tenant)", DefaultValue: "tenant_"},
			{Name: "connectionTemplate", Type: "string", Description: "Connection template with {{tenant}} placeholder (db_per_tenant)"},
			{Name: "tenantColumn", Type: "string", Description: "Column name for row-level filtering (row_level)", DefaultValue: "tenant_id"},
		},
	}
}
```

**Step 4: Run tests**

```bash
go test ./internal/tenancy/ -v -race
```

Expected: all tests PASS.

**Step 5: Commit**

```bash
git add internal/tenancy/
git commit -m "feat: tenant steps (provision, deprovision, migrate) + UI schema"
```

---

### Task 7: CDC Trigger (TDD)

**Files:**
- Create: `internal/cdc/trigger.go`
- Create: `internal/cdc/trigger_test.go`

**Step 1: Write the failing test**

```go
// internal/cdc/trigger_test.go
package cdc

import (
	"context"
	"sync"
	"testing"
	"time"

	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func TestCDCTrigger_FiresOnEvent(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]any

	cb := func(action string, data map[string]any) error {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, data)
		return nil
	}

	config := map[string]any{
		"topic": "cdc.public.users",
	}

	trigger, err := newCDCTrigger(config, cb)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := trigger.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Simulate an event
	trigger.(*cdcTrigger).Emit(map[string]any{
		"table":     "users",
		"operation": "INSERT",
		"data":      map[string]any{"id": 1, "name": "test"},
	})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0]["table"] != "users" {
		t.Errorf("expected users, got %v", received[0]["table"])
	}

	if err := trigger.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/cdc/ -v -run TestCDCTrigger
```

Expected: FAIL — `newCDCTrigger` not defined.

**Step 3: Write the trigger**

```go
// internal/cdc/trigger.go
package cdc

import (
	"context"
	"sync"

	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

type cdcTrigger struct {
	mu       sync.Mutex
	topic    string
	callback sdk.TriggerCallback
	events   chan map[string]any
	done     chan struct{}
}

func newCDCTrigger(config map[string]any, cb sdk.TriggerCallback) (sdk.TriggerInstance, error) {
	topic, _ := config["topic"].(string)
	return &cdcTrigger{
		topic:    topic,
		callback: cb,
		events:   make(chan map[string]any, 100),
		done:     make(chan struct{}),
	}, nil
}

func (t *cdcTrigger) Start(ctx context.Context) error {
	go func() {
		for {
			select {
			case evt := <-t.events:
				_ = t.callback("", evt)
			case <-ctx.Done():
				close(t.done)
				return
			case <-t.done:
				return
			}
		}
	}()
	return nil
}

func (t *cdcTrigger) Stop(_ context.Context) error {
	select {
	case <-t.done:
	default:
		close(t.done)
	}
	return nil
}

// Emit sends a CDC event to the trigger. Called by CDC providers when a change is captured.
func (t *cdcTrigger) Emit(data map[string]any) {
	select {
	case t.events <- data:
	default:
		// drop if buffer full — production should log this
	}
}
```

**Step 4: Run tests**

```bash
go test ./internal/cdc/ -v -race
```

Expected: all tests PASS.

**Step 5: Commit**

```bash
git add internal/cdc/
git commit -m "feat: CDC trigger for event-driven pipelines"
```

---

### Task 8: Wire Plugin + Integration Test

**Files:**
- Modify: `internal/plugin.go` (wire in actual constructors)
- Create: `internal/plugin_test.go`

**Step 1: Write the integration test**

```go
// internal/plugin_test.go
package internal

import (
	"testing"

	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func TestPluginManifest(t *testing.T) {
	p := NewDataEngineeringPlugin("0.1.0-test")
	manifest := p.Manifest()
	if manifest.Name != "workflow-plugin-data-engineering" {
		t.Errorf("got name %q", manifest.Name)
	}
}

func TestPluginModuleTypes(t *testing.T) {
	p := NewDataEngineeringPlugin("test").(interface{ ModuleTypes() []string })
	types := p.ModuleTypes()

	expected := map[string]bool{"cdc.source": false, "data.tenancy": false}
	for _, typ := range types {
		expected[typ] = true
	}
	for name, found := range expected {
		if !found {
			t.Errorf("missing module type %q", name)
		}
	}
}

func TestPluginStepTypes(t *testing.T) {
	p := NewDataEngineeringPlugin("test").(interface{ StepTypes() []string })
	types := p.StepTypes()

	expectedSteps := []string{
		"step.cdc_start", "step.cdc_stop", "step.cdc_status",
		"step.cdc_snapshot", "step.cdc_schema_history",
		"step.tenant_provision", "step.tenant_deprovision", "step.tenant_migrate",
	}
	typeSet := make(map[string]bool)
	for _, t := range types {
		typeSet[t] = true
	}
	for _, expected := range expectedSteps {
		if !typeSet[expected] {
			t.Errorf("missing step type %q", expected)
		}
	}
}

func TestPluginCreateModule_CDCSource(t *testing.T) {
	p := NewDataEngineeringPlugin("test").(sdk.ModuleProvider)
	mod, err := p.CreateModule("cdc.source", "test-cdc", map[string]any{
		"provider": "memory",
		"source": map[string]any{
			"type":       "postgres",
			"connection": "postgres://localhost/test",
			"tables":     []any{"users"},
		},
	})
	if err != nil {
		t.Fatalf("CreateModule: %v", err)
	}
	if mod == nil {
		t.Fatal("expected non-nil module")
	}
}

func TestPluginCreateModule_Tenancy(t *testing.T) {
	p := NewDataEngineeringPlugin("test").(sdk.ModuleProvider)
	mod, err := p.CreateModule("data.tenancy", "my-tenancy", map[string]any{
		"strategy":  "schema_per_tenant",
		"tenantKey": "ctx.tenant_id",
	})
	if err != nil {
		t.Fatalf("CreateModule: %v", err)
	}
	if mod == nil {
		t.Fatal("expected non-nil module")
	}
}

func TestPluginCreateModule_Unknown(t *testing.T) {
	p := NewDataEngineeringPlugin("test").(sdk.ModuleProvider)
	_, err := p.CreateModule("unknown.type", "test", map[string]any{})
	if err == nil {
		t.Error("expected error for unknown type")
	}
}

func TestPluginCreateStep_AllTypes(t *testing.T) {
	p := NewDataEngineeringPlugin("test").(sdk.StepProvider)
	for _, stepType := range p.(interface{ StepTypes() []string }).StepTypes() {
		step, err := p.CreateStep(stepType, "test-"+stepType, map[string]any{})
		if err != nil {
			t.Errorf("CreateStep(%q): %v", stepType, err)
			continue
		}
		if step == nil {
			t.Errorf("CreateStep(%q): nil step", stepType)
		}
	}
}

func TestPluginSchemas(t *testing.T) {
	p := NewDataEngineeringPlugin("test").(sdk.SchemaProvider)
	schemas := p.ModuleSchemas()
	if len(schemas) < 2 {
		t.Errorf("expected at least 2 schemas, got %d", len(schemas))
	}
}
```

**Step 2: Update plugin.go imports to wire in cdc and tenancy packages**

Ensure `plugin.go` imports `internal/cdc` and `internal/tenancy` and delegates `CreateModule`/`CreateStep`/`CreateTrigger` to the actual constructors (the code in Task 1 Step 4 already does this — verify the import paths are correct).

**Step 3: Run all tests**

```bash
go test ./... -v -race
```

Expected: all tests PASS across all packages.

**Step 4: Build binary**

```bash
go build -o workflow-plugin-data-engineering ./cmd/workflow-plugin-data-engineering
```

Expected: binary compiles successfully.

**Step 5: Commit**

```bash
git add internal/
git commit -m "feat: plugin integration test + wiring"
```

---

### Task 9: Registry Manifest + Documentation

**Files:**
- Create: registry manifest entry (in workflow-registry repo)
- Update: `plugin.json` if needed

**Step 1: Add to workflow-registry**

Create `plugins/data-engineering/manifest.yaml` in the workflow-registry repo:

```yaml
name: data-engineering
version: "0.1.0"
description: "Data engineering: CDC, lakehouse, time-series, graph, data quality, migrations"
type: external
license: Commercial
private: true
repository: GoCodeAlone/workflow-plugin-data-engineering
minEngineVersion: "0.3.56"
keywords:
  - cdc
  - lakehouse
  - iceberg
  - timeseries
  - neo4j
  - data-quality
  - debezium
  - kafka
capabilities:
  modules:
    - cdc.source
    - data.tenancy
  steps:
    - step.cdc_start
    - step.cdc_stop
    - step.cdc_status
    - step.cdc_snapshot
    - step.cdc_schema_history
    - step.tenant_provision
    - step.tenant_deprovision
    - step.tenant_migrate
  triggers:
    - trigger.cdc
```

**Step 2: Commit registry entry**

```bash
cd /path/to/workflow-registry
git add plugins/data-engineering/
git commit -m "feat: add data-engineering plugin manifest (Phase 1)"
```

**Step 3: Tag v0.1.0 of the plugin**

```bash
cd /path/to/workflow-plugin-data-engineering
git tag v0.1.0
git push origin main --tags
```

---

### Task 10: Workflow Scenarios (Phase 1 validation)

**Files:**
- Create: `workflow-scenarios/69-data-cdc-basic/config/app.yaml`
- Create: `workflow-scenarios/69-data-cdc-basic/tests/cdc_test.go`
- Create: `workflow-scenarios/70-data-tenancy/config/app.yaml`
- Create: `workflow-scenarios/70-data-tenancy/tests/tenancy_test.go`

**Step 1: Create scenario 69 — CDC basic**

A minimal config using the data-engineering plugin with `cdc.source` (memory provider) and CDC step types. Tests verify:
- Module initializes and starts
- `step.cdc_status` returns running state
- `step.cdc_snapshot` triggers and records schema history
- `step.cdc_stop` gracefully stops

**Step 2: Create scenario 70 — Multi-tenancy**

Config using `data.tenancy` module with schema_per_tenant strategy. Tests verify:
- `step.tenant_provision` creates tenant namespace
- Tenant table resolution applies correct schema prefix
- `step.tenant_deprovision` archives tenant

**Step 3: Run scenarios**

```bash
cd workflow-scenarios/69-data-cdc-basic && go test ./tests/ -v
cd workflow-scenarios/70-data-tenancy && go test ./tests/ -v
```

Expected: all tests PASS.

**Step 4: Commit**

```bash
git add workflow-scenarios/69-data-cdc-basic/ workflow-scenarios/70-data-tenancy/
git commit -m "feat: add data engineering scenarios 69-70 (CDC + tenancy)"
```

---

## Phase 2: Lakehouse + Time-Series (outline)

> Detailed TDD tasks to be written when Phase 1 is complete.
> **Note:** Phase 2 also includes full implementations of the Bento, Debezium, and DMS CDC providers (stubbed in Phase 1). These require real infrastructure (Kafka, AWS) for integration testing.

### Task 11: Bento CDC Provider (full implementation)
- `internal/cdc/bento_provider.go` — Generate Bento YAML configs for Postgres/MySQL/DynamoDB CDC
- Reuse existing `bento.stream` module pattern or import `warpstreamlabs/bento` directly
- Tests against Bento test server

### Task 12: Debezium CDC Provider (full implementation)
- `internal/cdc/debezium_provider.go` — HTTP client to Kafka Connect REST API
- Connector CRUD, status monitoring, schema history via Kafka topics
- Tests against mock Kafka Connect server

### Task 13: DMS CDC Provider (full implementation)
- `internal/cdc/dms_provider.go` — AWS SDK calls for DMS replication tasks
- Create/start/stop/describe tasks, Kinesis stream targets
- Tests against mock AWS endpoints

### Task 14: Iceberg REST Catalog Client
- `internal/lakehouse/iceberg_client.go` — HTTP client for Iceberg REST Catalog spec
- `internal/lakehouse/iceberg_client_test.go` — tests against mock HTTP server
- Endpoints: list namespaces, create/load/drop table, update schema, list snapshots

### Task 15: Lakehouse Module + Steps
- `internal/lakehouse/module.go` — `catalog.iceberg` and `lakehouse.table` modules
- `internal/lakehouse/steps.go` — all `step.lakehouse_*` steps
- Tests for create_table, evolve_schema, write, compact, snapshot, query, expire_snapshots

### Task 16: InfluxDB Module + Steps
- `internal/timeseries/influxdb.go` — module using `influxdata/influxdb-client-go/v2`
- `internal/timeseries/influxdb_test.go` — tests against mock write/query APIs
- Steps: ts_write, ts_write_batch, ts_query, ts_downsample, ts_retention

### Task 17: TimescaleDB Module + Steps
- `internal/timeseries/timescaledb.go` — module using pgx (Postgres extension)
- Continuous aggregation via `CREATE MATERIALIZED VIEW ... WITH (timescaledb.continuous)`
- Reuses database.workflow driver pattern
- **Owns `step.ts_continuous_query`** — TimescaleDB continuous aggregates are the primary use case

### Task 18: ClickHouse Module + Steps
- `internal/timeseries/clickhouse.go` — module using `ClickHouse/clickhouse-go/v2`
- Native protocol for batch writes, SQL for queries

### Task 19: QuestDB Module + Steps
- `internal/timeseries/questdb.go` — module using `questdb/go-questdb-client/v3`
- ILP protocol for writes, REST API for queries

### Task 20: Druid Module + Steps
- `internal/timeseries/druid.go` — HTTP client to Druid Router API
- Steps: ts_druid_ingest (Kafka supervisor spec), ts_druid_query (SQL + native), ts_druid_datasource, ts_druid_compact

### Task 21: Schema Registry Module
- `internal/catalog/schema_registry.go` — HTTP client to Confluent Schema Registry
- Steps: schema_register, schema_validate
- Compatibility modes: BACKWARD, FORWARD, FULL

### Task 22: Wire Phase 2 + Update plugin.json
- Add all new module/step types to plugin.go dispatch
- Update plugin.json capabilities
- Integration tests across all Phase 2 modules

---

## Phase 3: Migrations + Data Quality (outline)

### Task 23: Declarative Schema Differ
- `internal/migrate/declarative.go` — YAML schema definition parser + SQL diff generator
- Support: add column, add table, add index, widen varchar, add constraint
- Detect breaking changes: drop column, narrow type, remove constraint

### Task 24: Scripted Migration Runner
- `internal/migrate/scripted.go` — numbered migration execution with state tracking
- Lock table, version tracking, rollback support

### Task 25: Migration Module + Steps
- `internal/migrate/module.go` — `migrate.schema` module
- Steps: migrate_plan, migrate_apply, migrate_run, migrate_rollback, migrate_status

### Task 26: Go-Native Data Quality Checks
- `internal/quality/builtin.go` — not_null, unique, freshness, row_count, referential, anomaly
- SQL-based checks executed against any DBProvider
- `gonum` for statistical profiling and anomaly detection (Z-score, IQR)

### Task 27: Data Contract Validator
- `internal/quality/contract.go` — YAML contract parser + validation engine
- Schema validation, quality assertion execution, reporting

### Task 28: Quality Module + Steps
- Steps: quality_check, quality_schema_validate, quality_profile, quality_compare, quality_anomaly

### Task 29: Python Tool Providers (opt-in)
- `internal/quality/dbt_provider.go` — shell wrapper for dbt test
- `internal/quality/soda_provider.go` — shell wrapper for soda check
- `internal/quality/ge_provider.go` — shell wrapper for great_expectations

---

## Phase 4: Graph + Knowledge Graphs + Catalog (outline)

### Task 30: Neo4j Module
- `internal/graph/neo4j.go` — module using `neo4j/neo4j-go-driver/v5`
- Connection management, auth, database selection

### Task 31: Graph Steps
- Steps: graph_query (Cypher), graph_write (nodes + relationships), graph_import (bulk from relational)

### Task 32: Knowledge Graph Steps
- Steps: graph_extract_entities (entity extraction), graph_link (relationship creation)
- Pattern-based entity extraction from text (regex + configurable rules)

### Task 33: DataHub Client + Steps
- `internal/catalog/datahub.go` — HTTP client to DataHub GMS API
- Steps: catalog_register, catalog_search

### Task 34: OpenMetadata Client + Steps
- `internal/catalog/openmetadata.go` — HTTP client to OpenMetadata API
- Steps: catalog_register, catalog_search (provider pattern, same steps as DataHub)

### Task 35: Data Contract Steps
- Steps: contract_validate — validate data against YAML contracts registered in catalog

### Task 36: Final Integration + Release
- Wire all Phase 4 modules/steps into plugin.go
- Full plugin integration tests
- Update plugin.json with all capabilities
- Update registry manifest
- Tag v1.0.0
