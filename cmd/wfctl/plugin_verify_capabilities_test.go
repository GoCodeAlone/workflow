package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestVerifyCapabilitiesUsage(t *testing.T) {
	err := runPluginVerifyCapabilities([]string{})
	if err == nil {
		t.Fatal("want error for missing args")
	}
	if !strings.Contains(err.Error(), "--binary") {
		t.Errorf("error %q should mention --binary", err.Error())
	}
}

func TestVerifyCapabilitiesRequiresBinary(t *testing.T) {
	err := runPluginVerifyCapabilities([]string{"."})
	if err == nil {
		t.Fatal("want error when --binary missing")
	}
	if !strings.Contains(err.Error(), "--binary") {
		t.Errorf("error %q should mention --binary", err.Error())
	}
}

func TestPreflightBinaryEmpty(t *testing.T) {
	if err := preflightBinary(""); err == nil || !strings.Contains(err.Error(), "binary path") {
		t.Errorf("want empty-path error, got %v", err)
	}
}

func TestPreflightBinaryNull(t *testing.T) {
	if err := preflightBinary("null"); err == nil || !strings.Contains(err.Error(), "binary path") {
		t.Errorf("want null-path error (jq fallback), got %v", err)
	}
}

func TestPreflightBinaryMissing(t *testing.T) {
	if err := preflightBinary("/nonexistent/missing-xyz"); err == nil || !strings.Contains(err.Error(), "stat") {
		t.Errorf("want stat error, got %v", err)
	}
}

func TestPreflightBinaryDirectory(t *testing.T) {
	if err := preflightBinary(t.TempDir()); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("want directory error, got %v", err)
	}
}

func TestPreflightBinaryNonExecutable(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "p")
	if err := os.WriteFile(f, []byte("not-exec"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := preflightBinary(f); err == nil || !strings.Contains(err.Error(), "executable") {
		t.Errorf("want non-executable error, got %v", err)
	}
}

func TestPreflightBinaryOK(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "p")
	if err := os.WriteFile(f, []byte("#!/bin/sh\necho ok"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := preflightBinary(f); err != nil {
		t.Errorf("want PASS, got %v", err)
	}
}

func TestIsSentinel(t *testing.T) {
	cases := map[string]bool{
		"":                          true,
		"dev":                       true,
		"0.0.0":                     true,
		"(devel)":                   true,
		"(devel) [@ a1b2c3d]":       true,
		"(devel) [@ a1b2c3d.dirty]": true,
		"v1.2.3":                    false,
		"1.2.3":                     false,
		"v0.0.1":                    false,
	}
	for v, want := range cases {
		if got := isSentinel(v); got != want {
			t.Errorf("isSentinel(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestDiffVersion(t *testing.T) {
	cases := []struct {
		declared, runtime string
		wantPass          bool
		wantReason        string
	}{
		// 0.0.0 + non-sentinel -> PASS (CI artifact)
		{"0.0.0", "v1.2.3", true, ""},
		{"0.0.0", "0.1.0", true, ""},
		// 0.0.0 + sentinel -> FAIL (ldflag missing)
		{"0.0.0", "", false, "ldflag"},
		{"0.0.0", "(devel)", false, "ldflag"},
		{"0.0.0", "(devel) [@ abc1234]", false, "ldflag"},
		{"0.0.0", "dev", false, "ldflag"},
		{"0.0.0", "0.0.0", false, "ldflag"},
		// X.Y.Z + vX.Y.Z or X.Y.Z -> PASS (normalize leading v)
		{"1.2.3", "v1.2.3", true, ""},
		{"1.2.3", "1.2.3", true, ""},
		// X.Y.Z + sentinel -> FAIL
		{"1.2.3", "", false, "ldflag"},
		{"1.2.3", "(devel)", false, "ldflag"},
		{"1.2.3", "(devel) [@ deadbee]", false, "ldflag"},
		// X.Y.Z + drift -> FAIL
		{"1.2.3", "v0.9.0", false, "drift"},
		{"1.2.3", "v2.0.0", false, "drift"},
	}
	for _, c := range cases {
		pass, reason := diffVersion(c.declared, c.runtime)
		if pass != c.wantPass {
			t.Errorf("diffVersion(%q, %q) pass=%v want=%v reason=%q",
				c.declared, c.runtime, pass, c.wantPass, reason)
			continue
		}
		if !pass && !strings.Contains(reason, c.wantReason) {
			t.Errorf("diffVersion(%q, %q) reason=%q want substring %q",
				c.declared, c.runtime, reason, c.wantReason)
		}
	}
}

func TestCompareManifestWithRuntimeCanSkipRegistryAliasName(t *testing.T) {
	declared := plugin.PluginManifest{Name: "github", Version: "1.0.6"}
	runtime := &pb.Manifest{Name: "workflow-plugin-github", Version: "v1.0.6"}

	strict := compareManifestWithRuntime(declared, runtime, manifestCompareOptions{})
	if len(strict) != 1 || !strings.Contains(strict[0], "name:") {
		t.Fatalf("strict comparison failures = %v, want one name mismatch", strict)
	}

	registry := compareManifestWithRuntime(declared, runtime, manifestCompareOptions{SkipName: true})
	if len(registry) != 0 {
		t.Fatalf("registry comparison failures = %v, want none", registry)
	}
}

// buildFixtureBinaryForVerify builds the fixture scenario in-place and emits
// the binary to t.TempDir(). ldflag is the -X ...Version= value ("" = no flag,
// which makes ResolveBuildVersion fall back to "(devel) [@ sha]" for fixtures
// whose initial Version var is "dev").
func buildFixtureBinaryForVerify(t *testing.T, scenario, ldflagTag string) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "p")
	args := []string{"build", "-mod=readonly"}
	if ldflagTag != "" {
		// Fixture main.go is `package main` with `var Version` at fixture root,
		// so the linker symbol is `main.Version` (NOT `<module>/internal.Version`
		// as production plugins use). Empirically verified via `go tool nm`.
		args = append(args, "-ldflags",
			fmt.Sprintf("-X main.Version=%s", ldflagTag))
	}
	_ = scenario // retained for future scenario-specific build customization
	args = append(args, "-o", binPath, ".")
	cmd := exec.Command("go", args...)
	cmd.Dir = filepath.Join("testdata", "verify_capabilities", scenario)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", scenario, err, out)
	}
	return binPath
}

func TestVerifyCapabilities_Good(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "good", "v0.1.0")
	if err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/good"}); err != nil {
		t.Fatalf("want PASS, got: %v", err)
	}
}

