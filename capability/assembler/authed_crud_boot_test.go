package assembler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/plugins/all"
	"github.com/GoCodeAlone/workflow/schema"
)

// TestScaffold_AuthedCRUD_Functional is the M1 headline proof (design G7/D4/D11/
// D18): a grammar-assembled authed-CRUD app boots the REAL engine and exhibits
// MEASURABLE FUNCTION — not just /healthz 200:
//   - GET /orders unauthenticated → 401 (Category-A route middleware [auth] +
//     Category-B auth-provider runtime hook, D18/V13),
//   - POST /orders (token) → 201, GET /orders/{id} → 200 + body, PUT → Data
//     updated (D12: tolerate the api_crud_handler handlePut State/LastUpdate
//     drop — assert Data only), DELETE → gone.
//
// The assembler routes the wire through scaffold.GrammarWire (the cutover), so
// the crud-route fragment the grammar emits is what the engine serves.
func TestScaffold_AuthedCRUD_Functional(t *testing.T) {
	cfg := assembleAuthedCRUD(t)
	dbPath := filepath.Join(t.TempDir(), "crud.db")
	setDB(cfg, "sqlite", dbPath)
	setJWTSecret(cfg, "test-secret-at-least-32-bytes-long-for-proof!!")
	setAllowRegistration(cfg, true)
	wirePersistenceToDB(cfg)

	port := getFreePort(t)
	setServerAddress(cfg, fmt.Sprintf(":%d", port))

	logger := &nopLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := workflow.NewStdEngine(app, logger)
	for _, p := range all.DefaultPlugins() {
		if err := engine.LoadPlugin(p); err != nil {
			t.Fatalf("LoadPlugin(%s): %v", p.Name(), err)
		}
	}
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}
	ctx := t.Context()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("engine.Start: %v (auth-provider Category-B hook must fire)", err)
	}
	defer func() { _ = engine.Stop(ctx) }()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForHTTP(t, base, 5*time.Second)

	// 1. Unauthenticated GET → 401 (D18 auth enforced by the route middleware).
	if code := httpDo(t, base, http.MethodGet, "/orders", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /orders: got %d, want 401 (D18)", code)
	}

	// 2. Register a user → mint a token (auth.jwt allowRegistration path).
	token := registerAndGetToken(t, base)

	// 3. POST → 201 + Location with the new id.
	id := httpCreate(t, base, "/orders", token, map[string]any{"name": "widget", "qty": 3})

	// 4. GET /{id} → 200 + body contains the created data.
	if body := httpGetBody(t, base, "/orders/"+id, token); !strings.Contains(body, "widget") {
		t.Fatalf("GET /orders/%s body=%q want widget", id, body)
	}

	// 5. PUT → Data updated (D12: tolerate handlePut State/LastUpdate drop —
	//    assert only that Data reflects the update).
	httpUpdate(t, base, "/orders/"+id, token, map[string]any{"name": "gadget", "qty": 5})
	if body := httpGetBody(t, base, "/orders/"+id, token); !strings.Contains(body, "gadget") {
		t.Fatalf("PUT did not update Data (D12): %q", body)
	}

	// 6. DELETE → gone (subsequent GET → 404).
	httpDo(t, base, http.MethodDelete, "/orders/"+id, token, nil)
	if code := httpDo(t, base, http.MethodGet, "/orders/"+id, token, nil); code != http.StatusNotFound {
		t.Fatalf("GET after DELETE: got %d, want 404", code)
	}
}

// assembleAuthedCRUD runs Assemble → MarshalConfig → YAML round-trip →
// LoadFromFile for an authed-CRUD capability set + explicit api.handler +
// persistence.store modules, returning the loadable config.
func assembleAuthedCRUD(t *testing.T) *config.WorkflowConfig {
	t.Helper()
	reg := schema.NewModuleSchemaRegistry()
	app, err := Assemble(realInventory(t), AssemblyInput{
		Capabilities: []string{"auth.authn", "http.routing", "storage.database", "observability.health"},
		Modules: []ExplicitModule{
			{Type: "api.handler", Name: "orders-api", Config: map[string]any{"resourceName": "orders"}},
			{Type: "persistence.store", Name: "persistence"},
		},
	}, reg)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	addAuthRoutesApp(app)
	wfYAML, err := MarshalConfig(app)
	if err != nil {
		t.Fatalf("MarshalConfig: %v", err)
	}
	dir := t.TempDir()
	wfPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(wfPath, wfYAML, 0o600); err != nil {
		t.Fatalf("write workflow.yaml: %v", err)
	}
	cfg, err := config.LoadFromFile(wfPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if err := schema.ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig: %v", err)
	}
	return cfg
}

// setAllowRegistration enables open registration on the auth.jwt module so the
// test can create a user via POST /auth/register.
func setAllowRegistration(cfg *config.WorkflowConfig, allow bool) {
	for i := range cfg.Modules {
		if cfg.Modules[i].Type == "auth.jwt" {
			if cfg.Modules[i].Config == nil {
				cfg.Modules[i].Config = map[string]any{}
			}
			cfg.Modules[i].Config["allowRegistration"] = allow
		}
	}
}

