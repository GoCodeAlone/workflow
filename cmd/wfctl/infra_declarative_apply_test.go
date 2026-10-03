package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/sensitive"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
)

type declarativeCLIDriver struct {
	stubSensitiveDriver
	passwords []string
}

func (*declarativeCLIDriver) SensitiveInputPaths(context.Context) ([]string, error) {
	return []string{"/password"}, nil
}

func (d *declarativeCLIDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.passwords = append(d.passwords, spec.Config["password"].(string))
	spec.Config["password"] = "driver-mutation"
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "database-id"}, nil
}

type declarativeCLIProvider struct {
	applyCapture
	driver    interfaces.ResourceDriver
	driverErr error
}

type sensitiveAdoptionCLIDriver struct {
	declarativeCLIDriver
	reads int
}

func (*sensitiveAdoptionCLIDriver) AdoptionRef(spec interfaces.ResourceSpec) (interfaces.ResourceRef, bool, error) {
	return interfaces.ResourceRef{Name: spec.Name, Type: spec.Type, ProviderID: "existing-database"}, true, nil
}

func (d *sensitiveAdoptionCLIDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	d.reads++
	return &interfaces.ResourceOutput{Name: ref.Name, Type: ref.Type, ProviderID: ref.ProviderID}, nil
}

func TestApplyWithProvider_SensitiveRoutingRejectsBeforeAdoption(t *testing.T) {
	driver := &sensitiveAdoptionCLIDriver{}
	provider := &declarativeCLIProvider{driver: driver}
	store := &stubInfraStore{}
	err := applyWithProviderAndStore(t.Context(), provider, "test-cloud", []interfaces.ResourceSpec{{Name: "database", Type: "infra.database", Config: map[string]any{"password": "known-rejected-private-literal"}}}, nil, store, io.Discard, "", "", nil)
	if !errors.Is(err, interfaces.ErrValidation) || driver.reads != 0 || len(store.saved) != 0 {
		t.Fatalf("validation must precede adoption/persistence: err=%v reads=%d saved=%d", err, driver.reads, len(store.saved))
	}
	if strings.Contains(err.Error(), "known-rejected-private-literal") {
		t.Fatal("validation disclosed rejected credential")
	}
}

func (p *declarativeCLIProvider) ResourceDriver(string) (interfaces.ResourceDriver, error) {
	return p.driver, p.driverErr
}

func TestDeclarativePlanning_DiscoveryFailureStopsBeforePlan(t *testing.T) {
	for _, discoveryErr := range []error{interfaces.ErrForbidden, interfaces.ErrTransient, context.Canceled} {
		t.Run(discoveryErr.Error(), func(t *testing.T) {
			provider := &declarativeCLIProvider{driverErr: discoveryErr}
			plan, err := computeDeclarativeInfraPlan(t.Context(), provider, []interfaces.ResourceSpec{{Name: "database", Type: "infra.database", Config: map[string]any{"password": "rejected-unvalidated-literal"}}}, nil, nil, "")
			if !errors.Is(err, discoveryErr) || len(plan.Actions) != 0 {
				t.Fatalf("discovery failure produced an unvalidated plan: err=%v actions=%v", err, plan.Actions)
			}
		})
	}
}

type crossProviderCLIDriver struct{ declarativeCLIDriver }

func (*crossProviderCLIDriver) SensitiveInputPaths(context.Context) ([]string, error) {
	return []string{"/env_vars/*"}, nil
}

func (d *crossProviderCLIDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.passwords = append(d.passwords, spec.Config["env_vars"].(map[string]any)["PASSWORD"].(string))
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "child-id"}, nil
}

func TestInfraCLI_DeclarativeCrossProviderReferenceState(t *testing.T) {
	const marker = "known-cross-provider-runtime-value"
	t.Setenv(strings.ToUpper(sensitive.SecretKey("parent", "password")), marker)
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		t.Fatal(err)
	}
	writeStateFile(t, stateDir, "parent", "infra.database", "parent-id", map[string]any{"password": sensitive.Placeholder("parent", "password")}, map[string]any{"provider": "parent-provider"})
	cfgPath := filepath.Join(dir, "infra.yaml")
	cfg := `infra: {auto_bootstrap: false}
modules:
  - name: parent-provider
    type: iac.provider
    config: {provider: test-cloud}
  - name: child-provider
    type: iac.provider
    config: {provider: test-cloud}
  - name: state
    type: iac.state
    config: {backend: filesystem, directory: ` + stateDir + `}
  - name: parent
    type: infra.database
    config: {provider: parent-provider}
  - name: child
    type: infra.database
    config: {provider: child-provider, env_vars: {PASSWORD: "${parent.password}"}}
secrets: {provider: env}
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	driver := &crossProviderCLIDriver{}
	provider := &declarativeCLIProvider{driver: driver}
	original := resolveIaCProvider
	resolveIaCProvider = func(context.Context, string, map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return provider, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = original })
	if _, err := applyInfraModules(t.Context(), cfgPath, ""); err != nil {
		t.Fatal(err)
	}
	if len(driver.passwords) != 1 || driver.passwords[0] != marker {
		t.Fatalf("cross-provider parent was not hydrated: %v", driver.passwords)
	}
	state, err := loadCurrentState(cfgPath, "")
	if err != nil || len(state) != 2 {
		t.Fatalf("ownership reconciliation changed parent: state=%v err=%v", state, err)
	}
	for _, resource := range state {
		if resource.Name == "parent" && resource.ProviderID != "parent-id" {
			t.Fatal("parent ownership was mutated")
		}
		if resource.Name == "child" && resource.AppliedConfig["env_vars"].(map[string]any)["PASSWORD"] != "${parent.password}" {
			t.Fatal("cross-provider config was not declarative")
		}
	}
}

func TestInfraCLI_KnownValueAbsentFromPlanAndAppliedState(t *testing.T) {
	const marker = "known-cli-private-runtime-value"
	const ref = "${WFCTL_CLI_DECLARATIVE_TOKEN}"
	t.Setenv("WFCTL_CLI_DECLARATIVE_TOKEN", marker)
	t.Setenv("WFCTL_CLI_PUBLIC_REGION", "public-region")
	for _, mode := range []string{"direct", "persisted"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			stateDir := filepath.Join(dir, "state")
			if err := os.MkdirAll(stateDir, 0o750); err != nil {
				t.Fatal(err)
			}
			writeStateFile(t, stateDir, "parent", "infra.database", "parent-id",
				map[string]any{"password": sensitive.Placeholder("parent", "password")},
				map[string]any{"provider": "provider", "region": "public-region"})
			cfgPath := filepath.Join(dir, "infra.yaml")
			cfg := `infra:
  auto_bootstrap: false
