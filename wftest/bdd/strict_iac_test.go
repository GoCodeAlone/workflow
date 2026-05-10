package bdd_test

import (
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"github.com/GoCodeAlone/workflow/wftest/bdd"
)

// TestAssertProviderCapabilitiesMatchRegistration_AutoRegisteredAllOK
// asserts the helper passes silently when sdk.RegisterAllIaCProviderServices
// has been used as designed (every interface the provider satisfies IS
// also registered on the gRPC server).
func TestAssertProviderCapabilitiesMatchRegistration_AutoRegisteredAllOK(t *testing.T) {
	srv := grpc.NewServer()
	provider := &allCapabilitiesStub{}
	if err := sdk.RegisterAllIaCProviderServices(srv, provider); err != nil {
		t.Fatalf("auto-register: %v", err)
	}

	rec := &recordingT{}
	bdd.AssertProviderCapabilitiesMatchRegistration(rec, provider, srv)
	if rec.failed {
		t.Fatalf("expected silent pass; got failures: %v", rec.errors)
	}
}

// TestAssertProviderCapabilitiesMatchRegistration_ManuallyRegisteredMissingOptional_Fails
// asserts the cycle 4 belt-and-braces invariant: when a plugin author
// uses the per-service Register* helpers manually and forgets one, the
// helper surfaces the omission as a test failure with the missing
// service name in the error message.
//
// Reproduces the failure mode the SDK auto-registration helper closes
// (per cycle 3 I-1) — proves that even if a plugin author bypasses
// sdk.RegisterAllIaCProviderServices, the test-time guard still catches
// the omission.
func TestAssertProviderCapabilitiesMatchRegistration_ManuallyRegisteredMissingOptional_Fails(t *testing.T) {
	srv := grpc.NewServer()
	provider := &allCapabilitiesStub{}
	// Register Required + Enumerator manually, OMIT DriftDetector and others.
	pb.RegisterIaCProviderRequiredServer(srv, provider)
	pb.RegisterIaCProviderEnumeratorServer(srv, provider)
	// Intentionally not registering DriftDetector / CredentialRevoker /
	// MigrationRepairer / Validator / DriftConfigDetector / ResourceDriver
	// even though provider's Go type satisfies them all.

	rec := &recordingT{}
	bdd.AssertProviderCapabilitiesMatchRegistration(rec, provider, srv)
	if !rec.failed {
		t.Fatalf("expected failure for missing service registration; rec.errors=%v", rec.errors)
	}
	wantContains := []string{
		"IaCProviderDriftDetector",
		"IaCProviderCredentialRevoker",
		"IaCProviderMigrationRepairer",
		"IaCProviderValidator",
		"IaCProviderDriftConfigDetector",
		"ResourceDriver",
	}
	joined := strings.Join(rec.errors, "\n")
	for _, name := range wantContains {
		if !strings.Contains(joined, name) {
			t.Errorf("expected error mentioning %q; errors:\n%s", name, joined)
		}
	}
}

// TestAssertProviderCapabilitiesMatchRegistration_ProviderMissingRequired_Fails
// asserts the helper rejects a "provider" Go type that does not satisfy
// pb.IaCProviderRequiredServer at all. Distinct from
// RegisterAllIaCProviderServices's startup-time error: this is the
// test-author-writing-a-broken-fixture failure mode.
func TestAssertProviderCapabilitiesMatchRegistration_ProviderMissingRequired_Fails(t *testing.T) {
	srv := grpc.NewServer()
	provider := &noIaCStub{}

	rec := &recordingT{}
	bdd.AssertProviderCapabilitiesMatchRegistration(rec, provider, srv)
	if !rec.failed {
		t.Fatalf("expected failure for provider missing required interface")
	}
	if !strings.Contains(strings.Join(rec.errors, "\n"), "IaCProviderRequiredServer") {
		t.Errorf("error must name the missing required interface; got: %v", rec.errors)
	}
}

// TestAssertProviderCapabilitiesMatchRegistration_RegisteredButProviderDoesntSatisfy_Fails
// asserts the inverse of the previous case: a service is registered on
// the gRPC server, but the Go provider type does NOT satisfy the
// corresponding interface. This catches the case where the plugin
// author registered an OptionalServer with one type while the
// "provider" passed to the test is a different (narrower) type.
func TestAssertProviderCapabilitiesMatchRegistration_RegisteredButProviderDoesntSatisfy_Fails(t *testing.T) {
	srv := grpc.NewServer()
	all := &allCapabilitiesStub{}
	pb.RegisterIaCProviderRequiredServer(srv, all)
	pb.RegisterIaCProviderEnumeratorServer(srv, all)

	// Provider passed to the assertion only satisfies Required (no
	// Enumerator embed) — yet the server has Enumerator registered.
	provider := &requiredOnlyStub{}

	rec := &recordingT{}
	bdd.AssertProviderCapabilitiesMatchRegistration(rec, provider, srv)
	if !rec.failed {
		t.Fatalf("expected failure for over-registration (server has Enumerator, provider doesn't satisfy)")
	}
	if !strings.Contains(strings.Join(rec.errors, "\n"), "IaCProviderEnumerator") {
		t.Errorf("error must name the over-registered service; got: %v", rec.errors)
	}
}

// recordingT is a minimal testing.TB shape that captures Errorf and
// Fatalf without aborting the surrounding test goroutine. Used so the
// strict-IaC helper's failure path can be asserted without making the
// parent test fail. Only the methods AssertProviderCapabilitiesMatchRegistration
// touches are implemented; everything else delegates to a no-op base.
type recordingT struct {
	failed bool
	errors []string
}

func (r *recordingT) Errorf(format string, args ...any) {
	r.failed = true
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *recordingT) Fatalf(format string, args ...any) {
	r.failed = true
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *recordingT) Helper() {}

// allCapabilitiesStub satisfies every required + optional IaC service
// plus ResourceDriver.
type allCapabilitiesStub struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderEnumeratorServer
	pb.UnimplementedIaCProviderDriftDetectorServer
	pb.UnimplementedIaCProviderCredentialRevokerServer
	pb.UnimplementedIaCProviderMigrationRepairerServer
	pb.UnimplementedIaCProviderValidatorServer
	pb.UnimplementedIaCProviderDriftConfigDetectorServer
	pb.UnimplementedResourceDriverServer
}

// requiredOnlyStub satisfies Required ONLY.
type requiredOnlyStub struct {
	pb.UnimplementedIaCProviderRequiredServer
}

// noIaCStub satisfies no IaC interface.
type noIaCStub struct{}
