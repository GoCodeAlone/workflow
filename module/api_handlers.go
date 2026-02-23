package module

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/CrisisTextLine/modular"
)

// RESTResource represents a simple in-memory resource store for REST APIs
type RESTResource struct {
	ID         string         `json:"id"`
	Data       map[string]any `json:"data"`
	State      string         `json:"state,omitempty"`
	LastUpdate string         `json:"lastUpdate,omitempty"`
}

// WorkflowConfig holds the six workflow-related settings for a RESTAPIHandler.
// These fields are always configured together and are extracted here for clarity.
type WorkflowConfig struct {
	Type              string // The type of workflow to use (e.g., "order-workflow")
	Engine            string // The name of the workflow engine service to use
	InitialTransition string // The first transition to trigger after creating a workflow instance (defaults to "start_validation")
	InstanceIDPrefix  string // Optional prefix for workflow instance IDs
	InstanceIDField   string // Field in resource data to use for instance ID (defaults to "id")
	SeedFile          string // Path to JSON seed data file
}

// RESTAPIHandler provides CRUD operations for a REST API
type RESTAPIHandler struct {
	name         string
	resourceName string
	resources    map[string]RESTResource
	mu           sync.RWMutex
	logger       modular.Logger
	app          modular.Application
	persistence  *PersistenceStore // optional write-through backend

	WorkflowConfig

	// View/aggregation fields (e.g., a read-only handler over another collection)
	sourceResourceName string // Read from a different resource's persistence data (defaults to resourceName)
	stateFilter        string // Only include resources matching this state in GET responses

	// Dynamic field mapping (configurable via YAML, defaults match existing behavior)
	fieldMapping  *FieldMapping     // Maps logical field names to actual field names with fallback chains
	transitionMap map[string]string // Maps sub-action names to state machine transition names
	summaryFields []string          // Fields to include in summary sub-resource responses
}

// RESTAPIHandlerConfig contains configuration for a REST API handler
type RESTAPIHandlerConfig struct {
	ResourceName     string `json:"resourceName" yaml:"resourceName"`
	PublishEvents    bool   `json:"publishEvents" yaml:"publishEvents"`
	WorkflowType     string `json:"workflowType" yaml:"workflowType"`         // The type of workflow to use for state machine operations
	WorkflowEngine   string `json:"workflowEngine" yaml:"workflowEngine"`     // The name of the workflow engine to use
	InstanceIDPrefix string `json:"instanceIDPrefix" yaml:"instanceIDPrefix"` // Optional prefix for workflow instance IDs
	InstanceIDField  string `json:"instanceIDField" yaml:"instanceIDField"`   // Field in resource data to use for instance ID (defaults to "id")
}

// NewRESTAPIHandler creates a new REST API handler
func NewRESTAPIHandler(name, resourceName string) *RESTAPIHandler {
	h := &RESTAPIHandler{
		name:         name,
		resourceName: resourceName,
		resources:    make(map[string]RESTResource),
	}
	h.initFieldDefaults()
	return h
}

// SetWorkflowType sets the workflow type for state machine operations.
func (h *RESTAPIHandler) SetWorkflowType(wt string) {
	h.Type = wt
}

// SetWorkflowEngine sets the name of the workflow engine service to use.
func (h *RESTAPIHandler) SetWorkflowEngine(we string) {
	h.Engine = we
}

// SetInitialTransition sets the first transition to trigger after creating a workflow instance.
func (h *RESTAPIHandler) SetInitialTransition(t string) {
	h.InitialTransition = t
}

// SetInstanceIDPrefix sets the prefix used to build state machine instance IDs.
func (h *RESTAPIHandler) SetInstanceIDPrefix(prefix string) {
	h.InstanceIDPrefix = prefix
}

// SetSeedFile sets the path to a JSON seed data file.
func (h *RESTAPIHandler) SetSeedFile(path string) {
	h.SeedFile = path
}

// SetSourceResourceName sets a different resource name for read operations (e.g., queue reads from conversations).
func (h *RESTAPIHandler) SetSourceResourceName(name string) {
	h.sourceResourceName = name
}

