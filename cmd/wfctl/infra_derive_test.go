package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/derive"
)

func TestInfraDeriveHelpIncludesFlags(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return runInfraDerive([]string{"--help"})
	})
	if err == nil {
		t.Fatalf("help returned nil error, want flag.ErrHelp")
	}
	for _, want := range []string{"--write", "--dry-run", "--provider", "--runtime", "--env", "--non-interactive"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %s:\n%s", want, out)
		}
	}
}

func TestInfraDeriveDryRunWithFakeMapperDoesNotMutateFile(t *testing.T) {
	restore := installInfraDeriveFakeMapper(t, []derive.GeneratedModule{{
		Name:      "otel-collector",
		Type:      "infra.container_service",
		Satisfies: []string{"observability.telemetry.default"},
		Config:    map[string]any{"image": "otel/opentelemetry-collector-contrib:latest"},
	}})
	defer restore()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "workflow.yaml")
	original := "name: demo\nmodules: []\n"
	if err := os.WriteFile(cfgPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return runInfraDerive([]string{"--config", cfgPath, "--provider", "fake", "--dry-run", "--non-interactive"})
	})
	if err != nil {
		t.Fatalf("infra derive dry-run: %v", err)
	}
	if !strings.Contains(out, "satisfies:") || !strings.Contains(out, "observability.telemetry.default") {
		t.Fatalf("dry-run output missing derived module:\n%s", out)
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Fatalf("dry-run mutated file:\n%s", after)
	}
}

func TestInfraDeriveWriteIsIdempotent(t *testing.T) {
	restore := installInfraDeriveFakeMapper(t, []derive.GeneratedModule{{
		Name:      "otel-collector",
		Type:      "infra.container_service",
		Satisfies: []string{"observability.telemetry.default"},
	}})
	defer restore()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte("name: demo\nmodules: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runInfraDerive([]string{"--config", cfgPath, "--provider", "fake", "--write", "--non-interactive"}); err != nil {
		t.Fatalf("infra derive write: %v", err)
	}
	first, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "otel-collector") {
		t.Fatalf("write did not add module:\n%s", first)
	}
	out, err := captureStdout(t, func() error {
		return runInfraDerive([]string{"--config", cfgPath, "--provider", "fake", "--write", "--non-interactive"})
	})
	if err != nil {
		t.Fatalf("second infra derive write: %v", err)
	}
	second, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Fatalf("second write changed file\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if !strings.Contains(out, "No derived IaC changes") {
		t.Fatalf("second write output missing no-op message:\n%s", out)
	}
}

func TestInfraDeriveMutatesOnlyRootConfigWhenImportsContributeRequirements(t *testing.T) {
	restore := installInfraDeriveFakeMapper(t, []derive.GeneratedModule{{
		Name:      "web-runtime",
		Type:      "infra.container_service",
		Satisfies: []string{"web.api.api"},
	}})
	defer restore()

	dir := t.TempDir()
	importPath := filepath.Join(dir, "service.yaml")
	importOriginal := "services:\n  api:\n    binary: ./api\n"
	if err := os.WriteFile(importPath, []byte(importOriginal), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte("imports:\n  - service.yaml\nmodules: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runInfraDerive([]string{"--config", cfgPath, "--provider", "fake", "--write", "--non-interactive"}); err != nil {
		t.Fatalf("infra derive write: %v", err)
	}
	importAfter, err := os.ReadFile(importPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(importAfter) != importOriginal {
		t.Fatalf("import file mutated:\n%s", importAfter)
	}
	rootAfter, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rootAfter), "web-runtime") {
		t.Fatalf("root config missing derived module:\n%s", rootAfter)
	}
}

func TestInfraDeriveNonInteractiveAmbiguousProvider(t *testing.T) {
	restore := installInfraDeriveFakeMapper(t, nil)
	defer restore()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "workflow.yaml")
	cfg := `modules:
  - name: aws-provider
    type: iac.provider
    config:
      provider: aws
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runInfraDerive([]string{"--config", cfgPath, "--non-interactive"})
	if err == nil {
		t.Fatalf("infra derive succeeded, want ambiguity error")
	}
	if !strings.Contains(err.Error(), "aws") || !strings.Contains(err.Error(), "digitalocean") {
		t.Fatalf("ambiguity error missing choices: %v", err)
	}
}

func installInfraDeriveFakeMapper(t *testing.T, modules []derive.GeneratedModule) func() {
	t.Helper()
	prev := infraDeriveMapperFactory
	infraDeriveMapperFactory = func(context.Context, string, map[string]any) (derive.ProviderMapper, func(), error) {
		return deriveFakeMapper{modules: modules}, nil, nil
	}
	return func() { infraDeriveMapperFactory = prev }
}

type deriveFakeMapper struct {
	modules []derive.GeneratedModule
}

func (m deriveFakeMapper) MapRequirements(context.Context, derive.MapRequest) (derive.MapResult, error) {
	accepted := make([]string, 0)
	for _, mod := range m.modules {
		accepted = append(accepted, mod.Satisfies...)
	}
	return derive.MapResult{AcceptedKeys: accepted, Modules: m.modules}, nil
}
