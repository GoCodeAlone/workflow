package module

import (
	"encoding/json"
	"net/http"
)

// FeatureFlagAdmin is the interface the feature flag service must implement
// for the admin API handler. This is defined here to avoid a hard dependency
// on the featureflag package — the service is wired in via SetFeatureFlagService.
type FeatureFlagAdmin interface {
	// ListFlags returns all flag definitions as JSON-serializable objects.
	ListFlags() ([]any, error)
	// GetFlag returns a single flag by key.
	GetFlag(key string) (any, error)
	// CreateFlag creates a new flag from a JSON body.
	CreateFlag(data json.RawMessage) (any, error)
	// UpdateFlag updates an existing flag from a JSON body.
	UpdateFlag(key string, data json.RawMessage) (any, error)
	// DeleteFlag removes a flag by key.
	DeleteFlag(key string) error
	// SetOverrides replaces overrides for a flag from a JSON body.
	SetOverrides(key string, data json.RawMessage) (any, error)
	// EvaluateFlag evaluates a flag with the given user/group context.
	EvaluateFlag(key string, user string, group string) (any, error)
	// SSEHandler returns an http.Handler that streams flag change events.
	SSEHandler() http.Handler
}

// SetFeatureFlagService sets the optional feature flag service for admin API.
func (h *V1APIHandler) SetFeatureFlagService(svc FeatureFlagAdmin) {
	h.featureFlagService = svc
}

// handleFeatureFlags dispatches feature-flag admin API requests.
//
// Handles:
//
//	GET    /feature-flags              -> list all flags
//	POST   /feature-flags              -> create flag
//	GET    /feature-flags/stream       -> SSE stream
//	GET    /feature-flags/{key}        -> get flag
//	PUT    /feature-flags/{key}        -> update flag
//	DELETE /feature-flags/{key}        -> delete flag
//	PUT    /feature-flags/{key}/overrides  -> set overrides
//	GET    /feature-flags/{key}/evaluate   -> evaluate flag
func (h *V1APIHandler) handleFeatureFlags(w http.ResponseWriter, r *http.Request, rest []string) {
	if h.featureFlagService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "feature flag service not available"})
		return
	}

	claims := h.requireAuth(w, r)
	if claims == nil {
		return
	}

	switch {
	// /feature-flags (no key)
	case len(rest) == 0:
		switch r.Method {
		case http.MethodGet:
			h.listFeatureFlags(w)
		case http.MethodPost:
			h.createFeatureFlag(w, r)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}

	// /feature-flags/stream
	case len(rest) == 1 && rest[0] == "stream":
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		h.featureFlagService.SSEHandler().ServeHTTP(w, r)

	// /feature-flags/{key}
	case len(rest) == 1:
		key := rest[0]
		switch r.Method {
		case http.MethodGet:
			h.getFeatureFlag(w, key)
		case http.MethodPut:
			h.updateFeatureFlag(w, r, key)
		case http.MethodDelete:
			h.deleteFeatureFlag(w, key)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}

	// /feature-flags/{key}/overrides
	case len(rest) == 2 && rest[1] == "overrides":
		key := rest[0]
		if r.Method != http.MethodPut {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		h.setFeatureFlagOverrides(w, r, key)

	// /feature-flags/{key}/evaluate
	case len(rest) == 2 && rest[1] == "evaluate":
		key := rest[0]
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		h.evaluateFeatureFlag(w, r, key)

	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (h *V1APIHandler) listFeatureFlags(w http.ResponseWriter) {
	flags, err := h.featureFlagService.ListFlags()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if flags == nil {
		flags = []any{}
	}
	writeJSON(w, http.StatusOK, flags)
}

func (h *V1APIHandler) createFeatureFlag(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	flag, err := h.featureFlagService.CreateFlag(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, flag)
}

func (h *V1APIHandler) getFeatureFlag(w http.ResponseWriter, key string) {
	flag, err := h.featureFlagService.GetFlag(key)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, flag)
}

func (h *V1APIHandler) updateFeatureFlag(w http.ResponseWriter, r *http.Request, key string) {
	body, err := readBody(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	flag, err := h.featureFlagService.UpdateFlag(key, body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, flag)
}

func (h *V1APIHandler) deleteFeatureFlag(w http.ResponseWriter, key string) {
	if err := h.featureFlagService.DeleteFlag(key); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *V1APIHandler) setFeatureFlagOverrides(w http.ResponseWriter, r *http.Request, key string) {
	body, err := readBody(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	flag, err := h.featureFlagService.SetOverrides(key, body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, flag)
}

func (h *V1APIHandler) evaluateFeatureFlag(w http.ResponseWriter, r *http.Request, key string) {
	user := r.URL.Query().Get("user")
	group := r.URL.Query().Get("group")
	result, err := h.featureFlagService.EvaluateFlag(key, user, group)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"value": result})
}

// readBody reads the request body as raw JSON.
func readBody(r *http.Request) (json.RawMessage, error) {
	var body json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body, nil
}
