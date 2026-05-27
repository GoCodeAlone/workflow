package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/GoCodeAlone/workflow/plugin"
)

// runPluginValidateContract implements `wfctl plugin validate-contract`
// (workflow#758). Verifies that a plugin source directory satisfies the
// release contract: parseable plugin.json + populated capabilities +
// minEngineVersion + sdk.ResolveBuildVersion call site + goreleaser ldflag.
//
// With --for-publish, additionally enforces the strict-semver tag whitelist
// (^v\d+\.\d+\.\d+$). With --release-dir, asserts the shipped plugin.json's
// .version equals --tag (post-goreleaser-build verification).
func runPluginValidateContract(args []string) error {
	fs := flag.NewFlagSet("plugin validate-contract", flag.ContinueOnError)
	forPublish := fs.Bool("for-publish", false, "Apply publish-grade checks (strict-semver tag, etc.)")
	tag := fs.String("tag", "", "Release tag (e.g. v1.2.3); falls back to $GITHUB_REF_NAME then `git describe --tags --exact-match HEAD`")
	releaseDir := fs.String("release-dir", "", "Post-build verification: assert this dir's plugin.json carries --tag")
	requireContractKind := fs.String("require-contract-kind", "", "Require a static contract kind in plugin.contracts.json (for example: message)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl plugin validate-contract [options] <plugin-dir>

Validate a plugin source directory against the workflow release contract.

Checks (always):
  1. plugin.json exists, parses, passes PluginManifest.Validate()
  2. capabilities populated (non-empty)
  3. minEngineVersion populated
  4. main.go calls sdk.ResolveBuildVersion(...) and wires it via
     IaCServeOptions.BuildVersion or sdk.WithBuildVersion(...)
  5. .goreleaser.{yaml,yml} carries -X *.Version= ldflag injection

Additional with --for-publish:
  6. Resolved tag matches ^v\d+\.\d+\.\d+$
  7. With --release-dir: <dir>/plugin.json .version equals tag (minus leading v)

Examples:
  wfctl plugin validate-contract .
  wfctl plugin validate-contract --for-publish --tag v1.2.3 .
  wfctl plugin validate-contract --for-publish --tag v1.2.3 --release-dir .release .

See docs/PLUGIN_RELEASE_GATES.md for the full contract spec.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
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

	var failures []string
	addFail := func(msg string) { failures = append(failures, msg) }
	requiredKinds := parseRequiredContractKinds(*requireContractKind)

	// Check 1: plugin.json parses + Validate() OK
	manifestPath := filepath.Join(abs, "plugin.json")
	manifestBytes, err := os.ReadFile(manifestPath) // #nosec G304 -- operator-supplied path
	if err != nil {
		addFail(fmt.Sprintf("plugin.json: %v", err))
	}
	var manifest plugin.PluginManifest
	var rawManifest map[string]any
	if err == nil {
		if jerr := json.Unmarshal(manifestBytes, &manifest); jerr != nil {
			addFail(fmt.Sprintf("plugin.json: parse: %v", jerr))
		} else if verr := manifest.Validate(); verr != nil {
			addFail(fmt.Sprintf("plugin.json: validate: %v", verr))
		} else if manifest.Version == "0.0.0" {
			fmt.Fprintln(os.Stderr, "  INFO  plugin.json.version is dev sentinel \"0.0.0\" — release builds inject the tag via goreleaser ldflag")
		}
		_ = json.Unmarshal(manifestBytes, &rawManifest)
	}

	descriptors, _, _, _ := loadPluginContractDescriptors(abs, rawManifest, pluginAuditOptions{
		StrictContracts:      true,
		RequireContractKinds: requiredKinds,
	})
	auditResult := auditPluginRepoWithOptions(abs, pluginAuditOptions{
		StrictContracts:      true,
		RequireContractKinds: requiredKinds,
	})
	for _, finding := range auditResult.Findings {
		if isValidateContractBlockingFinding(finding) {
			addFail(fmt.Sprintf("%s: %s", finding.Code, finding.Message))
		}
	}
	staticMessageOnly := hasRequiredStaticMessageContract(descriptors, requiredKinds) && !hasPluginRuntimeSurface(abs, rawManifest)

	// Check 2 + 3: capabilities + minEngineVersion populated
	if err == nil && !staticMessageOnly {
		var raw map[string]any
		if jerr := json.Unmarshal(manifestBytes, &raw); jerr == nil {
			caps, ok := raw["capabilities"].(map[string]any)
			if !ok || len(caps) == 0 {
				addFail("plugin.json.capabilities: missing or empty")
			}
			mev, _ := raw["minEngineVersion"].(string)
			if strings.TrimSpace(mev) == "" {
				addFail("plugin.json.minEngineVersion: missing or empty")
			}
		}
	}

	// Check 4: any cmd/**/main.go contains ResolveBuildVersion AND BuildVersion wiring
	if !staticMessageOnly {
		mainFound, mainHasContract := scanMainGoFilesForContract(abs)
		if !mainFound {
			addFail("no cmd/**/main.go (or .go file under repo root) found to scan for contract")
		} else if !mainHasContract {
			addFail("no main.go contains both sdk.ResolveBuildVersion(...) AND (IaCServeOptions.BuildVersion: ... OR sdk.WithBuildVersion(...))")
		}
	}

	// Check 5: goreleaser config carries -X *.Version= ldflag
	if !staticMessageOnly && !goreleaserHasVersionLdflag(abs) {
		addFail(".goreleaser.{yaml,yml}: missing `-X *.Version=` ldflag (mandatory mechanism to deliver release tag into binary)")
	}

	// --for-publish: check 6 (tag format) + check 7 (release-dir match)
	if *forPublish {
		resolved := resolveTag(*tag)
		if resolved == "" {
			addFail("--for-publish: no tag supplied via --tag, $GITHUB_REF_NAME, or `git describe --tags --exact-match HEAD`")
		} else if !publishGradeSemverRe.MatchString(resolved) {
			addFail(fmt.Sprintf("--for-publish: tag %q is not release-grade semver (allowed: vN.N.N)", resolved))
		}
		if *releaseDir != "" && resolved != "" {
			rdManifest := filepath.Join(*releaseDir, "plugin.json")
			rdBytes, rerr := os.ReadFile(rdManifest) // #nosec G304 -- operator-supplied path
			if rerr != nil {
				addFail(fmt.Sprintf("--release-dir %q: %v", *releaseDir, rerr))
			} else {
				var rdRaw map[string]any
				_ = json.Unmarshal(rdBytes, &rdRaw)
				rdVer, _ := rdRaw["version"].(string)
				want := strings.TrimPrefix(resolved, "v")
				if rdVer != want {
					addFail(fmt.Sprintf("--release-dir %q: plugin.json.version=%q does not match --tag %q (want %q)", *releaseDir, rdVer, resolved, want))
				}
			}
		}
	}

	if len(failures) > 0 {
		fmt.Fprintln(os.Stderr, "wfctl plugin validate-contract: FAIL")
		for _, f := range failures {
			fmt.Fprintf(os.Stderr, "  - %s\n", f)
		}
		fmt.Fprintln(os.Stderr, "See docs/PLUGIN_RELEASE_GATES.md for contract details.")
		return fmt.Errorf("%d contract check(s) failed", len(failures))
	}
	fmt.Println("wfctl plugin validate-contract: PASS")
	return nil
}

func parseRequiredContractKinds(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func hasRequiredStaticMessageContract(descriptors []pluginContractDescriptor, requiredKinds []string) bool {
	var requiresMessage bool
	for _, required := range requiredKinds {
		if normalizePluginContractKind(required) == "message" {
			requiresMessage = true
			break
		}
	}
	if !requiresMessage {
		return false
	}
	for i := range descriptors {
		descriptor := &descriptors[i]
		if normalizePluginContractKind(descriptor.Kind) == "message" && strings.TrimSpace(descriptor.ContractType) != "" {
			return true
		}
	}
	return false
}

func hasPluginRuntimeSurface(dir string, manifest map[string]any) bool {
	if strings.TrimSpace(firstStringField(manifest, "minEngineVersion", "min_engine_version")) != "" {
		return true
	}
	switch capabilities := manifest["capabilities"].(type) {
	case []any:
		if len(capabilities) > 0 {
			return true
		}
	case map[string]any:
		if len(capabilities) > 0 {
			return true
		}
	}
	if info, err := os.Stat(filepath.Join(dir, "cmd")); err == nil && info.IsDir() {
		return true
	}
	if goreleaserHasVersionLdflag(dir) ||
		pluginAuditFileExists(filepath.Join(dir, ".goreleaser.yaml")) ||
		pluginAuditFileExists(filepath.Join(dir, ".goreleaser.yml")) ||
		pluginAuditFileExists(filepath.Join(dir, "go.mod")) {
		return true
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
				return true
			}
		}
	}
	return false
}