func TestProviderCapabilityDiscoveryUsesRealPluginProcess(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "provider-good", "v0.1.0")
	fixtureDir := filepath.Join("testdata", "verify_capabilities", "provider-good")
	if err := runPluginVerifyCapabilities([]string{"--binary", bin, fixtureDir}); err != nil {
		t.Fatalf("verify provider runtime declarations: %v", err)
	}

	pluginRoot := t.TempDir()
	installName := "verify-provider"
	installDir := filepath.Join(pluginRoot, installName)
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(fixtureDir, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "plugin.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, installName), binary, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, closeCredentialPlugin, declaration, pluginName, _, found, err := discoverCredentialIssuerCapability(ctx, pluginRoot, "example.source")
	if err != nil || !found || declaration.Source != "example.source" || pluginName != installName {
		t.Fatalf("credential discovery found=%v plugin=%q declaration=%+v error=%v", found, pluginName, declaration, err)
	}
	assertBuiltinCredentialResolverSelected(t)
	if closeCredentialPlugin != nil {
		closeCredentialPlugin()
	}
	_, closeRegistryPlugin, found, err := discoverContainerRegistryCapability(ctx, pluginRoot, "example-registry", "login")
	if err != nil || !found {
		t.Fatalf("registry discovery found=%v error=%v", found, err)
	}
	assertBuiltinCredentialResolverSelected(t)
	if closeRegistryPlugin != nil {
		closeRegistryPlugin()
	}

	t.Setenv("WFCTL_TEST_PROVIDER_DECLARATION_ERROR", "1")
	providerStderr, providerErr := captureStderr(t, func() error {
		_, closePlugin, _, _, _, _, discoverErr := discoverCredentialIssuerCapability(ctx, pluginRoot, "example.source")
		if closePlugin != nil {
			closePlugin()
		}
		return discoverErr
	})
	if providerErr == nil {
		t.Fatal("want invalid runtime declaration error")
	}
	if strings.Contains(providerStderr, "SENTINEL_PROVIDER_STDERR_SECRET") {
		t.Fatalf("generic provider dispatch leaked plugin stderr: %s", providerStderr)
	}
}

