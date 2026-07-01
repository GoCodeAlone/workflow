package main

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow"
	pluginexternal "github.com/GoCodeAlone/workflow/plugin/external"
)

type localExternalPluginLoader func(*workflow.StdEngine, string, *slog.Logger) (func(), error)

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
