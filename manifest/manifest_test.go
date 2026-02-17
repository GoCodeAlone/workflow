package manifest

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestAnalyze_EmptyConfig(t *testing.T) {
	cfg := config.NewEmptyWorkflowConfig()
	m := Analyze(cfg)

	if m.Name != "unknown" {
		t.Errorf("expected name 'unknown', got %q", m.Name)
	}
	if len(m.Databases) != 0 {
		t.Errorf("expected 0 databases, got %d", len(m.Databases))
	}
	if len(m.Ports) != 0 {
		t.Errorf("expected 0 ports, got %d", len(m.Ports))
	}
	if m.EventBus != nil {
		t.Error("expected nil EventBus")
	}
	if m.ResourceEst.CPUCores < 0.25 {
		t.Errorf("expected minimum 0.25 CPU cores, got %f", m.ResourceEst.CPUCores)
	}
	if m.ResourceEst.MemoryMB < 128 {
		t.Errorf("expected minimum 128 MB memory, got %d", m.ResourceEst.MemoryMB)
	}
}

func TestAnalyze_HTTPServer(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "web-server", Type: "http.server", Config: map[string]any{"address": ":9090"}},
		},
		Workflows: make(map[string]any),
		Triggers:  make(map[string]any),
		Pipelines: make(map[string]any),
	}

	m := Analyze(cfg)

	if len(m.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(m.Ports))
	}
	if m.Ports[0].Port != 9090 {
		t.Errorf("expected port 9090, got %d", m.Ports[0].Port)
	}
	if m.Ports[0].Protocol != "http" {
		t.Errorf("expected protocol 'http', got %q", m.Ports[0].Protocol)
	}
	if m.Ports[0].ModuleName != "web-server" {
		t.Errorf("expected module name 'web-server', got %q", m.Ports[0].ModuleName)
	}
}

func TestAnalyze_HTTPServerDefaultPort(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "srv", Type: "http.server"},
		},
		Workflows: make(map[string]any),
		Triggers:  make(map[string]any),
		Pipelines: make(map[string]any),
	}

	m := Analyze(cfg)

	if len(m.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(m.Ports))
	}
	if m.Ports[0].Port != 8080 {
		t.Errorf("expected default port 8080, got %d", m.Ports[0].Port)
	}
}

func TestAnalyze_Database(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "main-db",
				Type: "database.workflow",
				Config: map[string]any{
					"driver":       "postgres",
					"dsn":          "postgres://localhost/mydb",
					"maxOpenConns": 50,
					"maxIdleConns": 10,
				},
			},
		},
		Workflows: make(map[string]any),
		Triggers:  make(map[string]any),
		Pipelines: make(map[string]any),
	}

	m := Analyze(cfg)

	if len(m.Databases) != 1 {
		t.Fatalf("expected 1 database, got %d", len(m.Databases))
	}
	db := m.Databases[0]
	if db.Driver != "postgres" {
		t.Errorf("expected driver 'postgres', got %q", db.Driver)
	}
	if db.DSN != "postgres://localhost/mydb" {
		t.Errorf("expected DSN, got %q", db.DSN)
	}
	if db.MaxOpenConns != 50 {
		t.Errorf("expected maxOpenConns 50, got %d", db.MaxOpenConns)
	}
	if db.MaxIdleConns != 10 {
		t.Errorf("expected maxIdleConns 10, got %d", db.MaxIdleConns)
	}
}

func TestAnalyze_SQLiteDatabase(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "admin-db",
				Type: "storage.sqlite",
				Config: map[string]any{
					"dbPath":         "data/admin.db",
					"maxConnections": 5,
				},
			},
		},
		Workflows: make(map[string]any),
		Triggers:  make(map[string]any),
		Pipelines: make(map[string]any),
	}

	m := Analyze(cfg)

	if len(m.Databases) != 1 {
		t.Fatalf("expected 1 database, got %d", len(m.Databases))
	}
	db := m.Databases[0]
	if db.Driver != "sqlite3" {
		t.Errorf("expected driver 'sqlite3', got %q", db.Driver)
	}
	if db.DSN != "data/admin.db" {
		t.Errorf("expected DSN 'data/admin.db', got %q", db.DSN)
	}
	if db.MaxOpenConns != 5 {
		t.Errorf("expected maxOpenConns 5, got %d", db.MaxOpenConns)
	}
}

func TestAnalyze_Messaging(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "broker", Type: "messaging.broker"},
		},
		Workflows: map[string]any{
			"messaging": map[string]any{
				"subscriptions": []any{
					map[string]any{"topic": "order.created", "handler": "h1"},
					map[string]any{"topic": "order.completed", "handler": "h2"},
				},
				"producers": []any{
					map[string]any{
						"name":      "order-api",
						"forwardTo": []any{"order.created", "order.updated"},
					},
				},
			},
		},
		Triggers:  make(map[string]any),
		Pipelines: make(map[string]any),
	}

	m := Analyze(cfg)

	if m.EventBus == nil {
		t.Fatal("expected EventBus to be set")
	}
	if m.EventBus.Technology != "in-memory" {
		t.Errorf("expected technology 'in-memory', got %q", m.EventBus.Technology)
	}
	expectedTopics := []string{"order.created", "order.completed", "order.updated"}
	for _, exp := range expectedTopics {
		found := false
		for _, t := range m.EventBus.Topics {
			if t == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected topic %q in EventBus.Topics", exp)
		}
	}
}

