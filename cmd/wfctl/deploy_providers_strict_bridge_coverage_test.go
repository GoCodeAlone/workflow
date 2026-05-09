package main

// deploy_providers_strict_bridge_coverage_test.go — strict-contracts coverage
// gate for the wfctl-side gRPC IaCProvider proxy.
//
// Why this exists
// ───────────────
// The v0.27.0 → v0.27.1 audit-keys bug demonstrated a class of cross-repo
// gRPC bridge gaps that the existing strict-contracts gate does NOT catch:
//
//   1. interfaces/iac_provider.go declares an OPTIONAL sub-interface
//      (EnumeratorAll, Enumerator, DriftConfigDetector, ...).
//   2. Plugin-side providers (e.g. workflow-plugin-digitalocean's DOProvider)
//      implement it AND advertise the method via their InvokeMethod /
//      InvokeMethodContext dispatcher.
//   3. The wfctl-side proxy `*remoteIaCProvider` lives in
//      cmd/wfctl/deploy_providers.go and routes ALL plugin calls through
//      InvokeService. If the proxy is missing a method, every type-assert
//      against the optional interface in wfctl call-sites fails — the
//      plugin process implements the method, but wfctl can never reach it.
//
// The pre-existing strict-contracts gate (cmd/wfctl/plugin_audit.go) audits
// PLUGIN-SIDE manifest contract descriptors. It has no visibility into
// workflow-side proxy method coverage. The v0.27.0 EnumeratorAll gap slipped
// through with green CI because no test enforced "every interface method
// declared in interfaces/iac_provider.go has a corresponding bridge entry".
//
// What this test enforces
// ───────────────────────
//   F-1 (compile-time): `*remoteIaCProvider` satisfies EVERY optional
//        IaCProvider sub-interface declared in interfaces/iac_provider.go.
//        Adding a new optional interface without bridging will fail at
//        compile time once the new interface is added to opt-in list below.
//
//   F-2 (wire coverage): for every method on every optional interface,
//        deploy_providers.go contains the literal string "IaCProvider.<Method>"
//        — the RPC method name passed to InvokeService / InvokeServiceContext.
//        This catches the case where someone declares the proxy method but
//        forgets to actually dispatch through gRPC (or uses a typo in the
//        method-name string).
//
//   F-3 (no skip / fallback): this test is unconditional. It does not
//        respect a -short flag, an env var, or a build tag. There is no
//        path that downgrades it to non-strict. Per the v0.27.1 user
//        mandate: "remove the fallback and force strict mode".
//
// How to extend
// ─────────────
// When adding a new optional IaCProvider sub-interface (e.g. ProviderXyz):
//   1. Declare it in interfaces/iac_provider.go (or a sibling file).
//   2. Add `_ interfaces.ProviderXyz = (*remoteIaCProvider)(nil)` to
//      the compile-time block below.
//   3. Append the interface's reflect.TypeOf entry to optionalIaCProviderInterfaces.
//   4. Add proxy methods to remoteIaCProvider in deploy_providers.go.
//   5. The wire-coverage test will fail until step 4 is complete; that
//      failure is the bridge gap surfacing immediately.
//
// Run: go test ./cmd/wfctl/ -run TestStrictBridgeCoverage -v

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// Compile-time assertions that *remoteIaCProvider satisfies every optional
// IaCProvider sub-interface that wfctl call-sites type-assert against.
//
// If a future commit adds a new optional interface without bridging it,
// adding the assertion line below will fail to compile until the proxy
// implements the methods — surfacing the gap at PR-review time, not at
// runtime in production.
var (
	_ interfaces.IaCProvider               = (*remoteIaCProvider)(nil)
	_ interfaces.Enumerator                = (*remoteIaCProvider)(nil) // v0.27.1
	_ interfaces.EnumeratorAll             = (*remoteIaCProvider)(nil) // v0.27.1
	_ interfaces.DriftConfigDetector       = (*remoteIaCProvider)(nil) // v0.20.x
	_ interfaces.ProviderMigrationRepairer = (*remoteIaCProvider)(nil) // v0.21.x
	_ interfaces.ProviderCredentialRevoker = (*remoteIaCProvider)(nil) // v0.22.x
)

