package versioning

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVersionStore_Save(t *testing.T) {
	s := NewVersionStore()

	v1, err := s.Save("my-workflow", "config: v1", "initial", "alice")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if v1.Version != 1 {
		t.Errorf("expected version 1, got %d", v1.Version)
	}
	if v1.WorkflowName != "my-workflow" {
		t.Errorf("expected my-workflow, got %s", v1.WorkflowName)
	}

	v2, err := s.Save("my-workflow", "config: v2", "update", "bob")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if v2.Version != 2 {
		t.Errorf("expected version 2, got %d", v2.Version)
	}
}

func TestVersionStore_SaveValidation(t *testing.T) {
	s := NewVersionStore()

	_, err := s.Save("", "config", "", "")
	if err == nil {
		t.Error("expected error for empty name")
	}

	_, err = s.Save("wf", "", "", "")
	if err == nil {
		t.Error("expected error for empty config")
	}
}

func TestVersionStore_Get(t *testing.T) {
	s := NewVersionStore()
	s.Save("wf", "v1", "", "")
	s.Save("wf", "v2", "", "")

	v, ok := s.Get("wf", 1)
	if !ok || v.ConfigYAML != "v1" {
		t.Error("get v1 failed")
	}

	v, ok = s.Get("wf", 2)
	if !ok || v.ConfigYAML != "v2" {
		t.Error("get v2 failed")
	}

	_, ok = s.Get("wf", 3)
	if ok {
		t.Error("expected not found for v3")
	}

	_, ok = s.Get("nonexistent", 1)
	if ok {
		t.Error("expected not found for nonexistent workflow")
	}
}

func TestVersionStore_Latest(t *testing.T) {
	s := NewVersionStore()

	_, ok := s.Latest("wf")
	if ok {
		t.Error("expected not found for empty store")
	}

	s.Save("wf", "v1", "", "")
	s.Save("wf", "v2", "", "")

	v, ok := s.Latest("wf")
	if !ok || v.Version != 2 {
		t.Error("expected latest to be v2")
	}
}

func TestVersionStore_List(t *testing.T) {
	s := NewVersionStore()
	s.Save("wf", "v1", "", "")
	s.Save("wf", "v2", "", "")
	s.Save("wf", "v3", "", "")

	list := s.List("wf")
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
	// Newest first
	if list[0].Version != 3 {
		t.Errorf("expected v3 first, got v%d", list[0].Version)
	}
	if list[2].Version != 1 {
		t.Errorf("expected v1 last, got v%d", list[2].Version)
	}
}

func TestVersionStore_ListWorkflows(t *testing.T) {
	s := NewVersionStore()
	s.Save("bravo", "cfg", "", "")
	s.Save("alpha", "cfg", "", "")

	names := s.ListWorkflows()
	if len(names) != 2 {
		t.Fatalf("expected 2, got %d", len(names))
	}
	// Sorted alphabetically
	if names[0] != "alpha" || names[1] != "bravo" {
		t.Errorf("expected [alpha bravo], got %v", names)
	}
}

func TestVersionStore_Count(t *testing.T) {
	s := NewVersionStore()
	if s.Count("wf") != 0 {
		t.Error("expected 0")
	}
	s.Save("wf", "v1", "", "")
	s.Save("wf", "v2", "", "")
	if s.Count("wf") != 2 {
		t.Errorf("expected 2, got %d", s.Count("wf"))
	}
}

func TestRollback(t *testing.T) {
	s := NewVersionStore()
	s.Save("wf", "config-v1", "initial", "alice")
	s.Save("wf", "config-v2", "update", "bob")

	var appliedConfig string
	apply := func(name, config string) error {
		appliedConfig = config
		return nil
	}

	v, err := Rollback(s, "wf", 1, "alice", apply)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if v.Version != 3 {
		t.Errorf("expected version 3 (new), got %d", v.Version)
	}
	if v.ConfigYAML != "config-v1" {
		t.Errorf("expected config-v1, got %s", v.ConfigYAML)
	}
	if appliedConfig != "config-v1" {
		t.Errorf("apply not called with correct config")
	}
	if !strings.Contains(v.Description, "Rollback to version 1") {
		t.Errorf("expected rollback description, got %q", v.Description)
	}
}

func TestRollback_NotFound(t *testing.T) {
	s := NewVersionStore()
	_, err := Rollback(s, "wf", 1, "", nil)
	if err == nil {
		t.Error("expected error for nonexistent version")
	}
}