func TestAnalyze_NATSBroker(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "nats-broker",
				Type: "messaging.nats",
				Config: map[string]any{
					"url": "nats://nats.example.com:4222",
				},
			},
		},
		Workflows: make(map[string]any),
		Triggers:  make(map[string]any),
		Pipelines: make(map[string]any),
	}

	m := Analyze(cfg)

	if m.EventBus == nil {
		t.Fatal("expected EventBus to be set")
	}
	if m.EventBus.Technology != "nats" {
		t.Errorf("expected technology 'nats', got %q", m.EventBus.Technology)
	}
	if len(m.EventBus.Queues) != 1 || m.EventBus.Queues[0] != "nats://nats.example.com:4222" {
		t.Errorf("expected NATS URL in queues, got %v", m.EventBus.Queues)
	}
}

func TestAnalyze_KafkaBroker(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "kafka-broker",
				Type: "messaging.kafka",
				Config: map[string]any{
					"brokers": []any{"kafka-1:9092", "kafka-2:9092"},
					"groupId": "my-group",
				},
			},
		},
		Workflows: make(map[string]any),
		Triggers:  make(map[string]any),
		Pipelines: make(map[string]any),
	}

	m := Analyze(cfg)

	if m.EventBus == nil {
		t.Fatal("expected EventBus to be set")
	}
	if m.EventBus.Technology != "kafka" {
		t.Errorf("expected technology 'kafka', got %q", m.EventBus.Technology)
	}
	if len(m.EventBus.Queues) != 2 {
		t.Errorf("expected 2 queues, got %d", len(m.EventBus.Queues))
	}
}

func TestAnalyze_Storage(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "s3-store", Type: "storage.s3", Config: map[string]any{"bucket": "my-bucket", "region": "us-west-2"}},
			{Name: "local-store", Type: "storage.local", Config: map[string]any{"rootDir": "/data/files"}},
			{Name: "gcs-store", Type: "storage.gcs", Config: map[string]any{"bucket": "gcs-bucket"}},
		},
		Workflows: make(map[string]any),
		Triggers:  make(map[string]any),
		Pipelines: make(map[string]any),
	}

	m := Analyze(cfg)

	if len(m.Storage) != 3 {
		t.Fatalf("expected 3 storage requirements, got %d", len(m.Storage))
	}

	// S3
	if m.Storage[0].Type != "s3" || m.Storage[0].Bucket != "my-bucket" || m.Storage[0].Region != "us-west-2" {
		t.Errorf("unexpected S3 storage: %+v", m.Storage[0])
	}
	// Local
	if m.Storage[1].Type != "local" || m.Storage[1].RootDir != "/data/files" {
		t.Errorf("unexpected local storage: %+v", m.Storage[1])
	}
	// GCS
	if m.Storage[2].Type != "gcs" || m.Storage[2].Bucket != "gcs-bucket" {
		t.Errorf("unexpected GCS storage: %+v", m.Storage[2])
	}

	// Check disk estimate includes storage
	if m.ResourceEst.DiskMB < 1536 { // 3 * 512
		t.Errorf("expected at least 1536 MB disk for 3 storage modules, got %d", m.ResourceEst.DiskMB)
	}
}

func TestAnalyze_PipelineHTTPCall(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "srv", Type: "http.server", Config: map[string]any{"address": ":8080"}},
		},
		Workflows: make(map[string]any),
		Triggers:  make(map[string]any),
		Pipelines: map[string]any{
			"enrichment": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "call-external",
						"type": "step.http_call",
						"config": map[string]any{
							"url":    "https://api.example.com/enrich",
							"method": "POST",
						},
					},
					map[string]any{
						"name": "call-another",
						"type": "step.http_call",
						"config": map[string]any{
							"url": "https://api.other.com/lookup",
						},
					},
				},
			},
		},
	}

	m := Analyze(cfg)

	if len(m.ExternalAPIs) != 2 {
		t.Fatalf("expected 2 external APIs, got %d", len(m.ExternalAPIs))
	}
	if m.ExternalAPIs[0].URL != "https://api.example.com/enrich" {
		t.Errorf("unexpected URL: %q", m.ExternalAPIs[0].URL)
	}
	if m.ExternalAPIs[0].Method != "POST" {
		t.Errorf("expected method POST, got %q", m.ExternalAPIs[0].Method)
	}
	if m.ExternalAPIs[1].Method != "GET" {
		t.Errorf("expected default method GET, got %q", m.ExternalAPIs[1].Method)
	}
}

