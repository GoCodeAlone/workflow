package module_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/platform"
)

// ─── Mock IaCProvider ─────────────────────────────────────────────────────────

type recordingProvider struct {
	providerName string
	driverType   string
	sizingCalls  []recordedSizingCall
	driver       *recordingDriver
}

type recordedSizingCall struct {
	ResourceType string
	Size         interfaces.Size
	Hints        *interfaces.ResourceHints
}

type recordingDriver struct {
	createSpecs []interfaces.ResourceSpec
}

func newRecordingProvider(name string) *recordingProvider {
	return &recordingProvider{
		providerName: name,
		driver:       &recordingDriver{},
	}
}

func (p *recordingProvider) Name() string    { return p.providerName }
func (p *recordingProvider) Version() string { return "0.1.0" }
func (p *recordingProvider) Initialize(_ context.Context, _ map[string]any) error {
	return nil
}
func (p *recordingProvider) Capabilities() []interfaces.IaCCapabilityDeclaration { return nil }
func (p *recordingProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (p *recordingProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (p *recordingProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (p *recordingProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *recordingProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *recordingProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (p *recordingProvider) ResolveSizing(resourceType string, size interfaces.Size, hints *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	p.sizingCalls = append(p.sizingCalls, recordedSizingCall{
		ResourceType: resourceType,
		Size:         size,
		Hints:        hints,
	})
	return &interfaces.ProviderSizing{InstanceType: "mock." + string(size)}, nil
}
func (p *recordingProvider) ResourceDriver(resourceType string) (interfaces.ResourceDriver, error) {
	p.driverType = resourceType
	return p.driver, nil
}
func (p *recordingProvider) Close() error { return nil }

func (d *recordingDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.createSpecs = append(d.createSpecs, spec)
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, Status: "created"}, nil
}
func (d *recordingDriver) Read(_ context.Context, _ interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *recordingDriver) Update(_ context.Context, _ interfaces.ResourceRef, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *recordingDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }
func (d *recordingDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return nil, nil
}
func (d *recordingDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return nil, nil
}
func (d *recordingDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}

// configHashIntegration replicates platform.configHash for test assertions.
func configHashIntegration(config map[string]any) string {
	if len(config) == 0 {
		return ""
	}
	data, _ := json.Marshal(config)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// ─── Test 1: SameConfigDifferentProviders ─────────────────────────────────────

// TestInfraModule_SameConfigDifferentProviders verifies that the same infra
// config block produces identical ResourceSpecs regardless of which provider
// backs the module. Provider differences should be confined to ResolveSizing
// output, not the specs passed to the driver.
func TestInfraModule_SameConfigDifferentProviders(t *testing.T) {
	app := module.NewMockApplication()

	providerNames := []string{"aws", "gcp", "do"}
	providers := make(map[string]*recordingProvider, 3)

	for _, name := range providerNames {
		p := newRecordingProvider(name)
		providers[name] = p
		if err := app.RegisterService(name, p); err != nil {
			t.Fatalf("RegisterService %s: %v", name, err)
		}
	}

	// Each module uses the same logical config but points to a different provider.
	ctx := context.Background()
	for _, name := range providerNames {
		cfg := map[string]any{
			"provider": name,
			"size":     "m",
			"engine":   "postgres",
		}
		m := module.NewInfraModule("my-db", "infra.database", cfg)
		if err := m.Init(app); err != nil {
			t.Fatalf("Init[%s]: %v", name, err)
		}
		if _, err := m.Create(ctx); err != nil {
			t.Fatalf("Create[%s]: %v", name, err)
		}
	}

	// Collect the spec each provider's driver received.
	var reference *interfaces.ResourceSpec
	for _, name := range providerNames {
		p := providers[name]
		if len(p.driver.createSpecs) != 1 {
			t.Fatalf("provider %s: expected 1 Create call, got %d", name, len(p.driver.createSpecs))
		}
		spec := p.driver.createSpecs[0]

		// Spec values must match the declared config.
		if spec.Name != "my-db" {
			t.Errorf("provider %s: spec.Name = %q, want my-db", name, spec.Name)
		}
		if spec.Type != "infra.database" {
			t.Errorf("provider %s: spec.Type = %q, want infra.database", name, spec.Type)
		}
		if spec.Size != interfaces.SizeM {
			t.Errorf("provider %s: spec.Size = %q, want m", name, spec.Size)
		}
		if spec.Config["engine"] != "postgres" {
			t.Errorf("provider %s: spec.Config[engine] = %v, want postgres", name, spec.Config["engine"])
		}
		// Standard keys must be stripped from config.
		if _, ok := spec.Config["provider"]; ok {
			t.Errorf("provider %s: spec.Config contains 'provider' key (should be stripped)", name)
		}
		if _, ok := spec.Config["size"]; ok {
			t.Errorf("provider %s: spec.Config contains 'size' key (should be stripped)", name)
		}

		// All three specs must be identical to the first.
		if reference == nil {
			copy := spec
			reference = &copy
		} else {
			if spec.Name != reference.Name || spec.Type != reference.Type || spec.Size != reference.Size {
				t.Errorf("provider %s: spec differs from reference: got %+v, want %+v", name, spec, *reference)
			}
		}
	}
}

// ─── Test 2: PlanProducesCorrectActions ───────────────────────────────────────

// TestInfraModule_PlanProducesCorrectActions verifies that ComputePlan against
// an empty current state generates exactly 3 create actions in dependency order
// (vpc before its dependents).
func TestInfraModule_PlanProducesCorrectActions(t *testing.T) {
	desired := []interfaces.ResourceSpec{
		{
			Name:   "vpc",
			Type:   "infra.vpc",
			Size:   interfaces.SizeM,
			Config: map[string]any{"cidr": "10.0.0.0/16"},
		},
		{
			Name:      "database",
			Type:      "infra.database",
			Size:      interfaces.SizeM,
			Config:    map[string]any{"engine": "postgres"},
			DependsOn: []string{"vpc"},
		},
		{
			Name:      "container-service",
			Type:      "infra.container_service",
			Size:      interfaces.SizeS,
			Config:    map[string]any{"image": "nginx", "replicas": 2},
			DependsOn: []string{"vpc"},
		},
	}

	plan, err := platform.ComputePlan(desired, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}

	if len(plan.Actions) != 3 {
		t.Fatalf("expected 3 actions, got %d: %+v", len(plan.Actions), plan.Actions)
	}

	for i, a := range plan.Actions {
		if a.Action != "create" {
			t.Errorf("action[%d]: expected create, got %q", i, a.Action)
		}
	}

	pos := make(map[string]int, 3)
	for i, a := range plan.Actions {
		pos[a.Resource.Name] = i
	}

	if pos["vpc"] >= pos["database"] {
		t.Errorf("vpc (pos %d) must precede database (pos %d)", pos["vpc"], pos["database"])
	}
	if pos["vpc"] >= pos["container-service"] {
		t.Errorf("vpc (pos %d) must precede container-service (pos %d)", pos["vpc"], pos["container-service"])
	}
}

// ─── Test 3: DriftDetectionFlow ───────────────────────────────────────────────

// TestInfraModule_DriftDetectionFlow verifies that ComputePlan emits an update
// when state has a stale config hash, and emits nothing when the hash matches.
func TestInfraModule_DriftDetectionFlow(t *testing.T) {
	vpcConfig := map[string]any{"cidr": "10.0.0.0/16"}
	vpc := interfaces.ResourceSpec{
		Name:   "vpc",
		Type:   "infra.vpc",
		Config: vpcConfig,
	}

	// State with a mismatched config hash → expect an update action.
	staleState := []interfaces.ResourceState{
		{
			Name:       "vpc",
			Type:       "infra.vpc",
			ConfigHash: "stale-hash",
			UpdatedAt:  time.Now(),
		},
	}
	plan, err := platform.ComputePlan([]interfaces.ResourceSpec{vpc}, staleState)
	if err != nil {
		t.Fatalf("ComputePlan (drift): %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Action != "update" {
		t.Errorf("expected 1 update action, got %+v", plan.Actions)
	}

	// State whose config hash matches desired → expect no actions (no drift).
	currentHash := configHashIntegration(vpcConfig)
	freshState := []interfaces.ResourceState{
		{
			Name:       "vpc",
			Type:       "infra.vpc",
			ConfigHash: currentHash,
			UpdatedAt:  time.Now(),
		},
	}
	plan2, err := platform.ComputePlan([]interfaces.ResourceSpec{vpc}, freshState)
	if err != nil {
		t.Fatalf("ComputePlan (no drift): %v", err)
	}
	if len(plan2.Actions) != 0 {
		t.Errorf("expected empty plan (no drift), got %+v", plan2.Actions)
	}
}

// ─── Test 4: DestroyReverseOrder ──────────────────────────────────────────────

// TestInfraModule_DestroyReverseOrder verifies that when desired is empty
// (destroy all), ComputePlan orders deletes so dependents are removed before
// their dependencies: container-service → database → vpc.
func TestInfraModule_DestroyReverseOrder(t *testing.T) {
	current := []interfaces.ResourceState{
		{
			Name:         "vpc",
			Type:         "infra.vpc",
			Dependencies: nil,
		},
		{
			Name:         "database",
			Type:         "infra.database",
			Dependencies: []string{"vpc"},
		},
		{
			Name:         "container-service",
			Type:         "infra.container_service",
			Dependencies: []string{"vpc", "database"},
		},
	}

	plan, err := platform.ComputePlan(nil, current)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}

	if len(plan.Actions) != 3 {
		t.Fatalf("expected 3 delete actions, got %d: %+v", len(plan.Actions), plan.Actions)
	}
	for i, a := range plan.Actions {
		if a.Action != "delete" {
			t.Errorf("action[%d]: expected delete, got %q", i, a.Action)
		}
	}

	pos := make(map[string]int, 3)
	for i, a := range plan.Actions {
		pos[a.Resource.Name] = i
	}

	if pos["container-service"] >= pos["database"] {
		t.Errorf("container-service (pos %d) must be deleted before database (pos %d)",
			pos["container-service"], pos["database"])
	}
	if pos["database"] >= pos["vpc"] {
		t.Errorf("database (pos %d) must be deleted before vpc (pos %d)",
			pos["database"], pos["vpc"])
	}
}

// ─── Test 5: SizingPassthrough ────────────────────────────────────────────────

// TestInfraModule_SizingPassthrough verifies that size=l and resources.memory=32Gi
// are correctly parsed by InfraModule.Init and forwarded verbatim when
// ResolveSizing is called through the provider.
func TestInfraModule_SizingPassthrough(t *testing.T) {
	app := module.NewMockApplication()
	p := newRecordingProvider("aws")
	if err := app.RegisterService("aws", p); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	cfg := map[string]any{
		"provider": "aws",
		"size":     "l",
		"resources": map[string]any{
			"memory": "32Gi",
		},
		"engine": "postgres",
	}
	m := module.NewInfraModule("my-db", "infra.database", cfg)
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Verify the module parsed sizing correctly.
	if m.Size() != interfaces.SizeL {
		t.Errorf("Size = %q, want l", m.Size())
	}
	if m.Hints() == nil {
		t.Fatal("expected non-nil Hints")
	}
	if m.Hints().Memory != "32Gi" {
		t.Errorf("Hints.Memory = %q, want 32Gi", m.Hints().Memory)
	}

	// Call ResolveSizing through the provider with the module's values and
	// verify the provider receives exactly those arguments.
	if _, err := m.Provider().ResolveSizing(m.InfraType(), m.Size(), m.Hints()); err != nil {
		t.Fatalf("ResolveSizing: %v", err)
	}

	if len(p.sizingCalls) != 1 {
		t.Fatalf("expected 1 ResolveSizing call, got %d", len(p.sizingCalls))
	}
	call := p.sizingCalls[0]
	if call.ResourceType != "infra.database" {
		t.Errorf("ResolveSizing resourceType = %q, want infra.database", call.ResourceType)
	}
	if call.Size != interfaces.SizeL {
		t.Errorf("ResolveSizing size = %q, want l", call.Size)
	}
	if call.Hints == nil || call.Hints.Memory != "32Gi" {
		t.Errorf("ResolveSizing hints = %+v, want Memory=32Gi", call.Hints)
	}
}
