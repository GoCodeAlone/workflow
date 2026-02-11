package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// --- mock stores for link handler ---

type mockCrossWorkflowLinkStore struct {
	links map[uuid.UUID]*store.CrossWorkflowLink
}

func newMockCrossWorkflowLinkStore() *mockCrossWorkflowLinkStore {
	return &mockCrossWorkflowLinkStore{links: make(map[uuid.UUID]*store.CrossWorkflowLink)}
}

func (m *mockCrossWorkflowLinkStore) Create(_ context.Context, l *store.CrossWorkflowLink) error {
	for _, existing := range m.links {
		if existing.SourceWorkflowID == l.SourceWorkflowID &&
			existing.TargetWorkflowID == l.TargetWorkflowID &&
			existing.LinkType == l.LinkType {
			return store.ErrDuplicate
		}
	}
	m.links[l.ID] = l
	return nil
}

func (m *mockCrossWorkflowLinkStore) Get(_ context.Context, id uuid.UUID) (*store.CrossWorkflowLink, error) {
	l, ok := m.links[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return l, nil
}

func (m *mockCrossWorkflowLinkStore) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := m.links[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.links, id)
	return nil
}

func (m *mockCrossWorkflowLinkStore) List(_ context.Context, f store.CrossWorkflowLinkFilter) ([]*store.CrossWorkflowLink, error) {
	var result []*store.CrossWorkflowLink
	for _, l := range m.links {
		if f.SourceWorkflowID != nil && l.SourceWorkflowID != *f.SourceWorkflowID {
			continue
		}
		if f.TargetWorkflowID != nil && l.TargetWorkflowID != *f.TargetWorkflowID {
			continue
		}
		result = append(result, l)
	}
	return result, nil
}

// --- helpers ---

func newTestLinkHandler() (*LinkHandler, *mockCrossWorkflowLinkStore, *mockWorkflowStore) {
	links := newMockCrossWorkflowLinkStore()
	workflows := &mockWorkflowStore{workflows: make(map[uuid.UUID]*store.WorkflowRecord)}
	h := NewLinkHandler(links, workflows)
	return h, links, workflows
}

// --- tests ---

