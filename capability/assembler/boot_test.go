package assembler

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/plugins/all"
	"github.com/GoCodeAlone/workflow/schema"
)

// TestBoot_MinimalAllCore proves MC2-bis-minimal: an assembled all-engine-core
// scaffold (http.server + http.router + health.checker) boots the real engine
// and serves GET /healthz 200. The observability health-endpoints wiring hook
// (plugins/observability/wiring.go:wireHealthEndpoints) binds the route at boot
// by scanning the service registry for *module.HealthChecker + *module.StandardHTTPRouter
// — both auto-registered post-Init by the modular framework via ProvidesServices().
func TestBoot_MinimalAllCore(t *testing.T) {
	cfg := assembleToConfig(t, AssemblyInput{
		Capabilities: []string{"observability.health", "http.routing"},
	})
	bootAndCheckHealth(t, cfg)
}

// TestBoot_Representative proves MC2-bis-representative: a multi-capability
// http+auth+db+health scaffold boots and serves /healthz 200. The DB is set to
// a real sqlite connection (driver="sqlite" — the modernc.org/sqlite driver
// registered by module/database_drivers.go; NOT "sqlite3" which is unregistered)
// so WorkflowDatabase.Start() genuinely calls sql.Open and the wiring hook's
// persistence health check wires against a live connection.
func TestBoot_Representative(t *testing.T) {
	cfg := assembleToConfig(t, AssemblyInput{
		Capabilities: []string{"auth.authn", "http.routing", "storage.database", "observability.health"},
	})
	dbPath := filepath.Join(t.TempDir(), "asm-representative.db")
	setDB(cfg, "sqlite", dbPath)
	// The assembler emits secret=${JWT_SECRET} (a placeholder the user fills in per
	// NEXT_STEPS.md — config.LoadFromFile does NOT expand ${VAR}). Provide a real
	// 32-byte secret so JWTAuthModule.Init's security check passes — this is exactly
	// what an operator does before boot; NOT a weakened assertion.
	setJWTSecret(cfg, "test-secret-at-least-32-bytes-long-for-boot-proof!!")
	bootAndCheckHealth(t, cfg)
}

// assembleToConfig runs the full Assemble → MarshalConfig → YAML round-trip →
// config.LoadFromFile pipeline. This proves the EMITTED artifact (not an
// in-memory struct) loads + boots — the real boundary between the assembler
// and the engine (design D5/D6). Uses assembler.MarshalConfig, the SAME pure
// fn the CLI emitter uses (P4: no cmd/wfctl <-> capability/assembler import cycle).
func assembleToConfig(t *testing.T, in AssemblyInput) *config.WorkflowConfig {
	t.Helper()
	inv := realInventory(t)
	reg := schema.NewModuleSchemaRegistry()
	app, err := Assemble(inv, in, reg)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
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
	// Prove the loaded config passes the SAME structural gate `wfctl validate`
	// uses (Copilot: the round-trip-gate claim was previously untested). A real
	// boot is the stronger proof, but validate-pass is the explicit contract.
	if err := schema.ValidateConfig(cfg); err != nil {
		t.Fatalf("loaded config fails schema.ValidateConfig (the wfctl validate gate): %v", err)
	}
	return cfg
}

// bootAndCheckHealth builds the engine from cfg, starts it, and asserts
// GET /healthz returns 200. This IS the MC2-bis proof — a real engine boot
// of the assembled scaffold. The assertion is NOT weakened: a non-200 status
// or Start failure is a hard failure (V8 escalation path).
func bootAndCheckHealth(t *testing.T, cfg *config.WorkflowConfig) {
	t.Helper()
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

	ctx := t.Context() // Copilot: t.Context() respects test cancellation/timeout (⊥ context.Background)
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	defer func() { _ = engine.Stop(ctx) }()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForHTTP(t, baseURL, 5*time.Second)

	resp, err := http.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz: got %d, want 200 (health-endpoints wiring hook must bind /healthz at boot)", resp.StatusCode)
	}
}

// --- inline helpers (package `assembler` cannot import package-main test helpers) ---

// nopLogger satisfies modular.Logger (Debug/Info/Warn/Error/Fatal:
// format string + variadic args) — the interface NewStdEngine expects.
type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}
func (nopLogger) Fatal(string, ...any) {}

// getFreePort returns an available TCP port for the test server.
func getFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("getFreePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// waitForHTTP polls baseURL until it accepts connections or the timeout expires.
func waitForHTTP(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/healthz")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server at %s did not become ready within %v", baseURL, timeout)
}

// setServerAddress rewrites the http.server module's config.address to the
// chosen free port so the boot proof doesn't collide with other tests or a
// real :8080 binding.
func setServerAddress(cfg *config.WorkflowConfig, addr string) {
	for i := range cfg.Modules {
		if cfg.Modules[i].Type == "http.server" {
			if cfg.Modules[i].Config == nil {
				cfg.Modules[i].Config = map[string]any{}
			}
			cfg.Modules[i].Config["address"] = addr
		}
	}
}

// setDB sets the database.workflow module's driver + dsn so the connection is
// genuine (P12). driver MUST match a registered sql driver — "sqlite" is the
// modernc.org/sqlite driver registered by module/database_drivers.go (⊥ "sqlite3",
// which is unregistered → sql.Open would fail "unknown driver").
func setDB(cfg *config.WorkflowConfig, driver, dsn string) {
	for i := range cfg.Modules {
		if cfg.Modules[i].Type == "database.workflow" {
			if cfg.Modules[i].Config == nil {
				cfg.Modules[i].Config = map[string]any{}
			}
			cfg.Modules[i].Config["driver"] = driver
			cfg.Modules[i].Config["dsn"] = dsn
		}
	}
}

// setJWTSecret replaces the ${JWT_SECRET} placeholder on the auth.jwt module
// with a real ≥32-byte secret. config.LoadFromFile passes ${VAR} through
// literally (no env expansion), and JWTAuthModule.Init enforces len(secret)>=32,
// so the boot proof must supply a genuine value — exactly what NEXT_STEPS.md
// instructs the operator to do before first boot.
func setJWTSecret(cfg *config.WorkflowConfig, secret string) {
	for i := range cfg.Modules {
		if cfg.Modules[i].Type == "auth.jwt" {
			if cfg.Modules[i].Config == nil {
				cfg.Modules[i].Config = map[string]any{}
			}
			cfg.Modules[i].Config["secret"] = secret
		}
	}
}
