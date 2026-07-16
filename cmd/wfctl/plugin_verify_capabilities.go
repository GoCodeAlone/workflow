// Package main — `wfctl plugin verify-capabilities` subcommand.
// Spawns a plugin binary, calls PluginService.GetManifest directly via gRPC,
// diffs returned Manifest against plugin.json. Catches ldflag-missing
// truth-loop bug from workflow#762/#764.
//
// Design: docs/plans/2026-05-24-verify-capabilities-design.md
// Issue:  https://github.com/GoCodeAlone/workflow/issues/765
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	goplugin "github.com/GoCodeAlone/go-plugin"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin"
	external "github.com/GoCodeAlone/workflow/plugin/external"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	hclog "github.com/hashicorp/go-hclog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func runPluginVerifyCapabilities(args []string) error {
	fs := flag.NewFlagSet("plugin verify-capabilities", flag.ContinueOnError)
	binary := fs.String("binary", "", "Path to plugin binary (REQUIRED)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl plugin verify-capabilities --binary <path> <plugin-dir>

Spawn the plugin binary and verify its runtime PluginService.GetManifest
matches the declared plugin.json. Catches ldflag-missing / version-drift
bugs at release time (workflow#762 truth-loop closure).

REQUIRED: --binary <path>  (no build-from-source; operator builds the binary)

WARNING: this command EXECUTES <binary> as a subprocess. Only run against
build artifacts you trust.

Examples:
  # Local dev:
  go build -ldflags="-X github.com/.../internal.Version=v1.2.3" -o /tmp/p ./cmd/<name>
  wfctl plugin verify-capabilities --binary /tmp/p .

  # CI (post-goreleaser, in release.yml):
  RUNNER_ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
  BIN=$(jq -r --arg arch "$RUNNER_ARCH" \
    '[.[] | select(.type=="Binary" and .goos=="linux" and .goarch==$arch)] | .[0].path // ""' \
    dist/artifacts.json)
  wfctl plugin verify-capabilities --binary "$BIN" .

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *binary == "" {
		fs.Usage()
		return fmt.Errorf("--binary is required")
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("exactly one <plugin-dir> argument required")
	}
	pluginDir := fs.Arg(0)
	abs, err := filepath.Abs(pluginDir)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", pluginDir, err)
	}
	return verifyPluginManifestAgainstBinary(*binary, filepath.Join(abs, "plugin.json"))
}

func verifyPluginManifestAgainstBinary(binary, manifestPath string) error {
	return verifyPluginManifestAgainstBinaryWithOptions(binary, manifestPath, manifestCompareOptions{})
}

type manifestCompareOptions struct {
	SkipName bool
}

