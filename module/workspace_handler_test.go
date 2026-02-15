package module

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/store"
)

func setupWorkspaceHandler(t *testing.T) *WorkspaceHandler {
	t.Helper()
	dir := t.TempDir()
	wm := store.NewWorkspaceManager(dir)
	return NewWorkspaceHandler(wm)
}

func createMultipartRequest(t *testing.T, url, fieldName, fileName string, content []byte, extraFields map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add file field
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("Write file content: %v", err)
	}

	// Add extra fields
	for k, v := range extraFields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatalf("WriteField %s: %v", k, err)
		}
	}

	writer.Close()

	req := httptest.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestWorkspaceHandler_UploadAndList(t *testing.T) {
	h := setupWorkspaceHandler(t)

	// Upload a file
	req := createMultipartRequest(t,
		"/api/v1/workspaces/proj-1/files",
		"file", "test.txt", []byte("hello world"), nil,
	)
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("upload: got status %d, want %d: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	// List files
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/proj-1/files", nil)
	rr = httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list: got status %d, want %d: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var files []store.FileInfo
	if err := json.NewDecoder(rr.Body).Decode(&files); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}
	if len(files) > 0 && files[0].Name != "test.txt" {
		t.Errorf("expected file name 'test.txt', got %q", files[0].Name)
	}
}

func TestWorkspaceHandler_UploadWithPath(t *testing.T) {
	h := setupWorkspaceHandler(t)

	// Upload a file with custom path
	req := createMultipartRequest(t,
		"/api/v1/workspaces/proj-1/files",
		"file", "original.txt", []byte("custom path"),
		map[string]string{"path": "subdir/renamed.txt"},
	)
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("upload: got status %d, want %d: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	// Download the file by the custom path
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/proj-1/files/subdir/renamed.txt", nil)
	rr = httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("download: got status %d, want %d: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	body, _ := io.ReadAll(rr.Body)
	if string(body) != "custom path" {
		t.Errorf("expected 'custom path', got %q", string(body))
	}
}

func TestWorkspaceHandler_Download(t *testing.T) {
	h := setupWorkspaceHandler(t)

	// Upload
	content := []byte("download me")
	req := createMultipartRequest(t,
		"/api/v1/workspaces/proj-1/files",
		"file", "dl.txt", content, nil,
	)
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("upload: got status %d", rr.Code)
	}

	// Download
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/proj-1/files/dl.txt", nil)
	rr = httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("download: got status %d, want %d", rr.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rr.Body)
	if !bytes.Equal(body, content) {
		t.Errorf("content mismatch: got %q, want %q", body, content)
	}
}

func TestWorkspaceHandler_DownloadNotFound(t *testing.T) {
	h := setupWorkspaceHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/proj-1/files/missing.txt", nil)
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("got status %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestWorkspaceHandler_Delete(t *testing.T) {
	h := setupWorkspaceHandler(t)

	// Upload
	req := createMultipartRequest(t,
		"/api/v1/workspaces/proj-1/files",
		"file", "todelete.txt", []byte("bye"), nil,
	)
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("upload: got status %d", rr.Code)
	}

	// Delete
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/proj-1/files/todelete.txt", nil)
	rr = httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete: got status %d, want %d: %s", rr.Code, http.StatusNoContent, rr.Body.String())
	}

	// Verify it's gone
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/proj-1/files/todelete.txt", nil)
	rr = httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rr.Code)
	}
}

