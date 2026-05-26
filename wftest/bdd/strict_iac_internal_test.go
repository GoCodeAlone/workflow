package bdd

// Internal-package tests so iacServiceChecks (unexported) is in scope.
// External-package tests for AssertProviderCapabilitiesMatchRegistration
// behavior are in strict_iac_test.go.

import (
	"strings"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// TestIaCServiceChecks_CoversEveryProtoService walks the iac.proto
// FileDescriptor at runtime and asserts every gRPC service whose
// fully-qualified name starts with iacServicePrefix is also listed
// in iacServiceChecks. Without this guard, a new optional service
// added to iac.proto silently passes through
// AssertProviderCapabilitiesMatchRegistration's two-pass logic
// (the satisfies check is never invoked for unlisted services), so
// a missing-from-iacServiceChecks bug leaves a hole in the cycle 4
// belt-and-braces invariant.
//
// Per cycle 4 code-review PR 606 MINOR-1: tightens the manual-
// maintenance surface into a compile-time-discoverable test failure
// rather than a silent test-passing regression.
func TestIaCServiceChecks_CoversEveryProtoService(t *testing.T) {
	fd := pb.File_iac_proto
	if fd == nil {
		t.Fatalf("pb.File_iac_proto is nil — proto bindings not loaded")
	}
	services := fd.Services()
	if services.Len() == 0 {
		t.Fatalf("iac.proto descriptor has no services — proto regeneration broke?")
	}

	// Index of fully-qualified names that iacServiceChecks already covers.
	covered := make(map[string]bool, len(iacServiceChecks))
	for _, c := range iacServiceChecks {
		covered[c.serviceName] = true
	}

	// Every IaC service name in the proto must appear in iacServiceChecks.
	missing := make([]string, 0)
	for i := 0; i < services.Len(); i++ {
		fqn := string(services.Get(i).FullName())
		if !strings.HasPrefix(fqn, iacServicePrefix) {
			// Not an IaC service — skip (defensive; iac.proto's package
			// option pins every service to this prefix today).
			continue
		}
		if !covered[fqn] {
			missing = append(missing, fqn)
		}
	}
	if len(missing) > 0 {
		t.Errorf(
			"iacServiceChecks (wftest/bdd/strict_iac.go) is missing %d entr(y/ies) that "+
				"iac.proto declares: %v\n"+
				"Add a corresponding iacServiceCheck row for each so "+
				"AssertProviderCapabilitiesMatchRegistration covers them.",
			len(missing), missing,
		)
	}

	// Inverse check: every iacServiceCheck names a service that the
	// proto actually defines. Catches the rename-without-cleanup
	// failure mode.
	declared := make(map[string]bool, services.Len())
	for i := 0; i < services.Len(); i++ {
		declared[string(services.Get(i).FullName())] = true
	}
	stale := make([]string, 0)
	for _, c := range iacServiceChecks {
		if !declared[c.serviceName] {
			stale = append(stale, c.serviceName)
		}
	}
	if len(stale) > 0 {
		t.Errorf(
			"iacServiceChecks references %d service(s) not declared in iac.proto: %v\n"+
				"The proto likely renamed/removed them; remove the stale rows.",
			len(stale), stale,
		)
	}
}
