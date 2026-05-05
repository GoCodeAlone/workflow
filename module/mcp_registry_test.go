package module

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMCPRegistryConfig(t *testing.T) {
	cfg := MCPRegistryConfig{
		LogOnInit:      true,
		ExposeAdminAPI: true,
		AuditToolCalls: true,
	}
	if !cfg.LogOnInit {
		t.Error("expected log_on_init true")
	}
	if !cfg.ExposeAdminAPI {
		t.Error("expected expose_admin_api true")
	}
	if !cfg.AuditToolCalls {
		t.Error("expected audit_tool_calls true")
	}
}

func TestMCPRegistryRegisterServer(t *testing.T) {
	r := NewMCPRegistry()
	r.RegisterServer("wfctl", MCPServerInfo{
		Name:  "wfctl",
		Type:  "in-process",
		Tools: []string{"validate_config", "inspect_config"},
	})
	servers := r.ListServers()
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if servers[0].Name != "wfctl" {
		t.Errorf("expected wfctl, got %s", servers[0].Name)
	}
}

func TestMCPRegistryUnregisterServer(t *testing.T) {
	r := NewMCPRegistry()
	r.RegisterServer("wfctl", MCPServerInfo{Name: "wfctl", Type: "in-process", Tools: []string{"tool1"}})
	r.RegisterServer("custom", MCPServerInfo{Name: "custom", Type: "workflow", Tools: []string{"tool2"}})

	r.UnregisterServer("wfctl")
	servers := r.ListServers()
	if len(servers) != 1 {
		t.Fatalf("expected 1 server after unregister, got %d", len(servers))
	}
	if servers[0].Name != "custom" {
		t.Errorf("expected custom, got %s", servers[0].Name)
	}
}

func TestMCPRegistryListAllTools(t *testing.T) {
	r := NewMCPRegistry()
	r.RegisterServer("wfctl", MCPServerInfo{
		Name:  "wfctl",
		Type:  "in-process",
		Tools: []string{"validate_config", "inspect_config"},
	})
	r.RegisterServer("custom", MCPServerInfo{
		Name:  "custom",
		Type:  "workflow",
		Tools: []string{"my_tool"},
	})
	tools := r.ListAllTools()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}
}

func TestMCPRegistryHandleServers(t *testing.T) {
	r := NewMCPRegistry()
	r.RegisterServer("wfctl", MCPServerInfo{
		Name:  "wfctl",
		Type:  "in-process",
		Tools: []string{"validate_config"},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/mcp/servers", nil)
	w := httptest.NewRecorder()
	r.HandleServers(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}

	var servers []MCPServerInfo
	if err := json.NewDecoder(w.Body).Decode(&servers); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(servers) != 1 {
		t.Errorf("expected 1 server, got %d", len(servers))
	}
}

func TestMCPRegistryHandleTools(t *testing.T) {
	r := NewMCPRegistry()
	r.RegisterServer("wfctl", MCPServerInfo{
		Name:  "wfctl",
		Type:  "in-process",
		Tools: []string{"validate_config", "inspect_config"},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/mcp/tools", nil)
	w := httptest.NewRecorder()
	r.HandleTools(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var tools []MCPToolInfo
	if err := json.NewDecoder(w.Body).Decode(&tools); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}
