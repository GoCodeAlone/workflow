package module

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
)

// MCPRegistryConfig configures the mcp.registry module.
type MCPRegistryConfig struct {
	LogOnInit      bool `yaml:"log_on_init" json:"log_on_init"`
	ExposeAdminAPI bool `yaml:"expose_admin_api" json:"expose_admin_api"`
	AuditToolCalls bool `yaml:"audit_tool_calls" json:"audit_tool_calls"`
}

// MCPServerInfo describes a registered MCP server.
type MCPServerInfo struct {
	Name  string   `json:"name"`
	Type  string   `json:"type"` // "in-process", "workflow", "external"
	Tools []string `json:"tools"`
}

// MCPToolInfo describes a tool along with its server origin.
type MCPToolInfo struct {
	Name       string `json:"name"`
	ServerName string `json:"server_name"`
	ServerType string `json:"server_type"`
}

// MCPRegistry tracks all MCP servers and tools for audit and admin purposes.
type MCPRegistry struct {
	mu      sync.RWMutex
	servers map[string]MCPServerInfo
	logger  *slog.Logger
}

// NewMCPRegistry creates a new, empty MCPRegistry.
func NewMCPRegistry() *MCPRegistry {
	return &MCPRegistry{
		servers: make(map[string]MCPServerInfo),
		logger:  slog.Default(),
	}
}

// RegisterServer adds or replaces an MCP server entry in the registry.
func (r *MCPRegistry) RegisterServer(name string, info MCPServerInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.servers[name] = info
	r.logger.Info("MCP server registered",
		"name", name,
		"type", info.Type,
		"tool_count", len(info.Tools),
	)
}

// UnregisterServer removes a server from the registry.
func (r *MCPRegistry) UnregisterServer(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.servers, name)
}

// ListServers returns a snapshot of all registered MCP servers.
func (r *MCPRegistry) ListServers() []MCPServerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]MCPServerInfo, 0, len(r.servers))
	for _, info := range r.servers {
		result = append(result, info)
	}
	return result
}

// ListAllTools returns all tools across all registered servers, annotated with their origin.
func (r *MCPRegistry) ListAllTools() []MCPToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var tools []MCPToolInfo
	for _, info := range r.servers {
		for _, toolName := range info.Tools {
			tools = append(tools, MCPToolInfo{
				Name:       toolName,
				ServerName: info.Name,
				ServerType: info.Type,
			})
		}
	}
	return tools
}

// HandleServers writes the registered MCP servers as JSON.
// Suitable for use as an http.HandlerFunc.
func (r *MCPRegistry) HandleServers(w http.ResponseWriter, _ *http.Request) {
	servers := r.ListServers()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(servers); err != nil {
		http.Error(w, "failed to encode servers", http.StatusInternalServerError)
	}
}

// HandleTools writes all tools across all registered servers as JSON.
// Suitable for use as an http.HandlerFunc.
func (r *MCPRegistry) HandleTools(w http.ResponseWriter, _ *http.Request) {
	tools := r.ListAllTools()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tools); err != nil {
		http.Error(w, "failed to encode tools", http.StatusInternalServerError)
	}
}