func verifyPluginManifestAgainstBinaryWithOptions(binary, manifestPath string, opts manifestCompareOptions) error {
	if err := preflightBinary(binary); err != nil {
		return err
	}

	absManifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", manifestPath, err)
	}
	manifestPath = absManifestPath
	manifestBytes, err := os.ReadFile(manifestPath) //nolint:gosec // operator-supplied path.
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(manifestPath), err)
	}
	var declared plugin.PluginManifest
	if err := json.Unmarshal(manifestBytes, &declared); err != nil {
		return fmt.Errorf("%s parse: %w", filepath.Base(manifestPath), err)
	}
	if err := declared.Validate(); err != nil {
		return fmt.Errorf("%s validate: %w", filepath.Base(manifestPath), err)
	}

	ctx, stopProviderCommand := boundedProviderCommandContext(30 * time.Second)
	defer stopProviderCommand()

	binAbs, err := filepath.Abs(binary)
	if err != nil {
		return fmt.Errorf("resolve --binary %q: %w", binary, err)
	}

	var stdout, stderr tailBuffer
	cmd := exec.CommandContext(ctx, binAbs) //nolint:gosec // operator-supplied binary path.
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  external.Handshake,
		Plugins:          goplugin.PluginSet{"plugin": &external.GRPCPlugin{}},
		Cmd:              cmd,
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Stderr:           &stderr,
		SyncStdout:       &stdout,
		SyncStderr:       &stderr,
		Logger:           hclog.NewNullLogger(),
	})
	defer client.Kill()

	rpcClient, err := client.Client()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("plugin startup canceled or timed out; plugin stderr and provider error text suppressed")
		}
		return fmt.Errorf("plugin dial failed; plugin stderr and provider error text suppressed")
	}
	raw, err := rpcClient.Dispense("plugin")
	if err != nil {
		return fmt.Errorf("dispense plugin failed; plugin stderr and provider error text suppressed")
	}
	pluginClient, ok := raw.(*external.PluginClient)
	if !ok {
		return fmt.Errorf("dispensed object is %T, want *external.PluginClient", raw)
	}

	pbClient := pb.NewPluginServiceClient(pluginClient.Conn())
	runtime, err := pbClient.GetManifest(ctx, &emptypb.Empty{})
	if err != nil {
		return providerCapabilityTransportError("GetManifest", err)
	}

	failures := compareManifestWithRuntime(declared, runtime, opts)

	// Contract-diff (workflow#767). One new RPC after GetManifest.
	contractReg, regErr := pbClient.GetContractRegistry(ctx, &emptypb.Empty{})
	switch {
	case regErr != nil && status.Code(regErr) == codes.Unimplemented:
		// Empty registry semantics — skip-if-LHS-empty handles non-IaC plugins;
		// non-empty plugin.json.iacServices → directional diff FAILs every
		// declared service (correct: plugin advertises nothing).
		contractReg = nil
	case regErr != nil:
		return providerCapabilityTransportError("GetContractRegistry", regErr)
	}
	// Defense-in-depth: client-side namespace filter per ADR 0042
	// (decisions/0042-verify-capabilities-iac-namespace.md) and design §2.
	// Old-SDK plugin binaries (pre-Task-3 bridge) return ALL gRPC services
	// including PluginService + health — without this filter, every infra
	// service would WARN-spam as "extra in plugin.json" for unrebased plugins.
	iacPrefix := strings.TrimSuffix(pb.IaCProviderRequired_ServiceDesc.ServiceName, ".IaCProviderRequired") + "."
	advertisedServices := serviceNamesFromRegistry(contractReg, iacPrefix)
	missingSvc, extraSvc := diffIaCServices(declared.IaCServices, advertisedServices)
	for _, s := range missingSvc {
		failures = append(failures, fmt.Sprintf("iacServices: plugin.json declares %q but binary does not advertise it", s))
	}
	for _, s := range extraSvc {
		// WARN, not FAIL — directional diff per design §3.
		fmt.Fprintf(os.Stderr, "WARN  %s: binary advertises %q not in plugin.json.iacServices (additive — consider updating plugin.json)\n", declared.Name, s)
	}

	runtimeProviderDeclarations, providerErr := readRuntimeProviderDeclarations(ctx, pluginClient.Conn(), contractReg, declared)
	if providerErr != nil {
		return fmt.Errorf("provider capability verification: %w", providerErr)
	}
	declaredProviderDeclarations := config.ProviderDeclarations{
		CredentialSources:   declared.CredentialSources,
		CredentialResolvers: declared.CredentialResolvers,
		KubernetesBackends:  declared.KubernetesBackends,
		ContainerRegistries: declared.ContainerRegistries,
		SecretStores:        declared.SecretStores,
	}
	failures = append(failures, compareProviderDeclarationsWithRuntime(declaredProviderDeclarations, runtimeProviderDeclarations)...)

	if len(failures) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL  %s (plugin.json)\nerror: %d mismatch(es)\n", declared.Name, len(failures))
		for _, f := range failures {
			fmt.Fprintf(os.Stderr, "  - %s\n", f)
		}
		// Embed the joined failure list in the returned error so tests can assert
		// on specific field names (e.g. "name:" prefix) without capturing stderr.
		return fmt.Errorf("verify-capabilities: %d mismatch(es): %s", len(failures), strings.Join(failures, "; "))
	}
	fmt.Printf("OK    %s %s (plugin.json: %s)\n", declared.Name, runtime.GetVersion(), declared.Version)
	return nil
}

func providerCapabilityTransportError(operation string, err error) error {
	return fmt.Errorf("%s RPC: %s; provider error text suppressed", operation, status.Code(err))
}

