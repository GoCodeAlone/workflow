package requirements

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestDiscoverRequirementsFromConfigShape(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Services: map[string]*config.ServiceConfig{
			"api": {
				Binary: "./cmd/api",
				Expose: []config.ExposeConfig{{
					Port:     8080,
					Protocol: "http",
				}},
			},
		},
		Mesh: &config.MeshConfig{
			Transport: "nats",
		},
	}

	reqs, err := Discover(context.Background(), Input{Config: cfg})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	assertHasRequirement(t, reqs, "web.api.api", KindWebAPI)
	assertHasRequirement(t, reqs, "messaging.nats.default", KindMessageBroker)
}

func TestDiscoverRequirementsFromManifestV2(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{{Name: "telemetry", Type: "observability.telemetry"}},
	}
	manifests := map[string]*config.PluginManifestFile{
		"workflow-plugin-observability": {
			ModuleInfraRequirementsV2: config.PluginInfraRequirementsV2{
				"observability.telemetry": {
					Requires: []config.ModuleInfraRequirementV2{{
						Key:                   "observability.telemetry.default",
						Kind:                  "observability",
						Source:                "observability.telemetry",
						ResourceTypeHint:      "infra.container_service",
						Runtimes:              []string{"kubernetes"},
						TelemetrySignals:      []string{"traces", "metrics", "logs"},
						ObservabilityBackends: []string{"otel"},
						DeploymentModes:       []string{"sidecar"},
					}},
				},
			},
		},
	}

	reqs, err := Discover(context.Background(), Input{Config: cfg, Manifests: manifests})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	req := assertHasRequirement(t, reqs, "observability.telemetry.default", KindObservability)
	if req.ResourceTypeHint != "infra.container_service" {
		t.Fatalf("ResourceTypeHint = %q", req.ResourceTypeHint)
	}
	if len(req.TelemetrySignals) != 3 {
		t.Fatalf("TelemetrySignals = %v", req.TelemetrySignals)
	}
}

func TestDiscoverRequirementsFiltersSatisfiedKeys(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "telemetry", Type: "observability.telemetry"},
			{Name: "otel", Type: "infra.container_service", Satisfies: []string{"observability.telemetry.default"}},
		},
	}
	manifests := map[string]*config.PluginManifestFile{
		"workflow-plugin-observability": {
			ModuleInfraRequirementsV2: config.PluginInfraRequirementsV2{
				"observability.telemetry": {
					Requires: []config.ModuleInfraRequirementV2{{
						Key:  "observability.telemetry.default",
						Kind: "observability",
					}},
				},
			},
		},
	}

	reqs, err := Discover(context.Background(), Input{Config: cfg, Manifests: manifests})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(reqs) != 0 {
		t.Fatalf("expected satisfied requirement to be filtered; got %+v", reqs)
	}
}

func TestDiscoverRequirementsFromInProcessProvider(t *testing.T) {
	provider := ProviderFunc(func(context.Context, Input) ([]Requirement, error) {
		return []Requirement{{Key: "cache.redis.default", Kind: KindCache}}, nil
	})

	reqs, err := Discover(context.Background(), Input{Providers: []Provider{provider}})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	assertHasRequirement(t, reqs, "cache.redis.default", KindCache)
}

func assertHasRequirement(t *testing.T, reqs []Requirement, key string, kind Kind) Requirement {
	t.Helper()
	for _, req := range reqs {
		if req.Key == key {
			if req.Kind != kind {
				t.Fatalf("requirement %q kind = %q, want %q", key, req.Kind, kind)
			}
			return req
		}
	}
	t.Fatalf("requirement %q not found in %+v", key, reqs)
	return Requirement{}
}
