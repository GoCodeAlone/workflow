package main

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ── UUID-format driver ────────────────────────────────────────────────────────

// uuidRD is a ResourceDriver stub that declares IDFormatUUID.
type uuidRD struct {
	interfaces.ResourceDriver // embed for methods we don't use in these tests
}

func (u uuidRD) ProviderIDFormat() interfaces.ProviderIDFormat {
	return interfaces.IDFormatUUID
}

// ── freeform-format driver ────────────────────────────────────────────────────

// freeformRD is a ResourceDriver stub that declares IDFormatFreeform.
type freeformRD struct {
	interfaces.ResourceDriver
}

func (f freeformRD) ProviderIDFormat() interfaces.ProviderIDFormat {
	return interfaces.IDFormatFreeform
}

// ── no-validator driver ───────────────────────────────────────────────────────

// plainRD is a ResourceDriver stub that does NOT implement ProviderIDValidator.
type plainRD struct {
	interfaces.ResourceDriver
}

// ── fake IaCProvider that returns a fixed driver ──────────────────────────────

// fakeValidationProvider implements interfaces.IaCProvider and returns a
// configured driver from ResourceDriver(). All other methods are stubs.
type fakeValidationProvider struct {
	applyCapture // embed for IaCProvider stubs
	driver       interfaces.ResourceDriver
}

func (p *fakeValidationProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}

// newValidationProvider constructs a fakeValidationProvider with the given driver.
func newValidationProvider(d interfaces.ResourceDriver) interfaces.IaCProvider {
	return &fakeValidationProvider{driver: d}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestInfraApply_InputValidation_Warns verifies that validateInputProviderIDs
// logs a WARN when an update action has a stale (name-as-ProviderID) current
// state. The driver call is NOT blocked — soft-warn only.
func TestInfraApply_InputValidation_Warns(t *testing.T) {
	provider := newValidationProvider(uuidRD{})

	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
		Action:   "update",
		Resource: interfaces.ResourceSpec{Name: "bmw-staging", Type: "infra.app_platform"},
		Current: &interfaces.ResourceState{
			Name:       "bmw-staging",
			Type:       "infra.app_platform",
			ProviderID: "bmw-staging", // stale — name stored instead of UUID
		},
	}}}

	var logBuf bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(oldOut)

	validateInputProviderIDs(provider, plan)

	out := logBuf.String()
	if !strings.Contains(out, "non-conformant ProviderID") {
		t.Errorf("expected WARN about non-conformant ProviderID, got: %q", out)
	}
	if !strings.Contains(out, "bmw-staging") {
		t.Errorf("expected offending ProviderID in WARN, got: %q", out)
	}
	if !strings.Contains(out, "uuid") {
		t.Errorf("expected expected format 'uuid' in WARN, got: %q", out)
	}
}

