package module

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
)

// riskPatterns maps risk categories to keyword patterns for message analysis.
var riskPatterns = map[string][]string{
	"self-harm":         {"cut myself", "cutting myself", "hurt myself", "hurting myself", "self-harm", "self harm", "burning myself", "hitting myself"},
	"suicidal-ideation": {"kill myself", "suicide", "end my life", "not alive", "want to die", "better off dead", "no reason to live", "dont want to be alive"},
	"crisis-immediate":  {"right now", "tonight", "plan to", "going to do it", "goodbye", "final"},
	"substance-abuse":   {"drinking", "drugs", "overdose", "alcohol", "pills", "high right now", "substance"},
	"domestic-violence": {"hits me", "abuses me", "beats me", "violent", "domestic", "partner hurts", "partner hits"},
}

// assessRiskLevel analyzes messages and returns the risk level and detected tags.
func assessRiskLevel(messages []any) (string, []string) {
	var allText strings.Builder
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		for _, field := range []string{"body", "Body", "content", "message"} {
			if body, ok := msg[field].(string); ok && body != "" {
				allText.WriteString(strings.ToLower(body))
				allText.WriteString(" ")
				break
			}
		}
	}
	combined := allText.String()
	if combined == "" {
		return "low", nil
	}

	tagSet := make(map[string]bool)
	for category, patterns := range riskPatterns {
		for _, pattern := range patterns {
			if strings.Contains(combined, pattern) {
				tagSet[category] = true
				break
			}
		}
	}

	riskLevel := "low"
	if tagSet["substance-abuse"] || tagSet["domestic-violence"] {
		riskLevel = "medium"
	}
	if tagSet["self-harm"] {
		riskLevel = "high"
	}
	if tagSet["suicidal-ideation"] {
		riskLevel = "high"
	}
	if tagSet["crisis-immediate"] {
		riskLevel = "critical"
	}

	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	return riskLevel, tags
}

// RESTResource represents a simple in-memory resource store for REST APIs
type RESTResource struct {
	ID         string         `json:"id"`
	Data       map[string]any `json:"data"`
	State      string         `json:"state,omitempty"`
	LastUpdate string         `json:"lastUpdate,omitempty"`
}

// RESTAPIHandler provides CRUD operations for a REST API
type RESTAPIHandler struct {
	name         string
	resourceName string
	resources    map[string]RESTResource
	mu           sync.RWMutex
	eventBroker  MessageProducer // Optional dependency for publishing events
	logger       modular.Logger
	app          modular.Application
	persistence  *PersistenceStore // optional write-through backend

	// Workflow-related fields
	workflowType      string // The type of workflow to use (e.g., "order-workflow")
	workflowEngine    string // The name of the workflow engine service to use
	initialTransition string // The first transition to trigger after creating a workflow instance (defaults to "start_validation")
	instanceIDPrefix  string // Optional prefix for workflow instance IDs
	instanceIDField   string // Field in resource data to use for instance ID (defaults to "id")
	seedFile          string // Path to JSON seed data file

	// View/aggregation fields (e.g., queue-api reading from conversations)
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
	h.workflowType = wt
}

// SetWorkflowEngine sets the name of the workflow engine service to use.
func (h *RESTAPIHandler) SetWorkflowEngine(we string) {
	h.workflowEngine = we
}

// SetInitialTransition sets the first transition to trigger after creating a workflow instance.
func (h *RESTAPIHandler) SetInitialTransition(t string) {
	h.initialTransition = t
}

