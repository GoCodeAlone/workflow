package ai

import (
	"net/http"
	"strings"
)

// CombinedHandler wraps both the AI query handler and deploy handler into
// a single http.Handler for config-driven delegate dispatch.
type CombinedHandler struct {
	ai     *Handler
	deploy *DeployHandler
}

// NewCombinedHandler creates a handler that delegates to both AI and deploy handlers.
func NewCombinedHandler(ai *Handler, deploy *DeployHandler) *CombinedHandler {
	return &CombinedHandler{ai: ai, deploy: deploy}
}

// ServeHTTP implements http.Handler, routing to the appropriate sub-handler.
func (h *CombinedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Deploy routes (check first since "/deploy/component" contains "/deploy")
	if strings.Contains(path, "/deploy") {
		h.deploy.ServeHTTP(w, r)
		return
	}

	// All other AI routes
	h.ai.ServeHTTP(w, r)
}