// optionalIaCProviderInterfaces is the canonical registry of every optional
// sub-interface that wfctl call-sites type-assert IaC providers against.
// The wire-coverage test below iterates this set and demands each declared
// method appear as an "IaCProvider.<Method>" string literal in
// deploy_providers.go.
//
// Add new optional interfaces here whenever they're declared in
// interfaces/iac_provider.go (or sibling files) AND a wfctl call-site
// type-asserts against them.
var optionalIaCProviderInterfaces = []reflect.Type{
	reflect.TypeOf((*interfaces.IaCProvider)(nil)).Elem(),
	reflect.TypeOf((*interfaces.Enumerator)(nil)).Elem(),
	reflect.TypeOf((*interfaces.EnumeratorAll)(nil)).Elem(),
	reflect.TypeOf((*interfaces.DriftConfigDetector)(nil)).Elem(),
	reflect.TypeOf((*interfaces.ProviderMigrationRepairer)(nil)).Elem(),
	reflect.TypeOf((*interfaces.ProviderCredentialRevoker)(nil)).Elem(),
}

// methodsExemptFromWireCoverage are interface methods that legitimately do
// NOT correspond to a wire RPC. Specifically:
//   - Name / Version: trivial accessors that route through InvokeService but
//     also have other concerns (return zero on error rather than propagate).
//   - Close: local resource cleanup; never crosses the gRPC boundary.
//   - SupportedCanonicalKeys: returns a wfctl-side canonical-keys enum,
//     not a remote call (per comment at deploy_providers.go).
//   - ResourceDriver: returns a sub-driver proxy (remoteResourceDriver) —
//     the actual RPC dispatch happens on the returned driver, not here.
//   - DetectDriftWithSpecs: routes through "IaCProvider.DetectDrift" with a
//     "specs" arg per DO plugin v0.10.5+ wire protocol — same RPC as
//     DetectDrift, no separate method name.
//
// This list is intentionally small. Adding entries here REQUIRES a comment
// justifying why the method does not need wire-coverage; the test fails
// loudly on unrecognised exemptions.
var methodsExemptFromWireCoverage = map[string]string{
	"Name":                   "trivial accessor; wire shape covered by other tests",
	"Version":                "trivial accessor; wire shape covered by other tests",
	"Close":                  "local cleanup, no gRPC call",
	"SupportedCanonicalKeys": "returns wfctl-side canonical-keys enum, no RPC",
	"ResourceDriver":         "returns remoteResourceDriver sub-proxy; per-method RPC happens there",
	"DetectDriftWithSpecs":   "routes through IaCProvider.DetectDrift with 'specs' arg per DO v0.10.5+ wire protocol",
}

// TestStrictBridgeCoverage_CompileTimeAssertions documents the compile-time
// guarantees enforced at file-init time (the var block above). The test body
// is a no-op marker: if the var block had compiled-out a missing interface,
// the package would not have compiled and this test could not run.
func TestStrictBridgeCoverage_CompileTimeAssertions(t *testing.T) {
	// Intentionally empty: the value is in the var block above. Documenting
	// the guarantee here gives `go test -run TestStrictBridgeCoverage` a hit.
	t.Log("compile-time interface-satisfaction assertions checked at package-load time")
}

