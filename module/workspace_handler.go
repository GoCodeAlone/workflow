package module

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/GoCodeAlone/workflow/store"
)

// WorkspaceHandler handles file management API endpoints for project workspaces.
type WorkspaceHandler struct {
	workspaces *store.WorkspaceManager
}

// NewWorkspaceHandler creates a new handler backed by the given workspace manager.
func NewWorkspaceHandler(wm *store.WorkspaceManager) *WorkspaceHandler {
	return &WorkspaceHandler{workspaces: wm}
}

// HandleWorkspace dispatches workspace file API requests.
// Expected paths:
//
//	POST   /api/v1/workspaces/{project-id}/files       (upload)
//	GET    /api/v1/workspaces/{project-id}/files        (list)
//	GET    /api/v1/workspaces/{project-id}/files/{path} (download)
//	DELETE /api/v1/workspaces/{project-id}/files/{path} (delete)
//	POST   /api/v1/workspaces/{project-id}/mkdir        (create directory)
func (h *WorkspaceHandler) HandleWorkspace(w http.ResponseWriter, r *http.Request) {
	// Check for mkdir endpoint first
	if r.Method == http.MethodPost && isMkdirPath(r.URL.Path) {
		projectID, ok := parseMkdirPath(r.URL.Path)
		if !ok || projectID == "" {
			writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid workspace path"})
			return
		}

		storage, err := h.workspaces.StorageForProject(projectID)
		if err != nil {
			writeWorkspaceJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("storage error: %v", err)})
			return
		}

		h.handleMkdir(w, r, storage)
		return
	}

	// Extract project ID and file path from URL.
	// Expected format: .../workspaces/{project-id}/files[/{path...}]
	projectID, filePath, ok := parseWorkspacePath(r.URL.Path)
	if !ok || projectID == "" {
		writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid workspace path"})
		return
	}

	storage, err := h.workspaces.StorageForProject(projectID)
	if err != nil {
		writeWorkspaceJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("storage error: %v", err)})
		return
	}

	switch r.Method {
	case http.MethodGet:
		if filePath == "" || filePath == "/" {
			h.handleListFiles(w, r, storage)
		} else {
			h.handleDownloadFile(w, r, storage, filePath)
		}
	case http.MethodPost:
		h.handleUploadFile(w, r, storage)
	case http.MethodDelete:
		h.handleDeleteFile(w, r, storage, filePath)
	default:
		writeWorkspaceJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (h *WorkspaceHandler) handleListFiles(w http.ResponseWriter, r *http.Request, storage *store.LocalStorage) {
	prefix := r.URL.Query().Get("prefix")
	files, err := storage.List(r.Context(), prefix)
	if err != nil {
		writeWorkspaceJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("list files: %v", err)})
		return
	}
	writeWorkspaceJSON(w, http.StatusOK, files)
}

func (h *WorkspaceHandler) handleDownloadFile(w http.ResponseWriter, r *http.Request, storage *store.LocalStorage, filePath string) {
	rc, err := storage.Get(r.Context(), filePath)
	if err != nil {
		writeWorkspaceJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("file not found: %v", err)})
		return
	}
	defer rc.Close()

	// Try to detect content type from extension
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, rc); err != nil {
		// Headers already sent; can't change status
		return
	}
}

func (h *WorkspaceHandler) handleUploadFile(w http.ResponseWriter, r *http.Request, storage *store.LocalStorage) {
	// Parse multipart form (max 32 MB)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("parse form: %v", err)})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{"error": "file field required"})
		return
	}
	defer file.Close()

	// Use the "path" form field if provided, otherwise use the original filename
	uploadPath := r.FormValue("path")
	if uploadPath == "" {
		uploadPath = header.Filename
	}

	if err := storage.Put(r.Context(), uploadPath, file); err != nil {
		writeWorkspaceJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("upload failed: %v", err)})
		return
	}

	info, err := storage.Stat(r.Context(), uploadPath)
	if err != nil {
		writeWorkspaceJSON(w, http.StatusCreated, map[string]string{"path": uploadPath, "status": "uploaded"})
		return
	}
	writeWorkspaceJSON(w, http.StatusCreated, info)
}

func (h *WorkspaceHandler) handleDeleteFile(w http.ResponseWriter, r *http.Request, storage *store.LocalStorage, filePath string) {
	if filePath == "" || filePath == "/" {
		writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{"error": "file path required"})
		return
	}

	if err := storage.Delete(r.Context(), filePath); err != nil {
		writeWorkspaceJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("delete failed: %v", err)})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *WorkspaceHandler) handleMkdir(w http.ResponseWriter, r *http.Request, storage *store.LocalStorage) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Path == "" {
		writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	if err := storage.MkdirAll(r.Context(), req.Path); err != nil {
		writeWorkspaceJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("mkdir failed: %v", err)})
		return
	}

	info, err := storage.Stat(r.Context(), req.Path)
	if err != nil {
		writeWorkspaceJSON(w, http.StatusCreated, map[string]string{"path": req.Path, "status": "created"})
		return
	}
	writeWorkspaceJSON(w, http.StatusCreated, info)
}

// parseWorkspacePath extracts the project ID and file path from a URL path.
// Expected format: .../workspaces/{project-id}/files[/{path...}]
func parseWorkspacePath(urlPath string) (projectID, filePath string, ok bool) {
	idx := strings.Index(urlPath, "/workspaces/")
	if idx < 0 {
		return "", "", false
	}
	rest := urlPath[idx+len("/workspaces/"):]

	// Find /files separator
	filesIdx := strings.Index(rest, "/files")
	if filesIdx < 0 {
		return "", "", false
	}

	projectID = rest[:filesIdx]
	if projectID == "" {
		return "", "", false
	}

	afterFiles := rest[filesIdx+len("/files"):]
	if afterFiles == "" || afterFiles == "/" {
		filePath = ""
	} else {
		filePath = strings.TrimPrefix(afterFiles, "/")
	}
	return projectID, filePath, true
}

// isMkdirPath checks if the URL path targets the mkdir endpoint.
func isMkdirPath(urlPath string) bool {
	idx := strings.Index(urlPath, "/workspaces/")
	if idx < 0 {
		return false
	}
	rest := urlPath[idx+len("/workspaces/"):]
	return strings.HasSuffix(rest, "/mkdir")
}

// parseMkdirPath extracts the project ID from a mkdir URL path.
// Expected format: .../workspaces/{project-id}/mkdir
func parseMkdirPath(urlPath string) (projectID string, ok bool) {
	idx := strings.Index(urlPath, "/workspaces/")
	if idx < 0 {
		return "", false
	}
	rest := urlPath[idx+len("/workspaces/"):]
	if !strings.HasSuffix(rest, "/mkdir") {
		return "", false
	}
	projectID = rest[:len(rest)-len("/mkdir")]
	if projectID == "" {
		return "", false
	}
	return projectID, true
}

func writeWorkspaceJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
