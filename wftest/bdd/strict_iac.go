package bdd

import (
	"sort"
	"strings"

	"google.golang.org/grpc"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// strictIaCT is the minimal testing.TB shape that
// AssertProviderCapabilitiesMatchRegistration uses. Defined as an
// interface (rather than testing.TB) so tests can substitute a
// recording double — the helper's failure path is itself unit-tested.
type strictIaCT interface {
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Helper()
}

// iacServicePrefix identifies typed IaC service names emitted by
// iac.proto's package option.
const iacServicePrefix = "workflow.plugin.external.iac."

// iacServiceCheck pairs a typed service name with a runtime check that
// reports whether a provider Go type satisfies the corresponding
// generated server interface.
type iacServiceCheck struct {
	serviceName string
	satisfies   func(any) bool
}

// iacServiceChecks lists every typed IaC service this helper knows
// about. New optional services added to iac.proto must be appended
// here; the cycle 4 belt-and-braces invariant is the test failing
// loudly for any service the SDK auto-registration helper already
// covers.
var iacServiceChecks = []iacServiceCheck{
	{"workflow.plugin.external.iac.IaCProviderRequired", func(p any) bool {
		_, ok := p.(pb.IaCProviderRequiredServer)
		return ok
	}},
	{"workflow.plugin.external.iac.IaCProviderEnumerator", func(p any) bool {
		_, ok := p.(pb.IaCProviderEnumeratorServer)
		return ok
	}},
	{"workflow.plugin.external.iac.IaCProviderDriftDetector", func(p any) bool {
		_, ok := p.(pb.IaCProviderDriftDetectorServer)
		return ok
	}},
	{"workflow.plugin.external.iac.IaCProviderCredentialRevoker", func(p any) bool {
		_, ok := p.(pb.IaCProviderCredentialRevokerServer)
		return ok
	}},
	{"workflow.plugin.external.iac.IaCProviderMigrationRepairer", func(p any) bool {
		_, ok := p.(pb.IaCProviderMigrationRepairerServer)
		return ok
	}},
	{"workflow.plugin.external.iac.IaCProviderValidator", func(p any) bool {
		_, ok := p.(pb.IaCProviderValidatorServer)
		return ok
	}},
	{"workflow.plugin.external.iac.IaCProviderFinalizer", func(p any) bool {
		_, ok := p.(pb.IaCProviderFinalizerServer)
		return ok
	}},
	{"workflow.plugin.external.iac.IaCProviderDriftConfigDetector", func(p any) bool {
		_, ok := p.(pb.IaCProviderDriftConfigDetectorServer)
		return ok
	}},
	{"workflow.plugin.external.iac.IaCProviderLogCapture", func(p any) bool {
		_, ok := p.(pb.IaCProviderLogCaptureServer)
		return ok
	}},
	{"workflow.plugin.external.iac.ResourceDriver", func(p any) bool {
		_, ok := p.(pb.ResourceDriverServer)
		return ok
	}},
	{"workflow.plugin.external.iac.IaCStateBackend", func(p any) bool {
		_, ok := p.(pb.IaCStateBackendServer)
		return ok
	}},
	{"workflow.plugin.external.iac.IaCRequirementDiscovery", func(p any) bool {
		_, ok := p.(pb.IaCRequirementDiscoveryServer)
		return ok
	}},
	{"workflow.plugin.external.iac.IaCProviderRequirementMapper", func(p any) bool {
		_, ok := p.(pb.IaCProviderRequirementMapperServer)
		return ok
	}},
}

// AssertProviderCapabilitiesMatchRegistration asserts that every typed
// IaC gRPC service interface satisfied by provider's Go type IS
// registered on grpcSrv, AND no IaC service is registered that the
// provider's Go type does NOT satisfy. Reports specific missing /
// extra service names so test failures point at the exact omission.
//
// Per cycle 4 of the strict-contracts force-cutover design (belt-and-
// braces): the canonical registration path is
// sdk.RegisterAllIaCProviderServices, which uses Go type-assertion to
// auto-detect every interface and cannot omit a registration. The
// per-service Register* helpers are still exposed for advanced use
// cases (e.g., a plugin that registers a different Go type per
// optional service); this helper is the test-time guard that catches
// the manual-registration omission failure mode.
//
// The provider parameter is the live Go provider implementation
// (typically a *DOProvider or test stub). The grpcSrv parameter is
// the gRPC server with the plugin's service registrations on it
// (typically the result of grpc.NewServer() + RegisterAllIaCProviderServices,
// or — for the failure case this guard catches — a server with
// manual per-service registrations).
//
// Provider MUST satisfy pb.IaCProviderRequiredServer at minimum; if
// not, the helper reports a fatal-class failure naming the missing
// required interface (a broken fixture, not a runtime issue).
func AssertProviderCapabilitiesMatchRegistration(t strictIaCT, provider any, grpcSrv *grpc.Server) {
	t.Helper()
	if grpcSrv == nil {
		t.Fatalf("AssertProviderCapabilitiesMatchRegistration: grpcSrv is nil")
		return
	}
	if provider == nil {
		t.Fatalf("AssertProviderCapabilitiesMatchRegistration: provider is nil")
		return
	}
	if _, ok := provider.(pb.IaCProviderRequiredServer); !ok {
		t.Fatalf(
			"provider %T does not satisfy pb.IaCProviderRequiredServer "+
				"(broken test fixture); see decisions/0024-iac-typed-force-cutover.md",
			provider,
		)
		return
	}

	registered := grpcSrv.GetServiceInfo()
	expected := make(map[string]bool, len(iacServiceChecks))
	for _, c := range iacServiceChecks {
		if c.satisfies(provider) {
			expected[c.serviceName] = true
		}
	}

	// Pass 1: every interface the provider satisfies MUST be registered.
	missing := make([]string, 0, len(expected))
	for name := range expected {
		if _, ok := registered[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		t.Errorf(
			"provider %T satisfies %s but the corresponding gRPC service "+
				"is NOT registered on the plugin handle; "+
				"call sdk.RegisterAllIaCProviderServices to auto-register",
			provider, name,
		)
	}

	// Pass 2: no IaC service registered that the provider doesn't satisfy.
	// We only inspect services in the workflow.plugin.external.iac
	// namespace — non-IaC services (e.g., the legacy PluginService) are
	// out of scope.
	extra := make([]string, 0)
	for name := range registered {
		if !strings.HasPrefix(name, iacServicePrefix) {
			continue
		}
		if !expected[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		t.Errorf(
			"gRPC service %s is registered on the plugin handle but "+
				"provider %T does not satisfy the corresponding Go interface; "+
				"manual Register* call appears to bind a different impl",
			name, provider,
		)
	}
}