func readRuntimeProviderDeclarations(ctx context.Context, conn grpc.ClientConnInterface, registry *pb.ContractRegistry, declared plugin.PluginManifest) (providerRuntimeDeclarations, error) {
	runtime := providerRuntimeDeclarations{AdvertisedServices: make(map[string]bool)}
	for _, contract := range registry.GetContracts() {
		if contract.GetKind() == pb.ContractKind_CONTRACT_KIND_SERVICE {
			runtime.AdvertisedServices[contract.GetServiceName()] = true
		}
	}

	if runtime.AdvertisedServices[pb.CredentialIssuer_ServiceDesc.ServiceName] {
		response, err := pb.NewCredentialIssuerClient(conn).DescribeSources(ctx, &pb.CredentialSourceDeclarationsRequest{})
		if err != nil {
			return runtime, providerCapabilityTransportError("CredentialIssuer.DescribeSources", err)
		}
		if response.GetError() != nil {
			return runtime, fmt.Errorf("CredentialIssuer.DescribeSources: %s; provider error text suppressed", response.GetError().GetCode())
		}
		runtime.CredentialSources = response.GetSources()
	}
	if runtime.AdvertisedServices[pb.CredentialResolver_ServiceDesc.ServiceName] {
		response, err := pb.NewCredentialResolverClient(conn).DescribeResolvers(ctx, &pb.CredentialResolverDeclarationsRequest{})
		if err != nil {
			return runtime, providerCapabilityTransportError("CredentialResolver.DescribeResolvers", err)
		}
		if response.GetError() != nil {
			return runtime, fmt.Errorf("CredentialResolver.DescribeResolvers: %s; provider error text suppressed", response.GetError().GetCode())
		}
		runtime.CredentialResolvers = response.GetResolvers()
	}
	if runtime.AdvertisedServices[pb.ContainerRegistry_ServiceDesc.ServiceName] {
		response, err := pb.NewContainerRegistryClient(conn).DescribeRegistries(ctx, &pb.ContainerRegistryDeclarationsRequest{})
		if err != nil {
			return runtime, providerCapabilityTransportError("ContainerRegistry.DescribeRegistries", err)
		}
		if response.GetError() != nil {
			return runtime, fmt.Errorf("ContainerRegistry.DescribeRegistries: %s; provider error text suppressed", response.GetError().GetCode())
		}
		runtime.ContainerRegistries = response.GetRegistries()
	}
	if runtime.AdvertisedServices[pb.SecretStore_ServiceDesc.ServiceName] {
		response, err := pb.NewSecretStoreClient(conn).DescribeSecretStores(ctx, &pb.SecretStoreDeclarationsRequest{})
		if err != nil {
			return runtime, providerCapabilityTransportError("SecretStore.DescribeSecretStores", err)
		}
		if response.GetError() != nil {
			return runtime, fmt.Errorf("SecretStore.DescribeSecretStores: %s; provider error text suppressed", response.GetError().GetCode())
		}
		runtime.SecretStores = response.GetStores()
	}
	if len(declared.KubernetesBackends) > 0 && runtime.AdvertisedServices[pb.ResourceDriver_ServiceDesc.ServiceName] {
		response, err := pb.NewIaCProviderRequiredClient(conn).Capabilities(ctx, &pb.CapabilitiesRequest{})
		if err != nil {
			return runtime, providerCapabilityTransportError("IaCProviderRequired.Capabilities", err)
		}
		for _, capability := range response.GetCapabilities() {
			runtime.KubernetesResourceTypes = append(runtime.KubernetesResourceTypes, capability.GetResourceType())
		}
	}
	return runtime, nil
}

func compareManifestWithRuntime(declared plugin.PluginManifest, runtime *pb.Manifest, opts manifestCompareOptions) []string {
	var failures []string
	if !opts.SkipName && runtime.GetName() != declared.Name {
		failures = append(failures, fmt.Sprintf("name: declared manifest=%q; binary Manifest.Name=%q", declared.Name, runtime.GetName()))
	}
	if pass, reason := diffVersion(declared.Version, runtime.GetVersion()); !pass {
		failures = append(failures, "version: "+reason)
	}
	return failures
}

// preflightBinary validates the --binary path before exec:
//   - non-empty + not literal "null" (guards against jq fallback returning empty)
//   - file exists and is a regular file (not directory)
//   - has at least one executable bit set
func preflightBinary(path string) error {
	if path == "" || path == "null" {
		return fmt.Errorf("--binary path empty (jq filter may have returned no match)")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %q: %w", path, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("--binary %q is a directory", path)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("--binary %q is not a regular file (mode=%s)", path, fi.Mode())
	}
	if fi.Mode()&0o111 == 0 {
		return fmt.Errorf("--binary %q is not executable (mode=%s)", path, fi.Mode())
	}
	return nil
}

