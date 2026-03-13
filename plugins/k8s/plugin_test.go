package k8s

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestNew(t *testing.T) {
	p := New()
	if p.Name() != "kubernetes-deploy" {
		t.Fatalf("expected name kubernetes-deploy, got %s", p.Name())
	}
	if p.Version() != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", p.Version())
	}
}

func TestManifestValidates(t *testing.T) {
	p := New()
	m := p.EngineManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
}

func TestDeployTargets(t *testing.T) {
	p := New()
	targets := p.DeployTargets()

	for _, key := range []string{"kubernetes", "k8s"} {
		if _, ok := targets[key]; !ok {
			t.Errorf("missing deploy target: %s", key)
		}
	}

	if len(targets) != 2 {
		t.Errorf("expected 2 deploy targets, got %d", len(targets))
	}
}

func TestSidecarProviders(t *testing.T) {
	p := New()
	sidecars := p.SidecarProviders()

	for _, key := range []string{"sidecar.tailscale", "sidecar.generic"} {
		if _, ok := sidecars[key]; !ok {
			t.Errorf("missing sidecar provider: %s", key)
		}
	}

	if len(sidecars) != 2 {
		t.Errorf("expected 2 sidecar providers, got %d", len(sidecars))
	}
}

func TestPluginLoads(t *testing.T) {
	p := New()
	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}
}
