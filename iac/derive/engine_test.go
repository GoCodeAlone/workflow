package derive

import (
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/requirements"
)

func TestDeriveProviderRuntimePrecedence(t *testing.T) {
	cfg := &config.WorkflowConfig{Modules: []config.ModuleConfig{
		{Name: "do-provider", Type: "iac.provider", Config: map[string]any{"provider": "digitalocean"}},
	}}
	mapper := &fakeMapper{modules: []GeneratedModule{{Name: "api", Type: "infra.container_service"}}}

	result, err := Derive(context.Background(), cfg, nil, mapper, Options{
		Provider:       "aws",
		Runtime:        requirements.RuntimeECS,
		Environment:    "prod",
		NonInteractive: true,
	})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if mapper.last.Provider != "aws" {
		t.Fatalf("provider = %q, want aws", mapper.last.Provider)
	}
	if mapper.last.Runtime != requirements.RuntimeECS {
		t.Fatalf("runtime = %q, want ecs", mapper.last.Runtime)
	}
	if result.Provider != "aws" || result.Runtime != requirements.RuntimeECS {
		t.Fatalf("result provider/runtime = %q/%q", result.Provider, result.Runtime)
	}
}

func TestDeriveNonInteractiveAmbiguousProvider(t *testing.T) {
	cfg := &config.WorkflowConfig{Modules: []config.ModuleConfig{
		{Name: "aws-provider", Type: "iac.provider", Config: map[string]any{"provider": "aws"}},
		{Name: "do-provider", Type: "iac.provider", Config: map[string]any{"provider": "digitalocean"}},
	}}
	_, err := Derive(context.Background(), cfg, nil, &fakeMapper{}, Options{NonInteractive: true})
	if err == nil {
		t.Fatalf("derive succeeded, want ambiguity error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "aws") || !strings.Contains(msg, "digitalocean") || !strings.Contains(msg, "--provider") {
		t.Fatalf("ambiguity error did not include sorted choices and flag guidance: %v", err)
	}
}

func TestDeriveRejectsPlaintextSecretLikeConfig(t *testing.T) {
	cfg := &config.WorkflowConfig{}
	mapper := &fakeMapper{modules: []GeneratedModule{{
		Name:      "datadog-agent",
		Type:      "infra.container_service",
		Satisfies: []string{"observability.telemetry.default"},
		Config:    map[string]any{"api_key": "plain-value"},
	}}}
	_, err := Derive(context.Background(), cfg, nil, mapper, Options{Provider: "datadog", NonInteractive: true})
	if err == nil {
		t.Fatalf("derive succeeded, want plaintext secret rejection")
	}
	msg := err.Error()
	if !strings.Contains(msg, "datadog-agent") || !strings.Contains(msg, "api_key") {
		t.Fatalf("secret error should name module and key, got: %v", err)
	}
	if strings.Contains(msg, "plain-value") {
		t.Fatalf("secret error leaked value: %v", err)
	}
}

func TestDeriveAcceptsSecretPlaceholders(t *testing.T) {
	cfg := &config.WorkflowConfig{}
	mapper := &fakeMapper{modules: []GeneratedModule{{
		Name:   "datadog-agent",
		Type:   "infra.container_service",
		Config: map[string]any{"api_key": "${DATADOG_API_KEY}"},
	}}}
	if _, err := Derive(context.Background(), cfg, nil, mapper, Options{Provider: "datadog", NonInteractive: true}); err != nil {
		t.Fatalf("derive rejected placeholder secret: %v", err)
	}
}

func TestDeriveGeneratedModulesInheritAcceptedKeysAndSurfaceDiagnostics(t *testing.T) {
	cfg := &config.WorkflowConfig{}
	mapper := &fakeMapper{
		accepted: []string{"observability.telemetry.default"},
		modules:  []GeneratedModule{{Name: "otel", Type: "infra.container_service"}},
		rejected: []Diagnostic{{Key: "database.default", Code: "unsupported", Message: "no database mapper"}},
		notes:    []Note{{Key: "observability.telemetry.default", Message: "sidecar selected", Interactive: true}},
	}
	result, err := Derive(context.Background(), cfg, []requirements.Requirement{{
		Key:  "observability.telemetry.default",
		Kind: requirements.KindObservability,
	}}, mapper, Options{Provider: "digitalocean", NonInteractive: true})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if got := result.Modules[0].Satisfies; len(got) != 1 || got[0] != "observability.telemetry.default" {
		t.Fatalf("module satisfies = %v", got)
	}
	if len(result.Rejected) != 1 || result.Rejected[0].Key != "database.default" {
		t.Fatalf("rejected diagnostics not surfaced: %#v", result.Rejected)
	}
	if len(result.Notes) != 1 || !result.Notes[0].Interactive {
		t.Fatalf("notes not surfaced: %#v", result.Notes)
	}
}

type fakeMapper struct {
	last     MapRequest
	accepted []string
	modules  []GeneratedModule
	rejected []Diagnostic
	notes    []Note
}

func (m *fakeMapper) MapRequirements(_ context.Context, req MapRequest) (MapResult, error) {
	m.last = req
	return MapResult{
		AcceptedKeys: m.accepted,
		Modules:      m.modules,
		Rejected:     m.rejected,
		Notes:        m.notes,
	}, nil
}
