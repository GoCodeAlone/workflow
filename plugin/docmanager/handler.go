package docmanager

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type handler struct {
	db *sql.DB
	mu sync.Mutex
}

func newHandler(db *sql.DB) *handler {
	h := &handler{db: db}
	h.ensureTable()
	return h
}

func (h *handler) ensureTable() {
	h.mu.Lock()
	defer h.mu.Unlock()

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS workflow_docs (
		id TEXT PRIMARY KEY,
		workflow_id TEXT DEFAULT '',
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		category TEXT DEFAULT '',
		created_by TEXT DEFAULT '',
		updated_by TEXT DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_docs_workflow_id ON workflow_docs(workflow_id)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_docs_category ON workflow_docs(category)`,
	}

	for _, stmt := range stmts {
		for attempt := 0; attempt < 5; attempt++ {
			if _, err := h.db.Exec(stmt); err != nil {
				slog.Warn("doc-manager: retrying table init", "attempt", attempt+1, "error", err)
				time.Sleep(time.Duration(100*(attempt+1)) * time.Millisecond)
				continue
			}
			break
		}
	}
}

func (h *handler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/docs", h.handleDocs)
	mux.HandleFunc("/docs/", h.handleDocByID)
	mux.HandleFunc("/categories", h.handleCategories)
}

// docSummary is the JSON representation for list responses (no content).
type docSummary struct {
	ID         string `json:"id"`
	WorkflowID string `json:"workflow_id"`
	Title      string `json:"title"`
	Category   string `json:"category"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// doc is the full JSON representation including content.
type doc struct {
	ID         string `json:"id"`
	WorkflowID string `json:"workflow_id"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	Category   string `json:"category"`
	CreatedBy  string `json:"created_by"`
	UpdatedBy  string `json:"updated_by"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type createDocRequest struct {
	Title      string `json:"title"`
	Content    string `json:"content"`
	WorkflowID string `json:"workflow_id"`
	Category   string `json:"category"`
}

type updateDocRequest struct {
	Title    string `json:"title"`
	Content  string `json:"content"`
	Category string `json:"category"`
}

func (h *handler) handleDocs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listDocs(w, r)
	case http.MethodPost:
		h.createDoc(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *handler) handleDocByID(w http.ResponseWriter, r *http.Request) {
	id := parseDocID(r.URL.Path)
	if id == "" {
		http.Error(w, "doc id required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getDoc(w, id)
	case http.MethodPut:
		h.updateDoc(w, r, id)
	case http.MethodDelete:
		h.deleteDoc(w, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *handler) listDocs(w http.ResponseWriter, r *http.Request) {
	query := `SELECT id, workflow_id, title, category, created_at, updated_at FROM workflow_docs`
	var conditions []string
	var args []any

	if wfID := r.URL.Query().Get("workflow_id"); wfID != "" {
		conditions = append(conditions, "workflow_id = ?")
		args = append(args, wfID)
	}
	if cat := r.URL.Query().Get("category"); cat != "" {
		conditions = append(conditions, "category = ?")
		args = append(args, cat)
	}
	if search := r.URL.Query().Get("search"); search != "" {
		conditions = append(conditions, "title LIKE ?")
		args = append(args, "%"+search+"%")
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY updated_at DESC"

	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query docs: "+err.Error())
		return
	}
	defer rows.Close()

	docs := make([]docSummary, 0)
	for rows.Next() {
		var d docSummary
		if err := rows.Scan(&d.ID, &d.WorkflowID, &d.Title, &d.Category, &d.CreatedAt, &d.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan doc: "+err.Error())
			return
		}
		docs = append(docs, d)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "row iteration error: "+err.Error())
		return
	}

	writeJSONResponse(w, http.StatusOK, docs)
}

func (h *handler) createDoc(w http.ResponseWriter, r *http.Request) {
	var req createDocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.New().String()

	h.mu.Lock()
	_, err := h.db.Exec(
		`INSERT INTO workflow_docs (id, workflow_id, title, content, category, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, req.WorkflowID, req.Title, req.Content, req.Category, now, now,
	)
	h.mu.Unlock()

	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create doc: "+err.Error())
		return
	}

	d := doc{
		ID:         id,
		WorkflowID: req.WorkflowID,
		Title:      req.Title,
		Content:    req.Content,
		Category:   req.Category,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	writeJSONResponse(w, http.StatusCreated, d)
}

func (h *handler) getDoc(w http.ResponseWriter, id string) {
	var d doc
	err := h.db.QueryRow(
		`SELECT id, workflow_id, title, content, category, created_by, updated_by, created_at, updated_at FROM workflow_docs WHERE id = ?`,
		id,
	).Scan(&d.ID, &d.WorkflowID, &d.Title, &d.Content, &d.Category, &d.CreatedBy, &d.UpdatedBy, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "doc not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get doc: "+err.Error())
		return
	}

	writeJSONResponse(w, http.StatusOK, d)
}

func (h *handler) updateDoc(w http.ResponseWriter, r *http.Request, id string) {
	var req updateDocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	h.mu.Lock()
	result, err := h.db.Exec(
		`UPDATE workflow_docs SET title = ?, content = ?, category = ?, updated_at = ? WHERE id = ?`,
		req.Title, req.Content, req.Category, now, id,
	)
	h.mu.Unlock()

	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update doc: "+err.Error())
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeError(w, http.StatusNotFound, "doc not found")
		return
	}

	h.getDoc(w, id)
}

func (h *handler) deleteDoc(w http.ResponseWriter, id string) {
	h.mu.Lock()
	result, err := h.db.Exec(`DELETE FROM workflow_docs WHERE id = ?`, id)
	h.mu.Unlock()

	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete doc: "+err.Error())
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeError(w, http.StatusNotFound, "doc not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) handleCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := h.db.Query(`SELECT DISTINCT category FROM workflow_docs WHERE category != '' ORDER BY category`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query categories: "+err.Error())
		return
	}
	defer rows.Close()

	categories := make([]string, 0)
	for rows.Next() {
		var cat string
		if err := rows.Scan(&cat); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan category: "+err.Error())
			return
		}
		categories = append(categories, cat)
	}

	writeJSONResponse(w, http.StatusOK, categories)
}

func writeJSONResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func parseDocID(path string) string {
	// path is like /docs/{id} or /docs/{id}/...
	trimmed := strings.TrimPrefix(path, "/docs/")
	if idx := strings.Index(trimmed, "/"); idx >= 0 {
		return trimmed[:idx]
	}
	return trimmed
}
