package wftest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin"
	pluginactors "github.com/GoCodeAlone/workflow/plugins/actors"
	pluginai "github.com/GoCodeAlone/workflow/plugins/ai"
	pluginapi "github.com/GoCodeAlone/workflow/plugins/api"
	pluginauth "github.com/GoCodeAlone/workflow/plugins/auth"
	plugincicd "github.com/GoCodeAlone/workflow/plugins/cicd"
	pluginff "github.com/GoCodeAlone/workflow/plugins/featureflags"
	pluginhttp "github.com/GoCodeAlone/workflow/plugins/http"
	pluginintegration "github.com/GoCodeAlone/workflow/plugins/integration"
	pluginlicense "github.com/GoCodeAlone/workflow/plugins/license"
	pluginmessaging "github.com/GoCodeAlone/workflow/plugins/messaging"
	pluginmodcompat "github.com/GoCodeAlone/workflow/plugins/modularcompat"
	pluginobs "github.com/GoCodeAlone/workflow/plugins/observability"
	pluginopenapi "github.com/GoCodeAlone/workflow/plugins/openapi"
	pluginpipeline "github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
	pluginplatform "github.com/GoCodeAlone/workflow/plugins/platform"
	pluginscheduler "github.com/GoCodeAlone/workflow/plugins/scheduler"
	pluginsecrets "github.com/GoCodeAlone/workflow/plugins/secrets"
	pluginsm "github.com/GoCodeAlone/workflow/plugins/statemachine"
	pluginstorage "github.com/GoCodeAlone/workflow/plugins/storage"
)

// discardLogger satisfies modular.Logger by silently dropping all log output.
type discardLogger struct{}

func (d *discardLogger) Info(msg string, args ...any)  {}
func (d *discardLogger) Error(msg string, args ...any) {}
func (d *discardLogger) Warn(msg string, args ...any)  {}
func (d *discardLogger) Debug(msg string, args ...any) {}

// Harness is an in-process workflow engine for integration testing.
type Harness struct {
	t            *testing.T
	yamlConfig   string
	configPath   string
	engine       *workflow.StdEngine
	extraPlugins []plugin.EnginePlugin
	serverMode   bool
	httpServer   *httptest.Server
	baseURL      string
	// httpHandler is the HTTP router used for in-process request injection.
	// Set by startServer() (WithServer mode) or lazily by getHTTPHandler().
	httpHandler http.Handler
	startOnce   sync.Once
	mockSteps   map[string]StepHandler
	mockModules []*MockModule
}

// New creates a test harness with the given options.
func New(t *testing.T, opts ...Option) *Harness {
	t.Helper()
	h := &Harness{t: t}
	for _, opt := range opts {
		opt(h)
	}
	h.init()
	t.Cleanup(func() {
		if h.httpServer != nil {
			h.httpServer.Close()
		}
	})
	return h
}

func (h *Harness) init() {
	h.t.Helper()
	logger := &discardLogger{}
	app := modular.NewStdApplication(nil, logger)
	h.engine = workflow.NewStdEngine(app, logger)

	for _, p := range allBuiltinPlugins() {
		if err := h.engine.LoadPlugin(p); err != nil {
			h.t.Fatalf("wftest: LoadPlugin(%s) failed: %v", p.Name(), err)
		}
	}
	for _, p := range h.extraPlugins {
		if err := h.engine.LoadPluginWithOverride(p); err != nil {
			h.t.Fatalf("wftest: LoadPlugin(%s) failed: %v", p.Name(), err)
		}
	}

	// Register mock step factories (override real implementations).
	for stepType, handler := range h.mockSteps {
		h.engine.AddStepType(stepType, newMockStepFactory(handler))
	}

	// Register mock modules so their services are in the service registry.
	for _, mod := range h.mockModules {
		h.engine.App().RegisterModule(mod)
	}

	var cfg *config.WorkflowConfig
	var err error
	if h.yamlConfig != "" {
		cfg, err = config.LoadFromString(h.yamlConfig)
	} else if h.configPath != "" {
		cfg, err = config.LoadFromFile(h.configPath)
	}
	if err != nil {
		h.t.Fatalf("wftest: failed to load config: %v", err)
	}
	if cfg != nil {
		if err := h.engine.BuildFromConfig(cfg); err != nil {
			h.t.Fatalf("wftest: BuildFromConfig failed: %v", err)
		}
	}

	if h.serverMode {
		h.startServer()
	}
}

// getHTTPHandler returns the engine's HTTP router as an http.Handler for
// in-process request injection. The engine is started (once) on the first call;
// if WithServer() already started it, the cached handler is returned directly.
func (h *Harness) getHTTPHandler() http.Handler {
	h.t.Helper()
	h.startOnce.Do(func() {
		if h.httpHandler != nil {
			// Already set by startServer() — WithServer mode.
			return
		}
		ctx := h.t.Context()
		if err := h.engine.Start(ctx); err != nil {
			h.t.Fatalf("wftest: engine.Start failed: %v", err)
		}
		h.t.Cleanup(func() {
			_ = h.engine.Stop(context.Background())
		})
		for _, svc := range h.engine.App().SvcRegistry() {
			if handler, ok := svc.(http.Handler); ok {
				h.httpHandler = handler
				break
			}
		}
		if h.httpHandler == nil {
			h.t.Fatalf("wftest: no http.Handler found in service registry; ensure an http.router module is configured")
		}
	})
	return h.httpHandler
}

// ExecutePipeline runs a named pipeline with the given trigger data.
// StepResults in the returned Result is populated with per-step outputs.
func (h *Harness) ExecutePipeline(name string, data map[string]any) *Result {
	h.t.Helper()
	ctx := h.t.Context()
	start := time.Now()
	pc, err := h.engine.ExecutePipelineContext(ctx, name, data)
	if err != nil {
		return &Result{Error: err, Duration: time.Since(start)}
	}

	// Prefer explicit pipeline output if step.pipeline_output was used.
	output := pc.Current
	if pipeOut, ok := pc.Metadata["_pipeline_output"].(map[string]any); ok {
		output = pipeOut
	}

	return &Result{
		Output:      output,
		StepResults: pc.StepOutputs,
		Duration:    time.Since(start),
	}
}

// allBuiltinPlugins returns all built-in engine plugins.
func allBuiltinPlugins() []plugin.EnginePlugin {
	return []plugin.EnginePlugin{
		pluginhttp.New(),
		pluginobs.New(),
		pluginmessaging.New(),
		pluginsm.New(),
		pluginauth.New(),
		pluginstorage.New(),
		pluginapi.New(),
		pluginpipeline.New(),
		plugincicd.New(),
		pluginff.New(),
		pluginsecrets.New(),
		pluginmodcompat.New(),
		pluginscheduler.New(),
		pluginintegration.New(),
		pluginai.New(),
		pluginplatform.New(),
		pluginlicense.New(),
		pluginopenapi.New(),
		pluginactors.New(),
	}
}
