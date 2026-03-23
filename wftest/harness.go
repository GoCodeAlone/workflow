package wftest

import (
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
	t          *testing.T
	yamlConfig string
	configPath string
	engine     *workflow.StdEngine
}

// New creates a test harness with the given options.
func New(t *testing.T, opts ...Option) *Harness {
	t.Helper()
	h := &Harness{t: t}
	for _, opt := range opts {
		opt(h)
	}
	h.init()
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
}

// ExecutePipeline runs a named pipeline with the given trigger data.
func (h *Harness) ExecutePipeline(name string, data map[string]any) *Result {
	h.t.Helper()
	ctx := h.t.Context()
	start := time.Now()
	output, err := h.engine.ExecutePipeline(ctx, name, data)
	return &Result{
		Output:   output,
		Error:    err,
		Duration: time.Since(start),
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
