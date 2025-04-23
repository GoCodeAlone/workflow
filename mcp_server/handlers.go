package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/GoCodeAlone/modular"
)

// JSONRPCRequest defines the structure for incoming JSON-RPC requests
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"` // Can be string, number, or null
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse defines the structure for outgoing JSON-RPC responses
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError defines the structure for JSON-RPC errors
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCPTool defines a tool that can be executed via MCP commands
type MCPTool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Handler     func(params map[string]interface{}) (interface{}, error)
}

// ToolRegistry maintains the collection of available tools
type ToolRegistry struct {
	tools map[string]MCPTool
	mu    sync.RWMutex
}

// NewToolRegistry creates a new tool registry
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]MCPTool),
	}
}

// RegisterTool adds a tool to the registry
func (r *ToolRegistry) RegisterTool(tool MCPTool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.ID] = tool
}

// GetTool retrieves a tool by ID
func (r *ToolRegistry) GetTool(id string) (MCPTool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, exists := r.tools[id]
	return tool, exists
}

// ListTools returns all registered tools
func (r *ToolRegistry) ListTools() []MCPTool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]MCPTool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}

// globalToolRegistry is the application-wide tool registry
var globalToolRegistry = NewToolRegistry()

// RegisterDefaultTools populates the registry with standard modular and modcli tools
func RegisterDefaultTools() {
	// Module generation tools
	globalToolRegistry.RegisterTool(MCPTool{
		ID:          "modular.generate.module",
		Name:        "Generate Module",
		Description: "Generate a new module for the modular library",
		Category:    "Generation",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			moduleName, ok := params["moduleName"].(string)
			if !ok || moduleName == "" {
				return nil, fmt.Errorf("missing or invalid moduleName parameter")
			}

			path, _ := params["path"].(string)

			args := []string{"generate", "module", moduleName}
			if path != "" {
				args = append(args, "--path", path)
			}

			cmd := exec.Command("modcli", args...)
			output, err := cmd.CombinedOutput()

			result := map[string]interface{}{
				"command": strings.Join(cmd.Args, " "),
				"output":  string(output),
			}

			if err != nil {
				result["success"] = false
				result["error"] = err.Error()
				return result, err
			}

			result["success"] = true
			return result, nil
		},
	})

	globalToolRegistry.RegisterTool(MCPTool{
		ID:          "modular.generate.workflow",
		Name:        "Generate Workflow",
		Description: "Generate a new workflow configuration",
		Category:    "Generation",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			workflowName, ok := params["workflowName"].(string)
			if !ok || workflowName == "" {
				return nil, fmt.Errorf("missing or invalid workflowName parameter")
			}

			path, _ := params["path"].(string)

			args := []string{"generate", "workflow", workflowName}
			if path != "" {
				args = append(args, "--path", path)
			}

			cmd := exec.Command("modcli", args...)
			output, err := cmd.CombinedOutput()

			result := map[string]interface{}{
				"command": strings.Join(cmd.Args, " "),
				"output":  string(output),
			}

			if err != nil {
				result["success"] = false
				result["error"] = err.Error()
				return result, err
			}

			result["success"] = true
			return result, nil
		},
	})

	// Analysis tools
	globalToolRegistry.RegisterTool(MCPTool{
		ID:          "modular.analyze.config",
		Name:        "Analyze Configuration",
		Description: "Analyze a workflow configuration file for validation and optimization",
		Category:    "Analysis",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			configPath, ok := params["configPath"].(string)
			if !ok || configPath == "" {
				return nil, fmt.Errorf("missing or invalid configPath parameter")
			}

			args := []string{"analyze", "config", configPath}

			cmd := exec.Command("modcli", args...)
			output, err := cmd.CombinedOutput()

			result := map[string]interface{}{
				"command": strings.Join(cmd.Args, " "),
				"output":  string(output),
			}

			if err != nil {
				result["success"] = false
				result["error"] = err.Error()
				return result, err
			}

			result["success"] = true
			return result, nil
		},
	})

	// Documentation tools
	globalToolRegistry.RegisterTool(MCPTool{
		ID:          "modular.docs.modules",
		Name:        "List Available Modules",
		Description: "List all available module types with descriptions",
		Category:    "Documentation",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			modules := []map[string]string{
				{"id": "http.server", "name": "HTTP Server", "description": "Provides HTTP server capabilities"},
				{"id": "http.router", "name": "HTTP Router", "description": "Routes HTTP requests to handlers"},
				{"id": "http.handler", "name": "HTTP Handler", "description": "Handles HTTP requests"},
				{"id": "http.middleware", "name": "HTTP Middleware", "description": "Processes HTTP requests/responses"},
				{"id": "event.processor", "name": "Event Processor", "description": "Processes events from various sources"},
				{"id": "event.trigger", "name": "Event Trigger", "description": "Triggers workflows based on events"},
				{"id": "schedule.trigger", "name": "Schedule Trigger", "description": "Triggers workflows based on schedules"},
				{"id": "state.machine", "name": "State Machine", "description": "Manages workflow state transitions"},
				{"id": "messaging.broker", "name": "Messaging Broker", "description": "Handles message routing between modules"},
				{"id": "integration.connector", "name": "Integration Connector", "description": "Connects to external systems"},
				{"id": "service.registry", "name": "Service Registry", "description": "Manages service registration and discovery"},
			}

			return map[string]interface{}{
				"modules": modules,
			}, nil
		},
	})

	globalToolRegistry.RegisterTool(MCPTool{
		ID:          "modular.docs.examples",
		Name:        "List Example Workflows",
		Description: "List available example workflow configurations",
		Category:    "Documentation",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			examples := []map[string]string{
				{"id": "simple-workflow", "name": "Simple Workflow", "description": "Basic workflow with minimal components"},
				{"id": "api-gateway", "name": "API Gateway", "description": "HTTP API gateway pattern implementation"},
				{"id": "api-server", "name": "API Server", "description": "Full-featured API server with middleware"},
				{"id": "event-driven", "name": "Event-Driven Workflow", "description": "Event-based processing architecture"},
				{"id": "scheduled-jobs", "name": "Scheduled Jobs", "description": "Time-based job execution"},
				{"id": "state-machine", "name": "State Machine Workflow", "description": "Complex state transitions workflow"},
				{"id": "multi-workflow", "name": "Multi-Workflow", "description": "Multiple interacting workflows"},
				{"id": "dependency-injection", "name": "Dependency Injection", "description": "Service dependency management"},
				{"id": "data-pipeline", "name": "Data Pipeline", "description": "Data transformation pipeline"},
				{"id": "advanced-scheduler", "name": "Advanced Scheduler", "description": "Complex scheduling patterns"},
				{"id": "integration", "name": "Integration Workflow", "description": "External systems integration"},
			}

			return map[string]interface{}{
				"examples": examples,
			}, nil
		},
	})

	// Utility tools
	globalToolRegistry.RegisterTool(MCPTool{
		ID:          "modular.util.validateConfig",
		Name:        "Validate Configuration",
		Description: "Validate a workflow configuration file",
		Category:    "Utilities",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			configPath, ok := params["configPath"].(string)
			if !ok || configPath == "" {
				return nil, fmt.Errorf("missing or invalid configPath parameter")
			}

			args := []string{"validate", "config", configPath}

			cmd := exec.Command("modcli", args...)
			output, err := cmd.CombinedOutput()

			result := map[string]interface{}{
				"command": strings.Join(cmd.Args, " "),
				"output":  string(output),
			}

			if err != nil {
				result["success"] = false
				result["error"] = err.Error()
				return result, err
			}

			result["success"] = true
			return result, nil
		},
	})

	globalToolRegistry.RegisterTool(MCPTool{
		ID:          "modular.util.convertFormat",
		Name:        "Convert Configuration Format",
		Description: "Convert between YAML and JSON configuration formats",
		Category:    "Utilities",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			inputPath, ok := params["inputPath"].(string)
			if !ok || inputPath == "" {
				return nil, fmt.Errorf("missing or invalid inputPath parameter")
			}

			outputPath, ok := params["outputPath"].(string)
			if !ok || outputPath == "" {
				return nil, fmt.Errorf("missing or invalid outputPath parameter")
			}

			format, _ := params["format"].(string)
			if format != "yaml" && format != "json" {
				format = "yaml" // Default to YAML
			}

			args := []string{"convert", inputPath, outputPath, "--format", format}

			cmd := exec.Command("modcli", args...)
			output, err := cmd.CombinedOutput()

			result := map[string]interface{}{
				"command": strings.Join(cmd.Args, " "),
				"output":  string(output),
			}

			if err != nil {
				result["success"] = false
				result["error"] = err.Error()
				return result, err
			}

			result["success"] = true
			return result, nil
		},
	})
}