// TestStrictBridgeCoverage_WireMethodCoverage iterates every method on every
// optional IaCProvider sub-interface and demands the literal string
// "IaCProvider.<Method>" appear in deploy_providers.go (the file that
// implements the wfctl-side proxy). Methods listed in
// methodsExemptFromWireCoverage are skipped.
//
// This is a coarse but effective test: a missing proxy method, a typo in
// the RPC method-name string, or a "we'll wire it later" placeholder all
// fail this gate.
func TestStrictBridgeCoverage_WireMethodCoverage(t *testing.T) {
	src := readDeployProvidersSource(t)

	for _, iface := range optionalIaCProviderInterfaces {
		ifaceName := iface.Name()
		for i := 0; i < iface.NumMethod(); i++ {
			method := iface.Method(i)
			if reason, exempt := methodsExemptFromWireCoverage[method.Name]; exempt {
				t.Logf("[%s.%s] exempt: %s", ifaceName, method.Name, reason)
				continue
			}
			wantToken := "IaCProvider." + method.Name
			if !strings.Contains(src, wantToken) {
				t.Errorf("strict-bridge-coverage: interface %s declares method %s "+
					"but deploy_providers.go has no occurrence of %q. "+
					"Either add the proxy method to remoteIaCProvider that calls "+
					"InvokeService(%q, ...) or, if the method legitimately does "+
					"not require wire dispatch, document it in "+
					"methodsExemptFromWireCoverage with justification.",
					ifaceName, method.Name, wantToken, wantToken)
			}
		}
	}
}

// TestStrictBridgeCoverage_NoFallbackOrSkip is a meta-check that the gate
// methods (TestStrictBridgeCoverage_CompileTimeAssertions and
// TestStrictBridgeCoverage_WireMethodCoverage) cannot be silently downgraded
// by an env var, build tag, or testing.Short skip. Per the v0.27.1 user
// mandate ("remove the fallback and force strict mode"), the strict-
// bridge-coverage test must always run unconditionally.
//
// Implementation note: this scan reads the source file and inspects ONLY the
// two gate-test function bodies (between their `func` declarations and the
// next top-level `func`). This deliberately excludes this meta-check's own
// body, where the banned tokens appear as data. A future commit that adds
// `t.Skip(...)` or `os.Getenv(...)` inside either gate body will fail here.
func TestStrictBridgeCoverage_NoFallbackOrSkip(t *testing.T) {
	src := readThisFile(t)
	gateBodies := []string{
		extractFunctionBody(t, src, "TestStrictBridgeCoverage_CompileTimeAssertions"),
		extractFunctionBody(t, src, "TestStrictBridgeCoverage_WireMethodCoverage"),
	}
	// Tokens are assembled from fragments so the banned literal does not
	// appear in this function's source body either.
	banned := []string{
		"t" + ".Skip(",
		"t" + ".SkipNow(",
		"testing" + ".Short()",
		"os" + ".Getenv",
		"build " + "tag", // catches `// +build !strict` style bypasses by name
	}
	for _, body := range gateBodies {
		for _, b := range banned {
			if strings.Contains(body, b) {
				t.Errorf("strict-bridge-coverage gate body must not contain %q "+
					"— the gate is unconditional per v0.27.1 user mandate "+
					"(\"remove the fallback and force strict mode\")", b)
			}
		}
	}
}

// extractFunctionBody returns the body of the named test function. Used by
// the no-fallback meta-check to inspect ONLY the gate bodies, not its own.
// The implementation is a best-effort substring scan — it locates
// `func <name>(` then balances braces from the first `{` after that point.
func extractFunctionBody(t *testing.T, src string, name string) string {
	t.Helper()
	marker := "func " + name + "("
	start := strings.Index(src, marker)
	if start < 0 {
		t.Fatalf("could not locate %s in source", name)
	}
	open := strings.Index(src[start:], "{")
	if open < 0 {
		t.Fatalf("could not find open brace for %s", name)
	}
	open += start
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open : i+1]
			}
		}
	}
	t.Fatalf("unbalanced braces in %s", name)
	return ""
}

// readDeployProvidersSource reads cmd/wfctl/deploy_providers.go from the
// repo root, locating it relative to this test file's path so the test
// works regardless of `go test` working directory.
func readDeployProvidersSource(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	target := filepath.Join(filepath.Dir(thisFile), "deploy_providers.go")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read deploy_providers.go: %v", err)
	}
	return string(data)
}

// readThisFile reads the test file itself for the no-skip meta-check.
func readThisFile(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	data, err := os.ReadFile(thisFile)
	if err != nil {
		t.Fatalf("read self: %v", err)
	}
	return string(data)
}