// diffIaCServices computes directional set-difference of declared
// (plugin.json.iacServices) vs advertised (binary's filtered ContractRegistry).
// Returns (missing, extra) where:
//   - missing: declared but not advertised → caller emits FAIL (truth-loop bug).
//   - extra: advertised but not declared → caller emits WARN (additive doc-lag).
//
// Empty declared returns (nil, nil) → caller must skip the diff entirely.
func diffIaCServices(declared, advertised []string) (missing, extra []string) {
	if len(declared) == 0 {
		return nil, nil
	}
	declSet := make(map[string]bool, len(declared))
	for _, s := range declared {
		declSet[s] = true
	}
	advSet := make(map[string]bool, len(advertised))
	for _, s := range advertised {
		advSet[s] = true
	}
	for _, s := range declared {
		if !advSet[s] {
			missing = append(missing, s)
		}
	}
	for _, s := range advertised {
		if !declSet[s] {
			extra = append(extra, s)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

// serviceNamesFromRegistry returns SERVICE-kind contract names from reg
// whose ServiceName starts with namespacePrefix. Defense-in-depth: the SDK
// bridge (Task 3) also filters, but old-SDK plugins skip that filter — this
// client-side check prevents WARN-spam for unrebased plugin binaries.
// Returns nil for nil reg. Sorted for stable diff output.
func serviceNamesFromRegistry(reg *pb.ContractRegistry, namespacePrefix string) []string {
	if reg == nil {
		return nil
	}
	names := make([]string, 0, len(reg.Contracts))
	for _, c := range reg.Contracts {
		if c.GetKind() != pb.ContractKind_CONTRACT_KIND_SERVICE {
			continue
		}
		if !strings.HasPrefix(c.GetServiceName(), namespacePrefix) {
			continue
		}
		names = append(names, c.GetServiceName())
	}
	sort.Strings(names)
	return names
}

// isSentinel returns true when v is one of the SDK's dev-sentinel forms
// OR the on-disk plugin.json sentinel "0.0.0".
//
// SDK sentinel set (per plugin/external/sdk/buildversion.go:36-42):
//
//	"", "dev", "(devel)" — ResolveBuildVersion replaces these with build-info
//
// Plus build-info fallback produces "(devel) [@ <sha>[.dirty]]" — HasPrefix catches all forms.
// Plus on-disk plugin.json "0.0.0" sentinel (workflow#762 convention).
//
// The predicate MUST be a SUPERSET of the SDK's set; "dev" is defensive
// (canonical wiring through sdk.ResolveBuildVersion prevents literal "dev"
// from reaching the wire — included to catch non-canonical wiring accidents).
func isSentinel(v string) bool {
	switch v {
	case "", "dev", "0.0.0", "(devel)":
		return true
	}
	return strings.HasPrefix(v, "(devel)")
}

// diffVersion implements the Version-rule matrix from the design doc:
//
//	plugin.json   binary Manifest.Version   outcome
//	------------  ------------------------  -------
//	"0.0.0"       non-sentinel              PASS (CI artifact under verification)
//	"0.0.0"       sentinel                  FAIL (ldflag injection missing)
//	"X.Y.Z"       "vX.Y.Z" or "X.Y.Z"       PASS (normalize leading v)
//	"X.Y.Z"       sentinel                  FAIL (ldflag missing)
//	"X.Y.Z"       anything else             FAIL (version drift)
//
// Returns (pass bool, reason string). reason is non-empty only when pass=false.
func diffVersion(declared, runtime string) (bool, string) {
	runtimeSentinel := isSentinel(runtime)
	if declared == "0.0.0" {
		if runtimeSentinel {
			return false, fmt.Sprintf("ldflag injection missing: plugin.json=%q; binary Manifest.Version=%q (sentinel)", declared, runtime)
		}
		return true, ""
	}
	if runtimeSentinel {
		return false, fmt.Sprintf("ldflag injection missing: plugin.json=%q (release); binary Manifest.Version=%q (sentinel)", declared, runtime)
	}
	rNorm := strings.TrimPrefix(runtime, "v")
	if rNorm == declared {
		return true, ""
	}
	return false, fmt.Sprintf("version drift: plugin.json=%q; binary Manifest.Version=%q", declared, runtime)
}