func TestRollback_ApplyError(t *testing.T) {
	s := NewVersionStore()
	s.Save("wf", "cfg", "", "")

	apply := func(name, config string) error {
		return fmt.Errorf("apply failed")
	}

	_, err := Rollback(s, "wf", 1, "", apply)
	if err == nil {
		t.Error("expected error when apply fails")
	}
	// Should not have created a new version
	if s.Count("wf") != 1 {
		t.Errorf("expected 1 version (no new version on apply failure), got %d", s.Count("wf"))
	}
}

func TestRollback_NilApply(t *testing.T) {
	s := NewVersionStore()
	s.Save("wf", "cfg", "", "")

	v, err := Rollback(s, "wf", 1, "", nil)
	if err != nil {
		t.Fatalf("rollback with nil apply: %v", err)
	}
	if v.Version != 2 {
		t.Errorf("expected version 2, got %d", v.Version)
	}
}

func TestCompare(t *testing.T) {
	s := NewVersionStore()
	s.Save("wf", "config-a", "", "")
	s.Save("wf", "config-b", "", "")

	diff, err := Compare(s, "wf", 1, 2)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if !diff.Changed {
		t.Error("expected changed to be true")
	}
	if diff.FromConfig != "config-a" || diff.ToConfig != "config-b" {
		t.Error("incorrect diff contents")
	}
}

func TestCompare_Same(t *testing.T) {
	s := NewVersionStore()
	s.Save("wf", "same-config", "", "")
	s.Save("wf", "same-config", "", "")

	diff, err := Compare(s, "wf", 1, 2)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if diff.Changed {
		t.Error("expected changed to be false")
	}
}

func TestCompare_NotFound(t *testing.T) {
	s := NewVersionStore()
	s.Save("wf", "cfg", "", "")

	_, err := Compare(s, "wf", 1, 99)
	if err == nil {
		t.Error("expected error for missing version")
	}
}

// --- HTTP handler tests ---

func TestHandler_ListVersions(t *testing.T) {
	s := NewVersionStore()
	s.Save("my-wf", "v1", "", "")
	s.Save("my-wf", "v2", "", "")

	h := NewHandler(s, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/workflows/my-wf/versions", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["total"].(float64) != 2 {
		t.Errorf("expected 2 versions, got %v", resp["total"])
	}
}

func TestHandler_GetVersion(t *testing.T) {
	s := NewVersionStore()
	s.Save("wf", "config-v1", "", "")

	h := NewHandler(s, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/workflows/wf/versions/1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandler_GetVersionNotFound(t *testing.T) {
	s := NewVersionStore()
	h := NewHandler(s, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/workflows/wf/versions/1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandler_CreateVersion(t *testing.T) {
	s := NewVersionStore()
	h := NewHandler(s, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"configYaml":"modules: []","description":"test","createdBy":"alice"}`
	req := httptest.NewRequest("POST", "/api/workflows/new-wf/versions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_Rollback(t *testing.T) {
	s := NewVersionStore()
	s.Save("wf", "config-v1", "", "")
	s.Save("wf", "config-v2", "", "")

	var applied string
	apply := func(name, config string) error {
		applied = config
		return nil
	}

	h := NewHandler(s, apply)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"createdBy":"admin"}`
	req := httptest.NewRequest("POST", "/api/workflows/wf/rollback/1", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if applied != "config-v1" {
		t.Errorf("expected apply to be called with v1 config")
	}
}

func TestHandler_Diff(t *testing.T) {
	s := NewVersionStore()
	s.Save("wf", "config-a", "", "")
	s.Save("wf", "config-b", "", "")

	h := NewHandler(s, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/workflows/wf/diff?from=1&to=2", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var diff Diff
	_ = json.NewDecoder(rec.Body).Decode(&diff)
	if !diff.Changed {
		t.Error("expected changed")
	}
}

func TestHandler_ListWorkflows(t *testing.T) {
	s := NewVersionStore()
	s.Save("wf-a", "cfg", "", "")
	s.Save("wf-b", "cfg", "", "")

	h := NewHandler(s, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/workflows", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	workflows := resp["workflows"].([]any)
	if len(workflows) != 2 {
		t.Errorf("expected 2 workflows, got %d", len(workflows))
	}
}
