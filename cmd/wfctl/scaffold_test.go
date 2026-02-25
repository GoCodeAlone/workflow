package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleOpenAPISpec is a comprehensive OpenAPI 3.0 spec used across CLI tests.
const sampleOpenAPISpec = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Pet Store API",
    "version": "1.0.0",
    "description": "A sample pet store API"
  },
  "paths": {
    "/api/v1/pets": {
      "get": {
        "operationId": "listPets",
        "summary": "List all pets",
        "tags": ["pets"],
        "responses": {"200": {"description": "success"}}
      },
      "post": {
        "operationId": "createPet",
        "summary": "Create a pet",
        "tags": ["pets"],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "name": {"type": "string"},
                  "species": {"type": "string", "enum": ["dog", "cat", "bird"]},
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
        "tags": ["pets"],
        "parameters": [{"name": "id", "in": "path", "required": true}],
        "responses": {"200": {"description": "success"}}
      },
      "delete": {
        "operationId": "deletePet",
        "summary": "Delete a pet",
        "tags": ["pets"],
        "parameters": [{"name": "id", "in": "path", "required": true}],
        "responses": {"204": {"description": "deleted"}}
      }
    },
    "/auth/login": {
      "post": {
        "operationId": "login",
        "summary": "Log in",
        "tags": ["auth"],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "email": {"type": "string", "format": "email"},
                  "password": {"type": "string"}
                },
                "required": ["email", "password"]
              }
            }
          }
        },
        "responses": {"200": {"description": "token"}}
      }
    },
    "/auth/register": {
      "post": {
        "operationId": "register",
        "summary": "Register",
        "tags": ["auth"],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "email": {"type": "string"},
                  "password": {"type": "string"}
                },
                "required": ["email", "password"]
              }
            }
          }
        },
        "responses": {"201": {"description": "registered"}}
      }
    }
  }
}`

// sampleMinimalSpec is a minimal spec with no auth and one resource.
const sampleMinimalSpec = `
openapi: "3.0.3"
info:
  title: "Todo API"
  version: "0.1.0"
paths:
  /todos:
    get:
      operationId: listTodos
      responses:
        "200":
          description: success
    post:
      operationId: createTodo
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                title:
                  type: string
                done:
                  type: boolean
              required:
                - title
      responses:
        "201":
          description: created
  /todos/{id}:
    get:
      operationId: getTodo
      parameters:
        - name: id
          in: path
          required: true
      responses:
        "200":
          description: success
    delete:
      operationId: deleteTodo
      parameters:
        - name: id
          in: path
          required: true
      responses:
        "204":
          description: deleted
`

// --- runUIScaffold (CLI integration) ---

func TestRunUIScaffold_FromFile(t *testing.T) {
	outDir := t.TempDir()

	// Write spec to temp file.
	specFile := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(specFile, []byte(sampleOpenAPISpec), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runUIScaffold([]string{"-spec", specFile, "-output", outDir}); err != nil {
		t.Fatalf("runUIScaffold failed: %v", err)
	}

	// Quick sanity: package.json should exist.
	if _, err := os.Stat(filepath.Join(outDir, "package.json")); os.IsNotExist(err) {
		t.Error("expected package.json to be generated")
	}
}

func TestRunUIScaffold_WithTitleFlag(t *testing.T) {
	outDir := t.TempDir()
	specFile := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := os.WriteFile(specFile, []byte(sampleMinimalSpec), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runUIScaffold([]string{"-spec", specFile, "-output", outDir, "-title", "Custom Title"}); err != nil {
		t.Fatalf("runUIScaffold failed: %v", err)
	}

	indexHTML, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if !strings.Contains(string(indexHTML), "Custom Title") {
		t.Error("index.html should contain custom title")
	}
}

func TestRunUIScaffold_MissingSpec(t *testing.T) {
	err := runUIScaffold([]string{"-spec", "/nonexistent/path.yaml", "-output", t.TempDir()})
	if err == nil {
		t.Fatal("expected error for missing spec file")
	}
}

func TestRunUI_Dispatch(t *testing.T) {
	// Test that `ui` with no subcommand returns an error.
	err := runUI([]string{})
	if err == nil {
		t.Fatal("expected error when no subcommand given")
	}

	// Test unknown subcommand.
	err = runUI([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown ui subcommand") {
		t.Errorf("expected 'unknown ui subcommand' error, got: %v", err)
	}
}
