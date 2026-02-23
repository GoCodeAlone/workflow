package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, cfg *WorkflowConfig)
	}{
		{
			name: "valid minimal config",
			input: `
modules:
  - name: svc
    type: http.server
`,
			check: func(t *testing.T, cfg *WorkflowConfig) {
				if len(cfg.Modules) != 1 {
					t.Fatalf("expected 1 module, got %d", len(cfg.Modules))
				}
				if cfg.Modules[0].Name != "svc" {
					t.Errorf("expected module name 'svc', got %q", cfg.Modules[0].Name)
				}
			},
		},
		{
			name: "valid with workflows and triggers",
			input: `
modules:
  - name: router
    type: http.router
workflows:
  order-flow:
    initial: new
triggers:
  http-trigger:
    type: http
`,
			check: func(t *testing.T, cfg *WorkflowConfig) {
				if cfg.Workflows["order-flow"] == nil {
					t.Error("expected order-flow workflow")
				}
				if cfg.Triggers["http-trigger"] == nil {
					t.Error("expected http-trigger")
				}
			},
		},
		{
			name:    "invalid YAML",
			input:   "{{invalid",
			wantErr: true,
		},
		{
			name:  "empty string",
			input: "",
			check: func(t *testing.T, cfg *WorkflowConfig) {
				if cfg == nil {
					t.Fatal("expected non-nil config from empty string")
				}
			},
		},
		{
			name: "config with requires section",
			input: `
modules: []
requires:
  capabilities:
    - docker
    - kubernetes
  plugins:
    - name: monitoring
      version: ">=1.0.0"
`,
			check: func(t *testing.T, cfg *WorkflowConfig) {
				if cfg.Requires == nil {
					t.Fatal("expected non-nil requires")
				}
				if len(cfg.Requires.Capabilities) != 2 {
					t.Errorf("expected 2 capabilities, got %d", len(cfg.Requires.Capabilities))
				}
				if len(cfg.Requires.Plugins) != 1 {
					t.Errorf("expected 1 plugin requirement, got %d", len(cfg.Requires.Plugins))
				}
				if cfg.Requires.Plugins[0].Name != "monitoring" {
					t.Errorf("expected plugin name 'monitoring', got %q", cfg.Requires.Plugins[0].Name)
				}
			},
		},
		{
			name: "config with pipelines",
			input: `
modules: []
pipelines:
  build:
    trigger:
      type: webhook
    steps:
      - name: compile
        type: exec
`,
			check: func(t *testing.T, cfg *WorkflowConfig) {
				if cfg.Pipelines == nil || cfg.Pipelines["build"] == nil {
					t.Error("expected build pipeline")
				}
			},
		},
		{
			name: "module with dependsOn and branches",
			input: `
modules:
  - name: router
    type: http.router
    dependsOn:
      - server
    branches:
      success: handler
      error: fallback
`,
			check: func(t *testing.T, cfg *WorkflowConfig) {
				mod := cfg.Modules[0]
				if len(mod.DependsOn) != 1 || mod.DependsOn[0] != "server" {
					t.Errorf("unexpected dependsOn: %v", mod.DependsOn)
				}
				if mod.Branches["success"] != "handler" {
					t.Errorf("unexpected branches: %v", mod.Branches)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := LoadFromString(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestResolveRelativePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configDir string
		path      string
		want      string
	}{
		{
			name:      "relative path resolved",
			configDir: "/etc/workflow",
			path:      "plugins/my-plugin",
			want:      "/etc/workflow/plugins/my-plugin",
		},
		{
			name:      "absolute path unchanged",
			configDir: "/etc/workflow",
			path:      "/absolute/path",
			want:      "/absolute/path",
		},
		{
			name:      "empty path unchanged",
			configDir: "/etc/workflow",
			path:      "",
			want:      "",
		},
		{
			name:      "empty config dir returns path as-is",
			configDir: "",
			path:      "relative/path",
			want:      "relative/path",
		},
		{
			name:      "both empty",
			configDir: "",
			path:      "",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &WorkflowConfig{ConfigDir: tt.configDir}
			got := cfg.ResolveRelativePath(tt.path)
			if got != tt.want {
				t.Errorf("ResolveRelativePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestResolvePathInConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  map[string]any
		path string
		want string
	}{
		{
			name: "resolves relative path with _config_dir",
			cfg:  map[string]any{"_config_dir": "/etc/workflow"},
			path: "data/file.txt",
			want: "/etc/workflow/data/file.txt",
		},
		{
			name: "absolute path unchanged",
			cfg:  map[string]any{"_config_dir": "/etc/workflow"},
			path: "/absolute/file.txt",
			want: "/absolute/file.txt",
		},
		{
			name: "no _config_dir returns path as-is",
			cfg:  map[string]any{},
			path: "relative/file.txt",
			want: "relative/file.txt",
		},
		{
			name: "empty _config_dir returns path as-is",
			cfg:  map[string]any{"_config_dir": ""},
			path: "relative/file.txt",
			want: "relative/file.txt",
		},
		{
			name: "empty path returns empty",
			cfg:  map[string]any{"_config_dir": "/etc"},
			path: "",
			want: "",
		},
		{
			name: "non-string _config_dir returns path as-is",
			cfg:  map[string]any{"_config_dir": 42},
			path: "relative/file.txt",
			want: "relative/file.txt",
		},
		{
			name: "nil cfg map",
			cfg:  nil,
			path: "file.txt",
			want: "file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolvePathInConfig(tt.cfg, tt.path)
			if got != tt.want {
				t.Errorf("ResolvePathInConfig(%v, %q) = %q, want %q", tt.cfg, tt.path, got, tt.want)
			}
		})
	}
}

func TestLoadFromFile_ConfigDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fp := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(fp, []byte("modules: []"), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	cfg, err := LoadFromFile(fp)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if cfg.ConfigDir == "" {
		t.Error("expected ConfigDir to be set")
	}
	absDir, _ := filepath.Abs(dir)
	if cfg.ConfigDir != absDir {
		t.Errorf("expected ConfigDir %q, got %q", absDir, cfg.ConfigDir)
	}
}

func TestNewEmptyWorkflowConfig_Pipelines(t *testing.T) {
	t.Parallel()

	cfg := NewEmptyWorkflowConfig()
	if cfg.Pipelines == nil {
		t.Error("expected non-nil pipelines map")
	}
	if cfg.Requires != nil {
		t.Error("expected nil requires for empty config")
	}
}
