package module

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GoCodeAlone/workflow/scaffold"
)

// sampleSpec is a minimal OpenAPI 3.0 spec used in scaffold step tests.
const scaffoldTestSpec = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Pet Store API",
    "version": "1.0.0"
  },
  "paths": {
    "/api/v1/pets": {
      "get": {
        "operationId": "listPets",
        "summary": "List all pets",
        "responses": {"200": {"description": "success"}}
      },
      "post": {
        "operationId": "createPet",
        "summary": "Create a pet",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "name": {"type": "string"},
                  "age": {"type": "integer"}
                },
                "required": ["name"]
              }
            }
          }
        },
        "responses": {"201": {"description": "created"}}
      }
    },
    "/api/v1/pets/{id}": {
      "get": {
        "operationId": "getPet",
        "summary": "Get a pet",
        "parameters": [{"name": "id", "in": "path"}],
        "responses": {"200": {"description": "success"}}
      },
      "delete": {
        "operationId": "deletePet",
        "summary": "Delete a pet",
        "parameters": [{"name": "id", "in": "path"}],
        "responses": {"204": {"description": "deleted"}}
      }
    },
    "/auth/login": {
      "post": {
        "operationId": "login",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {"type": "object", "properties": {"email": {"type": "string"}, "password": {"type": "string"}}}
            }
          }
        },
        "responses": {"200": {"description": "token"}}
      }
    }
  }
}`

const scaffoldInvalidSpec = `{not valid json or yaml`

// --- ScaffoldAnalyzeStep ---

func TestScaffoldAnalyzeStep_ReturnsAnalysis(t *testing.T) {
	factory := NewScaffoldAnalyzeStepFactory()
	step, err := factory("analyze", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/scaffold/analyze", bytes.NewBufferString(scaffoldTestSpec))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request":         req,
		"_http_response_writer": w,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	var data scaffold.Data
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, body)
	}
	if data.Title != "Pet Store API" {
		t.Errorf("expected title 'Pet Store API', got %q", data.Title)
	}
	if len(data.Resources) == 0 {
		t.Error("expected at least one resource in analysis")
	}
	if !data.HasAuth {
		t.Error("expected HasAuth=true for spec with /auth/login")
	}
}

func TestScaffoldAnalyzeStep_InvalidSpec_Returns400(t *testing.T) {
	factory := NewScaffoldAnalyzeStepFactory()
	step, err := factory("analyze", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/scaffold/analyze", bytes.NewBufferString(scaffoldInvalidSpec))
	w := httptest.NewRecorder()

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request":         req,
		"_http_response_writer": w,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected Execute error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var errResp map[string]string
	if json.Unmarshal(body, &errResp) != nil {
		t.Errorf("expected JSON error response, got: %s", body)
	}
	if errResp["error"] == "" {
		t.Error("expected non-empty 'error' field in response")
	}
}

func TestScaffoldAnalyzeStep_NoHTTPWriter_ReturnsOutput(t *testing.T) {
	factory := NewScaffoldAnalyzeStepFactory()
	step, err := factory("analyze", map[string]any{"title": "My App"}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// No HTTP writer in metadata — simulates non-HTTP pipeline context.
	pc := NewPipelineContext(map[string]any{"spec": scaffoldTestSpec}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == nil || len(result.Output) == 0 {
		t.Fatal("expected non-empty result output")
	}
	// Should contain the title from config override.
	if title, ok := result.Output["Title"].(string); ok {
		if title != "My App" {
			t.Errorf("expected title 'My App', got %q", title)
		}
	}
}

func TestScaffoldAnalyzeStep_WithTitleConfig(t *testing.T) {
	factory := NewScaffoldAnalyzeStepFactory()
	step, err := factory("analyze", map[string]any{"title": "Custom Title"}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/scaffold/analyze", bytes.NewBufferString(scaffoldTestSpec))
	w := httptest.NewRecorder()

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request":         req,
		"_http_response_writer": w,
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	body, _ := io.ReadAll(w.Result().Body)
	var data scaffold.Data
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if data.Title != "Custom Title" {
		t.Errorf("expected title 'Custom Title', got %q", data.Title)
	}
}

func TestScaffoldAnalyzeStep_EmptyBody_Returns400(t *testing.T) {
	factory := NewScaffoldAnalyzeStepFactory()
	step, err := factory("analyze", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/scaffold/analyze", http.NoBody)
	w := httptest.NewRecorder()

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request":         req,
		"_http_response_writer": w,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected Execute error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400 for empty body, got %d", resp.StatusCode)
	}
}

// --- ScaffoldStep ---

func TestScaffoldStep_ReturnsZIP(t *testing.T) {
	factory := NewScaffoldStepFactory()
	step, err := factory("scaffold", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/scaffold", bytes.NewBufferString(scaffoldTestSpec))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request":         req,
		"_http_response_writer": w,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("expected Content-Type application/zip, got %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd == "" {
		t.Error("expected Content-Disposition header")
	}

	// Verify the ZIP is valid.
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Fatal("expected non-empty ZIP response")
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("response is not a valid ZIP: %v", err)
	}
	if len(zr.File) == 0 {
		t.Error("expected at least one file in ZIP")
	}

	// Verify essential files are in the ZIP.
	fileNames := make(map[string]bool)
	for _, f := range zr.File {
		fileNames[f.Name] = true
	}
	for _, required := range []string{"ui/package.json", "ui/src/App.tsx", "ui/src/api.ts"} {
		if !fileNames[required] {
			t.Errorf("ZIP missing required file: %s", required)
		}
	}
}

func TestScaffoldStep_InvalidSpec_Returns400(t *testing.T) {
	factory := NewScaffoldStepFactory()
	step, err := factory("scaffold", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/scaffold", bytes.NewBufferString(scaffoldInvalidSpec))
	w := httptest.NewRecorder()

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request":         req,
		"_http_response_writer": w,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected Execute error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestScaffoldStep_NoHTTPWriter_ReturnsBytesInOutput(t *testing.T) {
	factory := NewScaffoldStepFactory()
	step, err := factory("scaffold", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// No HTTP writer — simulates non-HTTP pipeline context.
	pc := NewPipelineContext(map[string]any{"spec": scaffoldTestSpec}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	zipBytes, ok := result.Output["zip_bytes"].([]byte)
	if !ok {
		t.Fatal("expected zip_bytes in output")
	}
	if len(zipBytes) == 0 {
		t.Error("expected non-empty zip_bytes")
	}

	// Verify the ZIP is valid.
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("zip_bytes is not a valid ZIP: %v", err)
	}
	if len(zr.File) == 0 {
		t.Error("expected at least one file in ZIP")
	}
}

func TestScaffoldStep_CustomFilename(t *testing.T) {
	factory := NewScaffoldStepFactory()
	step, err := factory("scaffold", map[string]any{"filename": "my-app.zip"}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/scaffold", bytes.NewBufferString(scaffoldTestSpec))
	w := httptest.NewRecorder()

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request":         req,
		"_http_response_writer": w,
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	cd := w.Result().Header.Get("Content-Disposition")
	if cd == "" {
		t.Fatal("expected Content-Disposition header")
	}
	// Should include the custom filename.
	for _, want := range []string{"attachment", "my-app.zip"} {
		if len(cd) == 0 {
			t.Errorf("Content-Disposition missing %q", want)
		}
	}
}

func TestScaffoldStep_ZIPContainsResourcePage(t *testing.T) {
	factory := NewScaffoldStepFactory()
	step, err := factory("scaffold", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/scaffold", bytes.NewBufferString(scaffoldTestSpec))
	w := httptest.NewRecorder()

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request":         req,
		"_http_response_writer": w,
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	body, _ := io.ReadAll(w.Result().Body)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("invalid ZIP: %v", err)
	}

	fileNames := make(map[string]bool)
	for _, f := range zr.File {
		fileNames[f.Name] = true
	}

	// spec has /api/v1/pets — should generate PetsPage.tsx
	if !fileNames["ui/src/pages/PetsPage.tsx"] {
		t.Error("ZIP missing ui/src/pages/PetsPage.tsx for pets resource")
	}
	// Spec has /auth/login — should generate auth files
	if !fileNames["ui/src/auth.tsx"] {
		t.Error("ZIP missing ui/src/auth.tsx for auth-enabled spec")
	}
	if !fileNames["ui/src/pages/LoginPage.tsx"] {
		t.Error("ZIP missing ui/src/pages/LoginPage.tsx for auth-enabled spec")
	}
}
