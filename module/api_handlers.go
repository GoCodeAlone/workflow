package module

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/GoCodeAlone/modular"
)

// RESTResource represents a simple in-memory resource store for REST APIs
type RESTResource struct {
	ID   string                 `json:"id"`
	Data map[string]interface{} `json:"data"`
}

// RESTAPIHandler provides CRUD operations for a REST API
type RESTAPIHandler struct {
	name         string
	resourceName string
	resources    map[string]RESTResource
	mu           sync.RWMutex
	eventBroker  MessageProducer // Optional dependency for publishing events
}

// RESTAPIHandlerConfig contains configuration for a REST API handler
type RESTAPIHandlerConfig struct {
	ResourceName  string `json:"resourceName" yaml:"resourceName"`
	PublishEvents bool   `json:"publishEvents" yaml:"publishEvents"`
}

// NewRESTAPIHandler creates a new REST API handler
func NewRESTAPIHandler(name, resourceName string) *RESTAPIHandler {
	return &RESTAPIHandler{
		name:         name,
		resourceName: resourceName,
		resources:    make(map[string]RESTResource),
	}
}

// Name returns the unique identifier for this module
func (h *RESTAPIHandler) Name() string {
	return h.name
}

// Constructor returns a function to construct this module with dependencies
func (h *RESTAPIHandler) Constructor() modular.ModuleConstructor {
	return func(app modular.Application, services map[string]any) (modular.Module, error) {
		// Create a new instance with the same name
		handler := NewRESTAPIHandler(h.name, h.resourceName)

		// Look for a message broker service for event publishing
		if broker, ok := services["message-broker"]; ok {
			if mb, ok := broker.(MessageBroker); ok {
				handler.eventBroker = mb.Producer()
			}
		}

		return handler, nil
	}
}

// Init initializes the module with the application context
func (h *RESTAPIHandler) Init(app modular.Application) error {
	// Get configuration if available
	configSection, err := app.GetConfigSection("workflow")
	if err == nil {
		if config := configSection.GetConfig(); config != nil {
			// Try to extract our module's configuration
			// This is a bit verbose but handles nested module configurations
			if modules, ok := config.(map[string]interface{})["modules"].([]interface{}); ok {
				for _, mod := range modules {
					if m, ok := mod.(map[string]interface{}); ok {
						if m["name"] == h.name {
							if cfg, ok := m["config"].(map[string]interface{}); ok {
								if rn, ok := cfg["resourceName"].(string); ok && rn != "" {
									h.resourceName = rn
								}
							}
						}
					}
				}
			}
		}
	}

	return nil
}

// Handle implements the HTTPHandler interface
func (h *RESTAPIHandler) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract ID from path if present (e.g., /api/users/123)
	path := strings.TrimPrefix(r.URL.Path, "/api/"+h.resourceName)
	id := ""
	if path != "" && path[0] == '/' {
		id = path[1:]
	}

	switch r.Method {
	case http.MethodGet:
		h.handleGet(id, w, r)
	case http.MethodPost:
		h.handlePost(id, w, r)
	case http.MethodPut:
		h.handlePut(id, w, r)
	case http.MethodDelete:
		h.handleDelete(id, w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
	}
}

// handleGet handles GET requests for listing or retrieving resources
func (h *RESTAPIHandler) handleGet(id string, w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if id == "" {
		// List all resources
		resources := make([]RESTResource, 0, len(h.resources))
		for _, resource := range h.resources {
			resources = append(resources, resource)
		}
		json.NewEncoder(w).Encode(resources)
		return
	}

	// Get a specific resource
	if resource, ok := h.resources[id]; ok {
		json.NewEncoder(w).Encode(resource)
		return
	}

	// Not found
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
}

// handlePost handles POST requests for creating resources
func (h *RESTAPIHandler) handlePost(id string, w http.ResponseWriter, r *http.Request) {
	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// If ID is provided in the URL, use it; otherwise use the ID from the body
	if id == "" {
		if idFromBody, ok := data["id"].(string); ok && idFromBody != "" {
			id = idFromBody
		} else {
			// Generate an ID (in a real app, use a proper UUID generator)
			id = fmt.Sprintf("%d", len(h.resources)+1)
		}
	}

	// Create or update the resource
	resource := RESTResource{
		ID:   id,
		Data: data,
	}
	h.resources[id] = resource

	// Publish event if broker is available
	if h.eventBroker != nil {
		eventData, _ := json.Marshal(map[string]interface{}{
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

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(h.resources[id])
}

// handlePut handles PUT requests for updating resources
func (h *RESTAPIHandler) handlePut(id string, w http.ResponseWriter, r *http.Request) {
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "ID is required for PUT"})
		return
	}

	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if resource exists
	if _, ok := h.resources[id]; !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
		return
	}

	// Update the resource
	h.resources[id] = RESTResource{
		ID:   id,
		Data: data,
	}

	json.NewEncoder(w).Encode(h.resources[id])

	// Existing implementation plus event publishing:
	if h.eventBroker != nil {
		eventData, _ := json.Marshal(map[string]interface{}{
			"eventType": h.resourceName + ".updated",
			"resource":  h.resources[id],
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
func (h *RESTAPIHandler) handleDelete(id string, w http.ResponseWriter, r *http.Request) {
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "ID is required for DELETE"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if resource exists
	if _, ok := h.resources[id]; !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
		return
	}

	// Delete the resource
	delete(h.resources, id)

	w.WriteHeader(http.StatusNoContent)

	// Existing implementation plus event publishing:
	if h.eventBroker != nil {
		eventData, _ := json.Marshal(map[string]interface{}{
			"eventType":  h.resourceName + ".deleted",
			"resourceId": id,
		})

		// Non-blocking event publishing
		go func() {
			if err := h.eventBroker.SendMessage(h.resourceName+"-events", eventData); err != nil {
				fmt.Printf("Failed to publish event: %v\n", err)
			}
		}()
	}
}

// Start is a no-op for this handler
func (h *RESTAPIHandler) Start(ctx context.Context) error {
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
	}
}