func isValidateContractBlockingFinding(finding planFinding) bool {
	switch finding.Code {
	case "invalid_plugin_contract_descriptors",
		"read_plugin_contract_descriptors",
		"unknown_contract_kind",
		"unknown_required_contract_kind",
		"missing_required_contract_kind",
		"invalid_message_contract_descriptor":
		return true
	default:
		return false
	}
}

var (
	// publishGradeSemverRe aliases the shared PublishGradeSemverRe (workflow#762)
	// so old in-file references keep working; new code should reference
	// PublishGradeSemverRe directly from plugin_release_grade_semver.go.
	publishGradeSemverRe  = PublishGradeSemverRe
	resolveBuildVersionRe = regexp.MustCompile(`sdk\.ResolveBuildVersion\s*\(`)
	buildVersionFieldRe   = regexp.MustCompile(`BuildVersion\s*:`)
	withBuildVersionRe    = regexp.MustCompile(`sdk\.WithBuildVersion\s*\(`)
	goreleaserLdflagRe    = regexp.MustCompile(`-X\s+\S*\.Version=`)
)

// scanMainGoFilesForContract walks dir/cmd/**/*.go and dir/*.go looking for
// the contract pattern. Returns (anyMainFound, anySatisfiesContract). The
// contract pattern is "file contains sdk.ResolveBuildVersion( AND (BuildVersion:
// OR sdk.WithBuildVersion()" — whole-file scoped (gofmt formats across lines).
func scanMainGoFilesForContract(dir string) (mainFound, satisfies bool) {
	candidates := []string{}
	// Walk cmd/**/main.go
	cmdDir := filepath.Join(dir, "cmd")
	if info, err := os.Stat(cmdDir); err == nil && info.IsDir() {
		_ = filepath.Walk(cmdDir, func(path string, fi os.FileInfo, werr error) error {
			if werr != nil {
				return werr
			}
			if fi.IsDir() {
				return nil
			}
			if filepath.Base(path) == "main.go" {
				candidates = append(candidates, path)
			}
			return nil
		})
	}
	// Also include *.go at repo root (some single-file plugins put main package there)
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
				candidates = append(candidates, filepath.Join(dir, e.Name()))
			}
		}
	}
	for _, c := range candidates {
		mainFound = true
		body, err := os.ReadFile(c) // #nosec G304 -- bounded set, operator-supplied root
		if err != nil {
			continue
		}
		hasResolve := resolveBuildVersionRe.Match(body)
		hasField := buildVersionFieldRe.Match(body)
		hasOpt := withBuildVersionRe.Match(body)
		if hasResolve && (hasField || hasOpt) {
			satisfies = true
			return
		}
	}
	return
}

func goreleaserHasVersionLdflag(dir string) bool {
	for _, name := range []string{".goreleaser.yaml", ".goreleaser.yml"} {
		body, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- bounded set
		if err != nil {
			continue
		}
		if goreleaserLdflagRe.Match(body) {
			return true
		}
	}
	return false
}

// resolveTag returns explicit --tag > GITHUB_REF_NAME env > git describe.
func resolveTag(explicit string) string {
	if t := strings.TrimSpace(explicit); t != "" {
		return t
	}
	if t := strings.TrimSpace(os.Getenv("GITHUB_REF_NAME")); t != "" {
		return t
	}
	cmd := exec.Command("git", "describe", "--tags", "--exact-match", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
