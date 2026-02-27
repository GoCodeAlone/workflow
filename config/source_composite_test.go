package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const compositeBaseYAML = `
modules:
  - name: server
    type: http.server
    config:
      port: 8080
  - name: router
    type: http.router
workflows:
  flow1:
    initial: start
triggers:
  t1:
    type: http
`

const compositeOverlayYAML = `
modules:
  - name: server
    type: http.server
    config:
      port: 9090
`

const compositeNewModuleYAML = `
modules:
  - name: cache
    type: redis
    config:
      addr: localhost:6379
`

func writeConfigFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	fp := filepath.Join(dir, name)
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return fp
}

func TestCompositeSource_MergeModules(t *testing.T) {
	dir := t.TempDir()
	base := writeConfigFile(t, dir, "base.yaml", compositeBaseYAML)
	overlay := writeConfigFile(t, dir, "overlay.yaml", compositeOverlayYAML)

	cs := NewCompositeSource(NewFileSource(base), NewFileSource(overlay))
	cfg, err := cs.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// 'server' module should be replaced by overlay (port 9090), 'router' stays.
	if len(cfg.Modules) != 2 {
		t.Fatalf("expected 2 modules after merge, got %d", len(cfg.Modules))
	}

	serverFound := false
	for _, m := range cfg.Modules {
		if m.Name == "server" {
			serverFound = true
			port := m.Config["port"]
			if port != 9090 {
				t.Errorf("expected overlaid server port 9090, got %v", port)
			}
		}
	}
	if !serverFound {
		t.Error("expected 'server' module in merged config")
	}
}

func TestCompositeSource_AddModules(t *testing.T) {
	dir := t.TempDir()
	base := writeConfigFile(t, dir, "base.yaml", compositeBaseYAML)
	overlay := writeConfigFile(t, dir, "new_mod.yaml", compositeNewModuleYAML)

	cs := NewCompositeSource(NewFileSource(base), NewFileSource(overlay))
	cfg, err := cs.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// base has 2 modules, overlay adds 1 new one → 3 total.
	if len(cfg.Modules) != 3 {
		t.Fatalf("expected 3 modules after overlay add, got %d", len(cfg.Modules))
	}

	cacheFound := false
	for _, m := range cfg.Modules {
		if m.Name == "cache" {
			cacheFound = true
			if m.Type != "redis" {
				t.Errorf("expected cache type 'redis', got %q", m.Type)
			}
		}
	}
	if !cacheFound {
		t.Error("expected 'cache' module in merged config")
	}
}

func TestCompositeSource_MergeWorkflows(t *testing.T) {
	dir := t.TempDir()

	base := writeConfigFile(t, dir, "base.yaml", `
modules: []
workflows:
  flow1:
    initial: start
triggers: {}
`)
	overlay := writeConfigFile(t, dir, "overlay.yaml", `
modules: []
workflows:
  flow2:
    initial: running
triggers: {}
`)

	cs := NewCompositeSource(NewFileSource(base), NewFileSource(overlay))
	cfg, err := cs.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Workflows["flow1"] == nil {
		t.Error("expected 'flow1' from base to be present")
	}
	if cfg.Workflows["flow2"] == nil {
		t.Error("expected 'flow2' from overlay to be present")
	}
}

func TestCompositeSource_NoSources(t *testing.T) {
	cs := NewCompositeSource()
	_, err := cs.Load(context.Background())
	if err == nil {
		t.Fatal("expected error for empty sources")
	}
}

func TestCompositeSource_Name(t *testing.T) {
	cs := NewCompositeSource()
	if cs.Name() != "composite" {
		t.Errorf("expected name 'composite', got %q", cs.Name())
	}
}

func TestCompositeSource_Hash(t *testing.T) {
	dir := t.TempDir()
	base := writeConfigFile(t, dir, "base.yaml", compositeBaseYAML)

	cs := NewCompositeSource(NewFileSource(base))
	ctx := context.Background()

	h1, err := cs.Hash(ctx)
	if err != nil {
		t.Fatalf("Hash() error: %v", err)
	}
	if h1 == "" {
		t.Fatal("expected non-empty hash")
	}

	// Same content → same hash.
	h2, err := cs.Hash(ctx)
	if err != nil {
		t.Fatalf("Hash() second call error: %v", err)
	}
	if h1 != h2 {
		t.Errorf("expected stable hashes, got %q and %q", h1, h2)
	}
}

func TestCompositeSource_OverlayTriggersAndPipelines(t *testing.T) {
	dir := t.TempDir()
	base := writeConfigFile(t, dir, "base.yaml", `
modules: []
triggers:
  t1:
    type: http
pipelines:
  p1:
    steps: []
`)
	overlay := writeConfigFile(t, dir, "overlay.yaml", `
modules: []
triggers:
  t2:
    type: cron
pipelines:
  p2:
    steps: []
`)

	cs := NewCompositeSource(NewFileSource(base), NewFileSource(overlay))
	cfg, err := cs.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Triggers["t1"] == nil {
		t.Error("expected 't1' trigger from base")
	}
	if cfg.Triggers["t2"] == nil {
		t.Error("expected 't2' trigger from overlay")
	}
	if cfg.Pipelines["p1"] == nil {
		t.Error("expected 'p1' pipeline from base")
	}
	if cfg.Pipelines["p2"] == nil {
		t.Error("expected 'p2' pipeline from overlay")
	}
}