// SetSeedFile sets the path to a JSON seed data file.
func (h *RESTAPIHandler) SetSeedFile(path string) {
	h.seedFile = path
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
		handler.workflowType = h.workflowType
		handler.workflowEngine = h.workflowEngine
		handler.initialTransition = h.initialTransition
		handler.instanceIDPrefix = h.instanceIDPrefix
		handler.instanceIDField = h.instanceIDField
		handler.seedFile = h.seedFile
		handler.sourceResourceName = h.sourceResourceName
		handler.stateFilter = h.stateFilter
		handler.fieldMapping = h.fieldMapping
		handler.transitionMap = h.transitionMap
		handler.summaryFields = h.summaryFields

		// Look for a message broker service for event publishing
		if broker, ok := services["message-broker"]; ok {
			if mb, ok := broker.(MessageBroker); ok {
				handler.eventBroker = mb.Producer()
			}
		}

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
	h.instanceIDField = "id" // Default to using "id" field if not specified
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
									h.workflowType = wt
								}

								// Extract workflow engine
								if we, ok := cfg["workflowEngine"].(string); ok && we != "" {
									h.workflowEngine = we
								}

								// Extract instance ID prefix
								if prefix, ok := cfg["instanceIDPrefix"].(string); ok {
									h.instanceIDPrefix = prefix
								}

								// Extract instance ID field
								if field, ok := cfg["instanceIDField"].(string); ok && field != "" {
									h.instanceIDField = field
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
			if h.workflowType == "" {
				if statemachine, ok := config.(map[string]any)["workflows"].(map[string]any)["statemachine"]; ok {
					if smConfig, ok := statemachine.(map[string]any); ok {
						if defs, ok := smConfig["definitions"].([]any); ok && len(defs) > 0 {
							if def, ok := defs[0].(map[string]any); ok {
								if name, ok := def["name"].(string); ok && name != "" {
									h.workflowType = name
									h.logger.Info(fmt.Sprintf("Using default workflow type from state machine definition: %s", h.workflowType))
								}
							}
						}
					}
				}
			}

			// If workflow engine is not set but we have a state machine configuration,
			// try to extract the engine name from there
			if h.workflowEngine == "" {
				if statemachine, ok := config.(map[string]any)["workflows"].(map[string]any)["statemachine"]; ok {
					if smConfig, ok := statemachine.(map[string]any); ok {
						if engine, ok := smConfig["engine"].(string); ok && engine != "" {
							h.workflowEngine = engine
							h.logger.Info(fmt.Sprintf("Using state machine engine from configuration: %s", h.workflowEngine))
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
	if h.workflowType != "" {
		h.logger.Info(fmt.Sprintf("REST API handler '%s' configured with workflow type: %s", h.name, h.workflowType))
		if h.workflowEngine != "" {
			h.logger.Info(fmt.Sprintf("Using workflow engine: %s", h.workflowEngine))
		}
		if h.instanceIDPrefix != "" {
			h.logger.Info(fmt.Sprintf("Using instance ID prefix: %s", h.instanceIDPrefix))
		}
		h.logger.Info(fmt.Sprintf("Using instance ID field: %s", h.instanceIDField))
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

	// For view handlers (e.g., queue-api), detect virtual endpoints like "health"
	if h.sourceResourceName != "" && resourceId == "" && len(pathSegments) >= 3 {
		lastSeg := pathSegments[len(pathSegments)-1]
		if lastSeg == "health" {
			resourceId = "health"
		}
	}

	// We expect paths like:
	// - /api/orders (collection)
	// - /api/orders/123 (specific resource)
	// - /api/orders/123/transition (resource action)
	// - /api/orders/123/assign (sub-resource action)

	if len(pathSegments) >= 4 {
		lastSegment := pathSegments[len(pathSegments)-1]
		if lastSegment == "transition" {
			isTransitionRequest = true
		} else if h.workflowType != "" {
			// Only detect sub-actions for handlers with a workflow engine.
			// This prevents non-workflow handlers (e.g. messages-api) from
			// misinterpreting nested resource paths as sub-actions.
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

// syncFromPersistence merges any resources from the persistence store that are
// not yet in the in-memory map. This allows resources created by other handler
// instances sharing the same persistence resourceName to be visible.
func (h *RESTAPIHandler) syncFromPersistence() {
	if h.persistence == nil {
		return
	}
	loadFrom := h.resourceName
	if h.sourceResourceName != "" {
		loadFrom = h.sourceResourceName
	}
	loaded, err := h.persistence.LoadResources(loadFrom)
	if err != nil || len(loaded) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	// For view handlers (sourceResourceName set), always refresh from persistence
	// to reflect state changes made by other handlers.
	isView := h.sourceResourceName != ""

	for id, data := range loaded {
		if !isView {
			if _, exists := h.resources[id]; exists {
				continue // don't overwrite in-memory state for primary handlers
			}
		}
		state := h.fieldMapping.ResolveString(data, "state")
		lastUpdate := h.fieldMapping.ResolveString(data, "lastUpdate")
		h.resources[id] = RESTResource{
			ID:         id,
			Data:       data,
			State:      state,
			LastUpdate: lastUpdate,
		}
	}

	// For view handlers, remove resources that no longer exist in persistence
	if isView {
		for id := range h.resources {
			if _, exists := loaded[id]; !exists {
				delete(h.resources, id)
			}
		}
	}
}

// handleGet handles GET requests for listing or retrieving resources
func (h *RESTAPIHandler) handleGet(resourceId string, w http.ResponseWriter, r *http.Request) {
	// Handle virtual "health" endpoint for view handlers (e.g., /api/queue/health)
	if resourceId == "health" && h.sourceResourceName != "" {
		h.handleQueueHealth(w, r)
		return
	}

	h.syncFromPersistence()
	h.mu.RLock()
	defer h.mu.RUnlock()

	if resourceId == "" {
		// List all resources
		resources := make([]RESTResource, 0, len(h.resources))
		for _, resource := range h.resources {
			resources = append(resources, resource)
		}
		if err := json.NewEncoder(w).Encode(resources); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
		return
	}

	// Get a specific resource
	if resource, ok := h.resources[resourceId]; ok {
		// Try to get the latest state and enrichment data from the workflow engine
		if h.workflowEngine != "" {
			instanceId := resourceId
			if h.instanceIDPrefix != "" {
				instanceId = h.instanceIDPrefix + resourceId
			}
			var engineSvc any
			if err := h.app.GetService(h.workflowEngine, &engineSvc); err == nil {
				if smEngine, ok := engineSvc.(*StateMachineEngine); ok {
					if instance, err := smEngine.GetInstance(instanceId); err == nil && instance != nil {
						resource.State = instance.CurrentState
						resource.LastUpdate = instance.LastUpdated.Format(time.RFC3339)
						// Merge enrichment data from processing pipeline.
						// Only set keys that don't already exist in the API handler's
						// resource data, since the handler is authoritative for fields
						// it manages directly (e.g., messages, tags, riskLevel).
						for k, v := range instance.Data {
							if k == "id" || k == "state" || k == "lastUpdate" {
								continue
							}
							if _, exists := resource.Data[k]; !exists {
								resource.Data[k] = v
							}
						}
					}
				}
			}
		}

		// Also check state tracker for additional data enrichment
		var stateTracker any
		_ = h.app.GetService(StateTrackerName, &stateTracker)
		if stateTracker != nil {
			if tracker, ok := stateTracker.(*StateTracker); ok {
				if stateInfo, exists := tracker.GetState(h.resourceName, resourceId); exists {
					// Use state tracker state if we didn't get one from the engine
					if resource.State == "" || resource.State == "new" {
						resource.State = stateInfo.CurrentState
						resource.LastUpdate = stateInfo.LastUpdate.Format(time.RFC3339)
					}
					// Merge data from the state tracker (only new keys)
					if stateInfo.Data != nil {
						for k, v := range stateInfo.Data {
							if _, exists := resource.Data[k]; !exists {
								resource.Data[k] = v
							}
						}
					}
				}
			}
		}

		if err := json.NewEncoder(w).Encode(resource); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
		return
	}

	// Not found
	w.WriteHeader(http.StatusNotFound)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"}); err != nil {
		// Log error but continue since response is already committed
		_ = err
	}
}

// handleGetAll handles GET requests for listing all resources
func (h *RESTAPIHandler) handleGetAll(w http.ResponseWriter, r *http.Request) {
	h.syncFromPersistence()
	h.mu.RLock()
	defer h.mu.RUnlock()

	// If there's an authenticated user, filter resources to only show theirs
	currentUserID := extractUserID(r)

	// Extract affiliate/program filtering from query params and JWT claims
	role, jwtAffiliateId, jwtProgramIds := extractAuthClaims(r)
	queryAffiliateId := r.URL.Query().Get("affiliateId")
	queryProgramId := r.URL.Query().Get("programId")
	queryRole := r.URL.Query().Get("role")

	// Determine effective filter values: query params take precedence, then JWT claims
	filterAffiliateId := queryAffiliateId
	if filterAffiliateId == "" && role != "admin" {
		filterAffiliateId = jwtAffiliateId
	}
	var filterProgramIds []string
	if queryProgramId != "" {
		filterProgramIds = strings.Split(queryProgramId, ",")
	} else if role != "admin" && len(jwtProgramIds) > 0 {
		filterProgramIds = jwtProgramIds
	}

	// Role-based query param filter (e.g., ?role=responder filters users by role)
	filterRole := queryRole

	// Admin role bypasses affiliate/program filtering
	isAdmin := role == "admin"

	// Optionally get the state machine engine for live state lookup
	var smEngine *StateMachineEngine
	if h.workflowEngine != "" {
		var engineSvc any
		if err := h.app.GetService(h.workflowEngine, &engineSvc); err == nil {
			smEngine, _ = engineSvc.(*StateMachineEngine)
		}
	}

	resources := make([]RESTResource, 0, len(h.resources))
	for _, resource := range h.resources {
		// If user is authenticated and resource has a userId, only include matching resources
		if currentUserID != "" {
			if resUserID, ok := resource.Data["userId"].(string); ok && resUserID != currentUserID {
				continue
			}
		}

		// Enrich with live state and data from the workflow engine BEFORE filtering,
		// so that fields added by processing steps (programId, affiliateId) are available.
		if smEngine != nil {
			instanceId := resource.ID
			if h.instanceIDPrefix != "" {
				instanceId = h.instanceIDPrefix + resource.ID
			}
			if instance, err := smEngine.GetInstance(instanceId); err == nil && instance != nil {
				resource.State = instance.CurrentState
				resource.LastUpdate = instance.LastUpdated.Format(time.RFC3339)
				// Only set keys that don't already exist in the API handler's
				// resource data, since the handler is authoritative for fields
				// it manages directly (e.g., messages, tags, riskLevel).
				for k, v := range instance.Data {
					if k == "id" || k == "state" || k == "lastUpdate" {
						continue
					}
					if _, exists := resource.Data[k]; !exists {
						resource.Data[k] = v
					}
				}
			}
		}

		// Apply affiliate filter (skip for admin).
		// Resources without an affiliateId are excluded when a filter is active.
		if !isAdmin && filterAffiliateId != "" {
			resAffiliateId, _ := resource.Data["affiliateId"].(string)
			if resAffiliateId != filterAffiliateId {
				continue
			}
		}

		// Apply program filter (skip for admin).
		// Only filter resources that have a programId field (e.g., conversations).
		// Resources without programId (e.g., users) are not filtered by program.
		if !isAdmin && len(filterProgramIds) > 0 {
			resProgramId, _ := resource.Data["programId"].(string)
			if resProgramId != "" {
				found := slices.Contains(filterProgramIds, resProgramId)
				if !found {
					continue
				}
			}
		}

		// Apply role query param filter (for user resources)
		if filterRole != "" {
			resRole, _ := resource.Data["role"].(string)
			if resRole != "" && resRole != filterRole {
				continue
			}
		}

		// Apply state filter if configured (e.g., queue handler only shows "queued" resources)
		if h.stateFilter != "" {
			resState := resource.State
			if resState == "" {
				resState, _ = resource.Data["state"].(string)
			}
			if resState != h.stateFilter {
				continue
			}
		}

		resources = append(resources, resource)
	}

	// For view handlers (sourceResourceName set), return summary with count
	if h.sourceResourceName != "" {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalQueued":   len(resources),
			"count":         len(resources),
			"conversations": resources,
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resources); err != nil {
		// Log error but continue since response is already committed
		_ = err
	}
}

// extractUserID extracts the authenticated user's ID from the request context.
// Returns empty string if no auth claims are present.
func extractUserID(r *http.Request) string {
	claims, ok := r.Context().Value(authClaimsContextKey).(map[string]any)
	if !ok {
		return ""
	}
	if sub, ok := claims["sub"].(string); ok {
		return sub
	}
	return ""
}

// extractAuthClaims extracts role, affiliateId, and programIds from the JWT claims
// in the request context. Returns empty values if no auth claims are present.
func extractAuthClaims(r *http.Request) (role, affiliateId string, programIds []string) {
	claims, ok := r.Context().Value(authClaimsContextKey).(map[string]any)
	if !ok {
		return "", "", nil
	}
	role, _ = claims["role"].(string)
	affiliateId, _ = claims["affiliateId"].(string)
	if pids, ok := claims["programIds"].([]any); ok {
		for _, pid := range pids {
			if s, ok := pid.(string); ok {
				programIds = append(programIds, s)
			}
		}
	}
	return role, affiliateId, programIds
}

// maxRequestBodySize is the maximum allowed request body size (1MB).
const maxRequestBodySize = 1 << 20

// handlePost handles POST requests for creating resources
func (h *RESTAPIHandler) handlePost(resourceId string, w http.ResponseWriter, r *http.Request) {
	// Limit request body size to prevent denial-of-service
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		if encErr := json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"}); encErr != nil {
			// Log error but continue since response is already committed
			_ = encErr
		}
		return
	}

	// Attach the authenticated user's ID to the resource data
	if userID := extractUserID(r); userID != "" {
		h.fieldMapping.SetValue(data, "userId", userID)
	}

	h.mu.Lock()

	// If ID is provided in the URL, use it; otherwise use the ID from the body
	if resourceId == "" {
		if idFromBody := h.fieldMapping.ResolveString(data, "id"); idFromBody != "" {
			resourceId = idFromBody
		} else {
			// Generate an ID (TODO: use a proper UUID generator)
			resourceId = fmt.Sprintf("%d", len(h.resources)+1)
		}
	}

	// Extract state if present, default to "new" for state machine resources
	state := "new"
	if stateVal := h.fieldMapping.ResolveString(data, "state"); stateVal != "" {
		state = stateVal
	}

	// Set the current time for last update
	lastUpdate := time.Now().Format(time.RFC3339)

	// Store the ID in data so it's available downstream
	h.fieldMapping.SetValue(data, "id", resourceId)

	// Assess risk level from initial message content if present
	if bodyText := h.fieldMapping.ResolveString(data, "body"); bodyText != "" {
		initialMsgs := []any{map[string]any{"body": bodyText}}
		riskLevel, riskTags := assessRiskLevel(initialMsgs)
		h.fieldMapping.SetValue(data, "riskLevel", riskLevel)
		if len(riskTags) > 0 {
			tagIfaces := make([]any, len(riskTags))
			for i, t := range riskTags {
				tagIfaces[i] = t
			}
			h.fieldMapping.SetValue(data, "tags", tagIfaces)
		}
	}

	// Initialize messages array with the initial inbound message if present.
	// This ensures the chat view shows the texter's first message immediately.
	if _, hasMessages := data["messages"]; !hasMessages {
		// Extract message body from various field names (webhooks use different casing)
		bodyText := ""
		for _, field := range []string{"body", "Body", "message", "content"} {
			if b, ok := data[field].(string); ok && b != "" {
				bodyText = b
				break
			}
		}
		if bodyText != "" {
			from := ""
			for _, field := range []string{"from", "From"} {
				if f, ok := data[field].(string); ok && f != "" {
					from = f
					break
				}
			}
			data["messages"] = []any{
				map[string]any{
					"body":      bodyText,
					"direction": "inbound",
					"from":      from,
					"sender":    "texter",
					"status":    "delivered",
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				},
			}
		} else {
			data["messages"] = []any{}
		}
	}

	// If this is a conversation resource and has a message body but no programId,
	// try to resolve routing from the message content using dynamic components.
	if h.resourceName == "conversations" {
		if _, hasProgramId := data["programId"]; !hasProgramId {
			// Extract message body from various field names
			msgBody := ""
			for _, field := range []string{"body", "Body", "message", "content"} {
				if b, ok := data[field].(string); ok && b != "" {
					msgBody = b
					break
				}
			}
			if msgBody != "" {
				h.resolveConversationRouting(data, msgBody)
			}
		}
	}

	// Create or update the resource
	resource := RESTResource{
		ID:         resourceId,
		Data:       data,
		State:      state,
		LastUpdate: lastUpdate,
	}
	h.resources[resourceId] = resource

	h.mu.Unlock()

	// Write-through to persistence
	if h.persistence != nil {
		h.fieldMapping.SetValue(resource.Data, "state", resource.State)
		h.fieldMapping.SetValue(resource.Data, "lastUpdate", resource.LastUpdate)
		_ = h.persistence.SaveResource(h.resourceName, resource.ID, resource.Data)
	}

	// Bridge: when a webhook/webchat handler creates a resource, also create a
	// corresponding conversation resource so the SPA can list it via /api/conversations.
	if h.resourceName != "conversations" && h.persistence != nil && h.workflowType != "" {
		h.bridgeToConversation(resourceId, data)
	}

	// Publish event if broker is available
	if h.eventBroker != nil {
		eventData, _ := json.Marshal(map[string]any{
			"eventType": h.resourceName + ".created",
			"resource":  resource,
		})

		// Non-blocking event publishing
		go func() {
			if err := h.eventBroker.SendMessage(h.resourceName+"-events", eventData); err != nil {
				fmt.Printf("Failed to publish event: %v\n", err)
			}
		}()
	}

	// If a workflow engine is configured, create an instance and trigger the initial transition
	if h.workflowType != "" && h.workflowEngine != "" {
		h.startWorkflowForResource(r.Context(), resourceId, resource)
	}

	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(resource); err != nil {
		// Log error but continue since response is already committed
		_ = err
	}
}

// resolveConversationRouting sets programId, affiliateId, and programName on the
// conversation data map by matching the message body against known keywords, then
// falling back to shortcode and provider. This mirrors the routing logic in the
// conversation-router dynamic component but runs synchronously at creation time
// so the fields are persisted before the async workflow pipeline starts.
func (h *RESTAPIHandler) resolveConversationRouting(data map[string]any, msgBody string) {
	// Keyword -> programId mapping (mirrors conversation_router.go)
	keywordProgram := map[string]string{
		"HELLO": "prog-001", "HELP": "prog-001", "CRISIS": "prog-001",
		"TEEN": "prog-002", "WELLNESS": "prog-003", "PARTNER": "prog-004",
	}
	// programId -> affiliateId
	programAffiliate := map[string]string{
		"prog-001": "aff-001", "prog-002": "aff-001",
		"prog-003": "aff-002", "prog-004": "aff-003",
	}
	// programId -> display name
	programName := map[string]string{
		"prog-001": "Crisis Text Line", "prog-002": "Teen Support Line",
		"prog-003": "Wellness Chat", "prog-004": "Partner Assist",
	}
	// shortCode -> programId
	shortCodeProgram := map[string]string{
		"741741": "prog-001", "741742": "prog-002",
	}
	// provider -> programId
	providerProgram := map[string]string{
		"twilio": "prog-001", "webchat": "prog-001",
		"aws": "prog-003", "partner": "prog-004",
	}

	var resolvedProgram string

	// 1. Keyword match (highest priority)
	words := strings.Fields(msgBody)
	if len(words) > 0 {
		firstWord := strings.ToUpper(words[0])
		if pid, ok := keywordProgram[firstWord]; ok {
			resolvedProgram = pid
		}
	}

	// 2. Shortcode match
	if resolvedProgram == "" {
		shortCode, _ := data["shortCode"].(string)
		if shortCode == "" {
			shortCode, _ = data["toNumber"].(string)
		}
		if pid, ok := shortCodeProgram[shortCode]; ok {
			resolvedProgram = pid
		}
	}

	// 3. Provider match
	if resolvedProgram == "" {
		provider, _ := data["provider"].(string)
		if pid, ok := providerProgram[strings.ToLower(provider)]; ok {
			resolvedProgram = pid
		}
	}

	// 4. Default fallback
	if resolvedProgram == "" {
		resolvedProgram = "prog-001"
	}

	data["programId"] = resolvedProgram
	data["affiliateId"] = programAffiliate[resolvedProgram]
	if name, ok := programName[resolvedProgram]; ok {
		data["programName"] = name
	}
}

// startWorkflowForResource creates a workflow instance and triggers the initial transition
// for a newly created resource. Uses background context for async processing since
// the HTTP request context is cancelled when the handler returns.
func (h *RESTAPIHandler) startWorkflowForResource(_ context.Context, resourceId string, resource RESTResource) {
	// Find the state machine engine
	var engineSvc any
	if err := h.app.GetService(h.workflowEngine, &engineSvc); err != nil {
		h.logger.Warn(fmt.Sprintf("Workflow engine '%s' not found: %v", h.workflowEngine, err))
		return
	}

	smEngine, ok := engineSvc.(*StateMachineEngine)
	if !ok {
		h.logger.Warn(fmt.Sprintf("Service '%s' is not a StateMachineEngine", h.workflowEngine))
		return
	}

	// Build the instance ID
	instanceId := resourceId
	if h.instanceIDPrefix != "" {
		instanceId = h.instanceIDPrefix + resourceId
	}

	// Create the workflow instance
	_, err := smEngine.CreateWorkflow(h.workflowType, instanceId, resource.Data)
	if err != nil {
		h.logger.Error(fmt.Sprintf("Failed to create workflow instance '%s': %v", instanceId, err))
		return
	}
	h.logger.Info(fmt.Sprintf("Created workflow instance '%s' for resource '%s'", instanceId, resourceId))

	// Trigger the initial transition asynchronously so we don't block the HTTP response.
	// Use context.Background() since the HTTP request context is cancelled when the
	// handler returns, which would abort the processing pipeline.
	go func() {
		bgCtx := context.Background()
		transitionName := h.initialTransition
		if transitionName == "" {
			transitionName = "start_validation" // default convention
		}
		if err := smEngine.TriggerTransition(bgCtx, instanceId, transitionName, resource.Data); err != nil {
			h.logger.Warn(fmt.Sprintf("Failed to trigger initial transition '%s' for '%s': %v",
				transitionName, instanceId, err))
		} else {
			h.logger.Info(fmt.Sprintf("Triggered '%s' for workflow instance '%s'", transitionName, instanceId))

			// Update the resource state from the engine after the transition chain completes
			h.syncResourceStateFromEngine(instanceId, resourceId, smEngine)
		}
	}()
}

// syncResourceStateFromEngine reads the workflow instance state and updates the in-memory resource.
func (h *RESTAPIHandler) syncResourceStateFromEngine(instanceId, resourceId string, engine *StateMachineEngine) {
	// Give the async processing pipeline a moment to progress
	time.Sleep(500 * time.Millisecond)

	instance, err := engine.GetInstance(instanceId)
	if err != nil || instance == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if res, exists := h.resources[resourceId]; exists {
		res.State = instance.CurrentState
		res.LastUpdate = instance.LastUpdated.Format(time.RFC3339)

		// Merge enrichment data from the workflow instance back into the resource.
		// This captures data added by processing steps (e.g., programId from keyword-matcher,
		// affiliateId from conversation-router).
		// Only set keys that don't already exist in the API handler's resource data,
		// since the handler is authoritative for fields it manages directly
		// (e.g., messages, tags, riskLevel).
		for k, v := range instance.Data {
			// Don't overwrite core fields that the resource already manages
			if k == "id" || k == "state" || k == "lastUpdate" {
				continue
			}
			if _, exists := res.Data[k]; !exists {
				res.Data[k] = v
			}
		}

		res.Data["state"] = res.State
		res.Data["lastUpdate"] = res.LastUpdate
		h.resources[resourceId] = res

		// Write-through to persistence
		if h.persistence != nil {
			_ = h.persistence.SaveResource(h.resourceName, res.ID, res.Data)
		}

		// Update the bridged conversation resource's state from the engine.
		// Only update the state field, not the full data (which was already
		// set by bridgeToConversation with routing info, messages, etc.).
		if h.resourceName != "conversations" && h.persistence != nil {
			convoId := fmt.Sprintf("conv-%s", resourceId)
			h.updateConversationState(convoId, res.State)
		}
	}
}

// updateConversationState updates just the state field of a bridged conversation resource.
// Uses LoadResources to read the existing data, then updates the state and saves back.
func (h *RESTAPIHandler) updateConversationState(convoId, newState string) {
	if h.persistence == nil {
		return
	}
	loaded, err := h.persistence.LoadResources("conversations")
	if err != nil {
		return
	}
	data, ok := loaded[convoId]
	if !ok {
		return
	}
	data["state"] = newState
	data["lastUpdate"] = time.Now().UTC().Format(time.RFC3339)
	_ = h.persistence.SaveResource("conversations", convoId, data)
}

// bridgeToConversation creates a conversation resource in the "conversations" persistence
// store from webhook/webchat data. This bridges the gap between inbound handlers
// (webhooks-api, webchat-api) and the conversations-api that the SPA reads from.
func (h *RESTAPIHandler) bridgeToConversation(webhookId string, data map[string]any) {
	convoId := fmt.Sprintf("conv-%s", webhookId)

	convoData := map[string]any{
		"id":        convoId,
		"state":     "queued",
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}

	// Copy key fields from the webhook data
	for _, field := range []string{
		"from", "From", "provider", "messages", "riskLevel", "tags",
	} {
		if v, ok := data[field]; ok {
			convoData[field] = v
		}
	}

	// Normalize: ensure "from" is set (Twilio sends "From")
	if _, ok := convoData["from"]; !ok {
		if f, ok := convoData["From"]; ok {
			convoData["from"] = f
		}
	}

	// Resolve routing (programId, affiliateId) from message content
	bodyText := ""
	for _, field := range []string{"body", "Body", "message", "content"} {
		if b, ok := data[field].(string); ok && b != "" {
			bodyText = b
			break
		}
	}
	if bodyText != "" {
		h.resolveConversationRouting(convoData, bodyText)
	}

	convoData["lastUpdate"] = time.Now().UTC().Format(time.RFC3339)

	if err := h.persistence.SaveResource("conversations", convoId, convoData); err != nil {
		h.logger.Warn(fmt.Sprintf("Failed to bridge conversation '%s': %v", convoId, err))
	}
}

// handlePut handles PUT requests for updating resources
func (h *RESTAPIHandler) handlePut(resourceId string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "ID is required for PUT"}); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	// Limit request body size to prevent denial-of-service
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		if encErr := json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"}); encErr != nil {
			// Log error but continue since response is already committed
			_ = encErr
		}
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if resource exists
	if _, ok := h.resources[resourceId]; !ok {
		w.WriteHeader(http.StatusNotFound)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"}); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	// Update the resource
	h.resources[resourceId] = RESTResource{
		ID:   resourceId,
		Data: data,
	}

	// Write-through to persistence
	if h.persistence != nil {
		_ = h.persistence.SaveResource(h.resourceName, resourceId, data)
	}

	if err := json.NewEncoder(w).Encode(h.resources[resourceId]); err != nil {
		// Log error but continue since response is already committed
		_ = err
	}

	// Existing implementation plus event publishing:
	if h.eventBroker != nil {
		eventData, _ := json.Marshal(map[string]any{
			"eventType": h.resourceName + ".updated",
			"resource":  h.resources[resourceId],
		})

		// Non-blocking event publishing
		go func() {
			if err := h.eventBroker.SendMessage(h.resourceName+"-events", eventData); err != nil {
				fmt.Printf("Failed to publish event: %v\n", err)
			}
		}()
	}
}

// handleDelete handles DELETE requests for removing resources
func (h *RESTAPIHandler) handleDelete(resourceId string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "ID is required for DELETE"}); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if resource exists
	if _, ok := h.resources[resourceId]; !ok {
		w.WriteHeader(http.StatusNotFound)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"}); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	// Delete the resource
	delete(h.resources, resourceId)

	// Write-through to persistence
	if h.persistence != nil {
		_ = h.persistence.DeleteResource(h.resourceName, resourceId)
	}

	w.WriteHeader(http.StatusNoContent)

	// Existing implementation plus event publishing:
	if h.eventBroker != nil {
		eventData, _ := json.Marshal(map[string]any{
			"eventType":  h.resourceName + ".deleted",
			"resourceId": resourceId,
		})

		// Non-blocking event publishing
		go func() {
			if err := h.eventBroker.SendMessage(h.resourceName+"-events", eventData); err != nil {
				fmt.Printf("Failed to publish event: %v\n", err)
			}
		}()
	}
}

// handleTransition handles state transitions for state machine resources
func (h *RESTAPIHandler) handleTransition(resourceId string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Resource ID is required for transition"}); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	// Parse the transition request
	var transitionRequest struct {
		Transition   string         `json:"transition"`
		Data         map[string]any `json:"data,omitempty"`
		WorkflowType string         `json:"workflowType,omitempty"` // Optional workflow type override
	}

	if err := json.NewDecoder(r.Body).Decode(&transitionRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		if encErr := json.NewEncoder(w).Encode(map[string]string{"error": "Invalid transition request format"}); encErr != nil {
			// Log error but continue since response is already committed
			_ = encErr
		}
		return
	}

	if transitionRequest.Transition == "" {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Transition name is required"}); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	// Prepare the workflow data
	workflowData := make(map[string]any)

	// Merge existing resource data
	h.mu.RLock()
	resource, exists := h.resources[resourceId]
	h.mu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"}); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	// Add resource data to workflow data
	maps.Copy(workflowData, resource.Data)

	// Add custom transition data if provided
	if transitionRequest.Data != nil {
		maps.Copy(workflowData, transitionRequest.Data)
	}

	// Determine the workflow type to use
	workflowType := h.workflowType // Use configured workflow type by default

	// If a workflow type was specified in the transition request, use that instead
	if transitionRequest.WorkflowType != "" {
		workflowType = transitionRequest.WorkflowType
	}

	// If we still don't have a workflow type, check the resource data for one
	if workflowType == "" {
		if wt, ok := workflowData["workflowType"].(string); ok && wt != "" {
			workflowType = wt
		} else {
			// Use a default workflow type if we have nothing else
			workflowType = "order-workflow" // Fallback default
		}
	}

	// Generate the instance ID using our configuration
	var instanceId string

	// Check if we have a specific instance ID field configured
	if h.instanceIDField != "" && h.instanceIDField != "id" {
		// Try to get the instance ID from the specified field in the resource data
		if idVal, ok := workflowData[h.instanceIDField].(string); ok && idVal != "" {
			instanceId = idVal
		}
	}

	// If we didn't get an ID from a custom field, use the resource ID
	if instanceId == "" {
		instanceId = resourceId
	}

	// Add prefix if configured
	if h.instanceIDPrefix != "" {
		instanceId = h.instanceIDPrefix + instanceId
	}

	// Set the required IDs in the workflow data
	workflowData["id"] = resourceId             // Original resource ID
	workflowData["instanceId"] = instanceId     // Workflow instance ID (with optional prefix)
	workflowData["workflowType"] = workflowType // Workflow type

	// Find the workflow engine to use
	var engine any
	var stateMachineEngine *StateMachineEngine
	var isStateMachineEngine bool

	// First, try to use the specifically configured engine if available
	if h.workflowEngine != "" {
		var engineSvc any
		if err := h.app.GetService(h.workflowEngine, &engineSvc); err == nil && engineSvc != nil {
			engine = engineSvc
			if sm, ok := engineSvc.(*StateMachineEngine); ok {
				stateMachineEngine = sm
				isStateMachineEngine = true
			}
			h.logger.Debug(fmt.Sprintf("Using configured workflow engine: %s", h.workflowEngine))
		} else {
			h.logger.Warn(fmt.Sprintf("Configured workflow engine '%s' not found, will try to discover one", h.workflowEngine))
		}
	}

	// If no specific engine was configured or found, try to find one from a connector
	if engine == nil {
		var stateConnector any
		if err := h.app.GetService(StateMachineStateConnectorName, &stateConnector); err == nil && stateConnector != nil {
			if connector, ok := stateConnector.(*StateMachineStateConnector); ok {
				// Get the engine name for this resource type
				if engineName, found := connector.GetEngineForResourceType(h.resourceName); found {
					// Get the state machine engine by name
					var engineSvc any
					if err := h.app.GetService(engineName, &engineSvc); err == nil && engineSvc != nil {
						engine = engineSvc
						if sm, ok := engineSvc.(*StateMachineEngine); ok {
							stateMachineEngine = sm
							isStateMachineEngine = true
						}
						h.logger.Debug(fmt.Sprintf("Found workflow engine from connector: %s", engineName))
					}
				}
			}
		}
	}

	// If still not found, try to find any state machine engine
	if engine == nil {
		for name, svc := range h.app.SvcRegistry() {
			if sm, ok := svc.(*StateMachineEngine); ok {
				engine = sm
				stateMachineEngine = sm
				isStateMachineEngine = true
				h.logger.Debug(fmt.Sprintf("Found state machine engine: %s", name))
				break
			}
		}
	}

	// If still not found, look for any engine-like service
	if engine == nil {
		for name, svc := range h.app.SvcRegistry() {
			if strings.Contains(strings.ToLower(name), "engine") ||
				strings.Contains(strings.ToLower(name), "workflow") ||
				strings.Contains(strings.ToLower(name), "processor") {
				engine = svc
				if sm, ok := svc.(*StateMachineEngine); ok {
					stateMachineEngine = sm
					isStateMachineEngine = true
				}
				h.logger.Debug(fmt.Sprintf("Found potential workflow engine: %s", name))
				break
			}
		}
	}

	if engine == nil {
		h.logger.Error("No workflow engine found. Available services:")
		for name := range h.app.SvcRegistry() {
			h.logger.Debug(" - " + name)
		}

		w.WriteHeader(http.StatusInternalServerError)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Workflow engine not found"}); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	// Check if the workflow instance exists, and create it if it doesn't
	var instanceExists bool
	if isStateMachineEngine {
		// Check if the instance exists
		existingInstance, err := stateMachineEngine.GetInstance(instanceId)
		instanceExists = (err == nil && existingInstance != nil)

		// If the instance doesn't exist, create it
		if !instanceExists {
			h.logger.Info(fmt.Sprintf("Creating new workflow instance '%s' of type '%s'", instanceId, workflowType))
			_, err := stateMachineEngine.CreateWorkflow(workflowType, instanceId, workflowData)
			if err != nil {
				h.logger.Error(fmt.Sprintf("Failed to create workflow instance: %s", err.Error()))
				w.WriteHeader(http.StatusInternalServerError)
				if encErr := json.NewEncoder(w).Encode(map[string]any{
					"success":    false,
					"error":      fmt.Sprintf("Failed to create workflow instance: %s", err.Error()),
					"id":         resourceId,
					"instanceId": instanceId,
				}); encErr != nil {
					// Log error but continue since response is already committed
					_ = encErr
				}
				return
			}
			h.logger.Info(fmt.Sprintf("Successfully created workflow instance '%s'", instanceId))
		}
	}

	// Try to trigger the workflow transition
	var result map[string]any
	var err error

	// Try different engine types
	switch e := engine.(type) {
	case interface {
		TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) error
	}:
		// Using the main workflow engine
		h.logger.Info(fmt.Sprintf("Triggering workflow '%s' with action '%s' for instance '%s'",
			workflowType, transitionRequest.Transition, instanceId))
		err = e.TriggerWorkflow(r.Context(), "statemachine", transitionRequest.Transition, workflowData)
		result = map[string]any{
			"success":    err == nil,
			"id":         resourceId,
			"instanceId": instanceId,
			"transition": transitionRequest.Transition,
		}

	case interface {
		TriggerTransition(ctx context.Context, instanceID string, transitionID string, data map[string]any) error
	}:
		// Using the state machine engine directly
		h.logger.Info(fmt.Sprintf("Triggering transition '%s' for instance '%s'",
			transitionRequest.Transition, instanceId))
		err = e.TriggerTransition(r.Context(), instanceId, transitionRequest.Transition, workflowData)
		result = map[string]any{
			"success":    err == nil,
			"id":         resourceId,
			"instanceId": instanceId,
			"transition": transitionRequest.Transition,
		}

	default:
		w.WriteHeader(http.StatusInternalServerError)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Workflow engine does not support transitions"}); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	if err != nil {
		h.logger.Error(fmt.Sprintf("Transition failed: %s", err.Error()))
		w.WriteHeader(http.StatusBadRequest)
		if encErr := json.NewEncoder(w).Encode(map[string]any{
			"success":    false,
			"error":      err.Error(),
			"id":         resourceId,
			"instanceId": instanceId,
			"transition": transitionRequest.Transition,
		}); encErr != nil {
			// Log error but continue since response is already committed
			_ = encErr
		}
		return
	}

	// Now we need to query the state machine for the current state
	var currentState string
	var lastUpdate = time.Now().Format(time.RFC3339)

	// Try to get the current state from the state machine engine
	switch e := engine.(type) {
	case interface {
		GetInstance(instanceID string) (*WorkflowInstance, error)
	}:
		// If the engine has a direct method to get instance state
		instance, err := e.GetInstance(instanceId)
		if err == nil && instance != nil {
			currentState = instance.CurrentState
			h.logger.Debug(fmt.Sprintf("Retrieved current state from engine: %s", currentState))
		} else if err != nil {
			h.logger.Warn(fmt.Sprintf("Failed to get instance state: %s", err.Error()))
		}
	case interface {
		GetWorkflowState(ctx context.Context, workflowType string, instanceID string) (map[string]any, error)
	}:
		// Try a more generic method
		stateData, err := e.GetWorkflowState(r.Context(), workflowType, instanceId)
		if err == nil && stateData != nil {
			if state, ok := stateData["currentState"].(string); ok {
				currentState = state
				h.logger.Debug(fmt.Sprintf("Retrieved current state from workflow state: %s", currentState))
			}
		} else if err != nil {
			h.logger.Warn(fmt.Sprintf("Failed to get workflow state: %s", err.Error()))
		}
	}

	// If we couldn't get the state from the engine, try the state tracker
	if currentState == "" {
		var stateTracker any
		if err := h.app.GetService(StateTrackerName, &stateTracker); err == nil && stateTracker != nil {
			if tracker, ok := stateTracker.(*StateTracker); ok {
				if stateInfo, exists := tracker.GetState(h.resourceName, resourceId); exists {
					currentState = stateInfo.CurrentState
					h.logger.Debug(fmt.Sprintf("Retrieved current state from state tracker: %s", currentState))
				}
			}
		}
	}

	// Update the resource with the current state
	if currentState != "" {
		h.mu.Lock()

		// Get the existing resource
		if existingResource, exists := h.resources[resourceId]; exists {
			// Update the state and lastUpdate fields
			existingResource.State = currentState
			existingResource.LastUpdate = lastUpdate

			// Also update the Data map to reflect the current state
			existingResource.Data["state"] = currentState
			existingResource.Data["lastUpdate"] = lastUpdate
			existingResource.Data["workflowType"] = workflowType // Save the workflow type
			existingResource.Data["instanceId"] = instanceId     // Save the instance ID

			// Save the updated resource
			h.resources[resourceId] = existingResource

			// Add the updated state to the result
			result["state"] = currentState
			result["lastUpdate"] = lastUpdate
			result["resource"] = existingResource
		}

		h.mu.Unlock()
	} else {
		h.logger.Warn("Could not determine the current state after transition")
	}

	h.logger.Info(fmt.Sprintf("Transition '%s' completed successfully for resource '%s'",
		transitionRequest.Transition, resourceId))

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(result); err != nil {
		// Log error but continue since response is already committed
		_ = err
	}
}

// handleSubAction handles POST requests to sub-resource actions like /assign, /transfer, etc.
// These map to state machine transitions on the parent resource.
func (h *RESTAPIHandler) handleSubAction(resourceId, subAction string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Resource ID is required"})
		return
	}

	// Parse request body for additional data
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body == nil {
		body = make(map[string]any)
	}

	// Attach the authenticated user's ID
	if userID := extractUserID(r); userID != "" {
		h.fieldMapping.SetValue(body, "responderId", userID)
	}

	// Tag is a data-only update, no state transition
	if subAction == "tag" {
		h.handleTagAction(resourceId, body, w)
		return
	}

	// Messages sub-action: append to the resource's messages array (no state transition)
	if subAction == "messages" {
		h.mu.Lock()
		resource, exists := h.resources[resourceId]
		if !exists {
			h.mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
			return
		}

		// Build message record
		msg := map[string]any{
			"body":      h.fieldMapping.ResolveString(body, "body"),
			"direction": h.fieldMapping.ResolveString(body, "direction"),
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		if from := h.fieldMapping.ResolveString(body, "from"); from != "" {
			msg["from"] = from
		}
		if userID := h.fieldMapping.ResolveString(body, "userId"); userID != "" {
			msg["sender"] = userID
		} else if respID := h.fieldMapping.ResolveString(body, "responderId"); respID != "" {
			msg["sender"] = respID
		}
		if direction := h.fieldMapping.ResolveString(body, "direction"); direction == "outbound" {
			msg["status"] = "sent"
		} else {
			msg["status"] = "delivered"
		}

		// Append to messages array (initialize if nil)
		msgs := h.fieldMapping.ResolveSlice(resource.Data, "messages")
		if msgs == nil {
			msgs = []any{}
		}
		msgs = append(msgs, msg)
		h.fieldMapping.SetValue(resource.Data, "messages", msgs)

		// Assess risk level from all messages
		riskLevel, riskTags := assessRiskLevel(msgs)
		h.fieldMapping.SetValue(resource.Data, "riskLevel", riskLevel)
		if len(riskTags) > 0 {
			existingTags := h.fieldMapping.ResolveSlice(resource.Data, "tags")
			tagSet := make(map[string]bool)
			for _, t := range existingTags {
				if s, ok := t.(string); ok {
					tagSet[s] = true
				}
			}
			for _, t := range riskTags {
				tagSet[t] = true
			}
			allTags := make([]any, 0, len(tagSet))
			for t := range tagSet {
				allTags = append(allTags, t)
			}
			h.fieldMapping.SetValue(resource.Data, "tags", allTags)
		}

		resource.LastUpdate = time.Now().UTC().Format(time.RFC3339)
		h.resources[resourceId] = resource
		h.mu.Unlock()

		// Persist
		if h.persistence != nil {
			h.fieldMapping.SetValue(resource.Data, "state", resource.State)
			h.fieldMapping.SetValue(resource.Data, "lastUpdate", resource.LastUpdate)
			_ = h.persistence.SaveResource(h.resourceName, resourceId, resource.Data)
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"messageId":      fmt.Sprintf("msg-%s-%d", resourceId, len(msgs)),
			"conversationId": resourceId,
			"direction":      h.fieldMapping.ResolveString(body, "direction"),
			"status":         msg["status"],
			"timestamp":      msg["timestamp"],
		})
		return
	}

	// Look up sub-action in the configurable transition map
	transitionName, ok := h.transitionMap[subAction]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Unknown action: %s", subAction)})
		return
	}

	// Refine transition based on request body or current state
	if subAction == "escalate" {
		if escType, ok := body["type"].(string); ok && escType == "police" {
			transitionName = "escalate_to_police"
		}
	}
	if subAction == "close" {
		h.mu.RLock()
		if res, exists := h.resources[resourceId]; exists {
			switch res.State {
			case "wrap_up":
				transitionName = "close_from_wrap_up"
			case "follow_up_active":
				transitionName = "close_from_followup"
			}
		}
		h.mu.RUnlock()
	}

	// Find the state machine engine
	var smEngine *StateMachineEngine
	if h.workflowEngine != "" {
		var engineSvc any
		if err := h.app.GetService(h.workflowEngine, &engineSvc); err == nil {
			smEngine, _ = engineSvc.(*StateMachineEngine)
		}
	}
	if smEngine == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Workflow engine not available"})
		return
	}

	// Build instance ID
	instanceId := resourceId
	if h.instanceIDPrefix != "" {
		instanceId = h.instanceIDPrefix + resourceId
	}

	// Merge existing resource data into the transition payload
	h.mu.RLock()
	resource, exists := h.resources[resourceId]
	h.mu.RUnlock()
	if !exists {
		// Try syncing from persistence first
		h.syncFromPersistence()
		h.mu.RLock()
		resource, exists = h.resources[resourceId]
		h.mu.RUnlock()
	}
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
		return
	}

	workflowData := make(map[string]any)
	maps.Copy(workflowData, resource.Data)
	maps.Copy(workflowData, body)

	// Ensure workflow instance exists
	if _, err := smEngine.GetInstance(instanceId); err != nil {
		// Create it if missing
		if _, err := smEngine.CreateWorkflow(h.workflowType, instanceId, workflowData); err != nil {
			h.logger.Error(fmt.Sprintf("Failed to create workflow instance for sub-action: %v", err))
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create workflow instance"})
			return
		}
	}

	// Trigger the transition
	if err := smEngine.TriggerTransition(r.Context(), instanceId, transitionName, workflowData); err != nil {
		h.logger.Error(fmt.Sprintf("Sub-action '%s' (transition '%s') failed for resource '%s': %v",
			subAction, transitionName, resourceId, err))
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":    false,
			"error":      err.Error(),
			"action":     subAction,
			"transition": transitionName,
		})
		return
	}

	// Read back the updated state
	var currentState string
	if instance, err := smEngine.GetInstance(instanceId); err == nil && instance != nil {
		currentState = instance.CurrentState
	}

	// Update the in-memory resource
	lastUpdate := time.Now().Format(time.RFC3339)
	h.mu.Lock()
	if res, ok := h.resources[resourceId]; ok {
		if currentState != "" {
			res.State = currentState
			res.Data["state"] = currentState
		}
		res.LastUpdate = lastUpdate
		res.Data["lastUpdate"] = lastUpdate
		// Merge body data into the resource
		maps.Copy(res.Data, body)
		h.resources[resourceId] = res

		if h.persistence != nil {
			_ = h.persistence.SaveResource(h.resourceName, res.ID, res.Data)
		}
	}
	h.mu.Unlock()

	// Publish event
	if h.eventBroker != nil {
		eventData, _ := json.Marshal(map[string]any{
			"eventType":  h.resourceName + "." + subAction,
			"resourceId": resourceId,
			"action":     subAction,
			"state":      currentState,
		})
		go func() {
			_ = h.eventBroker.SendMessage(h.resourceName+"-events", eventData)
		}()
	}

	h.logger.Info(fmt.Sprintf("Sub-action '%s' completed for resource '%s' → state '%s'",
		subAction, resourceId, currentState))

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":    true,
		"action":     subAction,
		"transition": transitionName,
		"id":         resourceId,
		"state":      currentState,
		"lastUpdate": lastUpdate,
	})
}

