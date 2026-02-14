package ai

import (
	"encoding/json"
	"net/http"
)

// DeployHandler provides HTTP handlers for the AI deploy API.
type DeployHandler struct {
	deploy *DeployService
}

// NewDeployHandler creates a new deploy API handler.
func NewDeployHandler(deploy *DeployService) *DeployHandler {
	return &DeployHandler{deploy: deploy}
}

// RegisterRoutes registers the deploy API routes on a ServeMux.
func (h *DeployHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/ai/deploy", h.HandleDeploy)
	mux.HandleFunc("POST /api/ai/deploy/component", h.HandleDeployComponent)
}

// deployRequest is the request body for POST /api/ai/deploy.
type deployRequest struct {
	Intent string `json:"intent"`
}

// deployResponse is the response body for POST /api/ai/deploy.
type deployResponse struct {
	Status     string   `json:"status"`
	Components []string `json:"components"`
	ConfigYAML string   `json:"configYaml,omitempty"`
}

// HandleDeploy handles POST /api/ai/deploy.
// It takes an intent string, generates the workflow and components,
// deploys components to the dynamic registry, and returns the result.
func (h *DeployHandler) HandleDeploy(w http.ResponseWriter, r *http.Request) {
	var req deployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Intent == "" {
		writeError(w, http.StatusBadRequest, "intent is required")
		return
	}

	cfg, err := h.deploy.GenerateAndDeploy(r.Context(), req.Intent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "deploy failed: "+err.Error())
		return
	}

	// Collect names of deployed components from the registry
	var componentNames []string
	for _, info := range h.deploy.registry.List() {
		componentNames = append(componentNames, info.Name)
	}

	resp := deployResponse{
		Status:     "deployed",
		Components: componentNames,
	}

	// Include the config if it was generated
	if cfg != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":     resp.Status,
			"components": resp.Components,
			"workflow":   cfg,
		})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// deployComponentRequest is the request body for POST /api/ai/deploy/component.
type deployComponentRequest struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// HandleDeployComponent handles POST /api/ai/deploy/component.
// It deploys a single component to the dynamic registry.
func (h *DeployHandler) HandleDeployComponent(w http.ResponseWriter, r *http.Request) {
	var req deployComponentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	spec := ComponentSpec{
		Name:        req.Name,
		Type:        req.Type,
		Description: req.Description,
		GoCode:      req.Source,
	}

	if err := h.deploy.DeployComponent(r.Context(), spec); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "deploy failed: "+err.Error())
		return
	}

	comp, ok := h.deploy.registry.Get(req.Name)
	if !ok {
		writeError(w, http.StatusInternalServerError, "component registered but not found")
		return
	}

	writeJSON(w, http.StatusCreated, comp.Info())
}
