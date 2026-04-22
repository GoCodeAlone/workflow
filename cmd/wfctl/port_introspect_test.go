package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"gopkg.in/yaml.v3"
)

// minimalConfig builds a WorkflowConfig with the given module list for testing.
func makePortTestConfig(t *testing.T, yamlSrc string) *config.WorkflowConfig {
	t.Helper()
	var cfg config.WorkflowConfig
	if err := yaml.Unmarshal([]byte(yamlSrc), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	return &cfg
}

func TestIntrospectPorts_HTTPServer(t *testing.T) {
	cfg := makePortTestConfig(t, `
modules:
  - name: api
    type: http.server
    config:
      port: 8080
`)
	ports := IntrospectPorts(cfg, "")
	if len(ports) == 0 {
		t.Fatal("expected at least one port from http.server module")
	}
	var found bool
	for _, p := range ports {
		if p.Port == 8080 && p.Protocol == "http" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected port 8080/http in results: %+v", ports)
	}
}

func TestIntrospectPorts_MetricsAndHTTP(t *testing.T) {
	cfg := makePortTestConfig(t, `
modules:
  - name: api
    type: http.server
    config:
      port: 8080
  - name: metrics
    type: observability.metrics
    config:
      port: 9090
`)
	ports := IntrospectPorts(cfg, "")
	portNums := make(map[int]bool)
	for _, p := range ports {
		portNums[p.Port] = true
	}
	if !portNums[8080] {
		t.Error("expected port 8080 (http.server)")
	}
	if !portNums[9090] {
		t.Error("expected port 9090 (observability.metrics)")
	}
}

func TestIntrospectPorts_ExposePortsOverride(t *testing.T) {
	// When ci.build.containers[].expose_ports is set explicitly, those override.
	cfg := makePortTestConfig(t, `
modules:
  - name: api
    type: http.server
    config:
      port: 8080
ci:
  build:
    containers:
      - name: app
        expose_ports:
          - 443
          - 8443
`)
	ports := IntrospectPorts(cfg, "")
	portNums := make(map[int]bool)
	for _, p := range ports {
		portNums[p.Port] = true
	}
	// Explicit override: only declared ports should appear.
	if !portNums[443] {
		t.Error("expected explicit port 443")
	}
	if !portNums[8443] {
		t.Error("expected explicit port 8443")
	}
}

func TestIntrospectPorts_GRPCServer(t *testing.T) {
	cfg := makePortTestConfig(t, `
modules:
  - name: grpc
    type: grpc.server
    config:
      port: 9000
`)
	ports := IntrospectPorts(cfg, "")
	var found bool
	for _, p := range ports {
		if p.Port == 9000 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected port 9000 (grpc.server): %+v", ports)
	}
}

func TestIntrospectPorts_PluginPortIntrospect(t *testing.T) {
	// If a plugin declares portIntrospect in its manifest (list of JSON paths),
	// the aggregator reads those paths from the module config.
	pluginsDir := t.TempDir()
	writePortIntrospectPlugin(t, pluginsDir, "my-plugin", []string{"config.api_port"})

	cfg := makePortTestConfig(t, `
modules:
  - name: my-api
    type: my-plugin.api
    config:
      api_port: 7777
`)
	ports := IntrospectPorts(cfg, pluginsDir)
	var found bool
	for _, p := range ports {
		if p.Port == 7777 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected port 7777 from plugin portIntrospect paths: %+v", ports)
	}
}

func TestIntrospectPorts_Empty(t *testing.T) {
	cfg := &config.WorkflowConfig{}
	ports := IntrospectPorts(cfg, "")
	if len(ports) != 0 {
		t.Errorf("empty config should return no ports, got: %v", ports)
	}
}

func TestIntrospectPorts_PublicFlag(t *testing.T) {
	// http.server ports should be public=true; internal modules should be public=false.
	cfg := makePortTestConfig(t, `
modules:
  - name: api
    type: http.server
    config:
      port: 8080
  - name: db
    type: database.postgres
    config:
      port: 5432
`)
	ports := IntrospectPorts(cfg, "")
	portMap := make(map[int]interfaces.PortSpec)
	for _, p := range ports {
		portMap[p.Port] = p
	}
	if !portMap[8080].Public {
		t.Error("http.server port should be public=true")
	}
	if portMap[5432].Public {
		t.Error("database port should be public=false")
	}
}

// writePortIntrospectPlugin writes a fake plugin that declares portIntrospect paths.
func writePortIntrospectPlugin(t *testing.T, pluginsDir, name string, paths []string) {
	t.Helper()
	dir := filepath.Join(pluginsDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"name":    name,
		"version": "0.1.0",
		"capabilities": map[string]any{
			"portPaths": paths, // JSON paths into module config
		},
	}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), b, 0644); err != nil {
		t.Fatal(err)
	}
}