func assertBuiltinCredentialResolverSelected(t *testing.T) {
	t.Helper()
	account := module.NewCloudAccount("provider-discovery-isolation", map[string]any{
		"provider": "aws",
		"credentials": map[string]any{
			"type": "static", "accessKey": "builtin-access", "secretKey": "builtin-secret",
		},
	})
	if err := account.Init(module.NewMockApplication()); err != nil {
		t.Fatal(err)
	}
	credentials, err := account.GetCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessKey != "builtin-access" {
		t.Fatalf("command-scoped discovery activated unrelated credential resolver: access key=%q", credentials.AccessKey)
	}
}

func TestProviderCapabilityVerifierStartupFailureOmitsPluginStderr(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "provider-good", "v0.1.0")
	t.Setenv("WFCTL_TEST_PROVIDER_DECLARATION_ERROR", "1")
	err := runPluginVerifyCapabilities([]string{
		"--binary", bin, filepath.Join("testdata", "verify_capabilities", "provider-good"),
	})
	if err == nil {
		t.Fatal("want provider capability verification failure")
	}
	if strings.Contains(err.Error(), "SENTINEL_PROVIDER_STDERR_SECRET") {
		t.Fatalf("provider stderr leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "plugin dial") || !strings.Contains(err.Error(), "provider error text suppressed") {
		t.Fatalf("stable provider diagnostic missing: %v", err)
	}
}

func TestProviderCapabilityVerifierCancelsRealStartup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable-script fixture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "hung-verifier")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 60\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"hung-verifier","version":"1.0.0","author":"Workflow tests","description":"non-handshaking verifier fixture"}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	originalCommandContext := providerCommandContext
	providerCommandContext = func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		timer := time.AfterFunc(100*time.Millisecond, cancel)
		return ctx, func() {
			timer.Stop()
			cancel()
		}
	}
	t.Cleanup(func() { providerCommandContext = originalCommandContext })

	started := time.Now()
	err := runPluginVerifyCapabilities([]string{"--binary", binary, dir})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("error=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("canceled verifier startup took %s", elapsed)
	}
}

type providerDeclarationTransportErrorConn struct{}

func (providerDeclarationTransportErrorConn) Invoke(context.Context, string, any, any, ...grpc.CallOption) error {
	return status.Error(codes.Internal, "SENTINEL_PROVIDER_TRANSPORT_SECRET")
}

func (providerDeclarationTransportErrorConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, status.Error(codes.Internal, "SENTINEL_PROVIDER_TRANSPORT_SECRET")
}

func TestReadRuntimeProviderDeclarationsSuppressesTransportTextForEveryFamily(t *testing.T) {
	for _, test := range []struct {
		name        string
		serviceName string
		declared    plugin.PluginManifest
		operation   string
	}{
		{name: "credential issuer", serviceName: pb.CredentialIssuer_ServiceDesc.ServiceName, operation: "CredentialIssuer.DescribeSources"},
		{name: "credential resolver", serviceName: pb.CredentialResolver_ServiceDesc.ServiceName, operation: "CredentialResolver.DescribeResolvers"},
		{name: "container registry", serviceName: pb.ContainerRegistry_ServiceDesc.ServiceName, operation: "ContainerRegistry.DescribeRegistries"},
		{name: "secret store", serviceName: pb.SecretStore_ServiceDesc.ServiceName, operation: "SecretStore.DescribeSecretStores"},
		{
			name: "kubernetes backend", serviceName: pb.ResourceDriver_ServiceDesc.ServiceName,
			declared:  plugin.PluginManifest{KubernetesBackends: []plugin.KubernetesBackendDecl{{Name: "managed", ResourceType: "infra.managed_cluster"}}},
			operation: "IaCProviderRequired.Capabilities",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := &pb.ContractRegistry{Contracts: []*pb.ContractDescriptor{{
				Kind: pb.ContractKind_CONTRACT_KIND_SERVICE, ServiceName: test.serviceName,
			}}}
			_, err := readRuntimeProviderDeclarations(context.Background(), providerDeclarationTransportErrorConn{}, registry, test.declared)
			if err == nil {
				t.Fatal("want transport failure")
			}
			if !strings.Contains(err.Error(), test.operation) || !strings.Contains(err.Error(), codes.Internal.String()) {
				t.Fatalf("stable transport diagnostic missing: %v", err)
			}
			if strings.Contains(err.Error(), "SENTINEL_PROVIDER_TRANSPORT_SECRET") {
				t.Fatalf("provider transport text leaked: %v", err)
			}
		})
	}
}

