package main

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// ── generateDevCompose ────────────────────────────────────────────────────────

func TestGenerateDevCompose_SingleService(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "db",
				Type: "database.postgres",
				Config: map[string]any{
					"port": 5432,
				},
			},
			{
				Name: "api",
				Type: "http.server",
				Config: map[string]any{
					"address": ":8080",
				},
			},
		},
	}

	out, err := generateDevCompose(cfg)
	if err != nil {
		t.Fatalf("generateDevCompose error: %v", err)
	}

	if !strings.Contains(out, "postgres:16") {
		t.Errorf("expected postgres:16 image in output:\n%s", out)
	}
	if !strings.Contains(out, "postgres") {
		t.Errorf("expected postgres service in output:\n%s", out)
	}
	if !strings.Contains(out, "app") {
		t.Errorf("expected app service in output:\n%s", out)
	}
}

func TestGenerateDevCompose_MultiService(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "cache",
				Type: "nosql.redis",
			},
		},
		Services: map[string]*config.ServiceConfig{
			"api": {
				Expose: []config.ExposeConfig{{Port: 8080, Protocol: "http"}},
			},
			"worker": {
				Expose: []config.ExposeConfig{{Port: 9090}},
			},
		},
	}

	out, err := generateDevCompose(cfg)
	if err != nil {
		t.Fatalf("generateDevCompose error: %v", err)
	}

	if !strings.Contains(out, "redis:7-alpine") {
		t.Errorf("expected redis:7-alpine image in output:\n%s", out)
	}
	if !strings.Contains(out, "api") {
		t.Errorf("expected api service in output:\n%s", out)
	}
	if !strings.Contains(out, "worker") {
		t.Errorf("expected worker service in output:\n%s", out)
	}
}

func TestGenerateDevCompose_PortMappings(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "db",
				Type: "database.postgres",
			},
		},
		Services: map[string]*config.ServiceConfig{
			"api": {
				Expose: []config.ExposeConfig{{Port: 8080}},
			},
		},
	}

	out, err := generateDevCompose(cfg)
	if err != nil {
		t.Fatalf("generateDevCompose error: %v", err)
	}

	if !strings.Contains(out, "5432:5432") {
		t.Errorf("expected postgres port 5432:5432 in output:\n%s", out)
	}
	if !strings.Contains(out, "8080:8080") {
		t.Errorf("expected app port 8080:8080 in output:\n%s", out)
	}
}

func TestGenerateDevCompose_DependsOnInfra(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "db", Type: "database.postgres"},
			{Name: "cache", Type: "nosql.redis"},
		},
		Services: map[string]*config.ServiceConfig{
			"api": {Expose: []config.ExposeConfig{{Port: 8080}}},
		},
	}

	out, err := generateDevCompose(cfg)
	if err != nil {
		t.Fatalf("generateDevCompose error: %v", err)
	}

	if !strings.Contains(out, "depends_on") {
		t.Errorf("expected depends_on in output:\n%s", out)
	}
}

func TestGenerateDevCompose_VolumeForPostgres(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "db", Type: "database.workflow"},
		},
	}

	out, err := generateDevCompose(cfg)
	if err != nil {
		t.Fatalf("generateDevCompose error: %v", err)
	}

	if !strings.Contains(out, "pgdata") {
		t.Errorf("expected pgdata volume in output:\n%s", out)
	}
}

func TestGenerateDevCompose_NatsMessaging(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "mq", Type: "messaging.nats"},
		},
	}

	out, err := generateDevCompose(cfg)
	if err != nil {
		t.Fatalf("generateDevCompose error: %v", err)
	}

	if !strings.Contains(out, "nats:latest") {
		t.Errorf("expected nats:latest image in output:\n%s", out)
	}
	if !strings.Contains(out, "4222:4222") {
		t.Errorf("expected nats port 4222:4222 in output:\n%s", out)
	}
}

func TestGenerateDevCompose_NoInfraModules(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name:   "api",
				Type:   "http.server",
				Config: map[string]any{"address": ":8080"},
			},
		},
	}

	out, err := generateDevCompose(cfg)
	if err != nil {
		t.Fatalf("generateDevCompose error: %v", err)
	}
	// Should have the app service but no infra services.
	if !strings.Contains(out, "app") {
		t.Errorf("expected app service in output:\n%s", out)
	}
	if strings.Contains(out, "postgres") {
		t.Errorf("did not expect postgres service when no postgres module:\n%s", out)
	}
}

// ── moduleTypeToDNS ──────────────────────────────────────────────────────────

func TestModuleTypeToDNS(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"database.postgres", "postgres"},
		{"database.workflow", "postgres"},
		{"nosql.redis", "redis"},
		{"cache.redis", "redis"},
		{"messaging.nats", "nats"},
	}
	for _, tt := range tests {
		got := moduleTypeToDNS(tt.input)
		if got != tt.want {
			t.Errorf("moduleTypeToDNS(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ── extractInfraPort ─────────────────────────────────────────────────────────

func TestExtractInfraPort(t *testing.T) {
	tests := []struct {
		modType string
		want    int
	}{
		{"database.postgres", 5432},
		{"database.workflow", 5432},
		{"nosql.redis", 6379},
		{"cache.redis", 6379},
		{"messaging.nats", 4222},
		{"messaging.kafka", 9092},
		{"http.server", 0},
	}
	for _, tt := range tests {
		mod := config.ModuleConfig{Type: tt.modType}
		got := extractInfraPort(mod)
		if got != tt.want {
			t.Errorf("extractInfraPort(%q) = %d, want %d", tt.modType, got, tt.want)
		}
	}
}