// handleQueueHealth returns aggregated queue health data grouped by program.
func (h *RESTAPIHandler) handleQueueHealth(w http.ResponseWriter, r *http.Request) {
	h.syncFromPersistence()
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Extract affiliate/program filtering
	role, jwtAffiliateId, jwtProgramIds := extractAuthClaims(r)
	queryAffiliateId := r.URL.Query().Get("affiliateId")
	filterAffiliateId := queryAffiliateId
	if filterAffiliateId == "" && role != "admin" {
		filterAffiliateId = jwtAffiliateId
	}
	var filterProgramIds []string
	if qp := r.URL.Query().Get("programId"); qp != "" {
		filterProgramIds = strings.Split(qp, ",")
	} else if role != "admin" && len(jwtProgramIds) > 0 {
		filterProgramIds = jwtProgramIds
	}
	isAdmin := role == "admin"

	type programStats struct {
		ProgramID      string  `json:"programId"`
		ProgramName    string  `json:"programName"`
		Depth          int     `json:"depth"`
		Queued         int     `json:"queued"`
		AvgWaitSeconds float64 `json:"avgWaitSeconds"`
		OldestMessage  string  `json:"oldestMessageAt,omitempty"`
		AlertThreshold int     `json:"alertThreshold"`
	}

	programs := make(map[string]*programStats)
	now := time.Now()

	for _, res := range h.resources {
		state := res.State
		if state == "" {
			state = h.fieldMapping.ResolveString(res.Data, "state")
		}
		if h.stateFilter != "" && state != h.stateFilter {
			continue
		}

		// Apply affiliate filter
		resAffiliateId := h.fieldMapping.ResolveString(res.Data, "affiliateId")
		if !isAdmin && filterAffiliateId != "" && resAffiliateId != "" && resAffiliateId != filterAffiliateId {
			continue
		}

		pid := h.fieldMapping.ResolveString(res.Data, "programId")

		// Apply program filter
		if !isAdmin && len(filterProgramIds) > 0 && pid != "" {
			found := slices.Contains(filterProgramIds, pid)
			if !found {
				continue
			}
		}
		if pid == "" {
			pid = "default"
		}

		ps, ok := programs[pid]
		if !ok {
			pName := h.fieldMapping.ResolveString(res.Data, "programName")
			if pName == "" {
				pName = pid
			}
			ps = &programStats{
				ProgramID:      pid,
				ProgramName:    pName,
				AlertThreshold: 10,
			}
			programs[pid] = ps
		}
		ps.Depth++
		ps.Queued++

		// Track oldest message for wait time calculation
		if created := h.fieldMapping.ResolveString(res.Data, "createdAt"); created != "" {
			if t, err := time.Parse(time.RFC3339, created); err == nil {
				if ps.OldestMessage == "" || created < ps.OldestMessage {
					ps.OldestMessage = created
				}
				waitSecs := now.Sub(t).Seconds()
				ps.AvgWaitSeconds = (ps.AvgWaitSeconds*float64(ps.Depth-1) + waitSecs) / float64(ps.Depth)
			}
		}
	}

	result := make([]programStats, 0, len(programs))
	for _, ps := range programs {
		result = append(result, *ps)
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"programs": result,
		"alerts":   0,
	})
}

