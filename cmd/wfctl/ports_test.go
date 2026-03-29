package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestCollectPorts_TopLevelModules(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "api-server",
				Type: "http.server",
				Config: map[string]any{
					"address": ":8080",
				},
			},
			{
				Name: "db",
				Type: "database.workflow",
				Config: map[string]any{
					"port": 5432,
				},
			},
		},
	}
	entries := collectPorts(cfg)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}

	// Entries sorted by service then port.
	if entries[0].Port != 5432 {
		t.Errorf("expected port 5432 first, got %d", entries[0].Port)
	}
	if entries[1].Port != 8080 {
		t.Errorf("expected port 8080 second, got %d", entries[1].Port)
	}
	if entries[1].Exposure != "public" {
		t.Errorf("expected http.server to be public, got %q", entries[1].Exposure)
	}
}

func TestCollectPorts_ServicesAndIngress(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Services: map[string]*config.ServiceConfig{
			"api": {
				Expose: []config.ExposeConfig{
					{Port: 8080, Protocol: "http"},
					{Port: 9090, Protocol: "grpc"},
				},
			},
		},
		Networking: &config.NetworkingConfig{
			Ingress: []config.IngressConfig{
				{Service: "api", Port: 8080, ExternalPort: 443, Protocol: "https"},
			},
		},
	}
	entries := collectPorts(cfg)
	// 2 from expose + 1 from ingress = 3
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(entries), entries)
	}
	// Check ingress entry is public.
	var hasPublic bool
	for _, e := range entries {
		if e.Exposure == "public" {
			hasPublic = true
		}
	}
	if !hasPublic {
		t.Error("expected at least one public entry from ingress")
	}
}

func TestPrintPortsTable(t *testing.T) {
	entries := []portEntry{
		{Service: "api", Module: "http", Port: 8080, Protocol: "http", Exposure: "public"},
	}
	var buf bytes.Buffer
	if err := printPortsTable(&buf, entries); err != nil {
		t.Fatalf("printPortsTable error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "8080") {
		t.Errorf("expected port 8080 in output: %q", out)
	}
	if !strings.Contains(out, "public") {
		t.Errorf("expected 'public' in output: %q", out)
	}
}

func TestExtractModulePort_AddressColon(t *testing.T) {
	mod := config.ModuleConfig{
		Name: "srv",
		Type: "http.server",
		Config: map[string]any{
			"address": ":9090",
		},
	}
	port, proto := extractModulePort(mod)
	if port != 9090 {
		t.Errorf("expected 9090, got %d", port)
	}
	if proto != "http" {
		t.Errorf("expected http, got %q", proto)
	}
}

func TestExtractModulePort_IntPort(t *testing.T) {
	mod := config.ModuleConfig{
		Name: "db",
		Type: "database.workflow",
		Config: map[string]any{
			"port": 5432,
		},
	}
	port, _ := extractModulePort(mod)
	if port != 5432 {
		t.Errorf("expected 5432, got %d", port)
	}
}