// SetStateFilter restricts GET responses to resources matching the given state.
func (h *RESTAPIHandler) SetStateFilter(state string) {
	h.stateFilter = state
}

// SetFieldMapping sets a custom field mapping, merged on top of defaults.
func (h *RESTAPIHandler) SetFieldMapping(fm *FieldMapping) {
	h.fieldMapping = fm
}

// SetTransitionMap sets a custom sub-action to transition name mapping.
func (h *RESTAPIHandler) SetTransitionMap(tm map[string]string) {
	h.transitionMap = tm
}

// SetSummaryFields sets the list of fields to include in summary responses.
func (h *RESTAPIHandler) SetSummaryFields(fields []string) {
	h.summaryFields = fields
}

// initFieldDefaults initializes fieldMapping, transitionMap, and summaryFields
// with default values if not already set.
func (h *RESTAPIHandler) initFieldDefaults() {
	if h.fieldMapping == nil {
		h.fieldMapping = DefaultRESTFieldMapping()
	}
	if h.transitionMap == nil {
		h.transitionMap = DefaultTransitionMap()
	}
	if h.summaryFields == nil {
		h.summaryFields = DefaultSummaryFields()
	}
}

// Name returns the unique identifier for this module
func (h *RESTAPIHandler) Name() string {
	return h.name
}

// Constructor returns a function to construct this module with dependencies
func (h *RESTAPIHandler) Constructor() modular.ModuleConstructor {
	return func(app modular.Application, services map[string]any) (modular.Module, error) {
		// Create a new instance with the same name and workflow config
		handler := NewRESTAPIHandler(h.name, h.resourceName)
		handler.app = app
		handler.logger = app.Logger()
		handler.WorkflowConfig = h.WorkflowConfig
		handler.sourceResourceName = h.sourceResourceName
		handler.stateFilter = h.stateFilter
		handler.fieldMapping = h.fieldMapping
		handler.transitionMap = h.transitionMap
		handler.summaryFields = h.summaryFields

		// Look for persistence store (optional)
		if ps, ok := services["persistence"]; ok {
			if store, ok := ps.(*PersistenceStore); ok {
				handler.persistence = store
			}
		}

		return handler, nil
	}
}

