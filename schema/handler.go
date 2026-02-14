package schema

import (
	"encoding/json"
	"net/http"
	"strings"
)

// RegisterRoutes registers the schema API endpoint on the given mux.
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/schema", HandleGetSchema)
}

// HandleGetSchema serves the workflow JSON schema.
func HandleGetSchema(w http.ResponseWriter, _ *http.Request) {
	s := GenerateWorkflowSchema()
	w.Header().Set("Content-Type", "application/schema+json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		http.Error(w, "failed to encode schema", http.StatusInternalServerError)
	}
}

// HandleSchemaAPI dispatches schema-related API requests. It handles:
//   - /api/schema            → workflow JSON schema
//   - /api/v1/module-schemas → module config schemas (all or by type)
func HandleSchemaAPI(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/module-schemas"):
		HandleGetModuleSchemas(w, r)
	default:
		HandleGetSchema(w, r)
	}
}

// moduleSchemaRegistry is the singleton registry used by the handler.
var moduleSchemaRegistry = NewModuleSchemaRegistry()

// GetModuleSchemaRegistry returns the global module schema registry,
// allowing callers to register additional schemas (e.g., custom module types).
func GetModuleSchemaRegistry() *ModuleSchemaRegistry {
	return moduleSchemaRegistry
}

// HandleGetModuleSchemas serves module config schemas.
// Query parameters:
//   - type: return schema for a specific module type (e.g. ?type=http.server)
//
// Without ?type, returns all schemas as a map keyed by module type.
func HandleGetModuleSchemas(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	moduleType := r.URL.Query().Get("type")
	if moduleType != "" {
		s := moduleSchemaRegistry.Get(moduleType)
		if s == nil {
			http.Error(w, `{"error":"unknown module type"}`, http.StatusNotFound)
			return
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(s); err != nil {
			http.Error(w, "failed to encode schema", http.StatusInternalServerError)
		}
		return
	}

	// Return all schemas as a map
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(moduleSchemaRegistry.AllMap()); err != nil {
		http.Error(w, "failed to encode schemas", http.StatusInternalServerError)
	}
}