func TestLinkHandler_Create_Success(t *testing.T) {
	h, _, workflows := newTestLinkHandler()
	sourceID := uuid.New()
	targetID := uuid.New()
	workflows.workflows[sourceID] = &store.WorkflowRecord{ID: sourceID}
	workflows.workflows[targetID] = &store.WorkflowRecord{ID: targetID}

	user := &store.User{ID: uuid.New(), Email: "link@example.com", Active: true}
	body := makeJSON(map[string]string{
		"target_workflow_id": targetID.String(),
		"link_type":          "dependency",
	})
	req := httptest.NewRequest("POST", "/api/v1/workflows/"+sourceID.String()+"/links", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sourceID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	body2 := decodeBody(t, w.Result())
	data, _ := body2["data"].(map[string]interface{})
	if data["link_type"] != "dependency" {
		t.Fatalf("expected link_type dependency, got %v", data["link_type"])
	}
}

func TestLinkHandler_Create_Unauthorized(t *testing.T) {
	h, _, workflows := newTestLinkHandler()
	sourceID := uuid.New()
	workflows.workflows[sourceID] = &store.WorkflowRecord{ID: sourceID}

	body := makeJSON(map[string]string{
		"target_workflow_id": uuid.New().String(),
		"link_type":          "dependency",
	})
	req := httptest.NewRequest("POST", "/api/v1/workflows/"+sourceID.String()+"/links", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sourceID.String())
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestLinkHandler_Create_InvalidSourceID(t *testing.T) {
	h, _, _ := newTestLinkHandler()
	user := &store.User{ID: uuid.New(), Email: "link@example.com", Active: true}

	body := makeJSON(map[string]string{
		"target_workflow_id": uuid.New().String(),
		"link_type":          "dependency",
	})
	req := httptest.NewRequest("POST", "/api/v1/workflows/bad-id/links", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "bad-id")
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestLinkHandler_Create_SourceNotFound(t *testing.T) {
	h, _, _ := newTestLinkHandler()
	user := &store.User{ID: uuid.New(), Email: "link@example.com", Active: true}
	sourceID := uuid.New()

	body := makeJSON(map[string]string{
		"target_workflow_id": uuid.New().String(),
		"link_type":          "dependency",
	})
	req := httptest.NewRequest("POST", "/api/v1/workflows/"+sourceID.String()+"/links", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sourceID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestLinkHandler_Create_InvalidTargetID(t *testing.T) {
	h, _, workflows := newTestLinkHandler()
	sourceID := uuid.New()
	workflows.workflows[sourceID] = &store.WorkflowRecord{ID: sourceID}
	user := &store.User{ID: uuid.New(), Email: "link@example.com", Active: true}

	body := makeJSON(map[string]string{
		"target_workflow_id": "not-a-uuid",
		"link_type":          "dependency",
	})
	req := httptest.NewRequest("POST", "/api/v1/workflows/"+sourceID.String()+"/links", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sourceID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestLinkHandler_Create_TargetNotFound(t *testing.T) {
	h, _, workflows := newTestLinkHandler()
	sourceID := uuid.New()
	workflows.workflows[sourceID] = &store.WorkflowRecord{ID: sourceID}
	user := &store.User{ID: uuid.New(), Email: "link@example.com", Active: true}

	body := makeJSON(map[string]string{
		"target_workflow_id": uuid.New().String(),
		"link_type":          "dependency",
	})
	req := httptest.NewRequest("POST", "/api/v1/workflows/"+sourceID.String()+"/links", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sourceID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestLinkHandler_Create_MissingLinkType(t *testing.T) {
	h, _, workflows := newTestLinkHandler()
	sourceID := uuid.New()
	targetID := uuid.New()
	workflows.workflows[sourceID] = &store.WorkflowRecord{ID: sourceID}
	workflows.workflows[targetID] = &store.WorkflowRecord{ID: targetID}
	user := &store.User{ID: uuid.New(), Email: "link@example.com", Active: true}

	body := makeJSON(map[string]string{
		"target_workflow_id": targetID.String(),
	})
	req := httptest.NewRequest("POST", "/api/v1/workflows/"+sourceID.String()+"/links", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sourceID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestLinkHandler_Create_Duplicate(t *testing.T) {
	h, links, workflows := newTestLinkHandler()
	sourceID := uuid.New()
	targetID := uuid.New()
	workflows.workflows[sourceID] = &store.WorkflowRecord{ID: sourceID}
	workflows.workflows[targetID] = &store.WorkflowRecord{ID: targetID}

	// Pre-create a link
	links.links[uuid.New()] = &store.CrossWorkflowLink{
		ID:               uuid.New(),
		SourceWorkflowID: sourceID,
		TargetWorkflowID: targetID,
		LinkType:         "dependency",
		CreatedAt:        time.Now(),
	}

	user := &store.User{ID: uuid.New(), Email: "link@example.com", Active: true}
	body := makeJSON(map[string]string{
		"target_workflow_id": targetID.String(),
		"link_type":          "dependency",
	})
	req := httptest.NewRequest("POST", "/api/v1/workflows/"+sourceID.String()+"/links", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sourceID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLinkHandler_List_Success(t *testing.T) {
	h, links, _ := newTestLinkHandler()
	sourceID := uuid.New()
	links.links[uuid.New()] = &store.CrossWorkflowLink{
		ID:               uuid.New(),
		SourceWorkflowID: sourceID,
		TargetWorkflowID: uuid.New(),
		LinkType:         "dependency",
	}

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+sourceID.String()+"/links", nil)
	req.SetPathValue("id", sourceID.String())
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestLinkHandler_List_Empty(t *testing.T) {
	h, _, _ := newTestLinkHandler()
	sourceID := uuid.New()

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+sourceID.String()+"/links", nil)
	req.SetPathValue("id", sourceID.String())
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestLinkHandler_List_InvalidID(t *testing.T) {
	h, _, _ := newTestLinkHandler()

	req := httptest.NewRequest("GET", "/api/v1/workflows/bad-id/links", nil)
	req.SetPathValue("id", "bad-id")
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestLinkHandler_Delete_Success(t *testing.T) {
	h, links, _ := newTestLinkHandler()
	linkID := uuid.New()
	links.links[linkID] = &store.CrossWorkflowLink{
		ID:               linkID,
		SourceWorkflowID: uuid.New(),
		TargetWorkflowID: uuid.New(),
		LinkType:         "dependency",
	}

	req := httptest.NewRequest("DELETE", "/api/v1/workflows/xxx/links/"+linkID.String(), nil)
	req.SetPathValue("linkId", linkID.String())
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestLinkHandler_Delete_NotFound(t *testing.T) {
	h, _, _ := newTestLinkHandler()
	linkID := uuid.New()

	req := httptest.NewRequest("DELETE", "/api/v1/workflows/xxx/links/"+linkID.String(), nil)
	req.SetPathValue("linkId", linkID.String())
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestLinkHandler_Delete_InvalidID(t *testing.T) {
	h, _, _ := newTestLinkHandler()

	req := httptest.NewRequest("DELETE", "/api/v1/workflows/xxx/links/bad-id", nil)
	req.SetPathValue("linkId", "bad-id")
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