// Init initializes the module with the application context
func (h *RESTAPIHandler) Init(app modular.Application) error {
	h.app = app
	h.logger = app.Logger()

	// Default values for workflow configuration
	h.InstanceIDField = "id" // Default to using "id" field if not specified
	h.initFieldDefaults()

	// Get configuration if available
	configSection, err := app.GetConfigSection("workflow")
	if err == nil && configSection != nil {
		if config := configSection.GetConfig(); config != nil {
			// Try to extract our module's configuration
			// This is a bit verbose but handles nested module configurations
			if modules, ok := config.(map[string]any)["modules"].([]any); ok {
				for _, mod := range modules {
					if m, ok := mod.(map[string]any); ok {
						if m["name"] == h.name {
							if cfg, ok := m["config"].(map[string]any); ok {
								// Extract resource name
								if rn, ok := cfg["resourceName"].(string); ok && rn != "" {
									h.resourceName = rn
								}

								// Extract workflow type
								if wt, ok := cfg["workflowType"].(string); ok && wt != "" {
									h.Type = wt
								}

								// Extract workflow engine
								if we, ok := cfg["workflowEngine"].(string); ok && we != "" {
									h.Engine = we
								}

								// Extract instance ID prefix
								if prefix, ok := cfg["instanceIDPrefix"].(string); ok {
									h.InstanceIDPrefix = prefix
								}

								// Extract instance ID field
								if field, ok := cfg["instanceIDField"].(string); ok && field != "" {
									h.InstanceIDField = field
								}

								// Extract source resource name (for view handlers like queue)
								if src, ok := cfg["sourceResourceName"].(string); ok && src != "" {
									h.sourceResourceName = src
								}

								// Extract state filter (for view handlers like queue)
								if sf, ok := cfg["stateFilter"].(string); ok && sf != "" {
									h.stateFilter = sf
								}

								// Extract dynamic field mapping (merged on top of defaults)
								if fmCfg, ok := cfg["fieldMapping"].(map[string]any); ok {
									override := FieldMappingFromConfig(fmCfg)
									h.fieldMapping.Merge(override)
								}

								// Extract custom transition map (merged on top of defaults)
								if tmCfg, ok := cfg["transitionMap"].(map[string]any); ok {
									for action, trans := range tmCfg {
										if t, ok := trans.(string); ok {
											h.transitionMap[action] = t
										}
									}
								}

								// Extract custom summary fields (replaces defaults)
								if sfCfg, ok := cfg["summaryFields"].([]any); ok {
									fields := make([]string, 0, len(sfCfg))
									for _, f := range sfCfg {
										if s, ok := f.(string); ok {
											fields = append(fields, s)
										}
									}
									if len(fields) > 0 {
										h.summaryFields = fields
									}
								}
							}
						}
					}
				}
			}

			// If workflowType is not set but we have a state machine configuration,
			// try to extract the default workflow type from there
			if h.Type == "" {
				if statemachine, ok := config.(map[string]any)["workflows"].(map[string]any)["statemachine"]; ok {
					if smConfig, ok := statemachine.(map[string]any); ok {
						if defs, ok := smConfig["definitions"].([]any); ok && len(defs) > 0 {
							if def, ok := defs[0].(map[string]any); ok {
								if name, ok := def["name"].(string); ok && name != "" {
									h.Type = name
									h.logger.Info(fmt.Sprintf("Using default workflow type from state machine definition: %s", h.Type))
								}
							}
						}
					}
				}
			}

			// If workflow engine is not set but we have a state machine configuration,
			// try to extract the engine name from there
			if h.Engine == "" {
				if statemachine, ok := config.(map[string]any)["workflows"].(map[string]any)["statemachine"]; ok {
					if smConfig, ok := statemachine.(map[string]any); ok {
						if engine, ok := smConfig["engine"].(string); ok && engine != "" {
							h.Engine = engine
							h.logger.Info(fmt.Sprintf("Using state machine engine from configuration: %s", h.Engine))
						}
					}
				}
			}
		}
	}

	// Wire persistence (optional)
	if h.persistence == nil {
		var ps any
		if err := app.GetService("persistence", &ps); err == nil && ps != nil {
			if store, ok := ps.(*PersistenceStore); ok {
				h.persistence = store
			}
		}
	}

	// Log workflow configuration
	if h.Type != "" {
		h.logger.Info(fmt.Sprintf("REST API handler '%s' configured with workflow type: %s", h.name, h.Type))
		if h.Engine != "" {
			h.logger.Info(fmt.Sprintf("Using workflow engine: %s", h.Engine))
		}
		if h.InstanceIDPrefix != "" {
			h.logger.Info(fmt.Sprintf("Using instance ID prefix: %s", h.InstanceIDPrefix))
		}
		h.logger.Info(fmt.Sprintf("Using instance ID field: %s", h.InstanceIDField))
	}

	return nil
}

