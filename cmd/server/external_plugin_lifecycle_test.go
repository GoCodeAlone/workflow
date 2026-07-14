package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	pluginexternal "github.com/GoCodeAlone/workflow/plugin/external"
)

type candidateEngineLifecycleFixture struct {
	buildErr  error
	stopErr   error
	stopCalls int
}

func (f *candidateEngineLifecycleFixture) BuildFromConfig(*config.WorkflowConfig) error {
	return f.buildErr
}

func (f *candidateEngineLifecycleFixture) Stop(context.Context) error {
	f.stopCalls++
	return f.stopErr
}

func (f *candidateEngineLifecycleFixture) RegisteredModuleTypes() []string {
	return []string{"module.fixture"}
}
func (f *candidateEngineLifecycleFixture) RegisteredStepTypes() []string {
	return []string{"step.fixture"}
}
func (f *candidateEngineLifecycleFixture) RegisteredTriggerTypes() []string {
	return []string{"trigger.fixture"}
}

func TestExternalPluginManagerLifecycleExposesStartupManagerAndStopsIt(t *testing.T) {
	manager := pluginexternal.NewExternalPluginManager(t.TempDir(), nil)
	lifecycle := newExternalPluginManagerLifecycleModule(manager)
	app := modular.NewStdApplication(nil, slog.Default())
	if err := lifecycle.Init(app); err != nil {
		t.Fatalf("lifecycle Init: %v", err)
	}
	resolved, err := externalPluginManagerFromApplication(app)
	if err != nil {
		t.Fatalf("externalPluginManagerFromApplication: %v", err)
	}
	if resolved != manager {
		t.Fatal("admin lookup did not return the exact startup plugin manager")
	}
	adminMux, err := newExternalPluginAdminMux(app)
	if err != nil {
		t.Fatalf("newExternalPluginAdminMux: %v", err)
	}
	if adminMux == nil {
		t.Fatal("newExternalPluginAdminMux returned nil")
	}

	stopCalls := 0
	lifecycle.shutdown = func() { stopCalls++ }
	if err := lifecycle.Stop(context.Background()); err != nil {
		t.Fatalf("lifecycle Stop: %v", err)
	}
	if stopCalls != 1 {
		t.Fatalf("plugin manager shutdown calls = %d, want 1", stopCalls)
	}
}

func TestBuildEngineFromConfigStopsCandidateOnBuildFailure(t *testing.T) {
	buildErr := errors.New("candidate build failed")
	stopErr := errors.New("candidate cleanup failed")
	engine := &candidateEngineLifecycleFixture{buildErr: buildErr, stopErr: stopErr}
	cleanupCalls := 0
	err := buildEngineFromConfig(engine, config.NewEmptyWorkflowConfig(), func() { cleanupCalls++ })
	if !errors.Is(err, buildErr) || !errors.Is(err, stopErr) {
		t.Fatalf("buildEngineFromConfig error = %v", err)
	}
	if engine.stopCalls != 1 {
		t.Fatalf("candidate stop calls = %d, want 1", engine.stopCalls)
	}
	if cleanupCalls != 1 {
		t.Fatalf("external plugin cleanup calls = %d, want 1", cleanupCalls)
	}
}

func TestInspectCandidateEngineStopsAfterCollectingTypes(t *testing.T) {
	engine := &candidateEngineLifecycleFixture{}
	result, err := inspectAndStopCandidateEngine(engine)
	if err != nil {
		t.Fatalf("inspectAndStopCandidateEngine: %v", err)
	}
	if engine.stopCalls != 1 {
		t.Fatalf("candidate stop calls = %d, want 1", engine.stopCalls)
	}
	if result.Status != "build_ok" || len(result.ModuleTypes) != 1 || result.ModuleTypes[0] != "module.fixture" {
		t.Fatalf("candidate result = %+v", result)
	}
}

func TestRegisterPostStartServicesReplacesExternalPluginAdminMuxForNewEngine(t *testing.T) {
	newEngine := func() (*workflow.StdEngine, *externalPluginManagerLifecycleModule) {
		application := modular.NewStdApplication(nil, slog.Default())
		engine := workflow.NewStdEngine(application, slog.Default())
		manager := pluginexternal.NewExternalPluginManager(t.TempDir(), nil)
		lifecycle := newExternalPluginManagerLifecycleModule(manager)
		if err := lifecycle.Init(application); err != nil {
			t.Fatalf("lifecycle Init: %v", err)
		}
		return engine, lifecycle
	}

	firstEngine, firstLifecycle := newEngine()
	app := &serverApp{engine: firstEngine}
	if err := app.registerPostStartServices(slog.Default()); err != nil {
		t.Fatalf("register first post-start services: %v", err)
	}
	firstMux := app.services.externalPluginMux
	if firstMux == nil {
		t.Fatal("first external plugin admin mux is nil")
	}
	if err := firstLifecycle.Stop(context.Background()); err != nil {
		t.Fatalf("stop first lifecycle: %v", err)
	}

	secondEngine, secondLifecycle := newEngine()
	t.Cleanup(func() { _ = secondLifecycle.Stop(context.Background()) })
	app.engine = secondEngine
	if err := app.registerPostStartServices(slog.Default()); err != nil {
		t.Fatalf("register second post-start services: %v", err)
	}
	if app.services.externalPluginMux == nil || app.services.externalPluginMux == firstMux {
		t.Fatal("engine replacement retained the stopped manager's external plugin admin mux")
	}
}
