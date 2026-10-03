package main

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/plugin"
	pluginexternal "github.com/GoCodeAlone/workflow/plugin/external"
)

type localExternalPluginLoader func(*workflow.StdEngine, string, *slog.Logger) (func(), error)

// NOTE: package-level state. Tests overriding this MUST NOT call t.Parallel(),
// or they can leak the override into other wfctl tests.
var loadExternalPluginsForLocalEngine localExternalPluginLoader = loadExternalPluginsFromDir

func loadExternalPluginsFromDir(eng *workflow.StdEngine, pluginDir string, logger *slog.Logger) (func(), error) {
	if pluginDir == "" {
		return func() {}, nil
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	extMgr := pluginexternal.NewExternalPluginManager(pluginDir, newLocalExternalPluginStdLogger(logger))
	discovered, err := extMgr.DiscoverPlugins()
	if err != nil {
		return nil, fmt.Errorf("discover external plugins: %w", err)
	}
	sort.Strings(discovered)

	for _, name := range discovered {
		adapter, err := extMgr.LoadPlugin(name)
		if err != nil {
			extMgr.Shutdown()
			return nil, fmt.Errorf("load external plugin %q: %w", name, err)
		}
		if err := eng.LoadPlugin(adapter); err != nil {
			extMgr.Shutdown()
			return nil, fmt.Errorf("register external plugin %q: %w", name, err)
		}
		logger.Debug("Loaded external plugin", "plugin", name)
	}

	return extMgr.Shutdown, nil
}

func newLocalExternalPluginStdLogger(logger *slog.Logger) *log.Logger {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return log.New(localExternalPluginLogWriter{logger: logger}, "", 0)
}

func loadPipelineExternalPlugins(eng *workflow.StdEngine, pluginDir string, names []string, logger *slog.Logger) (func(), error) {
	installed, err := inspectPipelineInstallations(pluginDir)
	if err != nil {
		return nil, err
	}
	manager := pluginexternal.NewExternalPluginManager(pluginDir, newLocalExternalPluginStdLogger(logger))
	manager.SetProcessOutput(os.Stdout, os.Stderr)
	for _, name := range names {
		adapter, err := manager.LoadPlugin(name)
		if err != nil {
			manager.Shutdown()
			return nil, fmt.Errorf("load selected external plugin %q: %w", name, err)
		}
		if adapter.EngineManifest().Name != name {
			manager.Shutdown()
			return nil, fmt.Errorf("external plugin runtime installation identity mismatch")
		}
		if len(adapter.ConfigFragmentBytes()) != 0 {
			manager.Shutdown()
			return nil, fmt.Errorf("record mode rejects external config fragments")
		}
		factories := adapter.StepFactories()
		if err := validatePipelineRuntimeTypes(installed.manifests[name], factories); err != nil {
			manager.Shutdown()
			return nil, err
		}
		if err := eng.LoadPlugin(adapter); err != nil {
			manager.Shutdown()
			return nil, fmt.Errorf("register selected external plugin: %w", err)
		}
		registerPipelineFailureFactories(eng, factories)
	}
	return manager.Shutdown, nil
}

func validatePipelineRuntimeTypes(manifest *plugin.PluginManifest, factories map[string]plugin.StepFactory) error {
	if manifest == nil || len(factories) != len(manifest.StepTypes) {
		return fmt.Errorf("external runtime step ownership differs from installed manifest")
	}
	for _, name := range manifest.StepTypes {
		if _, exists := factories[name]; !exists {
			return fmt.Errorf("external runtime step ownership differs from installed manifest")
		}
	}
	return nil
}

type localExternalPluginLogWriter struct {
	logger *slog.Logger
}

func (w localExternalPluginLogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		w.logger.Debug(msg)
	}
	return len(p), nil
}