func TestWorkspaceHandler_DeleteNoPath(t *testing.T) {
	h := setupWorkspaceHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/proj-1/files", nil)
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestWorkspaceHandler_DeleteNotFound(t *testing.T) {
	h := setupWorkspaceHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/proj-1/files/nonexistent.txt", nil)
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("got status %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestWorkspaceHandler_Mkdir(t *testing.T) {
	h := setupWorkspaceHandler(t)

	// Create directory
	body := `{"path": "data/configs"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/proj-1/mkdir", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("mkdir: got status %d, want %d: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	// Verify directory exists by listing it (should return empty)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/proj-1/files?prefix=data/configs", nil)
	rr = httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list after mkdir: got status %d, want %d", rr.Code, http.StatusOK)
	}

	var files []store.FileInfo
	if err := json.NewDecoder(rr.Body).Decode(&files); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files in new dir, got %d", len(files))
	}
}

func TestWorkspaceHandler_MkdirNoPath(t *testing.T) {
	h := setupWorkspaceHandler(t)

	body := `{"path": ""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/proj-1/mkdir", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestWorkspaceHandler_MkdirBadJSON(t *testing.T) {
	h := setupWorkspaceHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/proj-1/mkdir", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestWorkspaceHandler_InvalidPath(t *testing.T) {
	h := setupWorkspaceHandler(t)

	// Missing /files segment
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/proj-1/invalid", nil)
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestWorkspaceHandler_MethodNotAllowed(t *testing.T) {
	h := setupWorkspaceHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/proj-1/files/test.txt", nil)
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got status %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestWorkspaceHandler_ListWithPrefix(t *testing.T) {
	h := setupWorkspaceHandler(t)

	// Upload files in a subdirectory
	req := createMultipartRequest(t,
		"/api/v1/workspaces/proj-1/files",
		"file", "test.txt", []byte("root file"),
		map[string]string{"path": "subdir/nested.txt"},
	)
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("upload: got status %d", rr.Code)
	}

	// List with prefix
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/proj-1/files?prefix=subdir", nil)
	rr = httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list: got status %d", rr.Code)
	}

	var files []store.FileInfo
	json.NewDecoder(rr.Body).Decode(&files)
	if len(files) != 1 {
		t.Errorf("expected 1 file in subdir, got %d", len(files))
	}
}

func TestWorkspaceHandler_ProjectIsolation(t *testing.T) {
	h := setupWorkspaceHandler(t)

	// Upload to project 1
	req := createMultipartRequest(t,
		"/api/v1/workspaces/proj-1/files",
		"file", "proj1.txt", []byte("proj1 data"), nil,
	)
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("upload proj-1: got status %d", rr.Code)
	}

	// Upload to project 2
	req = createMultipartRequest(t,
		"/api/v1/workspaces/proj-2/files",
		"file", "proj2.txt", []byte("proj2 data"), nil,
	)
	rr = httptest.NewRecorder()
	h.HandleWorkspace(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("upload proj-2: got status %d", rr.Code)
	}

	// List project 1 - should only see proj1 files
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/proj-1/files", nil)
	rr = httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	var files1 []store.FileInfo
	json.NewDecoder(rr.Body).Decode(&files1)
	if len(files1) != 1 {
		t.Errorf("proj-1: expected 1 file, got %d", len(files1))
	}

	// List project 2 - should only see proj2 files
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/proj-2/files", nil)
	rr = httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	var files2 []store.FileInfo
	json.NewDecoder(rr.Body).Decode(&files2)
	if len(files2) != 1 {
		t.Errorf("proj-2: expected 1 file, got %d", len(files2))
	}
}

func TestWorkspaceHandler_UploadMissingFile(t *testing.T) {
	h := setupWorkspaceHandler(t)

	// POST without multipart file field
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/proj-1/files", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.HandleWorkspace(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d: %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

// --- parseWorkspacePath tests ---

func TestParseWorkspacePath(t *testing.T) {
	tests := []struct {
		input    string
		wantPID  string
		wantPath string
		wantOK   bool
	}{
		{"/api/v1/workspaces/proj-1/files", "proj-1", "", true},
		{"/api/v1/workspaces/proj-1/files/", "proj-1", "", true},
		{"/api/v1/workspaces/proj-1/files/subdir/file.txt", "proj-1", "subdir/file.txt", true},
		{"/api/v1/workspaces/proj-1/files/file.txt", "proj-1", "file.txt", true},
		{"/api/v1/workspaces//files", "", "", false},       // empty project ID
		{"/api/v1/other/proj-1/files", "", "", false},      // no /workspaces/
		{"/api/v1/workspaces/proj-1/other", "", "", false}, // no /files
	}

	for _, tt := range tests {
		pid, fp, ok := parseWorkspacePath(tt.input)
		if ok != tt.wantOK {
			t.Errorf("parseWorkspacePath(%q): ok=%v, want %v", tt.input, ok, tt.wantOK)
			continue
		}
		if pid != tt.wantPID {
			t.Errorf("parseWorkspacePath(%q): projectID=%q, want %q", tt.input, pid, tt.wantPID)
		}
		if fp != tt.wantPath {
			t.Errorf("parseWorkspacePath(%q): filePath=%q, want %q", tt.input, fp, tt.wantPath)
		}
	}
}

func TestParseMkdirPath(t *testing.T) {
	tests := []struct {
		input   string
		wantPID string
		wantOK  bool
	}{
		{"/api/v1/workspaces/proj-1/mkdir", "proj-1", true},
		{"/api/v1/workspaces/abc-123/mkdir", "abc-123", true},
		{"/api/v1/workspaces//mkdir", "", false},
		{"/api/v1/workspaces/proj-1/files", "", false},
		{"/api/v1/other/proj-1/mkdir", "", false},
	}

	for _, tt := range tests {
		pid, ok := parseMkdirPath(tt.input)
		if ok != tt.wantOK {
			t.Errorf("parseMkdirPath(%q): ok=%v, want %v", tt.input, ok, tt.wantOK)
			continue
		}
		if pid != tt.wantPID {
			t.Errorf("parseMkdirPath(%q): projectID=%q, want %q", tt.input, pid, tt.wantPID)
		}
	}
}

func TestIsMkdirPath(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"/api/v1/workspaces/proj-1/mkdir", true},
		{"/api/v1/workspaces/proj-1/files", false},
		{"/api/v1/other/proj-1/mkdir", false},
	}

	for _, tt := range tests {
		got := isMkdirPath(tt.input)
		if got != tt.want {
			t.Errorf("isMkdirPath(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// --- V1APIHandler workspace integration ---

func TestV1Handler_WorkspaceRouting(t *testing.T) {
	handler, _, _ := setupTestHandler(t)

	// Without workspace handler set, workspace paths should 404
	rr := doRequest(handler, "GET", "/api/v1/workspaces/proj-1/files", "", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("without workspace handler: got status %d, want %d", rr.Code, http.StatusNotFound)
	}

	// Set up workspace handler
	dir := t.TempDir()
	wm := store.NewWorkspaceManager(dir)
	wh := NewWorkspaceHandler(wm)
	handler.SetWorkspaceHandler(wh)

	// Now workspace paths should be dispatched
	rr = doRequest(handler, "GET", "/api/v1/workspaces/proj-1/files", "", "")
	// Should succeed (empty list, 200)
	if rr.Code != http.StatusOK {
		t.Fatalf("with workspace handler: got status %d, want %d: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}