// Handle implements the HTTPHandler interface
func (h *RESTAPIHandler) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract path segments for proper routing
	pathSegments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")

	// Check if this is a resource-specific request (has ID) or a collection request
	resourceId := r.PathValue("id")
	isTransitionRequest := false
	subAction := ""

	// We expect paths like:
	// - /api/orders (collection)
	// - /api/orders/123 (specific resource)
	// - /api/orders/123/transition (resource action)
	// - /api/orders/123/assign (sub-resource action)

	if len(pathSegments) >= 4 {
		lastSegment := pathSegments[len(pathSegments)-1]
		if lastSegment == "transition" {
			isTransitionRequest = true
		} else if h.Type != "" && lastSegment != resourceId {
			// Only detect sub-actions for handlers with a workflow engine.
			// This prevents non-workflow handlers from misinterpreting nested
			// resource paths as sub-actions.
			// Also skip when the last segment equals the resource ID (e.g.,
			// /api/webchat/poll/{id}) — that's just a deeper path, not a sub-action.
			subAction = lastSegment
		}
	}

	// Route based on method and path structure
	switch {
	case isTransitionRequest && (r.Method == http.MethodPut || r.Method == http.MethodPost):
		// Handle state machine transition request
		h.handleTransition(resourceId, w, r)
	case subAction != "" && r.Method == http.MethodPost:
		// Handle sub-resource action (assign, messages, transfer, etc.)
		h.handleSubAction(resourceId, subAction, w, r)
	case subAction != "" && r.Method == http.MethodGet:
		// Handle sub-resource GET (summary, etc.)
		h.handleSubActionGet(resourceId, subAction, w, r)
	case r.Method == http.MethodGet && resourceId != "":
		// Get a specific resource
		h.handleGet(resourceId, w, r)
	case r.Method == http.MethodGet:
		// List all resources
		h.handleGetAll(w, r)
	case r.Method == http.MethodPost:
		// Create a new resource
		h.handlePost(resourceId, w, r)
	case r.Method == http.MethodPut && resourceId != "":
		// Update an existing resource
		h.handlePut(resourceId, w, r)
	case r.Method == http.MethodDelete && resourceId != "":
		// Delete a resource
		h.handleDelete(resourceId, w, r)
	default:
		// Method not allowed or invalid path
		w.WriteHeader(http.StatusMethodNotAllowed)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed or invalid path"}); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}
}

// Start loads persisted resources (if available) and seed data.
func (h *RESTAPIHandler) Start(ctx context.Context) error {
	// Ensure field defaults are initialized (covers Constructor path where Init is skipped)
	h.initFieldDefaults()

	// Late-bind persistence if it wasn't available during Init().
	// This handles the case where the persistence module initializes after
	// this module (e.g., alphabetical ordering without explicit dependsOn).
	if h.persistence == nil && h.app != nil {
		var ps any
		if err := h.app.GetService("persistence", &ps); err == nil && ps != nil {
			if store, ok := ps.(*PersistenceStore); ok {
				h.persistence = store
			}
		}
	}

	// Load persisted resources
	if h.persistence != nil {
		loaded, err := h.persistence.LoadResources(h.resourceName)
		if err != nil {
			if h.logger != nil {
				h.logger.Warn(fmt.Sprintf("Failed to load persisted resources for %s: %v", h.resourceName, err))
			}
		} else if len(loaded) > 0 {
			h.mu.Lock()
			for id, data := range loaded {
				state := h.fieldMapping.ResolveString(data, "state")
				lastUpdate := h.fieldMapping.ResolveString(data, "lastUpdate")
				h.resources[id] = RESTResource{
					ID:         id,
					Data:       data,
					State:      state,
					LastUpdate: lastUpdate,
				}
			}
			h.mu.Unlock()
			if h.logger != nil {
				h.logger.Info(fmt.Sprintf("Loaded %d persisted %s resources", len(loaded), h.resourceName))
			}
			// Skip seed data if we loaded persisted data
			return nil
		}
	}

	// Load seed data only if no persisted data was loaded
	if h.seedFile != "" {
		if err := h.loadSeedData(h.seedFile); err != nil {
			if h.logger != nil {
				h.logger.Warn(fmt.Sprintf("Failed to load seed data from %s: %v", h.seedFile, err))
			}
		} else if h.logger != nil {
			h.logger.Info(fmt.Sprintf("Loaded seed data from %s", h.seedFile))
		}
	}

	return nil
}

// Stop is a no-op for this handler
func (h *RESTAPIHandler) Stop(ctx context.Context) error {
	return nil
}

// ProvidesServices returns the services provided by this module
func (h *RESTAPIHandler) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        h.name,
			Description: fmt.Sprintf("REST API handler for %s resource", h.resourceName),
			Instance:    h,
		},
	}
}

// RequiresServices returns the services required by this module
func (h *RESTAPIHandler) RequiresServices() []modular.ServiceDependency {
	return []modular.ServiceDependency{
		{
			Name:     "persistence",
			Required: false, // Optional dependency
		},
	}
}