func TestVerifyCapabilities_ReleaseGood(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "release-good", "v1.2.3")
	if err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/release-good"}); err != nil {
		t.Fatalf("want PASS, got: %v", err)
	}
}

func TestVerifyCapabilities_MissingLdflag(t *testing.T) {
	// No ldflag → Version stays "dev" → ResolveBuildVersion("dev") → "(devel) [@ sha]"
	bin := buildFixtureBinaryForVerify(t, "missing-ldflag", "")
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/missing-ldflag"})
	if err == nil {
		t.Fatal("want FAIL, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("want mismatch error, got: %v", err)
	}
}

func TestVerifyCapabilities_VersionDrift(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "version-drift", "v0.9.0")
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/version-drift"})
	if err == nil {
		t.Fatal("want FAIL, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("want mismatch error, got: %v", err)
	}
}

func TestVerifyCapabilities_NameDrift(t *testing.T) {
	// Build with non-sentinel ldflag tag so Version PASSes — matrix row that
	// fires: plugin.json="0.0.0" + binary="v0.0.0" → PASS via the
	// `declared == "0.0.0"` branch returning early (isSentinel("v0.0.0")==false
	// because the SDK sentinel set is {"", "dev", "0.0.0", "(devel)..."} — NOT
	// "v0.0.0"). This ISOLATES Name as the sole failure under test, so a
	// regression that breaks Name-diff while leaving Version-diff intact
	// doesn't silently pass through a lenient `Contains("mismatch")` check.
	bin := buildFixtureBinaryForVerify(t, "name-drift", "v0.0.0")
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/name-drift"})
	if err == nil {
		t.Fatal("want FAIL, got nil")
	}
	// Tighter assertion: error must specifically mention "name:" prefix from the diff report.
	if !strings.Contains(err.Error(), "name:") && !strings.Contains(fmt.Sprintf("%v", err), "name:") {
		t.Errorf("want name-mismatch error, got: %v", err)
	}
}

func TestDiffIaCServices_Match(t *testing.T) {
	missing, extra := diffIaCServices(
		[]string{"workflow.plugin.external.iac.IaCProviderRequired"},
		[]string{"workflow.plugin.external.iac.IaCProviderRequired"})
	if len(missing) != 0 || len(extra) != 0 {
		t.Errorf("missing=%v extra=%v", missing, extra)
	}
}

func TestDiffIaCServices_MissingFromBinary(t *testing.T) {
	declared := []string{
		"workflow.plugin.external.iac.IaCProviderRequired",
		"workflow.plugin.external.iac.IaCProviderFinalizer",
	}
	advertised := []string{"workflow.plugin.external.iac.IaCProviderRequired"}
	missing, extra := diffIaCServices(declared, advertised)
	if len(missing) != 1 || missing[0] != "workflow.plugin.external.iac.IaCProviderFinalizer" {
		t.Errorf("want Finalizer missing; got %v", missing)
	}
	if len(extra) != 0 {
		t.Errorf("want no extras; got %v", extra)
	}
}

func TestDiffIaCServices_ExtraInBinary(t *testing.T) {
	missing, extra := diffIaCServices(
		[]string{"workflow.plugin.external.iac.IaCProviderRequired"},
		[]string{
			"workflow.plugin.external.iac.IaCProviderRequired",
			"workflow.plugin.external.iac.IaCProviderFinalizer",
		})
	if len(missing) != 0 {
		t.Errorf("missing=%v", missing)
	}
	if len(extra) != 1 || extra[0] != "workflow.plugin.external.iac.IaCProviderFinalizer" {
		t.Errorf("want Finalizer extra; got %v", extra)
	}
}

func TestDiffIaCServices_EmptyDeclared_SkipsDiff(t *testing.T) {
	missing, extra := diffIaCServices(nil, []string{"workflow.plugin.external.iac.IaCProviderRequired"})
	if missing != nil || extra != nil {
		t.Errorf("empty LHS should skip; got missing=%v extra=%v", missing, extra)
	}
}

func TestVerifyCapabilities_IaCGood(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "iac-good", "v0.1.0")
	if err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/iac-good"}); err != nil {
		t.Fatalf("want PASS, got: %v", err)
	}
}