modules:
  - name: provider
    type: iac.provider
    config: {provider: test-cloud}
  - name: state
    type: iac.state
    config: {backend: filesystem, directory: ` + stateDir + `}
  - name: database
    type: infra.database
    config:
      provider: provider
      password: "${WFCTL_CLI_DECLARATIVE_TOKEN}"
      region: "${WFCTL_CLI_PUBLIC_REGION}"
  - name: parent
    type: infra.database
    config: {provider: provider, region: public-region}
secrets:
  provider: env
  generate:
    - key: WFCTL_CLI_DECLARATIVE_TOKEN
      type: infra_output
      source: parent.password
`
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
				t.Fatal(err)
			}
			driver := &declarativeCLIDriver{}
			provider := &declarativeCLIProvider{driver: driver}
			originalResolver := resolveIaCProvider
			resolveIaCProvider = func(context.Context, string, map[string]any) (interfaces.IaCProvider, io.Closer, error) {
				return provider, nil, nil
			}
			t.Cleanup(func() { resolveIaCProvider = originalResolver })
			originalCompute := computeInfraPlan
			var observedPlan []byte
			computeInfraPlan = func(ctx context.Context, p interfaces.IaCProvider, specs []interfaces.ResourceSpec, current []interfaces.ResourceState) (interfaces.IaCPlan, error) {
				plan, err := platform.ComputePlan(ctx, p, specs, current)
				observedPlan, _ = json.Marshal(plan)
				return plan, err
			}
			t.Cleanup(func() { computeInfraPlan = originalCompute })
			if mode == "direct" {
				if _, err := applyInfraModules(t.Context(), cfgPath, ""); err != nil {
					t.Fatal(err)
				}
			} else {
				planPath := filepath.Join(dir, "plan.json")
				if err := runInfraPlan([]string{"--config", cfgPath, "-o", planPath}); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(planPath)
				if err != nil || strings.Contains(string(data), marker) {
					t.Fatalf("persisted plan leaked known value: err=%v plan=%s", err, data)
				}
				plan, err := loadPlanFromFile(planPath)
				if err != nil {
					t.Fatal(err)
				}
				if plan.Actions[0].Resource.Config["password"] != ref || plan.Actions[0].ResolvedConfigHash != platform.ConfigHash(plan.Actions[0].Resource.Config) {
					t.Fatal("plan config/hash must remain declarative")
				}
				if err := runInfraApply([]string{"--config", cfgPath, "--auto-approve", "--plan", planPath}); err != nil {
					t.Fatal(err)
				}
			}
			if strings.Contains(string(observedPlan), marker) {
				t.Fatalf("plan computation already contained resolved credential: %s", observedPlan)
			}
			if len(driver.passwords) != 1 || driver.passwords[0] != marker {
				t.Fatalf("driver must receive resolved credential once: %v", driver.passwords)
			}
			state, err := loadCurrentState(cfgPath, "")
			if err != nil || len(state) != 2 {
				t.Fatalf("load persisted state: state=%v err=%v", state, err)
			}
			data, err := json.Marshal(state)
			if err != nil || strings.Contains(string(data), marker) || strings.Contains(string(data), "driver-mutation") {
				t.Fatalf("state leaked resolved or driver-mutated config: %s err=%v", data, err)
			}
			var applied interfaces.ResourceState
			for _, resource := range state {
				if resource.Name == "database" {
					applied = resource
				}
			}
			if applied.AppliedConfig["password"] != ref || applied.ConfigHash != platform.ConfigHash(applied.AppliedConfig) || applied.AppliedConfig["region"] != "public-region" {
				t.Fatalf("declarative state/hash or public resolution lost: %+v", applied)
			}
		})
	}
}