// wirePersistenceToDB points the persistence.store module's "database" config at
// the database.workflow module's instance name (so write-through CRUD persists).
func wirePersistenceToDB(cfg *config.WorkflowConfig) {
	dbName := ""
	for i := range cfg.Modules {
		if cfg.Modules[i].Type == "database.workflow" {
			dbName = cfg.Modules[i].Name
		}
	}
	if dbName == "" {
		return
	}
	for i := range cfg.Modules {
		if cfg.Modules[i].Type == "persistence.store" {
			if cfg.Modules[i].Config == nil {
				cfg.Modules[i].Config = map[string]any{}
			}
			cfg.Modules[i].Config["database"] = dbName
			if !containsDep(cfg.Modules[i].DependsOn, dbName) {
				cfg.Modules[i].DependsOn = append(cfg.Modules[i].DependsOn, dbName)
			}
		}
	}
}

func containsDep(deps []string, name string) bool {
	for _, d := range deps {
		if d == name {
			return true
		}
	}
	return false
}

// addAuthRoutesApp registers /auth/register + /auth/login on the http router,
// pointing at the auth.jwt module (a JWTAuthModule, which is an http.Handler),
// BEFORE marshal so they round-trip cleanly with the grammar-emitted crud routes.
// These routes are not grammar-emitted in M1 (auth login is an app concern, not
// a Category-A glue rule), so the authed-CRUD proof wires them explicitly —
// exactly as an operator's workflow.yaml would.
func addAuthRoutesApp(app *AssembledApp) {
	authName := ""
	for _, m := range app.Modules {
		if m.Type == "auth.jwt" {
			authName = m.Name
		}
	}
	if authName == "" {
		return
	}
	section, _ := app.Workflows["http"].(map[string]any)
	if section == nil {
		section = map[string]any{}
		app.Workflows["http"] = section
	}
	routes, _ := section["routes"].([]map[string]any)
	routes = append(routes,
		map[string]any{"method": "POST", "path": "/auth/register", "handler": authName},
		map[string]any{"method": "POST", "path": "/auth/login", "handler": authName},
	)
	section["routes"] = routes
}

// --- HTTP CRUD helpers ---

// crudClient is a bounded HTTP client so a stalled server fails the test fast
// (⊥ http.DefaultClient, which would hang until the global go test -timeout).
var crudClient = &http.Client{Timeout: 5 * time.Second}

// newReq builds a request bound to the test context (cancelled on test end).
func newReq(t *testing.T, method, url string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, body)
	if err != nil {
		t.Fatalf("newReq %s %s: %v", method, url, err)
	}
	return req
}

func marshalBody(t *testing.T, body map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return b
}

func httpDo(t *testing.T, base, method, path, token string, body map[string]any) int {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(marshalBody(t, body))
	}
	req := newReq(t, method, base+path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := crudClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func httpCreate(t *testing.T, base, path, token string, body map[string]any) string {
	t.Helper()
	req := newReq(t, http.MethodPost, base+path, bytes.NewReader(marshalBody(t, body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := crudClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST %s: got %d, want 201", path, resp.StatusCode)
	}
	// Prefer Location header (/orders/{id}); fall back to parsing the body id.
	if loc := resp.Header.Get("Location"); loc != "" {
		return strings.TrimPrefix(loc, path+"/")
	}
	rb, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if json.Unmarshal(rb, &m) == nil {
		if id, ok := m["id"].(string); ok && id != "" {
			return id
		}
	}
	t.Fatalf("POST %s: could not extract resource id (Location=%q body=%s)", path, resp.Header.Get("Location"), rb)
	return ""
}

func httpUpdate(t *testing.T, base, path, token string, body map[string]any) {
	t.Helper()
	if code := httpDo(t, base, http.MethodPut, path, token, body); code != http.StatusOK {
		t.Fatalf("PUT %s: got %d, want 200", path, code)
	}
}

func httpGetBody(t *testing.T, base, path, token string) string {
	t.Helper()
	req := newReq(t, http.MethodGet, base+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := crudClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: got %d, want 200", path, resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// registerAndGetToken POSTs /auth/register and extracts the JWT from the response.
func registerAndGetToken(t *testing.T, base string) string {
	t.Helper()
	body := marshalBody(t, map[string]any{"email": "crud@example.com", "name": "CRUD", "password": "pass12345"})
	req := newReq(t, http.MethodPost, base+"/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := crudClient.Do(req)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		rb, _ := io.ReadAll(resp.Body)
		t.Fatalf("register: got %d, want 200/201: %s", resp.StatusCode, rb)
	}
	rb, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if json.Unmarshal(rb, &m) == nil {
		if tok, ok := m["token"].(string); ok && tok != "" {
			return tok
		}
	}
	t.Fatalf("register: no token in response: %s", rb)
	return ""
}
