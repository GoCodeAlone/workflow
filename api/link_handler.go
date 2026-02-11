package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// LinkHandler handles cross-workflow link endpoints.
type LinkHandler struct {
	links     store.CrossWorkflowLinkStore
	workflows store.WorkflowStore
}

// NewLinkHandler creates a new LinkHandler.
func NewLinkHandler(links store.CrossWorkflowLinkStore, workflows store.WorkflowStore) *LinkHandler {
	return &LinkHandler{
		links:     links,
		workflows: workflows,
	}
}

// Create handles POST /api/v1/workflows/{id}/links.
func (h *LinkHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	sourceID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}

	// Verify source workflow exists
	if _, err := h.workflows.Get(r.Context(), sourceID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "source workflow not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var req struct {
		TargetWorkflowID string          `json:"target_workflow_id"`
		LinkType         string          `json:"link_type"`
		Config           json.RawMessage `json:"config,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	targetID, err := uuid.Parse(req.TargetWorkflowID)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid target_workflow_id")
		return
	}
	if req.LinkType == "" {
		WriteError(w, http.StatusBadRequest, "link_type is required")
		return
	}

	// Verify target workflow exists
	if _, err := h.workflows.Get(r.Context(), targetID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "target workflow not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	link := &store.CrossWorkflowLink{
		ID:               uuid.New(),
		SourceWorkflowID: sourceID,
		TargetWorkflowID: targetID,
		LinkType:         req.LinkType,
		Config:           req.Config,
		CreatedBy:        user.ID,
		CreatedAt:        time.Now(),
	}

	if err := h.links.Create(r.Context(), link); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			WriteError(w, http.StatusConflict, "link already exists")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	WriteJSON(w, http.StatusCreated, link)
}

// List handles GET /api/v1/workflows/{id}/links.
func (h *LinkHandler) List(w http.ResponseWriter, r *http.Request) {
	sourceID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}

	links, err := h.links.List(r.Context(), store.CrossWorkflowLinkFilter{
		SourceWorkflowID: &sourceID,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if links == nil {
		links = []*store.CrossWorkflowLink{}
	}
	WriteJSON(w, http.StatusOK, links)
}

// Delete handles DELETE /api/v1/workflows/{id}/links/{linkId}.
func (h *LinkHandler) Delete(w http.ResponseWriter, r *http.Request) {
	linkID, err := uuid.Parse(r.PathValue("linkId"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid link id")
		return
	}
	if err := h.links.Delete(r.Context(), linkID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "link not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
