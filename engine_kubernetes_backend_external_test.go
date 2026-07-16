package workflow

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/mock"
	workflowmodule "github.com/GoCodeAlone/workflow/module"
	pluginexternal "github.com/GoCodeAlone/workflow/plugin/external"
	platformplugin "github.com/GoCodeAlone/workflow/plugins/platform"
)

func TestExternalKubernetesBackendBindingCrossesProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("external plugin fixture layout uses a Unix executable name")
	}
	const pluginName = "verify-kubernetes"
	pluginsDir := t.TempDir()
	pluginDir := filepath.Join(pluginsDir, pluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("create plugin directory: %v", err)
	}

	fixtureDir := filepath.Join("cmd", "wfctl", "testdata", "verify_capabilities", "kubernetes-good")
	manifest, err := os.ReadFile(filepath.Join(fixtureDir, "plugin.json"))
	if err != nil {
		t.Fatalf("read fixture manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), manifest, 0o600); err != nil {
		t.Fatalf("write fixture manifest: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	binaryPath := filepath.Join(pluginDir, pluginName)
	command := exec.CommandContext(ctx, "go", "build", "-ldflags=-X main.Version=v0.1.0", "-o", binaryPath, "./"+fixtureDir)
	command.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build external Kubernetes fixture: %v\n%s", err, strings.TrimSpace(string(output)))
	}

	manager := pluginexternal.NewExternalPluginManager(pluginsDir, nil)
	t.Cleanup(manager.Shutdown)
	discovered, err := manager.DiscoverPlugins()
	if err != nil {
		t.Fatalf("DiscoverPlugins: %v", err)
	}
	if len(discovered) != 1 || discovered[0] != pluginName {
		t.Fatalf("discovered plugins = %v, want [%s]", discovered, pluginName)
	}
	adapter, err := manager.LoadPlugin(pluginName)
	if err != nil {
		t.Fatalf("LoadPlugin external fixture: %v", err)
	}

	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	engine := NewStdEngine(app, &mock.Logger{})
	if err := engine.LoadPlugin(platformplugin.New()); err != nil {
		t.Fatalf("LoadPlugin(platform): %v", err)
	}
	if err := engine.LoadPlugin(adapter); err != nil {
		t.Fatalf("LoadPlugin(external adapter): %v", err)
	}
	cfg := emptyKubernetesBackendWorkflowConfig()
	cfg.Modules = []config.ModuleConfig{{
		Name: "managed-cluster",
		Type: "platform.kubernetes",
		Config: map[string]any{
			"type":        "managed-b",
			"clusterName": "cluster-b",
			"version":     "1.31",
		},
	}}
	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}

	var cluster *workflowmodule.PlatformKubernetes
	if err := app.GetService("managed-cluster", &cluster); err != nil {
		t.Fatalf("GetService(managed-cluster): %v", err)
	}
	plan, err := cluster.Plan()
	if err != nil {
		t.Fatalf("Plan through exact external binding: %v", err)
	}
	if plan.Provider != "managed-b" || len(plan.Actions) != 1 || plan.Actions[0].Type != "noop" {
		t.Fatalf("external plan = %+v, want managed-b noop", plan)
	}
	rawState, err := cluster.Status()
	if err != nil {
		t.Fatalf("Status through exact external binding: %v", err)
	}
	state, ok := rawState.(*workflowmodule.KubernetesClusterState)
	if !ok {
		t.Fatalf("status type = %T, want *module.KubernetesClusterState", rawState)
	}
	if state.Provider != "managed-b" || state.Name != "managed-cluster" || state.Status != "running" || state.Endpoint != "https://managed-b.example.test" || state.Version != "1.31" {
		t.Fatalf("external state = %+v, want exact provider binding and projected output", state)
	}
}
