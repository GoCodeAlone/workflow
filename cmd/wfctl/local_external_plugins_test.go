package main

import (
	"bytes"
	"context"
	"log"
	"log/slog"
	"strings"
	"testing"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

func overrideLocalExternalPluginLoader(t *testing.T, wantPluginDir string) func() {
	t.Helper()
	old := loadExternalPluginsForLocalEngine
	called := false
	shutdownCalled := false
	loadExternalPluginsForLocalEngine = func(eng *workflow.StdEngine, gotPluginDir string, _ *slog.Logger) (func(), error) {
		called = true
		if gotPluginDir != wantPluginDir {
			t.Fatalf("plugin dir = %q, want %q", gotPluginDir, wantPluginDir)
		}
		if err := eng.LoadPlugin(&testExternalMarkerPlugin{shutdown: &shutdownCalled}); err != nil {
			return nil, err
		}
		return func() { shutdownCalled = true }, nil
	}
	return func() {
		loadExternalPluginsForLocalEngine = old
		if !called {
			t.Fatal("external plugin loader was not called")
		}
		if !shutdownCalled {
			t.Fatal("external plugin shutdown was not called")
		}
	}
}

func TestLocalExternalPluginStdLoggerUsesProvidedSlog(t *testing.T) {
	var defaultLog bytes.Buffer
	defaultWriter := log.Writer()
	log.SetOutput(&defaultLog)
	defer log.SetOutput(defaultWriter)

	var slogOutput bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&slogOutput, &slog.HandlerOptions{Level: slog.LevelDebug}))
	stdLogger := newLocalExternalPluginStdLogger(logger)

	stdLogger.Print("external plugin detail")

	if defaultLog.Len() != 0 {
		t.Fatalf("external plugin logger wrote to process default logger: %q", defaultLog.String())
	}
	if got := slogOutput.String(); !strings.Contains(got, "external plugin detail") {
		t.Fatalf("external plugin log was not routed to provided slog logger: %q", got)
	}
}

func TestLocalExternalPluginStdLoggerNilLoggerDiscards(t *testing.T) {
	t.Parallel()

	stdLogger := newLocalExternalPluginStdLogger(nil)
	stdLogger.Print("discarded detail")
}

type testExternalMarkerPlugin struct {
	plugin.BaseEnginePlugin
	shutdown *bool
}

func (testExternalMarkerPlugin) Name() string        { return "test-external-marker" }
func (testExternalMarkerPlugin) Version() string     { return "0.0.0" }
func (testExternalMarkerPlugin) Description() string { return "test external marker plugin" }

func (testExternalMarkerPlugin) EngineManifest() *plugin.PluginManifest {
	return &plugin.PluginManifest{
		Name:        "test-external-marker",
		Version:     "0.0.0",
		Author:      "GoCodeAlone",
		Description: "test external marker plugin",
	}
}

func (p *testExternalMarkerPlugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.test_external_marker": func(name string, cfg map[string]any, _ modular.Application) (any, error) {
			value, _ := cfg["value"].(string)
			return &testExternalMarkerStep{name: name, value: value, shutdown: p.shutdown}, nil
		},
	}
}

type testExternalMarkerStep struct {
	name     string
	value    string
	shutdown *bool
}

func (s *testExternalMarkerStep) Name() string { return s.name }

func (s *testExternalMarkerStep) Execute(context.Context, *module.PipelineContext) (*module.StepResult, error) {
	if s.shutdown != nil && *s.shutdown {
		return nil, context.Canceled
	}
	return &module.StepResult{Output: map[string]any{
		"external_marker_loaded": true,
		"external_marker_value":  s.value,
	}}, nil
}
