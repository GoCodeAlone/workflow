package wfctlhelpers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeDriverWithUpsert is a ResourceDriver test double that:
//   - returns createErr from Create (typically ErrResourceAlreadyExists)
//   - returns readResult / readErr from Read
//   - records whether Update was called via updateCalled
//   - implements UpsertSupporter when supportsUpsert is true
//
// Used to exercise doCreate's upsert-recovery path. Embedded fakeDriver
// supplies the no-op other methods so the recorder is minimal.
type fakeDriverWithUpsert struct {
	*fakeDriver
	createErr      error
	readResult     *interfaces.ResourceOutput
	readErr        error
	updateCalled   bool
	updateOut      *interfaces.ResourceOutput
	supportsUpsert bool
}

func (d *fakeDriverWithUpsert) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.fakeDriver.createCount++
	if d.createErr != nil {
		return nil, d.createErr
	}
	return &interfaces.ResourceOutput{}, nil
}

func (d *fakeDriverWithUpsert) Read(_ context.Context, _ interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	if d.readErr != nil {
		return nil, d.readErr
	}
	return d.readResult, nil
}

func (d *fakeDriverWithUpsert) Update(_ context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.updateCalled = true
	d.fakeDriver.updateCount++
	if d.updateOut != nil {
		return d.updateOut, nil
	}
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: ref.ProviderID}, nil
}

func (d *fakeDriverWithUpsert) SupportsUpsert() bool { return d.supportsUpsert }

// upsertFakeProvider returns the fakeDriverWithUpsert from ResourceDriver
// regardless of resource type.
type upsertFakeProvider struct {
	*fakeProvider
	upsert *fakeDriverWithUpsert
}

func newUpsertFakeProvider(d *fakeDriverWithUpsert) *upsertFakeProvider {
	if d.fakeDriver == nil {
		d.fakeDriver = &fakeDriver{}
	}
	return &upsertFakeProvider{fakeProvider: newFakeProvider(), upsert: d}
}

func (p *upsertFakeProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return p.upsert, nil
}

// TestApplyPlan_Create_UpsertOnAlreadyExists is the canonical T3.2 test:
// a Create that returns ErrResourceAlreadyExists must trigger
// Read + Update on a driver that opts in via UpsertSupporter. The
// recovery must clear the per-action error so result.Errors is empty.
func TestApplyPlan_Create_UpsertOnAlreadyExists(t *testing.T) {
	d := &fakeDriverWithUpsert{
		createErr:      interfaces.ErrResourceAlreadyExists,
		readResult:     &interfaces.ResourceOutput{ProviderID: "found-uuid"},
		supportsUpsert: true,
	}
	fp := newUpsertFakeProvider(d)
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "create", Resource: spec("a", "infra.vpc")},
	}}
	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) > 0 {
		t.Errorf("upsert should recover; got errors: %v", result.Errors)
	}
	if !d.updateCalled {
		t.Errorf("upsert path should call Update after Read; updateCalled=%v", d.updateCalled)
	}
}

// TestApplyPlan_Create_AlreadyExists_NoUpsertSupport verifies the
// fall-through: if the driver does NOT implement UpsertSupporter (or
// returns SupportsUpsert()==false), the original ErrResourceAlreadyExists
// surfaces unchanged via result.Errors. No Read or Update happens.
func TestApplyPlan_Create_AlreadyExists_NoUpsertSupport(t *testing.T) {
	d := &fakeDriverWithUpsert{
		createErr:      interfaces.ErrResourceAlreadyExists,
		supportsUpsert: false,
	}
	fp := newUpsertFakeProvider(d)
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "create", Resource: spec("a", "infra.vpc")},
	}}
	result, _ := ApplyPlan(context.Background(), fp, plan)
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 per-action error; got %d (%v)", len(result.Errors), result.Errors)
	}
	if !strings.Contains(result.Errors[0].Error, "already exists") {
		t.Errorf("expected ErrResourceAlreadyExists in error; got %q", result.Errors[0].Error)
	}
	if d.updateCalled {
		t.Errorf("Update must not be called when SupportsUpsert returns false")
	}
}

// alreadyExistsBareDriver is a ResourceDriver that does NOT implement
// UpsertSupporter at all (no SupportsUpsert method). Used to exercise
// doCreate's `!ok` interface-assertion fall-through — distinct from
// the `SupportsUpsert()==false` path covered above. Behavioral
// outcome is identical (conflict surfaces unchanged, no Read/Update),
// but the code path through the type assertion is different.
type alreadyExistsBareDriver struct {
	*fakeDriver
}

func (d *alreadyExistsBareDriver) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.fakeDriver.createCount++
	return nil, interfaces.ErrResourceAlreadyExists
}