// handleTagAction handles POST /tag — updates resource data without a state transition.
func (h *RESTAPIHandler) handleTagAction(resourceId string, body map[string]any, w http.ResponseWriter) {
	h.mu.Lock()
	defer h.mu.Unlock()

	res, exists := h.resources[resourceId]
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
		return
	}

	// Merge tag data into the resource
	tags := h.fieldMapping.ResolveSlice(res.Data, "tags")
	if newTag, ok := body["tag"].(string); ok && newTag != "" {
		tags = append(tags, newTag)
		h.fieldMapping.SetValue(res.Data, "tags", tags)
	}
	if newTags, ok := body["tags"].([]any); ok {
		tags = append(tags, newTags...)
		h.fieldMapping.SetValue(res.Data, "tags", tags)
	}
	res.LastUpdate = time.Now().Format(time.RFC3339)
	h.fieldMapping.SetValue(res.Data, "lastUpdate", res.LastUpdate)
	h.resources[resourceId] = res

	if h.persistence != nil {
		_ = h.persistence.SaveResource(h.resourceName, res.ID, res.Data)
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"id":      resourceId,
		"tags":    tags,
	})
}

// handleSubActionGet handles GET requests for sub-resource data (e.g., /summary).
func (h *RESTAPIHandler) handleSubActionGet(resourceId, subAction string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Resource ID is required"})
		return
	}

	h.syncFromPersistence()
	h.mu.RLock()
	resource, exists := h.resources[resourceId]
	h.mu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
		return
	}

	switch subAction {
	case "summary":
		// Return the resource data plus any summary fields
		summary := map[string]any{
			"id":         resourceId,
			"state":      resource.State,
			"lastUpdate": resource.LastUpdate,
		}
		// Copy relevant summary fields from resource data (configurable via summaryFields)
		for _, key := range h.summaryFields {
			if v, ok := resource.Data[key]; ok {
				summary[key] = v
			}
		}
		// Enrich with live state from workflow engine
		if h.workflowEngine != "" {
			instanceId := resourceId
			if h.instanceIDPrefix != "" {
				instanceId = h.instanceIDPrefix + resourceId
			}
			var engineSvc any
			if err := h.app.GetService(h.workflowEngine, &engineSvc); err == nil {
				if smEngine, ok := engineSvc.(*StateMachineEngine); ok {
					if instance, err := smEngine.GetInstance(instanceId); err == nil && instance != nil {
						summary["state"] = instance.CurrentState
						summary["lastUpdate"] = instance.LastUpdated.Format(time.RFC3339)
					}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(summary)

	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Unknown sub-resource: %s", subAction)})
	}
}

// loadSeedData reads a JSON file containing an array of resources and populates the resources map
func (h *RESTAPIHandler) loadSeedData(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading seed file: %w", err)
	}

	var seeds []struct {
		ID    string         `json:"id"`
		Data  map[string]any `json:"data"`
		State string         `json:"state"`
	}
	if err := json.Unmarshal(data, &seeds); err != nil {
		return fmt.Errorf("parsing seed file: %w", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for _, seed := range seeds {
		if seed.ID == "" {
			continue
		}
		resource := RESTResource{
			ID:         seed.ID,
			Data:       seed.Data,
			State:      seed.State,
			LastUpdate: time.Now().Format(time.RFC3339),
		}
		if resource.Data == nil {
			resource.Data = make(map[string]any)
		}
		h.resources[seed.ID] = resource
	}

	return nil
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
			Name:     "message-broker",
			Required: false, // Optional dependency
		},
		{
			Name:     "persistence",
			Required: false, // Optional dependency
		},
	}
}
