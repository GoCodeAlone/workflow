package config

import (
	"fmt"
	"os"
	"sync"
)

// inlinePluginDeprecationOnce ensures the warning fires at most once per process run.
var inlinePluginDeprecationOnce sync.Once

// resetInlinePluginDeprecationOnce resets the once for tests that need to observe
// the warning independently. Must only be called from test code.
func resetInlinePluginDeprecationOnce() {
	inlinePluginDeprecationOnce = sync.Once{}
}

// warnIfInlinePluginVersions emits a deprecation notice to stderr when a WorkflowConfig
// contains requires.plugins[] entries with non-empty version or source fields.
// The warning fires at most once per process lifetime.
func warnIfInlinePluginVersions(cfg *WorkflowConfig) {
	if cfg == nil || cfg.Requires == nil {
		return
	}
	for _, p := range cfg.Requires.Plugins {
		if p.Version != "" || p.Source != "" {
			inlinePluginDeprecationOnce.Do(func() {
				fmt.Fprintln(os.Stderr,
					"[DEPRECATED] requires.plugins[].version and requires.plugins[].source in app.yaml "+
						"are deprecated. Run 'wfctl migrate plugins' to migrate to wfctl.yaml + "+
						".wfctl-lock.yaml.")
			})
			return
		}
	}
}
