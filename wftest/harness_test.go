package wftest_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/wftest"
)

func TestHarness_POST_SimpleRoute(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
modules:
  - name: router
    type: http.router
pipelines:
  create-user:
    trigger:
      type: http
      config:
        path: /api/users
        method: POST
    steps:
      - name: respond
        type: step.json_response
        config:
          status: 201
          body:
            created: true
`))

	result := h.POST("/api/users", `{"email":"test@example.com"}`)
	if result.StatusCode != 201 {
		t.Errorf("expected 201, got %d", result.StatusCode)
	}
	body := result.JSON()
	if body["created"] != true {
		t.Errorf("expected created=true, got %v", body["created"])
	}
}

func TestHarness_GET_SimpleRoute(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
modules:
  - name: router
    type: http.router
pipelines:
  get-status:
    trigger:
      type: http
      config:
        path: /status
        method: GET
    steps:
      - name: respond
        type: step.json_response
        config:
          status: 200
          body:
            ok: true
`))

	result := h.GET("/status")
	if result.StatusCode != 200 {
		t.Errorf("expected 200, got %d", result.StatusCode)
	}
	body := result.JSON()
	if body["ok"] != true {
		t.Errorf("expected ok=true, got %v", body["ok"])
	}
}

func TestHarness_DELETE_Route(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
modules:
  - name: router
    type: http.router
pipelines:
  delete-item:
    trigger:
      type: http
      config:
        path: /api/items/{id}
        method: DELETE
    steps:
      - name: respond
        type: step.json_response
        config:
          status: 204
`))

	result := h.DELETE("/api/items/42")
	if result.StatusCode != 204 {
		t.Errorf("expected 204, got %d", result.StatusCode)
	}
}

func TestHarness_POST_WithHeader(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
modules:
  - name: router
    type: http.router
pipelines:
  auth-check:
    trigger:
      type: http
      config:
        path: /api/check
        method: POST
    steps:
      - name: respond
        type: step.json_response
        config:
          status: 200
          body:
            ok: true
`))

	result := h.POST("/api/check", `{}`, wftest.Header("Authorization", "Bearer test-token"))
	if result.StatusCode != 200 {
		t.Errorf("expected 200, got %d", result.StatusCode)
	}
}

func TestHarness_GET_MissingRoute(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
modules:
  - name: router
    type: http.router
`))

	result := h.GET("/not-found")
	if result.StatusCode != 404 {
		t.Errorf("expected 404, got %d", result.StatusCode)
	}
}

func TestHarness_ExecutePipeline_SetStep(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  greet:
    steps:
      - name: set_greeting
        type: step.set
        config:
          values:
            message: "hello world"
`))

	result := h.ExecutePipeline("greet", nil)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Output["message"] != "hello world" {
		t.Errorf("expected 'hello world', got %v", result.Output["message"])
	}
}

func TestHarness_ExecutePipeline_WithInput(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  echo:
    steps:
      - name: copy
        type: step.set
        config:
          values:
            echoed: "{{ .input_val }}"
`))

	result := h.ExecutePipeline("echo", map[string]any{"input_val": "test123"})
	if result.Output["echoed"] != "test123" {
		t.Errorf("expected 'test123', got %v", result.Output["echoed"])
	}
}

func TestHarness_WithConfig_LoadsFile(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  simple:
    steps:
      - name: done
        type: step.set
        config:
          values:
            status: "ok"
`))
	result := h.ExecutePipeline("simple", nil)
	if result.Output["status"] != "ok" {
		t.Errorf("expected 'ok', got %v", result.Output["status"])
	}
}

func TestHarness_ExecutePipeline_NotFound(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  exists:
    steps:
      - name: s
        type: step.set
        config:
          values: { x: 1 }
`))
	result := h.ExecutePipeline("does-not-exist", nil)
	if result.Error == nil {
		t.Error("expected error for missing pipeline")
	}
}
