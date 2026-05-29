package docmanager

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestHandler(t *testing.T) *handler {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return newHandler(db)
}

func createTestDoc(t *testing.T, h *handler, title, content, workflowID, category string) doc {
	t.Helper()
	body := createDocRequest{
		Title:      title,
		Content:    content,
		WorkflowID: workflowID,
		Category:   category,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/docs", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.handleDocs(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create doc: got status %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	var d doc
	if err := json.NewDecoder(w.Body).Decode(&d); err != nil {
		t.Fatalf("decode created doc: %v", err)
	}
	return d
}

func TestCreateDoc(t *testing.T) {
	h := setupTestHandler(t)

	body := `{"title":"My Doc","content":"# Hello","workflow_id":"wf-1","category":"guides"}`
	req := httptest.NewRequest(http.MethodPost, "/docs", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.handleDocs(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusCreated)
	}

	var d doc
	if err := json.NewDecoder(w.Body).Decode(&d); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if d.ID == "" {
		t.Error("expected non-empty ID")
	}
	if d.Title != "My Doc" {
		t.Errorf("got title %q, want %q", d.Title, "My Doc")
	}
	if d.Content != "# Hello" {
		t.Errorf("got content %q, want %q", d.Content, "# Hello")
	}
	if d.WorkflowID != "wf-1" {
		t.Errorf("got workflow_id %q, want %q", d.WorkflowID, "wf-1")
	}
	if d.Category != "guides" {
		t.Errorf("got category %q, want %q", d.Category, "guides")
	}
	if d.CreatedAt == "" {
		t.Error("expected non-empty created_at")
	}
}

func TestListDocs(t *testing.T) {
	h := setupTestHandler(t)
	createTestDoc(t, h, "Doc A", "content a", "", "")
	createTestDoc(t, h, "Doc B", "content b", "", "")

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	w := httptest.NewRecorder()
	h.handleDocs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var docs []docSummary
	if err := json.NewDecoder(w.Body).Decode(&docs); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("got %d docs, want 2", len(docs))
	}
}

func TestGetDoc(t *testing.T) {
	h := setupTestHandler(t)
	created := createTestDoc(t, h, "Full Doc", "## Full content here", "wf-2", "api")

	req := httptest.NewRequest(http.MethodGet, "/docs/"+created.ID, nil)
	w := httptest.NewRecorder()
	h.handleDocByID(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var d doc
	if err := json.NewDecoder(w.Body).Decode(&d); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if d.Content != "## Full content here" {
		t.Errorf("got content %q, want %q", d.Content, "## Full content here")
	}
	if d.WorkflowID != "wf-2" {
		t.Errorf("got workflow_id %q, want %q", d.WorkflowID, "wf-2")
	}
}

func TestUpdateDoc(t *testing.T) {
	h := setupTestHandler(t)
	created := createTestDoc(t, h, "Original", "old content", "", "")

	// Sleep briefly to ensure updated_at timestamp differs from created_at.
	time.Sleep(1100 * time.Millisecond)

	body := `{"title":"Updated","content":"new content","category":"updated-cat"}`
	req := httptest.NewRequest(http.MethodPut, "/docs/"+created.ID, bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.handleDocByID(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var d doc
	if err := json.NewDecoder(w.Body).Decode(&d); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if d.Title != "Updated" {
		t.Errorf("got title %q, want %q", d.Title, "Updated")
	}
	if d.Content != "new content" {
		t.Errorf("got content %q, want %q", d.Content, "new content")
	}
	if d.Category != "updated-cat" {
		t.Errorf("got category %q, want %q", d.Category, "updated-cat")
	}
	if d.UpdatedAt == created.UpdatedAt {
		t.Error("expected updated_at to change")
	}
}

func TestDeleteDoc(t *testing.T) {
	h := setupTestHandler(t)
	created := createTestDoc(t, h, "To Delete", "delete me", "", "")

	// Delete
	req := httptest.NewRequest(http.MethodDelete, "/docs/"+created.ID, nil)
	w := httptest.NewRecorder()
	h.handleDocByID(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: got status %d, want %d", w.Code, http.StatusNoContent)
	}

	// Verify gone
	req = httptest.NewRequest(http.MethodGet, "/docs/"+created.ID, nil)
	w = httptest.NewRecorder()
	h.handleDocByID(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("get after delete: got status %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestListCategories(t *testing.T) {
	h := setupTestHandler(t)
	createTestDoc(t, h, "Doc 1", "c", "", "guides")
	createTestDoc(t, h, "Doc 2", "c", "", "api")
	createTestDoc(t, h, "Doc 3", "c", "", "guides") // duplicate category
	createTestDoc(t, h, "Doc 4", "c", "", "")       // empty category — should be excluded

	req := httptest.NewRequest(http.MethodGet, "/categories", nil)
	w := httptest.NewRecorder()
	h.handleCategories(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var categories []string
	if err := json.NewDecoder(w.Body).Decode(&categories); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(categories) != 2 {
		t.Fatalf("got %d categories, want 2", len(categories))
	}
	if categories[0] != "api" {
		t.Errorf("got first category %q, want %q", categories[0], "api")
	}
	if categories[1] != "guides" {
		t.Errorf("got second category %q, want %q", categories[1], "guides")
	}
}

func TestFilterByWorkflowID(t *testing.T) {
	h := setupTestHandler(t)
	createTestDoc(t, h, "Doc A", "c", "wf-1", "")
	createTestDoc(t, h, "Doc B", "c", "wf-2", "")
	createTestDoc(t, h, "Doc C", "c", "wf-1", "")

	req := httptest.NewRequest(http.MethodGet, "/docs?workflow_id=wf-1", nil)
	w := httptest.NewRecorder()
	h.handleDocs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var docs []docSummary
	if err := json.NewDecoder(w.Body).Decode(&docs); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("got %d docs, want 2", len(docs))
	}
	for _, d := range docs {
		if d.WorkflowID != "wf-1" {
			t.Errorf("got workflow_id %q, want %q", d.WorkflowID, "wf-1")
		}
	}
}

func TestSearchDocs(t *testing.T) {
	h := setupTestHandler(t)
	createTestDoc(t, h, "Getting Started Guide", "c", "", "")
	createTestDoc(t, h, "API Reference", "c", "", "")
	createTestDoc(t, h, "Advanced Guide", "c", "", "")

	req := httptest.NewRequest(http.MethodGet, "/docs?search=Guide", nil)
	w := httptest.NewRecorder()
	h.handleDocs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var docs []docSummary
	if err := json.NewDecoder(w.Body).Decode(&docs); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("got %d docs, want 2", len(docs))
	}
}