// TestApplyPlan_Create_AlreadyExists_DriverDoesNotImplementUpsertSupporter
// covers the `!ok` arm of doCreate's `us, ok := d.(interfaces.UpsertSupporter)`
// type assertion: a driver that does not implement UpsertSupporter at
// all. Distinct from TestApplyPlan_Create_AlreadyExists_NoUpsertSupport,
// which covers the `ok && !us.SupportsUpsert()` arm.
func TestApplyPlan_Create_AlreadyExists_DriverDoesNotImplementUpsertSupporter(t *testing.T) {
	bare := &alreadyExistsBareDriver{fakeDriver: &fakeDriver{}}
	// Sanity-check the test premise: bare must NOT satisfy UpsertSupporter.
	// If a future refactor lifts SupportsUpsert onto the embedded fakeDriver,
	// this test would silently switch to the "ok && !SupportsUpsert" branch
	// and stop covering the `!ok` arm. The compile-time assertion locks
	// the premise.
	var _ interfaces.ResourceDriver = bare
	if _, ok := any(bare).(interfaces.UpsertSupporter); ok {
		t.Fatal("test premise broken: alreadyExistsBareDriver must not implement UpsertSupporter")
	}
	// Inject the bare driver via a one-off provider that returns it for
	// any resource type.
	fp := &bareDriverProvider{fakeProvider: newFakeProvider(), driver: bare}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "create", Resource: spec("a", "infra.vpc")},
	}}
	result, _ := ApplyPlan(context.Background(), fp, plan)
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 per-action error; got %d (%v)", len(result.Errors), result.Errors)
	}
	if !strings.Contains(result.Errors[0].Error, "already exists") {
		t.Errorf("expected ErrResourceAlreadyExists in error; got %q", result.Errors[0].Error)
	}
}

// bareDriverProvider returns the alreadyExistsBareDriver for any
// resource type so the test stays focused on the type-assertion
// fall-through.
type bareDriverProvider struct {
	*fakeProvider
	driver *alreadyExistsBareDriver
}

func (p *bareDriverProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}

// TestApplyPlan_Create_UpsertReadFailureWraps verifies the diagnostic
// when Read fails after an ErrResourceAlreadyExists conflict: the error
// wraps both the original create error and the read error so callers
// can match either via errors.Is.
func TestApplyPlan_Create_UpsertReadFailureWraps(t *testing.T) {
	readErr := errors.New("read failed: 503")
	d := &fakeDriverWithUpsert{
		createErr:      interfaces.ErrResourceAlreadyExists,
		readErr:        readErr,
		supportsUpsert: true,
	}
	fp := newUpsertFakeProvider(d)
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "create", Resource: spec("a", "infra.vpc")},
	}}
	result, _ := ApplyPlan(context.Background(), fp, plan)
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error; got %d (%v)", len(result.Errors), result.Errors)
	}
	got := result.Errors[0].Error
	if !strings.Contains(got, "upsert: read after conflict") {
		t.Errorf("expected canonical 'upsert: read after conflict' prefix; got %q", got)
	}
	if !strings.Contains(got, "503") {
		t.Errorf("expected wrapped read error in message; got %q", got)
	}
	if d.updateCalled {
		t.Errorf("Update must not be called when Read fails")
	}
}

// TestApplyPlan_Create_UpsertEmptyProviderIDFails verifies a defensive
// check: if Read finds a resource by name but its ProviderID is empty,
// the upsert path must NOT call Update with an empty ProviderID (which
// would route to a Create-by-spec semantics). Instead, fail with a
// diagnostic that names the resource.
func TestApplyPlan_Create_UpsertEmptyProviderIDFails(t *testing.T) {
	d := &fakeDriverWithUpsert{
		createErr:      interfaces.ErrResourceAlreadyExists,
		readResult:     &interfaces.ResourceOutput{ProviderID: ""}, // defensive: empty ID
		supportsUpsert: true,
	}
	fp := newUpsertFakeProvider(d)
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "create", Resource: spec("vpc-1", "infra.vpc")},
	}}
	result, _ := ApplyPlan(context.Background(), fp, plan)
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error; got %d (%v)", len(result.Errors), result.Errors)
	}
	got := result.Errors[0].Error
	if !strings.Contains(got, "ProviderID is empty") {
		t.Errorf("expected 'ProviderID is empty' in diagnostic; got %q", got)
	}
	if !strings.Contains(got, "vpc-1") {
		t.Errorf("diagnostic should name the resource; got %q", got)
	}
	if d.updateCalled {
		t.Errorf("Update must not be called with empty ProviderID")
	}
}

// TestApplyPlan_Create_UpsertAppendsResources verifies happy-path
// plumbing: a successful upsert recovery must also append the Update's
// output to result.Resources so downstream state-write paths see the
// recovered resource exactly as if Create had succeeded directly.
func TestApplyPlan_Create_UpsertAppendsResources(t *testing.T) {
	d := &fakeDriverWithUpsert{
		createErr:      interfaces.ErrResourceAlreadyExists,
		readResult:     &interfaces.ResourceOutput{ProviderID: "found-uuid"},
		updateOut:      &interfaces.ResourceOutput{Name: "vpc-1", Type: "infra.vpc", ProviderID: "found-uuid", Status: "active"},
		supportsUpsert: true,
	}
	fp := newUpsertFakeProvider(d)
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "create", Resource: spec("vpc-1", "infra.vpc")},
	}}
	result, _ := ApplyPlan(context.Background(), fp, plan)
	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource appended; got %d (%+v)", len(result.Resources), result.Resources)
	}
	if got := result.Resources[0].ProviderID; got != "found-uuid" {
		t.Errorf("ProviderID: got %q want %q", got, "found-uuid")
	}
}