// RootHandler provides basic info at the root path and handles SSE/MCP communication.
type RootHandler struct {
	name string
	app  modular.Application // Store app instance
	log  modular.Logger      // Store logger instance as interface type
}

func NewRootHandler(name string) *RootHandler {
	return &RootHandler{name: name}
}

func (h *RootHandler) Name() string { return h.name }
func (h *RootHandler) Init(app modular.Application) error {
	h.app = app
	h.log = app.Logger()
	h.log.Info("RootHandler initialized", "handler", h.name)
	return nil
}
func (h *RootHandler) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{{Name: h.name, Instance: h}}
}
func (h *RootHandler) RequiresServices() []modular.ServiceDependency { return nil }

// sendJSONRPCResponse sends a JSON-RPC response back to the client.
func (h *RootHandler) sendJSONRPCResponse(w http.ResponseWriter, r *http.Request, resp JSONRPCResponse) {
	logger := h.log
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("sendJSONRPCResponse: Sending response", "handler", h.name, "id", resp.ID, "remoteAddr", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error("sendJSONRPCResponse: Error encoding JSON-RPC response", "handler", h.name, "id", resp.ID, "error", err, "remoteAddr", r.RemoteAddr)
	} else {
		logger.Info("sendJSONRPCResponse: Successfully sent response", "handler", h.name, "id", resp.ID, "remoteAddr", r.RemoteAddr)
	}
}

