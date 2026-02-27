package modularcompat

import (
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/jsonschema"
)

// userSchema is a sample JSON Schema used across tests to validate user payloads.
const userSchema = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"type": "object",
	"properties": {
		"name": { "type": "string", "minLength": 1 },
		"age":  { "type": "integer", "minimum": 0 },
		"email": { "type": "string" }
	},
	"required": ["name", "age"],
	"additionalProperties": false
}`

// writeSchemaFile creates a temp file containing schema JSON and returns its path.
// The caller is responsible for removing the file.
func writeSchemaFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "wf-schema-*.json")
	if err != nil {
		t.Fatalf("create temp schema file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write schema file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close schema file: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	return f.Name()
}

// newTestApp initialises a modular application with the jsonschema module registered.
// It returns the started Application; the caller must call Stop.
func newTestApp(t *testing.T) modular.Application {
	t.Helper()
	app := modular.NewStdApplication(
		modular.NewStdConfigProvider(nil),
		slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	)
	app.RegisterModule(jsonschema.NewModule())
	if err := app.Init(); err != nil {
		t.Fatalf("app.Init: %v", err)
	}
	if err := app.Start(); err != nil {
		t.Fatalf("app.Start: %v", err)
	}
	t.Cleanup(func() {
		if err := app.Stop(); err != nil {
			t.Logf("app.Stop: %v", err)
		}
	})
	return app
}

// getJSONSchemaService retrieves the JSONSchemaService registered by the jsonschema module.
func getJSONSchemaService(t *testing.T, app modular.Application) jsonschema.JSONSchemaService {
	t.Helper()
	var svc jsonschema.JSONSchemaService
	if err := app.GetService("jsonschema.service", &svc); err != nil {
		t.Fatalf("GetService(jsonschema.service): %v", err)
	}
	return svc
}

// TestJSONSchemaModuleWiredIntoApp verifies that the jsonschema module registered
// via the modularcompat plugin factory initialises inside a modular application
// and exposes its service.
func TestJSONSchemaModuleWiredIntoApp(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	factory, ok := factories["jsonschema.modular"]
	if !ok {
		t.Fatal("jsonschema.modular factory not found in modularcompat plugin")
	}

	app := modular.NewStdApplication(
		modular.NewStdConfigProvider(nil),
		slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
	)
	app.RegisterModule(factory("schema-module", nil))
	if err := app.Init(); err != nil {
		t.Fatalf("app.Init: %v", err)
	}
	t.Cleanup(func() { _ = app.Stop() })

	var svc jsonschema.JSONSchemaService
	if err := app.GetService("jsonschema.service", &svc); err != nil {
		t.Fatalf("jsonschema.service not available after Init: %v", err)
	}
}

// TestJSONSchemaValidateBytes tests validating raw JSON bytes against a compiled schema.
func TestJSONSchemaValidateBytes(t *testing.T) {
	app := newTestApp(t)
	svc := getJSONSchemaService(t, app)

	schemaPath := writeSchemaFile(t, userSchema)
	schema, err := svc.CompileSchema(schemaPath)
	if err != nil {
		t.Fatalf("CompileSchema: %v", err)
	}

	cases := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{"valid payload", `{"name":"Alice","age":30}`, false},
		{"valid with optional email", `{"name":"Bob","age":25,"email":"bob@example.com"}`, false},
		{"missing required field age", `{"name":"Charlie"}`, true},
		{"missing required field name", `{"age":40}`, true},
		{"extra property rejected", `{"name":"Dave","age":20,"phone":"555"}`, true},
		{"wrong type for age", `{"name":"Eve","age":"old"}`, true},
		{"empty object", `{}`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.ValidateBytes(schema, []byte(tc.payload))
			if tc.wantErr && err == nil {
				t.Errorf("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

// TestJSONSchemaValidateReader tests validating JSON from an io.Reader.
func TestJSONSchemaValidateReader(t *testing.T) {
	app := newTestApp(t)
	svc := getJSONSchemaService(t, app)

	schemaPath := writeSchemaFile(t, userSchema)
	schema, err := svc.CompileSchema(schemaPath)
	if err != nil {
		t.Fatalf("CompileSchema: %v", err)
	}

	t.Run("valid", func(t *testing.T) {
		if err := svc.ValidateReader(schema, strings.NewReader(`{"name":"Alice","age":30}`)); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		if err := svc.ValidateReader(schema, strings.NewReader(`{"name":"Alice"}`)); err == nil {
			t.Error("expected validation error, got nil")
		}
	})
}

// TestJSONSchemaValidateInterface tests validating a Go map (unmarshaled JSON) directly.
func TestJSONSchemaValidateInterface(t *testing.T) {
	app := newTestApp(t)
	svc := getJSONSchemaService(t, app)

	schemaPath := writeSchemaFile(t, userSchema)
	schema, err := svc.CompileSchema(schemaPath)
	if err != nil {
		t.Fatalf("CompileSchema: %v", err)
	}

	valid := map[string]any{"name": "Alice", "age": float64(30)}
	if err := svc.ValidateInterface(schema, valid); err != nil {
		t.Errorf("unexpected error for valid interface: %v", err)
	}

	invalid := map[string]any{"name": "Alice"} // missing "age"
	if err := svc.ValidateInterface(schema, invalid); err == nil {
		t.Error("expected validation error for invalid interface, got nil")
	}
}

// TestJSONSchemaRegistryWorkflow simulates the schema-registry + validator use case
// described in the PR review: compile multiple schemas once into an in-memory
// "registry" map, then validate incoming payloads using the right schema by name.
func TestJSONSchemaRegistryWorkflow(t *testing.T) {
	app := newTestApp(t)
	svc := getJSONSchemaService(t, app)

	// --- schema definitions ---
	schemas := map[string]string{
		"user": `{
			"type": "object",
			"properties": {
				"name": {"type": "string"},
				"age":  {"type": "integer", "minimum": 0}
			},
			"required": ["name", "age"],
			"additionalProperties": false
		}`,
		"product": `{
			"type": "object",
			"properties": {
				"id":    {"type": "string"},
				"price": {"type": "number", "minimum": 0}
			},
			"required": ["id", "price"],
			"additionalProperties": false
		}`,
	}

	// Compile all schemas once and store in a registry map.
	schemaRegistry := make(map[string]jsonschema.Schema, len(schemas))
	for name, def := range schemas {
		path := writeSchemaFile(t, def)
		compiled, err := svc.CompileSchema(path)
		if err != nil {
			t.Fatalf("CompileSchema(%s): %v", name, err)
		}
		schemaRegistry[name] = compiled
	}

	// --- validation table ---
	type testCase struct {
		schemaName string
		payload    string
		wantErr    bool
	}
	cases := []testCase{
		{"user", `{"name":"Alice","age":30}`, false},
		{"user", `{"name":"Bob"}`, true},                     // missing age
		{"user", `{"name":"Carol","age":25,"x":"y"}`, true},  // extra field
		{"product", `{"id":"sku-1","price":9.99}`, false},
		{"product", `{"id":"sku-2"}`, true},                   // missing price
		{"product", `{"id":"sku-3","price":-1}`, true},        // price below minimum
	}

	for _, tc := range cases {
		t.Run(tc.schemaName+"/"+tc.payload, func(t *testing.T) {
			sc, ok := schemaRegistry[tc.schemaName]
			if !ok {
				t.Fatalf("schema %q not in registry", tc.schemaName)
			}
			err := svc.ValidateBytes(sc, []byte(tc.payload))
			if tc.wantErr && err == nil {
				t.Errorf("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}
