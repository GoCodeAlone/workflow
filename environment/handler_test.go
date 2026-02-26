package environment

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// setupTestServer creates a SQLiteStore in a temp dir and returns an
// http.Handler with all environment routes registered.
func setupTestServer(t *testing.T) (*Handler, *http.ServeMux) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "env_test.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	handler := NewHandler(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return handler, mux
}

func TestCRUDLifecycle(t *testing.T) {
	_, mux := setupTestServer(t)

	// Start a test server to act as the environment endpoint for connectivity checks.
	testEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer testEndpoint.Close()

	// --- Create ---
	createBody := `{"name":"staging","provider":"aws","workflow_id":"wf-1","region":"us-east-1","config":{"instance_type":"t3.medium","endpoint":"` + testEndpoint.URL + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/environments", bytes.NewBufferString(createBody))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created Environment
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("Create decode: %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create: ID should not be empty")
	}
	if created.Name != "staging" {
		t.Fatalf("Create: expected name=staging, got %s", created.Name)
	}
	if created.Provider != "aws" {
		t.Fatalf("Create: expected provider=aws, got %s", created.Provider)
	}

	envID := created.ID

	// --- Get ---
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/environments/"+envID, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Get: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var fetched Environment
	if err := json.NewDecoder(w.Body).Decode(&fetched); err != nil {
		t.Fatalf("Get decode: %v", err)
	}
	if fetched.ID != envID {
		t.Fatalf("Get: expected ID=%s, got %s", envID, fetched.ID)
	}

	// --- List (no filter) ---
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/environments", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("List: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var listed []Environment
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("List decode: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("List: expected 1 environment, got %d", len(listed))
	}

	// --- List (with filter) ---
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/environments?provider=aws", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("List filtered: expected 200, got %d", w.Code)
	}

	var filteredList []Environment
	if err := json.NewDecoder(w.Body).Decode(&filteredList); err != nil {
		t.Fatalf("List filtered decode: %v", err)
	}
	if len(filteredList) != 1 {
		t.Fatalf("List filtered: expected 1, got %d", len(filteredList))
	}

	// --- Update ---
	updateBody := `{"name":"production","provider":"aws","workflow_id":"wf-1","region":"us-west-2","status":"active","config":{"instance_type":"m5.large","endpoint":"` + testEndpoint.URL + `"}}`
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/environments/"+envID, bytes.NewBufferString(updateBody))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Update: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated Environment
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("Update decode: %v", err)
	}
	if updated.Name != "production" {
		t.Fatalf("Update: expected name=production, got %s", updated.Name)
	}
	if updated.Status != "active" {
		t.Fatalf("Update: expected status=active, got %s", updated.Status)
	}

	// --- Test Connection ---
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/environments/"+envID+"/test", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("TestConnection: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var testResult ConnectionTestResult
	if err := json.NewDecoder(w.Body).Decode(&testResult); err != nil {
		t.Fatalf("TestConnection decode: %v", err)
	}
	if !testResult.Success {
		t.Fatal("TestConnection: expected success=true")
	}

	// --- Delete ---
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/environments/"+envID, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Delete: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// --- Verify deleted ---
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/environments/"+envID, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Get after delete: expected 404, got %d", w.Code)
	}
}

func TestCreateValidation(t *testing.T) {
	_, mux := setupTestServer(t)

	// Missing required fields
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/environments", bytes.NewBufferString(`{"name":"test"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Create without provider: expected 400, got %d", w.Code)
	}
}

func TestGetNotFound(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/environments/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Get nonexistent: expected 404, got %d", w.Code)
	}
}

func TestDeleteNotFound(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/environments/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Delete nonexistent: expected 404, got %d", w.Code)
	}
}

func TestUpdateNotFound(t *testing.T) {
	_, mux := setupTestServer(t)

	body := `{"name":"test","provider":"aws","workflow_id":"wf-1"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/environments/nonexistent", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Update nonexistent: expected 404, got %d", w.Code)
	}
}

func TestListEmpty(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/environments", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("List empty: expected 200, got %d", w.Code)
	}

	var envs []Environment
	if err := json.NewDecoder(w.Body).Decode(&envs); err != nil {
		t.Fatalf("List empty decode: %v", err)
	}
	if len(envs) != 0 {
		t.Fatalf("List empty: expected 0 environments, got %d", len(envs))
	}
}

func TestTestConnectionNotFound(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/environments/nonexistent/test", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("TestConnection nonexistent: expected 404, got %d", w.Code)
	}
}