// sendJSONRPCError sends a JSON-RPC error response back to the client.
func (h *RootHandler) sendJSONRPCError(w http.ResponseWriter, r *http.Request, id interface{}, code int, message string, data interface{}) {
	logger := h.log
	if logger == nil {
		logger = slog.Default()
	}
	remoteAddr := "unknown"
	if r != nil {
		remoteAddr = r.RemoteAddr
	}
	logger.Error("sendJSONRPCError: Sending error", "handler", h.name, "id", id, "code", code, "message", message, "remoteAddr", remoteAddr)
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	httpStatus := http.StatusInternalServerError
	switch code {
	case -32700:
		httpStatus = http.StatusBadRequest // Parse error
	case -32600:
		httpStatus = http.StatusBadRequest // Invalid Request
	case -32601:
		httpStatus = http.StatusNotFound // Method not found
	case -32602:
		httpStatus = http.StatusBadRequest // Invalid params
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(httpStatus)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error("sendJSONRPCError: Error encoding JSON-RPC error response", "handler", h.name, "id", id, "error", err, "remoteAddr", remoteAddr)
	} else {
		logger.Info("sendJSONRPCError: Successfully sent error response", "handler", h.name, "id", id, "code", code, "remoteAddr", remoteAddr)
	}
}

// handleMCPMethod handles MCP-specific JSON-RPC methods
func (h *RootHandler) handleMCPMethod(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	h.log.Info("handleMCPMethod entered", "handler", h.name, "method", req.Method, "id", req.ID, "remoteAddr", r.RemoteAddr)

	switch req.Method {
	case "mcp.getServerStatus":
		// Return server status information
		h.sendJSONRPCResponse(w, r, JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"status":     "running",
				"version":    "1.0.0",
				"uptime":     "N/A", // Could track actual uptime
				"serverTime": time.Now().Format(time.RFC3339),
			},
		})
		return

	case "mcp.echo":
		// Simple echo method for testing
		h.sendJSONRPCResponse(w, r, JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  req.Params,
		})
		return

	case "mcp.listTools":
		// List all available tools
		tools := globalToolRegistry.ListTools()

		// Convert tools to a format suitable for JSON response
		// (excluding the Handler function which can't be serialized)
		toolsData := make([]map[string]string, 0, len(tools))
		for _, tool := range tools {
			toolsData = append(toolsData, map[string]string{
				"id":          tool.ID,
				"name":        tool.Name,
				"description": tool.Description,
				"category":    tool.Category,
			})
		}

		h.sendJSONRPCResponse(w, r, JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"tools": toolsData,
			},
		})
		return

	case "mcp.executeTool":
		h.executeToolHandler(w, r, req)
		return

	case "mcp.getToolInfo":
		// Get detailed information about a specific tool
		var params struct {
			ToolID string `json:"toolId"`
		}

		paramsData, err := json.Marshal(req.Params)
		if err != nil {
			h.sendJSONRPCError(w, r, req.ID, -32602, "Invalid params", "Could not process parameters")
			return
		}

		if err := json.Unmarshal(paramsData, &params); err != nil {
			h.sendJSONRPCError(w, r, req.ID, -32602, "Invalid params", "Could not parse parameters")
			return
		}

		if params.ToolID == "" {
			h.sendJSONRPCError(w, r, req.ID, -32602, "Invalid params", "Missing toolId parameter")
			return
		}

		tool, exists := globalToolRegistry.GetTool(params.ToolID)
		if !exists {
			h.sendJSONRPCError(w, r, req.ID, -32601, "Tool not found", fmt.Sprintf("Tool with ID '%s' not found", params.ToolID))
			return
		}

		h.sendJSONRPCResponse(w, r, JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]string{
				"id":          tool.ID,
				"name":        tool.Name,
				"description": tool.Description,
				"category":    tool.Category,
			},
		})
		return

	default:
		h.log.Warn("Unknown MCP method", "handler", h.name, "method", req.Method)
		h.sendJSONRPCError(w, r, req.ID, -32601, "Method not found", fmt.Sprintf("Method '%s' not implemented", req.Method))
	}
}