func TestAnalyze_PipelineDBStep(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: make(map[string]any),
		Triggers:  make(map[string]any),
		Pipelines: map[string]any{
			"query-pipeline": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "get-user",
						"type": "step.db_query",
						"config": map[string]any{
							"database": "users-db",
							"query":    "SELECT * FROM users WHERE id = ?",
						},
					},
				},
			},
		},
	}

	m := Analyze(cfg)

	if len(m.Databases) != 1 {
		t.Fatalf("expected 1 database from pipeline step, got %d", len(m.Databases))
	}
	if m.Databases[0].ModuleName != "users-db" {
		t.Errorf("expected database 'users-db', got %q", m.Databases[0].ModuleName)
	}
}

func TestAnalyze_EventBusModule(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "bus", Type: "messaging.broker.eventbus"},
		},
		Workflows: make(map[string]any),
		Triggers:  make(map[string]any),
		Pipelines: make(map[string]any),
	}

	m := Analyze(cfg)

	if m.EventBus == nil {
		t.Fatal("expected EventBus to be set")
	}
	if m.EventBus.Technology != "eventbus-bridge" {
		t.Errorf("expected technology 'eventbus-bridge', got %q", m.EventBus.Technology)
	}
}

func TestAnalyze_ComplexWorkflow(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "web", Type: "http.server", Config: map[string]any{"address": ":8080"}},
			{Name: "router", Type: "http.router"},
			{Name: "api", Type: "api.handler"},
			{Name: "db", Type: "database.workflow", Config: map[string]any{"driver": "postgres", "dsn": "postgres://localhost/app", "maxOpenConns": 25}},
			{Name: "broker", Type: "messaging.broker"},
			{Name: "s3", Type: "storage.s3", Config: map[string]any{"bucket": "uploads", "region": "us-east-1"}},
			{Name: "metrics", Type: "metrics.collector"},
		},
		Workflows: map[string]any{
			"messaging": map[string]any{
				"subscriptions": []any{
					map[string]any{"topic": "events.user", "handler": "h1"},
				},
			},
		},
		Triggers:  make(map[string]any),
		Pipelines: make(map[string]any),
	}

	m := Analyze(cfg)

	// Ports
	if len(m.Ports) != 1 || m.Ports[0].Port != 8080 {
		t.Errorf("expected port 8080, got %+v", m.Ports)
	}

	// Databases
	if len(m.Databases) != 1 {
		t.Fatalf("expected 1 database, got %d", len(m.Databases))
	}

	// Storage
	if len(m.Storage) != 1 {
		t.Fatalf("expected 1 storage, got %d", len(m.Storage))
	}

	// EventBus
	if m.EventBus == nil {
		t.Fatal("expected EventBus")
	}
	if len(m.EventBus.Topics) != 1 || m.EventBus.Topics[0] != "events.user" {
		t.Errorf("unexpected topics: %v", m.EventBus.Topics)
	}

	// Services: all 7 modules
	if len(m.Services) != 7 {
		t.Errorf("expected 7 services, got %d", len(m.Services))
	}

	// Resources: 7 modules
	if m.ResourceEst.CPUCores < 0.7 {
		t.Errorf("expected >= 0.7 CPU cores, got %f", m.ResourceEst.CPUCores)
	}
	if m.ResourceEst.MemoryMB < 448 { // 7 * 64
		t.Errorf("expected >= 448 MB memory, got %d", m.ResourceEst.MemoryMB)
	}
}

func TestAnalyzeWithName(t *testing.T) {
	cfg := config.NewEmptyWorkflowConfig()
	m := AnalyzeWithName(cfg, "my-workflow")
	if m.Name != "my-workflow" {
		t.Errorf("expected name 'my-workflow', got %q", m.Name)
	}
}

func TestSummary(t *testing.T) {
	m := &WorkflowManifest{
		Name:      "test-wf",
		Ports:     []PortRequirement{{ModuleName: "srv", Port: 8080, Protocol: "http"}},
		Databases: []DatabaseRequirement{{ModuleName: "db", Driver: "postgres", DSN: "localhost"}},
		ResourceEst: ResourceEstimate{
			CPUCores: 0.5,
			MemoryMB: 256,
			DiskMB:   512,
		},
	}

	s := m.Summary()
	if s == "" {
		t.Error("expected non-empty summary")
	}
	if !contains(s, "test-wf") {
		t.Error("summary should contain workflow name")
	}
	if !contains(s, "8080") {
		t.Error("summary should contain port")
	}
	if !contains(s, "postgres") {
		t.Error("summary should contain database driver")
	}
}

func TestParsePort(t *testing.T) {
	tests := []struct {
		address string
		want    int
	}{
		{":8080", 8080},
		{"0.0.0.0:9090", 9090},
		{"localhost:3000", 3000},
		{":0", 0},
		{"invalid", 0},
	}

	for _, tt := range tests {
		got := parsePort(tt.address)
		if got != tt.want {
			t.Errorf("parsePort(%q) = %d, want %d", tt.address, got, tt.want)
		}
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		input any
		want  int
		ok    bool
	}{
		{42, 42, true},
		{int64(100), 100, true},
		{float64(25), 25, true},
		{"10", 10, true},
		{"abc", 0, false},
		{nil, 0, false},
	}

	for _, tt := range tests {
		got, ok := toInt(tt.input)
		if got != tt.want || ok != tt.ok {
			t.Errorf("toInt(%v) = (%d, %v), want (%d, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
