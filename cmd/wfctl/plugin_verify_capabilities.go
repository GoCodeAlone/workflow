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
	"strings"
	"time"

	goplugin "github.com/GoCodeAlone/go-plugin"
	"github.com/GoCodeAlone/workflow/plugin"
	external "github.com/GoCodeAlone/workflow/plugin/external"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	hclog "github.com/hashicorp/go-hclog"
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
	if err := preflightBinary(*binary); err != nil {
		return err
	}

	abs, err := filepath.Abs(pluginDir)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", pluginDir, err)
	}
	manifestPath := filepath.Join(abs, "plugin.json")
	manifestBytes, err := os.ReadFile(manifestPath) //nolint:gosec // operator-supplied path.
	if err != nil {
		return fmt.Errorf("plugin.json: %w", err)
	}
	var declared plugin.PluginManifest
	if err := json.Unmarshal(manifestBytes, &declared); err != nil {
		return fmt.Errorf("plugin.json parse: %w", err)
	}
	if err := declared.Validate(); err != nil {
		return fmt.Errorf("plugin.json validate: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	binAbs, err := filepath.Abs(*binary)
	if err != nil {
		return fmt.Errorf("resolve --binary %q: %w", *binary, err)
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
			return fmt.Errorf("timeout waiting for plugin handshake (stderr: %s)", stderr.String())
		}
		return fmt.Errorf("plugin dial: %w (stderr: %s)", err, stderr.String())
	}
	raw, err := rpcClient.Dispense("plugin")
	if err != nil {
		return fmt.Errorf("dispense plugin: %w (stderr: %s)", err, stderr.String())
	}
	pluginClient, ok := raw.(*external.PluginClient)
	if !ok {
		return fmt.Errorf("dispensed object is %T, want *external.PluginClient", raw)
	}

	pbClient := pb.NewPluginServiceClient(pluginClient.Conn())
	runtime, err := pbClient.GetManifest(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("GetManifest RPC: %w (stderr: %s)", err, stderr.String())
	}

	var failures []string
	if runtime.GetName() != declared.Name {
		failures = append(failures, fmt.Sprintf("name: plugin.json=%q; binary Manifest.Name=%q", declared.Name, runtime.GetName()))
	}
	if pass, reason := diffVersion(declared.Version, runtime.GetVersion()); !pass {
		failures = append(failures, "version: "+reason)
	}
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