// executeToolHandler handles tool execution requests
func (h *RootHandler) executeToolHandler(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	h.log.Info("executeToolHandler entered", "handler", h.name, "id", req.ID)

	var params struct {
		ToolID string                 `json:"toolId"`
		Params map[string]interface{} `json:"params"`
	}

	paramsData, err := json.Marshal(req.Params)
	if err != nil {
		h.sendJSONRPCError(w, r, req.ID, -32602, "Invalid params", "Could not process parameters")
		return
	}

	if err := json.Unmarshal(paramsData, &params); err != nil {
		h.sendJSONRPCError(w, r, req.ID, -32602, "Invalid params", "Could not parse parameters")
		return
	}

	if params.ToolID == "" {
		h.sendJSONRPCError(w, r, req.ID, -32602, "Invalid params", "Missing toolId parameter")
		return
	}

	h.log.Info("Looking up tool", "handler", h.name, "toolId", params.ToolID)
	tool, exists := globalToolRegistry.GetTool(params.ToolID)
	if !exists {
		h.sendJSONRPCError(w, r, req.ID, -32601, "Tool not found", fmt.Sprintf("Tool with ID '%s' not found", params.ToolID))
		return
	}

	// Create params map if none was provided
	if params.Params == nil {
		params.Params = make(map[string]interface{})
	}

	h.log.Info("Executing tool", "handler", h.name, "toolId", params.ToolID, "params", params.Params)
	result, err := tool.Handler(params.Params)
	if err != nil {
		h.log.Error("Tool execution failed", "handler", h.name, "toolId", params.ToolID, "error", err)
		h.sendJSONRPCError(w, r, req.ID, -32603, "Tool execution failed", err.Error())
		return
	}

	h.log.Info("Tool execution completed successfully", "handler", h.name, "toolId", params.ToolID)
	h.sendJSONRPCResponse(w, r, JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	})
}