// TestInfraApply_OutputValidation_RejectsBadProviderID verifies that
// validateOutputProviderID returns an error when the driver's declared format
// is IDFormatUUID but the returned ProviderID is not a valid UUID.
func TestInfraApply_OutputValidation_RejectsBadProviderID(t *testing.T) {
	provider := newValidationProvider(uuidRD{})
	output := interfaces.ResourceOutput{
		Name:       "bmw-staging",
		Type:       "infra.app_platform",
		ProviderID: "bmw-staging", // driver bug: returned name, not UUID
	}

	err := validateOutputProviderID(provider, "digitalocean", &output)

	if err == nil {
		t.Fatal("expected error for malformed ProviderID, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "digitalocean") {
		t.Errorf("expected driver/provider name in error, got: %q", msg)
	}
	if !strings.Contains(msg, "bmw-staging") {
		t.Errorf("expected offending ProviderID in error, got: %q", msg)
	}
	if !strings.Contains(msg, "uuid") {
		t.Errorf("expected expected format in error, got: %q", msg)
	}
	if !strings.Contains(msg, "state not persisted") {
		t.Errorf("expected 'state not persisted' in error, got: %q", msg)
	}
}

// TestInfraApply_OutputValidation_AcceptsNonEmptyFreeform verifies that
// freeform drivers accept arbitrary non-empty provider IDs.
func TestInfraApply_OutputValidation_AcceptsNonEmptyFreeform(t *testing.T) {
	provider := newValidationProvider(freeformRD{})
	output := interfaces.ResourceOutput{
		Name:       "my-bucket",
		Type:       "infra.spaces",
		ProviderID: "any-bucket-name-is-fine", // valid for freeform
	}

	err := validateOutputProviderID(provider, "digitalocean", &output)

	if err != nil {
		t.Errorf("Freeform format should not fail, got: %v", err)
	}
}

func TestInfraApply_OutputValidation_RejectsEmptyFreeform(t *testing.T) {
	provider := newValidationProvider(freeformRD{})
	output := interfaces.ResourceOutput{
		Name:       "my-bucket",
		Type:       "infra.spaces",
		ProviderID: "",
	}

	err := validateOutputProviderID(provider, "digitalocean", &output)

	if err == nil {
		t.Fatal("expected error for empty freeform ProviderID, got nil")
	}
	if !strings.Contains(err.Error(), "freeform") || !strings.Contains(err.Error(), "state not persisted") {
		t.Fatalf("error = %v, want freeform ProviderID validation failure", err)
	}
}

// TestInfraApply_NoValidator_BackwardCompat verifies that drivers that do not
// implement ProviderIDValidator are not validated — any ProviderID passes.
func TestInfraApply_NoValidator_BackwardCompat(t *testing.T) {
	provider := newValidationProvider(plainRD{})
	output := interfaces.ResourceOutput{
		Name:       "x",
		Type:       "infra.app_platform",
		ProviderID: "literally-anything", // no format declared → not validated
	}

	err := validateOutputProviderID(provider, "digitalocean", &output)

	if err != nil {
		t.Errorf("driver without ProviderIDValidator should pass through, got: %v", err)
	}
}

// TestInfraApply_InputValidation_NoWarnForCreate verifies that create actions
// are not validated on input (no current state exists yet).
func TestInfraApply_InputValidation_NoWarnForCreate(t *testing.T) {
	provider := newValidationProvider(uuidRD{})

	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
		Action:   "create",
		Resource: interfaces.ResourceSpec{Name: "new-app", Type: "infra.app_platform"},
		Current:  nil, // no current state for creates
	}}}

	var logBuf bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(oldOut)

	validateInputProviderIDs(provider, plan)

	out := logBuf.String()
	if strings.Contains(out, "non-conformant") {
		t.Errorf("create action should not trigger WARN, got: %q", out)
	}
}

// TestInfraApply_InputValidation_ValidUUIDNoWarn verifies that a well-formed
// UUID in current state does not trigger a WARN.
func TestInfraApply_InputValidation_ValidUUIDNoWarn(t *testing.T) {
	provider := newValidationProvider(uuidRD{})

	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
		Action:   "update",
		Resource: interfaces.ResourceSpec{Name: "bmw-staging", Type: "infra.app_platform"},
		Current: &interfaces.ResourceState{
			Name:       "bmw-staging",
			Type:       "infra.app_platform",
			ProviderID: "f8b6200c-3bba-48a7-8bf1-7a3e3a885eb5", // valid UUID
		},
	}}}

	var logBuf bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(oldOut)

	validateInputProviderIDs(provider, plan)

	out := logBuf.String()
	if strings.Contains(out, "non-conformant") {
		t.Errorf("valid UUID should not trigger WARN, got: %q", out)
	}
}

// ── regression-proof: fakeValidationProvider.ResourceDriver returns the driver ─

func TestFakeValidationProvider_ResourceDriverReturnsDriver(t *testing.T) {
	d := uuidRD{}
	p := newValidationProvider(d)
	rd, err := p.ResourceDriver("any")
	if err != nil {
		t.Fatalf("ResourceDriver returned error: %v", err)
	}
	if _, ok := rd.(interfaces.ProviderIDValidator); !ok {
		t.Error("expected uuidRD to implement ProviderIDValidator")
	}
}
