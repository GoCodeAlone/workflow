package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

const (
	defaultHookTimeout = 60 * time.Second
	hookFailPolicyFail = "fail"
	hookFailPolicyWarn = "warn"
	hookFailPolicySkip = "skip"
	wfctlHookFlag      = "--wfctl-hook"
)

// hookEntry is a resolved handler entry ready to dispatch.
type hookEntry struct {
	pluginName string
	binaryPath string
	event      interfaces.HookEvent
	priority   int
	timeout    time.Duration
	failPolicy string // fail | warn | skip
}

// HookDispatcher scans installed plugins for build-hook capability declarations
// and dispatches hook events to them via subprocess invocation.
type HookDispatcher struct {
	pluginsDir string
}

// NewHookDispatcher creates a dispatcher that reads manifests from pluginsDir.
func NewHookDispatcher(pluginsDir string) *HookDispatcher {
	return &HookDispatcher{pluginsDir: pluginsDir}
}

// Dispatch fires all handlers registered for the given event in priority order.
// Returns an error if any handler with fail policy exits non-zero.
func (d *HookDispatcher) Dispatch(ctx context.Context, event interfaces.HookEvent, payload interfaces.HookPayload) error {
	handlers, err := d.loadHandlers(event)
	if err != nil {
		return fmt.Errorf("load hook handlers: %w", err)
	}
	if len(handlers) == 0 {
		return nil
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal hook payload: %w", err)
	}

	for _, h := range handlers {
		if err := d.invoke(ctx, h, payloadBytes); err != nil {
			switch h.failPolicy {
			case hookFailPolicyFail, "":
				return fmt.Errorf("hook %s from plugin %s: %w", event, h.pluginName, err)
			case hookFailPolicyWarn:
				fmt.Fprintf(os.Stderr, "warning: hook %s from plugin %s failed (continuing): %v\n", event, h.pluginName, err)
			case hookFailPolicySkip:
				// silent
			}
		}
	}
	return nil
}

// loadHandlers reads all plugin manifests and returns handlers for the given
// event sorted by (priority ascending, plugin-name lexical ascending).
func (d *HookDispatcher) loadHandlers(event interfaces.HookEvent) ([]hookEntry, error) {
	manifests, err := LoadPluginManifests(d.pluginsDir)
	if err != nil {
		return nil, err
	}

	var handlers []hookEntry
	for name, manifest := range manifests {
		for _, decl := range manifest.Capabilities.BuildHooks {
			if interfaces.HookEvent(decl.Event) != event {
				continue
			}
			timeout := defaultHookTimeout
			if decl.TimeoutSeconds > 0 {
				timeout = time.Duration(decl.TimeoutSeconds) * time.Second
			}
			policy := manifest.Capabilities.OnHookFailure
			if policy == "" {
				policy = hookFailPolicyFail
			}
			handlers = append(handlers, hookEntry{
				pluginName: name,
				binaryPath: filepath.Join(d.pluginsDir, name, name),
				event:      event,
				priority:   decl.Priority,
				timeout:    timeout,
				failPolicy: policy,
			})
		}
	}

	sort.Slice(handlers, func(i, j int) bool {
		if handlers[i].priority != handlers[j].priority {
			return handlers[i].priority < handlers[j].priority
		}
		return handlers[i].pluginName < handlers[j].pluginName
	})

	return handlers, nil
}

// invoke runs a single plugin handler as a subprocess.
// JSON payload is written to stdin; the process must exit 0 for success.
func (d *HookDispatcher) invoke(ctx context.Context, h hookEntry, payload []byte) error {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, h.binaryPath, wfctlHookFlag, string(h.event)) //nolint:gosec // binaryPath comes from validated plugin config, not user input
	cmd.Stdin = bytes.NewReader(payload)

	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("timeout after %v: %w", h.timeout, ctx.Err())
		}
		return fmt.Errorf("exit error (output: %s): %w", string(out), err)
	}
	return nil
}