// Handle processes incoming HTTP requests and routes them to the appropriate handler.
func (h *RootHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.log.Info("Request received", "handler", h.name, "method", r.Method, "path", r.URL.Path, "remoteAddr", r.RemoteAddr)

	// Log all headers for debugging
	h.log.Debug("Request headers:", "handler", h.name)
	for name, values := range r.Header {
		h.log.Debug("Header", "name", name, "values", strings.Join(values, ", "))
	}

	// Check for WebSocket upgrade request
	if websocket := r.Header.Get("Upgrade"); websocket == "websocket" {
		h.log.Info("WebSocket upgrade request detected", "handler", h.name, "remoteAddr", r.RemoteAddr)
		h.handleWebSocket(w, r)
		return
	}

	// Handle preflight OPTIONS requests for CORS
	if r.Method == http.MethodOptions {
		h.log.Info("Handling OPTIONS preflight request", "handler", h.name, "path", r.URL.Path, "remoteAddr", r.RemoteAddr)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")
		w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours
		w.WriteHeader(http.StatusOK)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleSSE(w, r)
	case http.MethodPost:
		// Check if this might be a Language Server Protocol request
		contentType := r.Header.Get("Content-Type")
		if contentType == "application/json" || contentType == "application/vscode-jsonrpc" || contentType == "" {
			h.log.Info("Handling potential LSP JSON-RPC request", "handler", h.name, "contentType", contentType)
			h.handleJSONRPC(w, r)
		} else {
			h.log.Warn("Unrecognized POST content type", "handler", h.name, "contentType", contentType)
			http.Error(w, "Unsupported Media Type", http.StatusUnsupportedMediaType)
		}
	default:
		h.log.Warn("Method not allowed", "handler", h.name, "method", r.Method, "path", r.URL.Path)
		w.Header().Set("Allow", "GET, POST, OPTIONS")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// handleSSE establishes and maintains the Server-Sent Events connection.
func (h *RootHandler) handleSSE(w http.ResponseWriter, r *http.Request) {
	h.log.Info("handleSSE entered", "handler", h.name, "remoteAddr", r.RemoteAddr)
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// Send initial connected event
	h.log.Info("handleSSE: Sending mcp.connected event", "handler", h.name, "remoteAddr", r.RemoteAddr)
	fmt.Fprintf(w, "event: mcp.connected\ndata: {\"message\": \"MCP Server Connected\"}\n\n")
	flusher.Flush()

	// Get the list of available tools for capabilities
	tools := globalToolRegistry.ListTools()
	h.log.Info("handleSSE: Number of tools available", "count", len(tools))

	// Format tool info for client
	toolsData := make([]map[string]string, 0, len(tools))
	for i, tool := range tools {
		h.log.Debug("Tool for client", "index", i, "id", tool.ID, "name", tool.Name)
		toolsData = append(toolsData, map[string]string{
			"id":          tool.ID,
			"name":        tool.Name,
			"description": tool.Description,
			"category":    tool.Category,
		})
	}

	// Send tool discovery event in multiple formats to ensure client compatibility
	h.log.Info("handleSSE: Sending tools discovered event", "handler", h.name, "toolCount", len(tools))

	// Format 1: Send as a standard event with the specific event name the VS Code extension is looking for
	toolsMessage := map[string]interface{}{
		"tools": toolsData,
	}
	toolsJSON1, _ := json.Marshal(toolsMessage)
	fmt.Fprintf(w, "event: mcp.toolsDiscovered\ndata: %s\n\n", string(toolsJSON1))
	flusher.Flush()

	// Format 2: Send as a JSON-RPC notification via the message event
	toolsMessage2 := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "mcp.toolsDiscovered",
		"params": map[string]interface{}{
			"tools": toolsData,
		},
	}
	toolsJSON2, _ := json.Marshal(toolsMessage2)
	fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(toolsJSON2))
	flusher.Flush()

	// Format 3: Send with explicit VS Code notification event type
	fmt.Fprintf(w, "event: vscode.notification\ndata: %s\n\n", string(toolsJSON2))
	flusher.Flush()

	// Format 4: Send as custom tools event (some clients might look for this)
	fmt.Fprintf(w, "event: tools\ndata: %s\n\n", string(toolsJSON1))
	flusher.Flush()

	// Send automatic initialization response for VS Code MCP client
	h.log.Info("handleSSE: Sending automatic MCP initialization response", "handler", h.name, "remoteAddr", r.RemoteAddr)

	// Format as a JSON-RPC message that the VS Code client expects
	toolCommands := make([]string, 0, len(tools))
	for _, tool := range tools {
		toolCommands = append(toolCommands, tool.ID)
	}

	initResponse := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"result": map[string]interface{}{
			"capabilities": map[string]interface{}{
				"textDocumentSync": 1,
				"completionProvider": map[string]interface{}{
					"resolveProvider":   true,
					"triggerCharacters": []string{".", ",", "("},
				},
				"codeActionProvider": true,
				"executeCommandProvider": map[string]interface{}{
					"commands": toolCommands,
				},
				"workspace": map[string]interface{}{
					"workspaceFolders": map[string]interface{}{
						"supported":           true,
						"changeNotifications": true,
					},
				},
			},
			"serverInfo": map[string]string{
				"name":    "MCP Server",
				"version": "1.0.0",
			},
		},
	}

	// Marshal the response to JSON
	initJSON, _ := json.Marshal(initResponse)

	// Send as a message event that the VS Code extension is likely expecting
	fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(initJSON))
	flusher.Flush()

	// Also send another event as the older format in case that's what it needs
	fmt.Fprintf(w, "event: initialized\ndata: {\"status\": \"server_initialized\", \"tools\": %s}\n\n",
		string(toolsJSON1))
	flusher.Flush()

	// Send ready notification with tools embedded
	readyMessage := map[string]interface{}{
		"status": "ready",
		"tools":  toolsData,
	}
	readyJSON, _ := json.Marshal(readyMessage)
	fmt.Fprintf(w, "event: ready\ndata: %s\n\n", string(readyJSON))
	flusher.Flush()

	ctx := r.Context()
	h.log.Info("handleSSE: Entering keep-alive loop", "handler", h.name, "remoteAddr", r.RemoteAddr)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.log.Info("handleSSE: Client disconnected", "handler", h.name, "remoteAddr", r.RemoteAddr)
			return
		case <-ticker.C:
			// Send keep-alive comment
			fmt.Fprintf(w, ": keep-alive\n\n")
			// Also periodically send a status update to ensure the client knows the server is still running
			statusMessage := map[string]interface{}{
				"status":    "running",
				"timestamp": time.Now().Format(time.RFC3339),
				"toolCount": len(tools),
			}
			statusJSON, _ := json.Marshal(statusMessage)
			fmt.Fprintf(w, "event: status\ndata: %s\n\n", string(statusJSON))
			flusher.Flush()

			// Periodically re-send tools to ensure they're discovered
			// (but less frequently to avoid flooding the connection)
			if time.Now().Second()%30 == 0 {
				h.log.Debug("handleSSE: Periodic tools reminder", "handler", h.name)
				fmt.Fprintf(w, "event: mcp.toolsDiscovered\ndata: %s\n\n", string(toolsJSON1))
				flusher.Flush()
			}
		}
	}
}