func TestVerifyCapabilities_IaCMissingService(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "iac-missing-service", "v0.1.0")
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/iac-missing-service"})
	if err == nil {
		t.Fatal("want FAIL on missing Finalizer, got nil")
	}
	if !strings.Contains(err.Error(), "iacServices:") {
		t.Errorf("want iacServices: error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "IaCProviderFinalizer") {
		t.Errorf("want Finalizer-specific error, got: %v", err)
	}
}

func TestVerifyCapabilities_IaCExtraService(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "iac-extra-service", "v0.1.0")
	// Extra services produce WARN (stderr) but exit 0 per design §3.
	if err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/iac-extra-service"}); err != nil {
		t.Fatalf("want PASS (extra=WARN, not FAIL); got: %v", err)
	}
}

func writeVerifyCapabilitiesManifest(t *testing.T, manifest plugin.PluginManifest) string {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("json.Marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0o600); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	return dir
}

func TestVerifyCapabilities_KubernetesBackendsMatchRuntime(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "kubernetes-good", "v0.1.0")
	if err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/kubernetes-good"}); err != nil {
		t.Fatalf("want PASS, got: %v", err)
	}
}

func TestVerifyCapabilities_KubernetesBackendRequiresResourceDriverAdvertisement(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "iac-missing-service", "v0.1.0")
	dir := writeVerifyCapabilitiesManifest(t, plugin.PluginManifest{
		Name:        "verify-iac-missing",
		Version:     "0.0.0",
		Author:      "test fixture",
		Description: "declares a backend without serving ResourceDriver",
		KubernetesBackends: []plugin.KubernetesBackendDecl{
			{Name: "managed", ResourceType: "infra.managed_cluster"},
		},
	})

	err := runPluginVerifyCapabilities([]string{"--binary", bin, dir})
	if err == nil || !strings.Contains(err.Error(), "ResourceDriver") {
		t.Fatalf("error = %v, want missing ResourceDriver advertisement mismatch", err)
	}
}

func TestVerifyCapabilities_KubernetesBackendRequiresRuntimeResourceType(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "kubernetes-good", "v0.1.0")
	dir := writeVerifyCapabilitiesManifest(t, plugin.PluginManifest{
		Name:        "verify-kubernetes",
		Version:     "0.0.0",
		Author:      "test fixture",
		Description: "declares a backend absent from runtime capabilities",
		KubernetesBackends: []plugin.KubernetesBackendDecl{
			{Name: "managed", ResourceType: "infra.missing_cluster"},
		},
	})

	err := runPluginVerifyCapabilities([]string{"--binary", bin, dir})
	if err == nil || !strings.Contains(err.Error(), "infra.missing_cluster") {
		t.Fatalf("error = %v, want missing runtime resource type mismatch", err)
	}
}

func TestVerifyCapabilities_KubernetesBackendsRejectNonCanonicalNames(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "kubernetes-good", "v0.1.0")
	tests := []struct {
		name         string
		declarations []plugin.KubernetesBackendDecl
		want         string
	}{
		{"kind with whitespace", []plugin.KubernetesBackendDecl{{Name: " kind ", ResourceType: "infra.managed_cluster"}}, "reserved"},
		{"k3s with whitespace", []plugin.KubernetesBackendDecl{{Name: "k3s ", ResourceType: "infra.managed_cluster"}}, "reserved"},
		{"canonical duplicate", []plugin.KubernetesBackendDecl{
			{Name: "foo", ResourceType: "infra.managed_cluster"},
			{Name: " foo ", ResourceType: "infra.managed_cluster"},
		}, "duplicate kubernetes backend"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeVerifyCapabilitiesManifest(t, plugin.PluginManifest{
				Name:               "verify-kubernetes",
				Version:            "0.0.0",
				Author:             "test fixture",
				Description:        "non-canonical kubernetes backend declaration",
				KubernetesBackends: tt.declarations,
			})
			err := runPluginVerifyCapabilities([]string{"--binary", bin, dir})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want manifest validation rejection", err)
			}
		})
	}
}