// handleWebSocket handles WebSocket connections for the Language Server Protocol
func (h *RootHandler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	h.log.Info("handleWebSocket entered", "handler", h.name, "remoteAddr", r.RemoteAddr)

	// Since we don't have a WebSocket library imported yet, we'll return a "not implemented" response
	// In a production app, you would use a library like gorilla/websocket to handle the WebSocket connection
	h.log.Warn("WebSocket support not fully implemented", "handler", h.name)
	http.Error(w, "WebSocket support not implemented. Use HTTP POST for JSON-RPC instead.", http.StatusNotImplemented)
}

// handleJSONRPC processes incoming JSON-RPC requests from the client.
func (h *RootHandler) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	h.log.Info("handleJSONRPC entered", "handler", h.name, "remoteAddr", r.RemoteAddr)
	h.log.Debug("handleJSONRPC request details", "handler", h.name, "method", r.Method, "path", r.URL.Path,
		"contentType", r.Header.Get("Content-Type"), "contentLength", r.ContentLength)

	// Check for VS Code client
	isVSCodeClient := false
	for name, values := range r.Header {
		h.log.Debug("handleJSONRPC request header", "handler", h.name, "name", name, "values", strings.Join(values, ", "))
		if strings.ToLower(name) == "user-agent" && strings.Contains(strings.ToLower(strings.Join(values, "")), "vscode") {
			isVSCodeClient = true
		}
	}

	if isVSCodeClient {
		h.log.Info("VS Code client detected", "handler", h.name)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.log.Error("handleJSONRPC: Error reading request body", "handler", h.name, "error", err, "remoteAddr", r.RemoteAddr)
		h.sendJSONRPCError(w, r, nil, -32700, "Parse error", fmt.Sprintf("Error reading request body: %v", err))
		return
	}
	defer r.Body.Close()

	// If body is empty, VS Code might be doing a connection test
	if len(body) == 0 {
		h.log.Info("handleJSONRPC: Empty request body, possible connection test", "handler", h.name, "remoteAddr", r.RemoteAddr)
		// For empty requests, just send a simple success response
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "{}")
		return
	}

	h.log.Info("handleJSONRPC: Received raw body", "handler", h.name, "bodySize", len(body), "remoteAddr", r.RemoteAddr)
	// Log body contents for debugging, but limit size for large requests
	if len(body) < 1000 {
		h.log.Debug("handleJSONRPC: Request body", "handler", h.name, "body", string(body))
	} else {
		h.log.Debug("handleJSONRPC: Request body (truncated)", "handler", h.name, "body", string(body[:1000])+"...")
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.log.Error("handleJSONRPC: Error parsing JSON request", "handler", h.name, "error", err, "remoteAddr", r.RemoteAddr)

		// Try to handle non-standard JSON-RPC format that some clients might use
		if isVSCodeClient {
			h.log.Info("Attempting to handle non-standard VS Code request format", "handler", h.name)

			// Special handling for VS Code LSP client
			// If we can detect "initialize" in the body, send a basic response
			if strings.Contains(string(body), "initialize") {
				h.log.Info("Detected initialize request in non-standard format", "handler", h.name)

				// Respond with a basic initialization response
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{
					"jsonrpc": "2.0",
					"id": 1,
					"result": {
						"capabilities": {
							"textDocumentSync": 1,
							"completionProvider": {
								"resolveProvider": true,
								"triggerCharacters": [".", ",", "("]
							},
							"codeActionProvider": true,
							"executeCommandProvider": {
								"commands": ["mcp.command.generateModule", "mcp.command.listModules"]
							}
						},
						"serverInfo": {
							"name": "MCP Server",
							"version": "1.0.0"
						}
					}
				}`)
				return
			}
		}

		h.sendJSONRPCError(w, r, nil, -32700, "Parse error", fmt.Sprintf("Error parsing JSON request: %v", err))
		return
	}
	h.log.Info("handleJSONRPC: Parsed request", "handler", h.name, "id", req.ID, "method", req.Method, "remoteAddr", r.RemoteAddr)

	if req.Method == "initialize" {
		h.log.Info("handleJSONRPC: Handling 'initialize' request", "handler", h.name, "id", req.ID, "remoteAddr", r.RemoteAddr)

		// Log the params for debugging
		paramsJSON, _ := json.Marshal(req.Params)
		h.log.Debug("handleJSONRPC: initialize params", "handler", h.name, "params", string(paramsJSON))

		// Get the list of available tools for capabilities
		tools := globalToolRegistry.ListTools()
		toolCommands := make([]string, 0, len(tools))
		for _, tool := range tools {
			toolCommands = append(toolCommands, tool.ID)
		}

		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"capabilities": map[string]interface{}{
					"textDocumentSync": 1,
					"completionProvider": map[string]interface{}{
						"resolveProvider":   true,
						"triggerCharacters": []string{".", ",", "("},
					},
					"codeActionProvider": true,
					"executeCommandProvider": map[string]interface{}{
						"commands": toolCommands,
					},
					"workspace": map[string]interface{}{
						"workspaceFolders": map[string]interface{}{
							"supported":           true,
							"changeNotifications": true,
						},
					},
				},
				"serverInfo": map[string]string{
					"name":    "MCP Server",
					"version": "1.0.0",
				},
			},
		}
		h.log.Info("handleJSONRPC: Sending 'initialize' response", "handler", h.name, "id", req.ID, "remoteAddr", r.RemoteAddr)

		// Log the response for debugging
		responseJSON, _ := json.Marshal(resp)
		h.log.Debug("handleJSONRPC: initialize response", "handler", h.name, "response", string(responseJSON))

		h.sendJSONRPCResponse(w, r, resp)

		// Immediately after responding to initialize, send a notification with tools
		// This is a custom notification that VS Code MCP extensions might be looking for
		h.log.Info("handleJSONRPC: Sending tools notification after initialize", "handler", h.name)

		// Send the toolsDiscovered notification directly
		toolsData := make([]map[string]string, 0, len(tools))
		for _, tool := range tools {
			toolsData = append(toolsData, map[string]string{
				"id":          tool.ID,
				"name":        tool.Name,
				"description": tool.Description,
				"category":    tool.Category,
			})
		}

		toolsNotification := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "mcp.toolsDiscovered",
			"params": map[string]interface{}{
				"tools": toolsData,
			},
		}

		toolsJSON, _ := json.Marshal(toolsNotification)
		h.log.Debug("Tools notification JSON", "json", string(toolsJSON))

		// Set headers for notification
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, string(toolsJSON))

		return
	} else if req.Method == "initialized" {
		// The client sends this after initialization is complete
		h.log.Info("handleJSONRPC: Received 'initialized' notification", "handler", h.name, "remoteAddr", r.RemoteAddr)

		// Send tools message after initialized notification
		tools := globalToolRegistry.ListTools()
		toolsData := make([]map[string]string, 0, len(tools))
		for _, tool := range tools {
			toolsData = append(toolsData, map[string]string{
				"id":          tool.ID,
				"name":        tool.Name,
				"description": tool.Description,
				"category":    tool.Category,
			})
		}

		toolsMessage := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "mcp.toolsDiscovered",
			"params": map[string]interface{}{
				"tools": toolsData,
			},
		}

		h.log.Info("handleJSONRPC: Sending tools notification after initialized", "handler", h.name, "toolCount", len(tools))
		toolsJSON, _ := json.Marshal(toolsMessage)

		// Need to respond to initialized and also send notification
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, string(toolsJSON))
		return
	} else if req.Method == "mcp.getTools" || req.Method == "workspace/executeCommand" && strings.Contains(strings.ToLower(string(body)), "gettools") {
		// Handle explicit tools request
		h.log.Info("handleJSONRPC: Handling tools discovery request", "handler", h.name, "id", req.ID, "method", req.Method)

		// Get all tools
		tools := globalToolRegistry.ListTools()
		toolsData := make([]map[string]string, 0, len(tools))
		for _, tool := range tools {
			toolsData = append(toolsData, map[string]string{
				"id":          tool.ID,
				"name":        tool.Name,
				"description": tool.Description,
				"category":    tool.Category,
			})
		}

		// Send response
		h.sendJSONRPCResponse(w, r, JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"tools": toolsData,
			},
		})
		return
	} else if req.Method == "exit" {
		// Handle exit request - don't actually exit but log it
		h.log.Info("handleJSONRPC: Received 'exit' request", "handler", h.name, "id", req.ID, "remoteAddr", r.RemoteAddr)
		h.sendJSONRPCResponse(w, r, JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  nil,
		})
		return
	} else if req.Method == "shutdown" {
		// Handle shutdown request (LSP protocol)
		h.log.Info("handleJSONRPC: Received 'shutdown' request", "handler", h.name, "id", req.ID, "remoteAddr", r.RemoteAddr)
		h.sendJSONRPCResponse(w, r, JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  nil,
		})
		return
	} else if req.Method == "workspace/executeCommand" {
		// Handle custom command execution
		h.executeToolHandler(w, r, req)
		return
	} else if strings.HasPrefix(req.Method, "mcp.") {
		// Handle any MCP-specific methods
		h.handleMCPMethod(w, r, req)
		return
	}

	h.log.Warn("handleJSONRPC: Method not found", "handler", h.name, "method", req.Method, "id", req.ID, "remoteAddr", r.RemoteAddr)
	h.sendJSONRPCError(w, r, req.ID, -32601, "Method not found", fmt.Sprintf("Method '%s' not implemented", req.Method))
}

// DocsHandler provides information about the Modular library.
type DocsHandler struct {
	name string
	app  modular.Application
	log  modular.Logger
}

// NewDocsHandler creates and returns a new DocsHandler instance
func NewDocsHandler(name string) *DocsHandler {
	return &DocsHandler{name: name}
}

func (h *DocsHandler) Name() string { return h.name }
func (h *DocsHandler) Init(app modular.Application) error {
	h.app = app
	h.log = app.Logger()
	h.log.Info("DocsHandler initialized", "handler", h.name)
	return nil
}
func (h *DocsHandler) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{{Name: h.name, Instance: h}}
}
func (h *DocsHandler) RequiresServices() []modular.ServiceDependency { return nil }

func (h *DocsHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.log.Info("DocsHandler request received", "handler", h.name, "method", r.Method, "path", r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"message":             "Modular Library Documentation and Tools",
		"documentation_url":   "https://github.com/GoCodeAlone/modular",
		"generation_endpoint": "/generate (POST)",
		"api_version":         "1.0.0",
		"endpoints": []map[string]string{
			{"method": "GET", "path": "/", "description": "Root endpoint for SSE and JSON-RPC communication"},
			{"method": "POST", "path": "/", "description": "JSON-RPC endpoint for MCP commands"},
			{"method": "GET", "path": "/docs", "description": "Documentation endpoint (this page)"},
			{"method": "POST", "path": "/generate", "description": "Module generation endpoint"},
		},
		"available_tools": len(globalToolRegistry.ListTools()),
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.log.Error("Failed to encode docs response", "handler", h.name, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// GenerateHandler processes module generation requests
type GenerateHandler struct {
	name string
	app  modular.Application
	log  modular.Logger
}

// NewGenerateHandler creates and returns a new GenerateHandler instance
func NewGenerateHandler(name string) *GenerateHandler {
	return &GenerateHandler{name: name}
}

func (h *GenerateHandler) Name() string { return h.name }
func (h *GenerateHandler) Init(app modular.Application) error {
	h.app = app
	h.log = app.Logger()
	h.log.Info("GenerateHandler initialized", "handler", h.name)
	return nil
}
func (h *GenerateHandler) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{{Name: h.name, Instance: h}}
}
func (h *GenerateHandler) RequiresServices() []modular.ServiceDependency { return nil }

func (h *GenerateHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.log.Info("GenerateHandler request received", "handler", h.name, "method", r.Method, "path", r.URL.Path)

	// Only accept POST requests
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the request body
	var req struct {
		ModuleName string `json:"module_name"`
		Path       string `json:"path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.log.Error("Failed to decode request body", "handler", h.name, "error", err)
		http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.ModuleName == "" {
		http.Error(w, "Bad Request: module_name is required", http.StatusBadRequest)
		return
	}

	h.log.Info("Generating module", "handler", h.name, "moduleName", req.ModuleName, "path", req.Path)

	// Execute the module generation command
	args := []string{"generate", "module", req.ModuleName}
	if req.Path != "" {
		args = append(args, "--path", req.Path)
	}

	cmd := exec.Command("modcli", args...)
	output, err := cmd.CombinedOutput()

	// Create response with command output
	response := map[string]interface{}{
		"command": strings.Join(cmd.Args, " "),
		"output":  string(output),
		"success": err == nil,
	}

	if err != nil {
		h.log.Error("Module generation failed", "handler", h.name, "error", err, "output", string(output))
		response["error"] = err.Error()
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		h.log.Info("Module generation successful", "handler", h.name, "moduleName", req.ModuleName)
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.log.Error("Failed to encode response", "handler", h.name, "error", err)
	}
}
