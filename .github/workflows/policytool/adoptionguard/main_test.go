package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"mvdan.cc/sh/v3/syntax"
)

const (
	expectedAuthorizationMarker       = "candidate_policy_authorized=true"
	githubExpressionPlaceholderPrefix = "github_expression_"
	testPolicyLauncherPath            = ".github/workflows/policytool/adoptionguard/run.sh"
	testPolicyHarnessPath             = "scripts/test-check-public-workflow-policy.sh"
	bridgePredecessorSHA              = "49e1803424f303f6f98cef51cc9ea2dee96c7d38"
	bridgePredecessorSnapshotSHA256   = "3df6364c48ea2a961f7f81b792c326e5c6141df123f2c8321cc014c5b009d58f"
)

const expectedPolicyLauncher = `#!/usr/bin/env bash
set -euo pipefail

launcher_source="${BASH_SOURCE[0]}"
if [[ -L "${launcher_source}" ]]; then
  echo "adoption guard launcher must not be a symlink" >&2
  exit 1
fi
if [[ "${launcher_source##*/}" != "run.sh" ]]; then
  echo "adoption guard launcher must use its canonical filename" >&2
  exit 1
fi
launcher_dir="$(cd -- "$(dirname -- "${launcher_source}")" && pwd -P)"
if [[ "${launcher_dir##*/}" != "adoptionguard" || ! -d "${launcher_dir}" || -L "${launcher_dir}" ]]; then
  echo "adoption guard launcher must reside in a real adoptionguard directory" >&2
  exit 1
fi
policytool_dir="${launcher_dir%/adoptionguard}"
if [[ "${policytool_dir}" == "${launcher_dir}" || "${policytool_dir##*/}" != "policytool" ||
  ! -d "${policytool_dir}" || -L "${policytool_dir}" ]]; then
  echo "adoption guard launcher could not locate the trusted policytool directory" >&2
  exit 1
fi
case "${policytool_dir}" in
  */.github/workflows/policytool) ;;
  *)
    echo "adoption guard launcher is outside the trusted policytool path" >&2
    exit 1
    ;;
esac

cd -- "${policytool_dir}"
exec env GOWORK=off GOFLAGS=-mod=readonly go run ./adoptionguard "$@"
`

type testAuthorityFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type testAuthorityBundle struct {
	State string              `json:"state"`
	Files []testAuthorityFile `json:"files"`
}

type testAuthorityManifest struct {
	Version int                   `json:"version"`
	Bundles []testAuthorityBundle `json:"bundles"`
}

type testExecutableEntry struct {
	Path          string `json:"path"`
	WorkflowPath  string `json:"workflowPath"`
	ContextSHA256 string `json:"contextSHA256"`
	SHA256        string `json:"sha256"`
	State         string `json:"state"`
	Rationale     string `json:"rationale"`
}

type adoptionFixture struct {
	base, candidate string
	activeWrapper   string
	stagedWrapper   string
}

type bridgeWorkflow struct {
	Name        string               `yaml:"name"`
	On          bridgeTriggers       `yaml:"on"`
	Permissions map[string]string    `yaml:"permissions"`
	Defaults    bridgeDefaults       `yaml:"defaults"`
	Jobs        map[string]bridgeJob `yaml:"jobs"`
}

type bridgeTriggers struct {
	PullRequestTarget struct {
		Branches []string `yaml:"branches"`
		Types    []string `yaml:"types"`
	} `yaml:"pull_request_target"`
	Push struct {
		Branches []string `yaml:"branches"`
	} `yaml:"push"`
}

type bridgeDefaults struct {
	Run struct {
		Shell string `yaml:"shell"`
	} `yaml:"run"`
}

type bridgeJob struct {
	RunsOn      string            `yaml:"runs-on"`
	Permissions map[string]string `yaml:"permissions"`
	Steps       []bridgeStep      `yaml:"steps"`
}

type bridgeStep struct {
	Name             string            `yaml:"name"`
	ID               string            `yaml:"id"`
	If               string            `yaml:"if"`
	ContinueOnError  bool              `yaml:"continue-on-error"`
	Uses             string            `yaml:"uses"`
	Env              map[string]string `yaml:"env"`
	With             map[string]any    `yaml:"with"`
	Run              string            `yaml:"run"`
	Shell            string            `yaml:"shell"`
	WorkingDirectory string            `yaml:"working-directory"`
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeFile(t *testing.T, root, relative string, data []byte) {
	t.Helper()
	writeFileMode(t, root, relative, data, 0o644)
}

func writeFileMode(t *testing.T, root, relative string, data []byte, mode os.FileMode) {
	t.Helper()
	filePath := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, data, mode); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, root, relative string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	writeFile(t, root, relative, data)
}

func readJSON[T any](t *testing.T, root, relative string) T {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func newAdoptionFixture(t *testing.T) adoptionFixture {
	t.Helper()
	base := filepath.Join(t.TempDir(), "base")
	candidate := filepath.Join(t.TempDir(), "candidate")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(candidate, 0o755); err != nil {
		t.Fatal(err)
	}
	var err error
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err = filepath.EvalSymlinks(candidate)
	if err != nil {
		t.Fatal(err)
	}

	workflow := []byte(`name: Public Workflow Policy
on:
  pull_request_target:
permissions:
  contents: read
jobs:
  policy:
    runs-on: ubuntu-latest
    steps:
      - run: echo policy
`)
	context, err := workflowContext(workflow)
	if err != nil {
		t.Fatal(err)
	}

	activeFiles := map[string][]byte{
		".github/workflows/policytool/adoptionguard/main.go":      []byte("trusted adoption guard\n"),
		".github/workflows/policytool/adoptionguard/main_test.go": []byte("trusted adoption guard tests\n"),
		testPolicyLauncherPath:                                    []byte(expectedPolicyLauncher),
		".github/workflows/policytool/main.go":                    []byte("active analyzer\n"),
		".github/workflows/policytool/main_test.go":               []byte("analyzer tests\n"),
		"scripts/check-public-workflow-policy.sh":                 []byte("active wrapper\n"),
	}
	stagedFiles := map[string][]byte{
		".github/workflows/policytool/adoptionguard/main.go":      activeFiles[".github/workflows/policytool/adoptionguard/main.go"],
		".github/workflows/policytool/adoptionguard/main_test.go": activeFiles[".github/workflows/policytool/adoptionguard/main_test.go"],
		testPolicyLauncherPath:                                    activeFiles[testPolicyLauncherPath],
		".github/workflows/policytool/main.go":                    []byte("staged analyzer\n"),
		".github/workflows/policytool/main_test.go":               activeFiles[".github/workflows/policytool/main_test.go"],
		"scripts/check-public-workflow-policy.sh":                 []byte("staged wrapper\n"),
	}

	activeBundle := testAuthorityBundle{State: "active"}
	stagedBundle := testAuthorityBundle{State: "staged"}
	for _, relative := range []string{
		".github/workflows/policytool/adoptionguard/main.go",
		".github/workflows/policytool/adoptionguard/main_test.go",
		testPolicyLauncherPath,
		".github/workflows/policytool/main.go",
		".github/workflows/policytool/main_test.go",
		"scripts/check-public-workflow-policy.sh",
	} {
		activeBundle.Files = append(activeBundle.Files, testAuthorityFile{Path: relative, SHA256: digest(activeFiles[relative])})
		stagedBundle.Files = append(stagedBundle.Files, testAuthorityFile{Path: relative, SHA256: digest(stagedFiles[relative])})
		mode := os.FileMode(0o644)
		if relative == "scripts/check-public-workflow-policy.sh" || relative == testPolicyLauncherPath {
			mode = 0o755
		}
		writeFileMode(t, base, relative, activeFiles[relative], mode)
		writeFileMode(t, candidate, relative, stagedFiles[relative], mode)
	}
	authority := testAuthorityManifest{Version: 1, Bundles: []testAuthorityBundle{activeBundle, stagedBundle}}
	writeJSON(t, base, ".github/public-workflow-authority.json", authority)
	writeJSON(t, candidate, ".github/public-workflow-authority.json", authority)

	writeFile(t, base, ".github/workflows/public-workflow-policy.yml", workflow)
	writeFile(t, candidate, ".github/workflows/public-workflow-policy.yml", workflow)
	presence := []map[string]string{{
		"path":          ".github/workflows/public-workflow-policy.yml",
		"contextSHA256": context,
		"state":         "active",
		"presence":      "present",
	}}
	writeJSON(t, base, ".github/public-workflow-presence-allowlist.json", presence)
	writeJSON(t, candidate, ".github/public-workflow-presence-allowlist.json", presence)
	writeJSON(t, base, ".github/public-workflow-action-allowlist.json", []any{})
	writeJSON(t, candidate, ".github/public-workflow-action-allowlist.json", []any{})
	writeJSON(t, base, ".github/public-workflow-command-allowlist.json", []any{})
	writeJSON(t, candidate, ".github/public-workflow-command-allowlist.json", []any{})
	writeJSON(t, base, ".github/public-workflow-secret-allowlist.json", []any{})
	writeJSON(t, candidate, ".github/public-workflow-secret-allowlist.json", []any{})

	activeWrapper := digest(activeFiles["scripts/check-public-workflow-policy.sh"])
	stagedWrapper := digest(stagedFiles["scripts/check-public-workflow-policy.sh"])
	baseExecutables := []testExecutableEntry{
		{
			Path:          "scripts/check-public-workflow-policy.sh",
			WorkflowPath:  ".github/workflows/ci.yml",
			ContextSHA256: strings.Repeat("a", 64),
			SHA256:        activeWrapper,
			State:         "active",
			Rationale:     "Run the trusted policy wrapper from CI.",
		},
		{
			Path:          "scripts/check-public-workflow-policy.sh",
			WorkflowPath:  ".github/workflows/public-workflow-policy.yml",
			ContextSHA256: context,
			SHA256:        activeWrapper,
			State:         "active",
			Rationale:     "Run the trusted policy wrapper against inert candidate data.",
		},
		{
			Path:          "scripts/unchanged-check.sh",
			WorkflowPath:  ".github/workflows/ci.yml",
			ContextSHA256: strings.Repeat("a", 64),
			SHA256:        strings.Repeat("b", 64),
			State:         "active",
			Rationale:     "Keep unrelated reviewed executable authority unchanged.",
		},
	}
	candidateExecutables := append([]testExecutableEntry(nil), baseExecutables...)
	for index := range candidateExecutables {
		if candidateExecutables[index].Path == "scripts/check-public-workflow-policy.sh" {
			candidateExecutables[index].SHA256 = stagedWrapper
		}
	}
	writeJSON(t, base, ".github/public-workflow-executable-allowlist.json", baseExecutables)
	writeJSON(t, candidate, ".github/public-workflow-executable-allowlist.json", candidateExecutables)

	return adoptionFixture{
		base: base, candidate: candidate,
		activeWrapper: activeWrapper, stagedWrapper: stagedWrapper,
	}
}

func invokeGuard(fixture adoptionFixture) (int, string, string) {
	return invokeGuardArgs([]string{"--base", fixture.base, "--candidate", fixture.candidate})
}

func invokeGuardArgs(args []string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func requireRejectedWithoutMarker(t *testing.T, fixture adoptionFixture) {
	t.Helper()
	code, stdout, stderr := invokeGuard(fixture)
	if code == 0 {
		t.Fatalf("guard unexpectedly authorized candidate: stdout=%q stderr=%q", stdout, stderr)
	}
	if strings.Contains(stdout, expectedAuthorizationMarker) || strings.Contains(stderr, expectedAuthorizationMarker) {
		t.Fatalf("failed guard emitted candidate-invocation marker: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestRunAuthorizesOnlyExactStagedAuthorityAdoption(t *testing.T) {
	fixture := newAdoptionFixture(t)
	code, stdout, stderr := invokeGuard(fixture)
	if code != 0 {
		t.Fatalf("guard exit = %d, stderr = %q", code, stderr)
	}
	if stdout != expectedAuthorizationMarker+"\n" {
		t.Fatalf("guard stdout = %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("guard stderr = %q", stderr)
	}
}

func newHarnessAdoptionFixture(t *testing.T, changed bool) adoptionFixture {
	t.Helper()
	fixture := newAdoptionFixture(t)
	activeHarness := []byte("#!/usr/bin/env bash\n# reviewed harness\n")
	stagedHarness := bytes.Clone(activeHarness)
	if changed {
		stagedHarness = append(stagedHarness, []byte("# staged harness\n")...)
	}
	writeFileMode(t, fixture.base, testPolicyHarnessPath, activeHarness, 0o755)
	writeFileMode(t, fixture.candidate, testPolicyHarnessPath, stagedHarness, 0o755)
	manifest := readJSON[testAuthorityManifest](t, fixture.base, authorityManifestPath)
	for index, data := range [][]byte{activeHarness, stagedHarness} {
		manifest.Bundles[index].Files = append(manifest.Bundles[index].Files, testAuthorityFile{Path: testPolicyHarnessPath, SHA256: digest(data)})
		slices.SortFunc(manifest.Bundles[index].Files, func(a, b testAuthorityFile) int { return strings.Compare(a.Path, b.Path) })
	}
	writeJSON(t, fixture.base, authorityManifestPath, manifest)
	writeJSON(t, fixture.candidate, authorityManifestPath, manifest)
	for _, root := range []string{fixture.base, fixture.candidate} {
		entries := readJSON[[]testExecutableEntry](t, root, executableManifestPath)
		wrapper := entries[0]
		wrapper.State = "staged"
		wrapper.ContextSHA256 = strings.Repeat("c", 64)
		entries = append(entries, wrapper)
		for _, state := range []string{"active", "staged"} {
			row := wrapper
			row.Path, row.State = testPolicyHarnessPath, state
			row.Rationale = "Run the reviewed policy mutation harness."
			row.SHA256 = digest(activeHarness)
			if root == fixture.candidate {
				row.SHA256 = digest(stagedHarness)
			}
			entries = append(entries, row)
		}
		writeJSON(t, root, executableManifestPath, entries)
	}
	return fixture
}

func TestRunAuthorizesWrapperAndOptionalHarnessDigests(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprintf("harness-changed-%t", changed), func(t *testing.T) {
			code, stdout, stderr := invokeGuard(newHarnessAdoptionFixture(t, changed))
			if code != 0 || stdout != expectedAuthorizationMarker+"\n" || stderr != "" {
				t.Fatalf("exact all-row wrapper/harness adoption: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}
}

func TestRunRejectsHarnessPinDriftWithoutAuthorizationMarker(t *testing.T) {
	for _, mutation := range []string{
		"partial-active-harness", "partial-staged-harness", "wrong-harness", "wrong-base-harness", "missing-harness-rows",
		"unrelated-digest", "duplicate", "base-duplicate", "metadata", "order", "cardinality", "missing-harness-file", "harness-mode",
		"partial-staged-wrapper", "unchanged-harness-pin-update",
	} {
		t.Run(mutation, func(t *testing.T) {
			fixture := newHarnessAdoptionFixture(t, mutation != "unchanged-harness-pin-update")
			entries := readJSON[[]testExecutableEntry](t, fixture.candidate, executableManifestPath)
			baseEntries := readJSON[[]testExecutableEntry](t, fixture.base, executableManifestPath)
			switch mutation {
			case "partial-active-harness":
				entries[4].SHA256 = baseEntries[4].SHA256
			case "partial-staged-harness":
				entries[5].SHA256 = baseEntries[5].SHA256
			case "wrong-harness", "unchanged-harness-pin-update":
				entries[4].SHA256 = strings.Repeat("d", 64)
			case "wrong-base-harness":
				baseEntries[4].SHA256 = strings.Repeat("d", 64)
			case "missing-harness-rows":
				entries, baseEntries = entries[:4], baseEntries[:4]
			case "unrelated-digest":
				entries[2].SHA256 = strings.Repeat("d", 64)
			case "duplicate":
				entries[5] = entries[4]
			case "base-duplicate":
				baseEntries[5] = baseEntries[4]
			case "metadata":
				entries[4].Rationale = "changed rationale"
			case "order":
				entries[4], entries[5] = entries[5], entries[4]
			case "cardinality":
				entries = entries[:5]
			case "missing-harness-file":
				if err := os.Remove(filepath.Join(fixture.candidate, testPolicyHarnessPath)); err != nil {
					t.Fatal(err)
				}
			case "harness-mode":
				if err := os.Chmod(filepath.Join(fixture.candidate, testPolicyHarnessPath), 0o644); err != nil {
					t.Fatal(err)
				}
			case "partial-staged-wrapper":
				entries[3].SHA256 = fixture.activeWrapper
			}
			writeJSON(t, fixture.base, executableManifestPath, baseEntries)
			writeJSON(t, fixture.candidate, executableManifestPath, entries)
			requireRejectedWithoutMarker(t, fixture)
		})
	}
}

func TestRunRejectsExecutableRowOrderWithoutAuthorizationMarker(t *testing.T) {
	fixture := newHarnessAdoptionFixture(t, false)
	entries := readJSON[[]testExecutableEntry](t, fixture.candidate, executableManifestPath)
	entries[0], entries[1] = entries[1], entries[0]
	writeJSON(t, fixture.candidate, executableManifestPath, entries)
	requireRejectedWithoutMarker(t, fixture)
}

func TestRunRejectsAuthorityAndTreeDriftWithoutAuthorizationMarker(t *testing.T) {
	tests := map[string]func(*testing.T, adoptionFixture){
		"malformed authority": func(t *testing.T, fixture adoptionFixture) {
			writeFile(t, fixture.base, ".github/public-workflow-authority.json", []byte("{"))
			writeFile(t, fixture.candidate, ".github/public-workflow-authority.json", []byte("{"))
		},
		"unequal authority": func(t *testing.T, fixture adoptionFixture) {
			manifest := readJSON[testAuthorityManifest](t, fixture.candidate, ".github/public-workflow-authority.json")
			manifest.Version = 2
			writeJSON(t, fixture.candidate, ".github/public-workflow-authority.json", manifest)
		},
		"base authority not realized": func(t *testing.T, fixture adoptionFixture) {
			writeFile(t, fixture.base, ".github/workflows/policytool/main.go", []byte("drift\n"))
		},
		"candidate authority partially realized": func(t *testing.T, fixture adoptionFixture) {
			data, err := os.ReadFile(filepath.Join(fixture.base, "scripts/check-public-workflow-policy.sh"))
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, fixture.candidate, "scripts/check-public-workflow-policy.sh", data)
		},
		"candidate authority mixed": func(t *testing.T, fixture adoptionFixture) {
			writeFile(t, fixture.candidate, ".github/workflows/policytool/main.go", []byte("neither bundle\n"))
		},
		"unrelated candidate diff": func(t *testing.T, fixture adoptionFixture) {
			writeFile(t, fixture.candidate, "docs/unrelated.md", []byte("not authority\n"))
		},
		"missing candidate file": func(t *testing.T, fixture adoptionFixture) {
			if err := os.Remove(filepath.Join(fixture.candidate, ".github/workflows/policytool/main.go")); err != nil {
				t.Fatal(err)
			}
		},
		"missing candidate adoption guard launcher": func(t *testing.T, fixture adoptionFixture) {
			if err := os.Remove(filepath.Join(fixture.candidate, filepath.FromSlash(testPolicyLauncherPath))); err != nil {
				t.Fatal(err)
			}
		},
		"changed active adoption guard": func(t *testing.T, fixture adoptionFixture) {
			candidateGuard := []byte("candidate guard\n")
			manifest := readJSON[testAuthorityManifest](t, fixture.candidate, ".github/public-workflow-authority.json")
			for index := range manifest.Bundles[1].Files {
				if manifest.Bundles[1].Files[index].Path == ".github/workflows/policytool/adoptionguard/main.go" {
					manifest.Bundles[1].Files[index].SHA256 = digest(candidateGuard)
				}
			}
			writeJSON(t, fixture.base, ".github/public-workflow-authority.json", manifest)
			writeJSON(t, fixture.candidate, ".github/public-workflow-authority.json", manifest)
			writeFile(t, fixture.candidate, ".github/workflows/policytool/adoptionguard/main.go", candidateGuard)
		},
		"changed active adoption guard launcher": func(t *testing.T, fixture adoptionFixture) {
			candidateLauncher := []byte("#!/usr/bin/env bash\nexit 0\n")
			manifest := readJSON[testAuthorityManifest](t, fixture.candidate, ".github/public-workflow-authority.json")
			for index := range manifest.Bundles[1].Files {
				if manifest.Bundles[1].Files[index].Path == testPolicyLauncherPath {
					manifest.Bundles[1].Files[index].SHA256 = digest(candidateLauncher)
				}
			}
			writeJSON(t, fixture.base, ".github/public-workflow-authority.json", manifest)
			writeJSON(t, fixture.candidate, ".github/public-workflow-authority.json", manifest)
			writeFileMode(t, fixture.candidate, testPolicyLauncherPath, candidateLauncher, 0o755)
		},
		"candidate symlink": func(t *testing.T, fixture adoptionFixture) {
			filePath := filepath.Join(fixture.candidate, ".github/workflows/policytool/main.go")
			if err := os.Remove(filePath); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(fixture.base, ".github/workflows/policytool/main.go"), filePath); err != nil {
				t.Fatal(err)
			}
		},
		"authority bundle order": func(t *testing.T, fixture adoptionFixture) {
			manifest := readJSON[testAuthorityManifest](t, fixture.base, ".github/public-workflow-authority.json")
			manifest.Bundles[0], manifest.Bundles[1] = manifest.Bundles[1], manifest.Bundles[0]
			writeJSON(t, fixture.base, ".github/public-workflow-authority.json", manifest)
			writeJSON(t, fixture.candidate, ".github/public-workflow-authority.json", manifest)
		},
		"authority bundle membership": func(t *testing.T, fixture adoptionFixture) {
			manifest := readJSON[testAuthorityManifest](t, fixture.base, ".github/public-workflow-authority.json")
			manifest.Bundles[1].Files = manifest.Bundles[1].Files[:len(manifest.Bundles[1].Files)-1]
			writeJSON(t, fixture.base, ".github/public-workflow-authority.json", manifest)
			writeJSON(t, fixture.candidate, ".github/public-workflow-authority.json", manifest)
		},
		"authority trailing JSON": func(t *testing.T, fixture adoptionFixture) {
			data, err := os.ReadFile(filepath.Join(fixture.base, ".github/public-workflow-authority.json"))
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, []byte("{}\n")...)
			writeFile(t, fixture.base, ".github/public-workflow-authority.json", data)
			writeFile(t, fixture.candidate, ".github/public-workflow-authority.json", data)
		},
		"authority unknown JSON field": func(t *testing.T, fixture adoptionFixture) {
			manifest := readJSON[map[string]any](t, fixture.base, ".github/public-workflow-authority.json")
			manifest["unexpected"] = true
			writeJSON(t, fixture.base, ".github/public-workflow-authority.json", manifest)
			writeJSON(t, fixture.candidate, ".github/public-workflow-authority.json", manifest)
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newAdoptionFixture(t)
			mutate(t, fixture)
			requireRejectedWithoutMarker(t, fixture)
		})
	}
}

func TestRunRejectsAuthorityExecutableModeDriftWithoutAuthorizationMarker(t *testing.T) {
	tests := map[string]func(*testing.T, adoptionFixture){
		"content and executable mode drift": func(t *testing.T, fixture adoptionFixture) {
			if err := os.Chmod(filepath.Join(fixture.candidate, ".github/workflows/policytool/main.go"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"policy wrapper non-executable in both trees": func(t *testing.T, fixture adoptionFixture) {
			for _, root := range []string{fixture.base, fixture.candidate} {
				if err := os.Chmod(filepath.Join(root, filepath.FromSlash(policyWrapperPath)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		},
		"adoption guard launcher non-executable in both trees": func(t *testing.T, fixture adoptionFixture) {
			for _, root := range []string{fixture.base, fixture.candidate} {
				if err := os.Chmod(filepath.Join(root, filepath.FromSlash(testPolicyLauncherPath)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		},
		"adoption guard launcher mode-only drift": func(t *testing.T, fixture adoptionFixture) {
			if err := os.Chmod(filepath.Join(fixture.candidate, filepath.FromSlash(testPolicyLauncherPath)), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"executable mode-only drift": func(t *testing.T, fixture adoptionFixture) {
			if err := os.Chmod(filepath.Join(fixture.candidate, ".github/workflows/policytool/adoptionguard/main_test.go"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newAdoptionFixture(t)
			mutate(t, fixture)
			requireRejectedWithoutMarker(t, fixture)
		})
	}
}

func TestRunRejectsTrustAndWrapperAuthorityDriftWithoutAuthorizationMarker(t *testing.T) {
	tests := map[string]func(*testing.T, adoptionFixture){
		"workflow changed": func(t *testing.T, fixture adoptionFixture) {
			writeFile(t, fixture.candidate, ".github/workflows/public-workflow-policy.yml", []byte("name: changed\n"))
		},
		"presence changed": func(t *testing.T, fixture adoptionFixture) {
			writeJSON(t, fixture.candidate, ".github/public-workflow-presence-allowlist.json", []any{})
		},
		"action authority changed": func(t *testing.T, fixture adoptionFixture) {
			writeJSON(t, fixture.candidate, ".github/public-workflow-action-allowlist.json", []map[string]string{{"path": "changed"}})
		},
		"command authority changed": func(t *testing.T, fixture adoptionFixture) {
			writeJSON(t, fixture.candidate, ".github/public-workflow-command-allowlist.json", []map[string]string{{"path": "changed"}})
		},
		"secret authority added": func(t *testing.T, fixture adoptionFixture) {
			secret := []map[string]string{{
				"path": ".github/workflows/public-workflow-policy.yml", "secret": "TOKEN",
				"contextSHA256": strings.Repeat("a", 64), "state": "active", "rationale": "forbidden",
			}}
			writeJSON(t, fixture.base, ".github/public-workflow-secret-allowlist.json", secret)
			writeJSON(t, fixture.candidate, ".github/public-workflow-secret-allowlist.json", secret)
		},
		"wrapper cardinality changed": func(t *testing.T, fixture adoptionFixture) {
			entries := readJSON[[]testExecutableEntry](t, fixture.candidate, ".github/public-workflow-executable-allowlist.json")
			writeJSON(t, fixture.candidate, ".github/public-workflow-executable-allowlist.json", entries[1:])
		},
		"wrapper metadata changed": func(t *testing.T, fixture adoptionFixture) {
			entries := readJSON[[]testExecutableEntry](t, fixture.candidate, ".github/public-workflow-executable-allowlist.json")
			entries[0].Rationale = "different rationale"
			writeJSON(t, fixture.candidate, ".github/public-workflow-executable-allowlist.json", entries)
		},
		"wrapper key changed": func(t *testing.T, fixture adoptionFixture) {
			entries := readJSON[[]testExecutableEntry](t, fixture.candidate, ".github/public-workflow-executable-allowlist.json")
			entries[0].WorkflowPath = ".github/workflows/other.yml"
			writeJSON(t, fixture.candidate, ".github/public-workflow-executable-allowlist.json", entries)
		},
		"wrapper digest not promoted": func(t *testing.T, fixture adoptionFixture) {
			entries := readJSON[[]testExecutableEntry](t, fixture.candidate, ".github/public-workflow-executable-allowlist.json")
			entries[0].SHA256 = fixture.activeWrapper
			writeJSON(t, fixture.candidate, ".github/public-workflow-executable-allowlist.json", entries)
		},
		"unrelated executable row changed": func(t *testing.T, fixture adoptionFixture) {
			entries := readJSON[[]testExecutableEntry](t, fixture.candidate, ".github/public-workflow-executable-allowlist.json")
			entries[2].Rationale = "changed unrelated metadata"
			writeJSON(t, fixture.candidate, ".github/public-workflow-executable-allowlist.json", entries)
		},
		"duplicate executable metadata": func(t *testing.T, fixture adoptionFixture) {
			entries := readJSON[[]testExecutableEntry](t, fixture.candidate, ".github/public-workflow-executable-allowlist.json")
			entries[1] = entries[0]
			writeJSON(t, fixture.candidate, ".github/public-workflow-executable-allowlist.json", entries)
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newAdoptionFixture(t)
			mutate(t, fixture)
			requireRejectedWithoutMarker(t, fixture)
		})
	}
}

func TestRunRejectsSecretOIDCAndActorInfluenceInActiveWorkflow(t *testing.T) {
	tests := map[string]string{
		"secret expression":     "name: Public Workflow Policy\non: push\npermissions:\n  contents: read\njobs:\n  policy:\n    runs-on: ubuntu-latest\n    steps:\n      - run: '${{ secrets.POLICY_TOKEN }}'\n",
		"actor expression":      "name: Public Workflow Policy\non: push\npermissions:\n  contents: read\njobs:\n  policy:\n    runs-on: ubuntu-latest\n    steps:\n      - run: '${{ github.actor }}'\n",
		"triggering actor":      "name: Public Workflow Policy\non: push\npermissions:\n  contents: read\njobs:\n  policy:\n    runs-on: ubuntu-latest\n    steps:\n      - run: '${{ github.triggering_actor }}'\n",
		"OIDC permission":       "name: Public Workflow Policy\non: push\npermissions:\n  contents: read\n  id-token: write\njobs:\n  policy:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo policy\n",
		"self-hosted runner":    "name: Public Workflow Policy\non: push\npermissions:\n  contents: read\njobs:\n  policy:\n    runs-on: self-hosted\n    steps:\n      - run: echo policy\n",
		"provider credential":   "name: Public Workflow Policy\non: push\npermissions:\n  contents: read\njobs:\n  policy:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo DIGITALOCEAN_TOKEN\n",
		"live provider command": "name: Public Workflow Policy\non: push\npermissions:\n  contents: read\njobs:\n  policy:\n    runs-on: ubuntu-latest\n    steps:\n      - run: doctl apps create\n",
	}
	for name, document := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newAdoptionFixture(t)
			workflow := []byte(document)
			context, err := workflowContext(workflow)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, fixture.base, ".github/workflows/public-workflow-policy.yml", workflow)
			writeFile(t, fixture.candidate, ".github/workflows/public-workflow-policy.yml", workflow)
			presence := []map[string]string{{
				"path": ".github/workflows/public-workflow-policy.yml", "contextSHA256": context,
				"state": "active", "presence": "present",
			}}
			writeJSON(t, fixture.base, ".github/public-workflow-presence-allowlist.json", presence)
			writeJSON(t, fixture.candidate, ".github/public-workflow-presence-allowlist.json", presence)
			requireRejectedWithoutMarker(t, fixture)
		})
	}
}

func TestRunRejectsInvalidRootRelationshipsWithoutAuthorizationMarker(t *testing.T) {
	tests := map[string]func(*testing.T, adoptionFixture) []string{
		"equal roots": func(t *testing.T, fixture adoptionFixture) []string {
			return []string{"--base", fixture.base, "--candidate", fixture.base}
		},
		"overlapping roots": func(t *testing.T, fixture adoptionFixture) []string {
			nested := filepath.Join(fixture.base, "candidate")
			if err := os.Mkdir(nested, 0o755); err != nil {
				t.Fatal(err)
			}
			return []string{"--base", fixture.base, "--candidate", nested}
		},
		"base symlink alias": func(t *testing.T, fixture adoptionFixture) []string {
			parent, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(parent, "base-alias")
			if err := os.Symlink(fixture.base, alias); err != nil {
				t.Fatal(err)
			}
			return []string{"--base", alias, "--candidate", fixture.candidate}
		},
		"candidate symlink alias": func(t *testing.T, fixture adoptionFixture) []string {
			parent, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(parent, "candidate-alias")
			if err := os.Symlink(fixture.candidate, alias); err != nil {
				t.Fatal(err)
			}
			return []string{"--base", fixture.base, "--candidate", alias}
		},
		"relative base root": func(t *testing.T, fixture adoptionFixture) []string {
			return []string{"--base", ".", "--candidate", fixture.candidate}
		},
		"unclean candidate root": func(t *testing.T, fixture adoptionFixture) []string {
			return []string{"--base", fixture.base, "--candidate", fixture.candidate + string(filepath.Separator) + "."}
		},
	}
	expectedError := map[string]string{
		"equal roots":             "distinct non-overlapping",
		"overlapping roots":       "distinct non-overlapping",
		"base symlink alias":      "must not contain symlink aliases",
		"candidate symlink alias": "must not contain symlink aliases",
		"relative base root":      "path must be absolute and clean",
		"unclean candidate root":  "path must be absolute and clean",
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newAdoptionFixture(t)
			code, stdout, stderr := invokeGuardArgs(args(t, fixture))
			if code == 0 {
				t.Fatalf("guard unexpectedly authorized invalid roots: stdout=%q stderr=%q", stdout, stderr)
			}
			if strings.Contains(stdout, expectedAuthorizationMarker) || strings.Contains(stderr, expectedAuthorizationMarker) {
				t.Fatalf("failed root guard emitted candidate-invocation marker: stdout=%q stderr=%q", stdout, stderr)
			}
			if !strings.Contains(stderr, expectedError[name]) {
				t.Fatalf("root rejection stderr = %q, want %q", stderr, expectedError[name])
			}
		})
	}
}

func TestWorkflowContextRejectsAliasesAndDuplicateKeys(t *testing.T) {
	for name, document := range map[string]string{
		"alias":     "name: policy\nshared: &shared value\nalias: *shared\n",
		"duplicate": "name: policy\nname: duplicate\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := workflowContext([]byte(document)); err == nil {
				t.Fatal("malformed workflow structure was accepted")
			}
		})
	}
}

func decodeBridgeWorkflow(data []byte) (bridgeWorkflow, error) {
	var workflow bridgeWorkflow
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&workflow); err != nil {
		return bridgeWorkflow{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return bridgeWorkflow{}, errors.New("public workflow policy contains multiple YAML documents")
		}
		return bridgeWorkflow{}, err
	}
	return workflow, nil
}

func readBridgeWorkflow(t *testing.T) bridgeWorkflow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "public-workflow-policy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := decodeBridgeWorkflow(data)
	if err != nil {
		t.Fatal(err)
	}
	return workflow
}

func bridgeStepByID(steps []bridgeStep, id string) (bridgeStep, int, error) {
	foundIndex := -1
	var found bridgeStep
	for index, step := range steps {
		if step.ID != id {
			continue
		}
		if foundIndex >= 0 {
			return bridgeStep{}, -1, fmt.Errorf("duplicate bridge step id %q", id)
		}
		found, foundIndex = step, index
	}
	if foundIndex < 0 {
		return bridgeStep{}, -1, fmt.Errorf("missing bridge step id %q", id)
	}
	return found, foundIndex, nil
}

func evaluateBridgeCondition(condition string, state map[string]string) (bool, error) {
	clauses := strings.Split(strings.TrimSpace(condition), " && ")
	if len(clauses) == 0 {
		return false, errors.New("empty bridge condition")
	}
	for _, clause := range clauses {
		parts := strings.SplitN(clause, " == ", 2)
		if len(parts) != 2 {
			return false, fmt.Errorf("unsupported bridge condition clause %q", clause)
		}
		reference := strings.TrimSpace(parts[0])
		expected := strings.Trim(strings.TrimSpace(parts[1]), "'")
		if expected == strings.TrimSpace(parts[1]) {
			return false, fmt.Errorf("bridge condition value must be a single-quoted literal in %q", clause)
		}
		if state[reference] != expected {
			return false, nil
		}
	}
	return true, nil
}

func normalizedShellStatements(source string) ([]string, error) {
	if strings.Contains(source, githubExpressionPlaceholderPrefix) {
		return nil, errors.New("shell source uses reserved GitHub expression placeholder namespace")
	}
	expressionPattern := regexp.MustCompile(`\$\{\{[^{}\r\n]+\}\}`)
	source = expressionPattern.ReplaceAllStringFunc(source, func(expression string) string {
		return githubExpressionPlaceholderPrefix + digest([]byte(expression))
	})
	file, err := syntax.NewParser(syntax.KeepComments(true)).Parse(strings.NewReader(source), "")
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	printer := syntax.NewPrinter()
	if err := printer.Print(&output, file); err != nil {
		return nil, err
	}
	return []string{strings.TrimSpace(output.String())}, nil
}

func validateExactRun(step bridgeStep, expected string) error {
	actualStatements, err := normalizedShellStatements(step.Run)
	if err != nil {
		return fmt.Errorf("%s run syntax: %w", step.ID, err)
	}
	expectedStatements, err := normalizedShellStatements(expected)
	if err != nil {
		return fmt.Errorf("test expected %s run syntax: %w", step.ID, err)
	}
	if !reflect.DeepEqual(actualStatements, expectedStatements) {
		return fmt.Errorf("%s normalized command sequence = %q, want %q", step.ID, actualStatements, expectedStatements)
	}
	return nil
}

func validateReadOnlyPermissions(scope string, permissions map[string]string) error {
	if len(permissions) != 1 || permissions["contents"] != "read" {
		return fmt.Errorf("%s permissions must be exactly contents: read", scope)
	}
	return nil
}

func validateTrustedCheckout(step bridgeStep) error {
	const checkoutAction = "actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5"
	if step.Uses != checkoutAction {
		return errors.New("trusted checkout must use the reviewed commit-pinned actions/checkout")
	}
	if !step.ContinueOnError {
		return errors.New("trusted checkout must preserve one-time bootstrap failure handling")
	}
	if step.If != "" || len(step.Env) != 0 {
		return errors.New("trusted checkout must be unconditional and receive no environment overrides")
	}
	expectedWith := map[string]any{
		"ref":                 "${{ github.event_name == 'push' && github.event.before || github.event.pull_request.base.sha }}",
		"path":                "trusted",
		"persist-credentials": false,
	}
	if !reflect.DeepEqual(step.With, expectedWith) {
		return fmt.Errorf("trusted checkout inputs = %v, want exact reviewed inputs", step.With)
	}
	return nil
}

func validatePinnedSetup(step bridgeStep) error {
	const setupGo = "actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff"
	if step.Uses != setupGo {
		return fmt.Errorf("%s must use the reviewed commit-pinned actions/setup-go", step.ID)
	}
	if step.ContinueOnError {
		return fmt.Errorf("%s must fail closed", step.ID)
	}
	expectedWith := map[string]any{
		"cache":      false,
		"go-version": "1.27.1",
	}
	if !reflect.DeepEqual(step.With, expectedWith) {
		return fmt.Errorf("%s inputs = %v, want exact reviewed inputs", step.ID, step.With)
	}
	return nil
}

func validateBridgeWorkflow(workflow bridgeWorkflow) error {
	if workflow.Name != "Public Workflow Policy" {
		return fmt.Errorf("workflow name = %q, want Public Workflow Policy", workflow.Name)
	}
	if !reflect.DeepEqual(workflow.On.PullRequestTarget.Branches, []string{"main"}) {
		return fmt.Errorf("pull_request_target branches = %v, want [main]", workflow.On.PullRequestTarget.Branches)
	}
	expectedPullRequestTypes := []string{"opened", "synchronize", "reopened", "ready_for_review", "edited"}
	if !reflect.DeepEqual(workflow.On.PullRequestTarget.Types, expectedPullRequestTypes) {
		return fmt.Errorf("pull_request_target types = %v, want %v", workflow.On.PullRequestTarget.Types, expectedPullRequestTypes)
	}
	if !reflect.DeepEqual(workflow.On.Push.Branches, []string{"main"}) {
		return fmt.Errorf("push branches = %v, want [main]", workflow.On.Push.Branches)
	}
	if err := validateReadOnlyPermissions("workflow", workflow.Permissions); err != nil {
		return err
	}
	if workflow.Defaults.Run.Shell != "bash" {
		return fmt.Errorf("workflow default shell = %q, want bash", workflow.Defaults.Run.Shell)
	}
	if len(workflow.Jobs) != 1 {
		return fmt.Errorf("workflow job count = %d, want exactly the policy job", len(workflow.Jobs))
	}
	job, ok := workflow.Jobs["policy"]
	if !ok {
		return errors.New("missing policy job")
	}
	if job.RunsOn != "ubuntu-latest" {
		return fmt.Errorf("policy runner = %q, want ubuntu-latest", job.RunsOn)
	}
	if err := validateReadOnlyPermissions("policy job", job.Permissions); err != nil {
		return err
	}
	expected := []struct {
		id, name string
	}{
		{"trusted-checkout", "Check out trusted base policy"},
		{"candidate-fetch", "Fetch candidate as inert data"},
		{"policy-root", "Select exact trusted policy root"},
		{"trusted-toolchain", "Set up Go for the selected trusted policy"},
		{"trusted-policy", "Scan candidate workflows with trusted base policy"},
		{"bootstrap-rejection", "Fail a rejected one-time bootstrap"},
		{"adoption-guard", "Authorize exact staged policy adoption"},
		{"candidate-toolchain", "Set up Go for the authorized candidate analyzer"},
		{"candidate-policy", "Scan candidate workflows with authorized candidate policy"},
	}
	if len(job.Steps) != len(expected) {
		return fmt.Errorf("policy step count = %d, want %d", len(job.Steps), len(expected))
	}
	for index, want := range expected {
		step := job.Steps[index]
		if step.ID != want.id || step.Name != want.name {
			return fmt.Errorf("policy step %d identity = %q/%q, want %q/%q", index, step.ID, step.Name, want.id, want.name)
		}
		if step.Shell != "" || step.WorkingDirectory != "" {
			return fmt.Errorf("%s must use the reviewed workflow-level shell and working directory", step.ID)
		}
		switch step.ID {
		case "candidate-fetch", "policy-root":
		default:
			if len(step.Env) != 0 {
				return fmt.Errorf("%s must receive no environment overrides", step.ID)
			}
		}
	}

	trustedCheckout, _, err := bridgeStepByID(job.Steps, "trusted-checkout")
	if err != nil {
		return err
	}
	if err := validateTrustedCheckout(trustedCheckout); err != nil {
		return err
	}

	candidateFetch, _, err := bridgeStepByID(job.Steps, "candidate-fetch")
	if err != nil {
		return err
	}
	expectedCandidateEnv := map[string]string{
		"EVENT_NAME":           "${{ github.event_name }}",
		"BASE_REF":             "${{ github.base_ref }}",
		"EVENT_REF":            "${{ github.ref }}",
		"CANDIDATE_REPOSITORY": "${{ github.event_name == 'push' && github.repository || github.event.pull_request.head.repo.full_name }}",
		"CANDIDATE_SHA":        "${{ github.event_name == 'push' && github.sha || github.event.pull_request.head.sha }}",
		"GIT_ASKPASS":          "/bin/false",
		"GIT_CONFIG_GLOBAL":    "/dev/null",
		"GIT_CONFIG_NOSYSTEM":  "1",
		"GIT_TERMINAL_PROMPT":  "0",
	}
	if !reflect.DeepEqual(candidateFetch.Env, expectedCandidateEnv) {
		return fmt.Errorf("candidate fetch environment = %v, want exact reviewed environment", candidateFetch.Env)
	}
	if err := validateExactRun(candidateFetch, `
case "${EVENT_NAME}:${BASE_REF}:${EVENT_REF}" in
  pull_request_target:main:refs/heads/main|push::refs/heads/main) ;;
  *)
    echo "public workflow policy accepts only push or pull_request_target for main" >&2
    exit 1
    ;;
esac
if [[ ! "$CANDIDATE_REPOSITORY" =~ ^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$ ]]; then
  echo "candidate repository must be an exact GitHub owner/name" >&2
  exit 1
fi
if [[ ! "$CANDIDATE_SHA" =~ ^[0-9a-f]{40}$ ]]; then
  echo "candidate SHA must be exactly 40 lowercase hexadecimal characters" >&2
  exit 1
fi
origin="https://github.com/${CANDIDATE_REPOSITORY}.git"
mkdir candidate-source candidate
git -C candidate-source init --quiet
git -C candidate-source \
  -c credential.helper= \
  -c core.askPass=/bin/false \
  -c credential.interactive=never \
  -c http.https://github.com/.extraheader= \
  fetch --no-tags --depth=1 "$origin" "$CANDIDATE_SHA"
fetched_sha="$(git -C candidate-source rev-parse 'FETCH_HEAD^{commit}')"
if [[ "$fetched_sha" != "$CANDIDATE_SHA" ]]; then
  echo "fetched candidate does not match the requested SHA" >&2
  exit 1
fi
git -C candidate-source ls-tree -r --format='%(objectmode)' FETCH_HEAD > candidate-tree-modes
while read -r object_mode; do
  case "$object_mode" in
    100644|100755) ;;
    *)
      echo "candidate tree contains forbidden object mode: $object_mode" >&2
      exit 1
      ;;
  esac
done < candidate-tree-modes
git -C candidate-source archive --worktree-attributes --format=tar FETCH_HEAD > candidate.tar
if ! LC_ALL=C tar -tvf candidate.tar > candidate.tar.list; then
  echo "candidate archive listing failed" >&2
  exit 1
fi
while read -r archive_entry; do
  case "${archive_entry:0:1}" in
    -|d) ;;
    *)
      echo "candidate archive contains a forbidden entry type" >&2
      exit 1
      ;;
  esac
done < candidate.tar.list
tar --extract --file=candidate.tar --directory=candidate --no-same-owner --no-same-permissions
symlink_path="$(find candidate -type l -print -quit)"
if [[ -n "$symlink_path" ]]; then
  echo "candidate archive contains a forbidden symlink" >&2
  exit 1
fi
`); err != nil {
		return err
	}

	policyRoot, _, err := bridgeStepByID(job.Steps, "policy-root")
	if err != nil {
		return err
	}
	expectedPolicyRootEnv := map[string]string{
		"BEFORE_SHA": "${{ github.event.before }}",
		"EVENT_NAME": "${{ github.event_name }}",
	}
	if !reflect.DeepEqual(policyRoot.Env, expectedPolicyRootEnv) {
		return fmt.Errorf("policy-root environment = %v, want exact reviewed environment", policyRoot.Env)
	}
	if policyRoot.If != "" || policyRoot.ContinueOnError {
		return errors.New("policy-root must be unconditional and fail closed")
	}
	if err := validateExactRun(policyRoot, `
root=trusted
if [ ! -x trusted/scripts/check-public-workflow-policy.sh ]; then
  # This one-time push executes newly merged main, never PR-head data.
  if [ "$EVENT_NAME" = push ] && [ "$BEFORE_SHA" = 9c364dd4e6dad83808f8a87c1ba990d0132f0372 ]; then
    root=candidate
  else
    echo "trusted workflow policy is missing or incomplete" >&2
    exit 1
  fi
fi
echo "root=$root" >> "$GITHUB_OUTPUT"
`); err != nil {
		return err
	}

	trustedSetup, _, err := bridgeStepByID(job.Steps, "trusted-toolchain")
	if err != nil {
		return err
	}
	if err := validatePinnedSetup(trustedSetup); err != nil {
		return err
	}
	if trustedSetup.If != "" {
		return errors.New("trusted policy toolchain setup must be unconditional")
	}
	trustedScan, trustedIndex, err := bridgeStepByID(job.Steps, "trusted-policy")
	if err != nil {
		return err
	}
	if trustedScan.If != "" || !trustedScan.ContinueOnError {
		return errors.New("trusted policy scan must be unconditional and continue on error so the active guard can evaluate an exact adoption")
	}
	if err := validateExactRun(trustedScan, `
if [ "${{ steps.policy-root.outputs.root }}" = candidate ]; then
  cd candidate && ./scripts/check-public-workflow-policy.sh --scan-root "${GITHUB_WORKSPACE}/candidate" --bootstrap-authority
else
  cd trusted && ./scripts/check-public-workflow-policy.sh --scan-root "${GITHUB_WORKSPACE}/candidate"
fi
`); err != nil {
		return err
	}

	bootstrap, bootstrapIndex, err := bridgeStepByID(job.Steps, "bootstrap-rejection")
	if err != nil {
		return err
	}
	const bootstrapCondition = "steps.policy-root.outputs.root == 'candidate' && steps.trusted-policy.outcome == 'failure'"
	if bootstrap.If != bootstrapCondition || bootstrap.ContinueOnError {
		return errors.New("bootstrap rejection must use trusted scan outcome and fail closed")
	}
	if err := validateExactRun(bootstrap, `
echo "one-time public workflow policy bootstrap was rejected" >&2
exit 1
`); err != nil {
		return err
	}

	guard, guardIndex, err := bridgeStepByID(job.Steps, "adoption-guard")
	if err != nil {
		return err
	}
	const guardCondition = "steps.policy-root.outputs.root == 'trusted' && steps.trusted-policy.outcome == 'failure'"
	if guard.If != guardCondition || guard.ContinueOnError {
		return errors.New("active adoption guard must run only after trusted-root scan failure and must fail the job on rejection")
	}
	if err := validateExactRun(guard, `
authorization="$(
  cd trusted
  ./.github/workflows/policytool/adoptionguard/run.sh \
    --base "${GITHUB_WORKSPACE}/trusted" \
    --candidate "${GITHUB_WORKSPACE}/candidate"
)"
if [ "$authorization" != "candidate_policy_authorized=true" ]; then
  echo "trusted adoption guard did not authorize candidate policy execution" >&2
  exit 1
fi
printf '%s\n' "$authorization" >> "$GITHUB_OUTPUT"
`); err != nil {
		return err
	}

	candidateSetup, setupIndex, err := bridgeStepByID(job.Steps, "candidate-toolchain")
	if err != nil {
		return err
	}
	const authorizationCondition = "steps.adoption-guard.outputs.candidate_policy_authorized == 'true'"
	if candidateSetup.If != authorizationCondition {
		return errors.New("candidate setup must consume only the exact guard authorization output")
	}
	if err := validatePinnedSetup(candidateSetup); err != nil {
		return err
	}
	candidateScan, candidateIndex, err := bridgeStepByID(job.Steps, "candidate-policy")
	if err != nil {
		return err
	}
	if candidateScan.If != authorizationCondition || candidateScan.ContinueOnError {
		return errors.New("candidate wrapper scan must be fail-closed and consume only exact guard authorization")
	}
	if err := validateExactRun(candidateScan, `
cd candidate
./scripts/check-public-workflow-policy.sh --scan-root "${GITHUB_WORKSPACE}/candidate"
`); err != nil {
		return err
	}
	if trustedIndex >= bootstrapIndex || bootstrapIndex >= guardIndex || guardIndex >= setupIndex || setupIndex >= candidateIndex {
		return errors.New("trusted scan, bootstrap rejection, active guard, candidate setup, and candidate scan order changed")
	}

	type scenario struct {
		name                          string
		state                         map[string]string
		bootstrap, guard, setup, scan bool
	}
	scenarios := []scenario{
		{
			name: "trusted success skips every fallback",
			state: map[string]string{
				"steps.policy-root.outputs.root": "trusted",
				"steps.trusted-policy.outcome":   "success",
			},
		},
		{
			name: "bootstrap rejection fails closed",
			state: map[string]string{
				"steps.policy-root.outputs.root": "candidate",
				"steps.trusted-policy.outcome":   "failure",
			},
			bootstrap: true,
		},
		{
			name: "trusted failure invokes guard but no marker invokes no candidate",
			state: map[string]string{
				"steps.policy-root.outputs.root": "trusted",
				"steps.trusted-policy.outcome":   "failure",
			},
			guard: true,
		},
		{
			name: "wrong marker invokes no candidate",
			state: map[string]string{
				"steps.policy-root.outputs.root":                           "trusted",
				"steps.trusted-policy.outcome":                             "failure",
				"steps.adoption-guard.outputs.candidate_policy_authorized": "false",
			},
			guard: true,
		},
		{
			name: "exact marker is sole candidate path",
			state: map[string]string{
				"steps.policy-root.outputs.root":                           "trusted",
				"steps.trusted-policy.outcome":                             "failure",
				"steps.adoption-guard.outputs.candidate_policy_authorized": "true",
			},
			guard: true, setup: true, scan: true,
		},
	}
	for _, scenario := range scenarios {
		actual := make([]bool, 4)
		for index, condition := range []string{bootstrap.If, guard.If, candidateSetup.If, candidateScan.If} {
			allowed, err := evaluateBridgeCondition(condition, scenario.state)
			if err != nil {
				return fmt.Errorf("%s: %w", scenario.name, err)
			}
			actual[index] = allowed
		}
		want := []bool{scenario.bootstrap, scenario.guard, scenario.setup, scenario.scan}
		if !reflect.DeepEqual(actual, want) {
			return fmt.Errorf("%s state table = %v, want %v", scenario.name, actual, want)
		}
	}
	return nil
}

func TestPublicWorkflowPolicyBridgeStateTable(t *testing.T) {
	if err := validateBridgeWorkflow(readBridgeWorkflow(t)); err != nil {
		t.Fatal(err)
	}
}

// Only the compiler's stdout/exit is substituted to exercise malformed-output
// handling. The workflow shell gate and trusted launcher remain unchanged;
// the actual-chain test separately executes the real guard and policy code.
func TestPublicWorkflowPolicyGuardRejectsMalformedOutputWithoutCandidateExecution(t *testing.T) {
	guard, _, err := bridgeStepByID(readBridgeWorkflow(t).Jobs["policy"].Steps, "adoption-guard")
	if err != nil {
		t.Fatal(err)
	}
	launcher, err := os.ReadFile("run.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, response := range []struct {
		name, stdout, exit string
	}{
		{"empty", "", "0"},
		{"false", "candidate_policy_authorized=false\n", "0"},
		{"leading-space", " candidate_policy_authorized=true\n", "0"},
		{"trailing-space", "candidate_policy_authorized=true \n", "0"},
		{"extra-output", "diagnostic\ncandidate_policy_authorized=true\n", "0"},
		{"duplicate-marker", "candidate_policy_authorized=true\ncandidate_policy_authorized=true\n", "0"},
		{"carriage-return", "candidate_policy_authorized=true\r\n", "0"},
		{"failed-exact-marker", "candidate_policy_authorized=true\n", "1"},
	} {
		t.Run(response.name, func(t *testing.T) {
			workspace := bridgeTempRoot(t)
			writeFileMode(t, workspace, "trusted/"+testPolicyLauncherPath, launcher, 0o755)
			writeFileMode(t, workspace, "bin/go", []byte("#!/usr/bin/env bash\nprintf '%s' \"${BRIDGE_GUARD_STDOUT}\"\nexit \"${BRIDGE_GUARD_EXIT}\"\n"), 0o755)
			writeFile(t, workspace, "guard.outputs", nil)
			source := guard.Run + "\nprintf 'candidate executed\\n' > \"$GITHUB_WORKSPACE/candidate.marker\"\n"
			stdout, stderr, err := bridgeCommand(t, workspace, []string{
				"PATH=" + filepath.Join(workspace, "bin") + string(os.PathListSeparator) + os.Getenv("PATH"),
				"GITHUB_WORKSPACE=" + workspace, "GITHUB_OUTPUT=" + filepath.Join(workspace, "guard.outputs"),
				"BRIDGE_GUARD_STDOUT=" + response.stdout, "BRIDGE_GUARD_EXIT=" + response.exit,
			}, "bash", "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", source)
			if err == nil || len(mustBridgeRead(t, workspace, "guard.outputs")) != 0 {
				t.Fatalf("malformed guard response authorized execution: err=%v stdout=%q stderr=%q", err, stdout, stderr)
			}
			if _, err := os.Stat(filepath.Join(workspace, "candidate.marker")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("malformed guard response reached candidate execution: %v", err)
			}
		})
	}
}

func TestPublicWorkflowPolicyBridgeGo127Pins(t *testing.T) {
	workflow := readBridgeWorkflow(t)
	for _, id := range []string{"trusted-toolchain", "candidate-toolchain"} {
		t.Run(id, func(t *testing.T) {
			step, _, err := bridgeStepByID(workflow.Jobs["policy"].Steps, id)
			if err != nil {
				t.Fatal(err)
			}
			if got := step.With["go-version"]; got != "1.27.1" {
				t.Errorf("%s compiler pin = %v, want 1.27.1 (not the isolated module floor)", id, got)
			}
			if _, present := step.With["go-version-file"]; present {
				t.Error("setup must not select the isolated module's minimum compiler")
			}
		})
	}
	module, err := os.ReadFile(filepath.Join("..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(module, []byte("\ngo 1.26.5\n")) {
		t.Fatal("isolated module minimum must remain Go 1.26.5")
	}
}

// Gzip/base64 canonical snapshot of the actual 49e predecessor: all 35 authority
// files, original trust manifests, public workflows and active executables.
// Each historical regular file retains its original bytes, SHA256 and mode.
const bridgePredecessorSnapshot = `
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
pHeh7Rl3sHqZ9Qm+N9gOZJ+Ijwc4PNCQxBg+EfIge/BDVs4e+ua3v7mOIwsXBquYC1NRjElq2havLSxlpltK4GeEE966HI0tP9EtfmRZuuyw+tuSseaoFqX3
GZX8jzvMhy9PWZ7iLmmcSSEO6O0DLgXfhfDK1GvczQm/tCaisDQviiWma8MUl0vGWt59Zj74IBoRx3dbRPowNTw0O3ozU3202Kbq0tI9h2FH5vYgmmnFKVPT
tyD5ZoQNX45wffsu/aHnvhw8FKsj+jDPjkfBzdLio8nxYoc5ky9Hdwd+7Cfph+An+sEyxZFoxgPPMH+mvYVPOetprJuB39XaF0RzIb37LPydREdQAOIcqEWH
fYwFLbLP9yXhV8VngKMssFd52piDXb4v+G2ctS3GHtqnM5e3xTadGlt+qRvMDsUrUS0t5upUcFwKnRNkgWUhhxhK4afPoblTnPkhXnxGcQthg/8btKsJnhSd
82PQ/UR5LRQfoJ4TIX0R8t9duENlepgaE15YmuHcYBXxtbfhW/bCezGIy71rECaeCVWDURSdGbyZWwtj6Qk3Kol3oL0L+0yk7RmO75Nv3Po9nXwfcL9d0H+3
9XOYpdcV2kvQXCn4o7H2Df7bDr/XYmThgPaaIkZT6di36dhUscKWWBnkUyR/S3KzXesnDjBnHRmD4oz7tlz836pNbNrEhbbGdYD33NH5on74htdauOkbjEFv
GSOSPyprAGVhxPo7wNKPanOD2g9BU6nA9W9C6sxLnpVjHS3lH5nDRX0ecNNXMbSa3kl9PRF+SNCuIt8Le/sG8lm1Plse+mvQWsB7dq0vkF9lMN3jc9+nRLoS
xOehdRCQj4e0CQjHSPldyxHK1XqODGsmhbwK0c75iu/SXiewrLCuRX8Wa6L4ztc4ODb35Hky+dW1Dyv5B8UDP4yqMwtV34HOKh+7WfxyXEmYU3m2SzewblVj
37FnyLvX+CttxnV9hMfrp9tN79E/1ObFbhZjbX1HGoD/t1nleG/67XajGomPnWWNFrolH22TVb9yX9CzYtjr8T4X/NmyhmCOkfieXtSw/Kkg/f9D6uF+2r3e
8XdDiT/PbML3WdO6RvmmCOmqI72cC2gB5K59/oR91EN89HhvBT5DSquAmQlILwTxneJ9gL26tpfWMH4t/EnN8Xk6VtbhJ4T2AL135uiHAPuIV89ZRK8cykEX
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
`

// This fixture helper is compiled beside an unchanged copy of the trusted
// analyzer. It calls the production YAML/shell parsers to derive inert rows.
const bridgeTrustDerivationTest = `package main
import (
 "bytes"
 "encoding/json"
 "fmt"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "gopkg.in/yaml.v3"
 "mvdan.cc/sh/v3/syntax"
)
func TestBridgeTrustRows(t *testing.T) {
 root := os.Getenv("BRIDGE_FIXTURE_ROOT")
 const relative = ".github/workflows/public-workflow-policy.yml"
 data, err := os.ReadFile(filepath.Join(root, relative)); if err != nil { t.Fatal(err) }
 var doc yaml.Node
 if err := yaml.Unmarshal(data, &doc); err != nil { t.Fatal(err) }
 workflow := doc.Content[0]
 findings := &findingSet{}
 validateYAMLStructure(relative, workflow, findings)
 if len(findings.items) != 0 { t.Fatal(findings.items) }
 context := authorizationContextDigest(workflow, nil, nil)
 actions := []actionEntry{}
 commands := []commandEntry{}
 executables := []executableEntry{}
 seenCommands, seenExecutables := map[string]bool{}, map[string]bool{}
 addCommand := func(name, sha string) {
  key := commandKey(relative, name, sha, context)
  if seenCommands[key] { return }; seenCommands[key] = true
  commands = append(commands, commandEntry{Path:relative, Command:name, StatementSHA256:sha, ContextSHA256:context, State:"staged", Rationale:"Owned actual-chain fixture statement."})
 }
 jobs := mappingValue(workflow, "jobs")
 for i := 1; i < len(jobs.Content); i += 2 {
  steps := mappingValue(jobs.Content[i], "steps")
  for _, step := range steps.Content {
   if uses := mappingValue(step, "uses"); uses != nil {
    actions = append(actions, actionEntry{Path:relative, Uses:uses.Value, NodeSHA256:actionNodeDigest(step), ContextSHA256:context, State:"staged", Rationale:"Owned actual-chain fixture action."})
   }
   run := mappingValue(step, "run"); if run == nil { continue }
   normalized, err := normalizeGithubExpressions(run.Value); if err != nil { t.Fatal(err) }
   shell, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(normalized), relative); if err != nil { t.Fatal(err) }
   for _, stmt := range shell.Stmts {
    sha, err := statementDigest(stmt); if err != nil { t.Fatal(err) }
    subject := false
    syntax.Walk(stmt, func(node syntax.Node) bool {
     call, ok := node.(*syntax.CallExpr); if !ok { return true }
     if assignmentOnlyCall(call) { subject = true; addCommand("$assignment", sha) }
     if len(call.Args) == 0 { return true }; subject = true
     word, _, resolved := resolvedWorkflowCommand(call)
     name, literal := literalWord(word); if !resolved || !literal { t.Fatal("nonliteral fixture command") }
     addCommand(normalizedCommandName(name), sha)
     if strings.HasPrefix(name, "./") {
      path := strings.TrimPrefix(name, "./")
      if !seenExecutables[path] {
       seenExecutables[path] = true
       sum, err := hashAuthorityFile(filepath.Join(root, path)); if err != nil { t.Fatal(err) }
       executables = append(executables, executableEntry{Path:path, WorkflowPath:relative, ContextSHA256:context, SHA256:sum, State:"staged", Rationale:"Owned actual-chain fixture executable."})
      }
     }
     return true
    })
    if !subject { addCommand("$statement", sha) }
   }
  }
 }
 inventory, err := authorityInventory(root); if err != nil { t.Fatal(err) }
 rows := map[string]any{
  ".github/public-workflow-presence-allowlist.json": []trustGroup{{Path:relative, ContextSHA256:context, State:"staged", Presence:"present"}},
  ".github/public-workflow-action-allowlist.json": actions,
  ".github/public-workflow-command-allowlist.json": commands,
  ".github/public-workflow-executable-allowlist.json": executables,
 }
 encoded, err := json.Marshal(rows); if err != nil { t.Fatal(err) }
 fmt.Println("bridge-trust:" + string(encoded))
 encoded, err = json.Marshal(inventory); if err != nil { t.Fatal(err) }
 fmt.Println("bridge-inventory:" + string(encoded))
 // The wrapper bytes remain the real hash-verifying launcher, not a stub.
 wrapper, err := os.ReadFile(filepath.Join(root, "scripts/check-public-workflow-policy.sh")); if err != nil { t.Fatal(err) }
 if !bytes.Contains(wrapper, []byte("verify_policytool")) { t.Fatal("missing real wrapper") }
}
`

func bridgeCopyFile(t *testing.T, source, target, relative string) {
	t.Helper()
	data, err := secureRead(source, relative)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(source, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, target, relative, data, info.Mode().Perm())
}

func bridgeHistoricalRoot(t *testing.T) string {
	t.Helper()
	compressed, err := base64.StdEncoding.DecodeString(bridgePredecessorSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := io.ReadAll(io.LimitReader(reader, 4<<20))
	closeErr := reader.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("decode historical snapshot: read=%v close=%v", err, closeErr)
	}
	if digest(canonical) != bridgePredecessorSnapshotSHA256 {
		t.Fatal("historical snapshot canonical checksum mismatch")
	}
	var snapshot struct {
		Commit string `json:"commit"`
		Files  []struct {
			Path   string `json:"path"`
			Mode   uint32 `json:"mode"`
			SHA256 string `json:"sha256"`
			Data   []byte `json:"data"`
		} `json:"files"`
	}
	if err := json.Unmarshal(canonical, &snapshot); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil || !bytes.Equal(encoded, canonical) || snapshot.Commit != bridgePredecessorSHA || len(snapshot.Files) != 53 {
		t.Fatal("historical snapshot is not the complete canonical predecessor")
	}
	root := bridgeTempRoot(t)
	previous := ""
	for _, file := range snapshot.Files {
		if file.Path <= previous || strings.Contains(file.Path, "\\") || !filepath.IsLocal(filepath.FromSlash(file.Path)) ||
			filepath.ToSlash(filepath.Clean(file.Path)) != file.Path || (file.Mode != 0o644 && file.Mode != 0o755) || digest(file.Data) != file.SHA256 {
			t.Fatalf("historical file path, mode or digest is invalid: %s", file.Path)
		}
		writeFileMode(t, root, file.Path, file.Data, os.FileMode(file.Mode))
		previous = file.Path
	}
	return root
}

func bridgeCopyTree(t *testing.T, source, target string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == ".git" && entry.IsDir() {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(target, relative), 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, filepath.Join(target, relative))
		}
		bridgeCopyFile(t, source, target, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func bridgeTempRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func bridgeCommand(t *testing.T, dir string, environment []string, command string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), command, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	cmd.Env = append(cmd.Env, environment...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

type actualBridgeFixture struct {
	initial, future string
	active, staged  testAuthorityBundle
	rows            map[string][]map[string]any
}

func newActualBridgeFixture(t *testing.T) actualBridgeFixture {
	t.Helper()
	repo, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	initial := bridgeHistoricalRoot(t)
	manifest := readJSON[testAuthorityManifest](t, initial, authorityManifestPath)
	if len(manifest.Bundles) != 1 || len(manifest.Bundles[0].Files) != 35 {
		t.Fatal("actual predecessor must retain the expected active 35-file inventory")
	}
	for _, file := range manifest.Bundles[0].Files {
		if digest(mustBridgeRead(t, initial, file.Path)) != file.SHA256 {
			t.Fatalf("historical authority inventory mismatch: %s", file.Path)
		}
	}
	future := bridgeTempRoot(t)
	bridgeCopyTree(t, initial, future)
	for _, relative := range append(append([]string{}, immutableGuardPaths...), policyWorkflowPath, "docs/public-workflow-policy.md") {
		bridgeCopyFile(t, repo, future, relative)
	}
	parser := bridgeTempRoot(t)
	for _, name := range []string{"main.go", "go.mod", "go.sum"} {
		data, err := secureRead(initial, ".github/workflows/policytool/"+name)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, parser, name, data)
	}
	writeFile(t, parser, "bridge_fixture_test.go", []byte(bridgeTrustDerivationTest))
	stdout, stderr, err := bridgeCommand(t, parser, []string{"BRIDGE_FIXTURE_ROOT=" + future}, "go", "test", ".", "-run", "^TestBridgeTrustRows$", "-count=1", "-v")
	if err != nil {
		t.Fatalf("derive fixture through real trusted parser: %v\n%s\n%s", err, stdout, stderr)
	}
	fixture := actualBridgeFixture{initial: initial, future: future, active: manifest.Bundles[0]}
	for line := range strings.SplitSeq(stdout, "\n") {
		if data, ok := strings.CutPrefix(line, "bridge-trust:"); ok {
			if err := json.Unmarshal([]byte(data), &fixture.rows); err != nil {
				t.Fatal(err)
			}
		}
		if data, ok := strings.CutPrefix(line, "bridge-inventory:"); ok {
			if err := json.Unmarshal([]byte(data), &fixture.staged.Files); err != nil {
				t.Fatal(err)
			}
		}
	}
	fixture.staged.State = "staged"
	if len(fixture.rows) != 4 || len(fixture.staged.Files) != 38 {
		t.Fatal("real parser must derive all four workflow groups and the complete 38-file future inventory")
	}
	if !bytes.Equal(mustBridgeRead(t, initial, testPolicyHarnessPath), mustBridgeRead(t, future, testPolicyHarnessPath)) {
		t.Fatal("five-file bridge must preserve the immutable historical harness")
	}
	return fixture
}

func (fixture actualBridgeFixture) root(t *testing.T, bundlePhase, workflowPhase int) string {
	t.Helper()
	root := bridgeTempRoot(t)
	bridgeCopyTree(t, fixture.initial, root)
	if bundlePhase >= 2 {
		for _, relative := range append(append([]string{}, immutableGuardPaths...), "docs/public-workflow-policy.md") {
			bridgeCopyFile(t, fixture.future, root, relative)
		}
	}
	manifest := testAuthorityManifest{Version: 1, Bundles: []testAuthorityBundle{fixture.active}}
	switch bundlePhase {
	case 1, 2:
		manifest.Bundles = append(manifest.Bundles, fixture.staged)
	case 3:
		active := fixture.staged
		active.State = "active"
		manifest.Bundles = []testAuthorityBundle{active}
	}
	writeJSON(t, root, authorityManifestPath, manifest)
	if workflowPhase >= 2 {
		bridgeCopyFile(t, fixture.future, root, policyWorkflowPath)
	}
	if workflowPhase > 0 {
		for path, additions := range fixture.rows {
			entries := readJSON[[]map[string]any](t, fixture.initial, path)
			if workflowPhase == 3 {
				retained := entries[:0]
				for _, entry := range entries {
					if entry["path"] != policyWorkflowPath && entry["workflowPath"] != policyWorkflowPath {
						retained = append(retained, entry)
					}
				}
				entries = retained
			}
			for _, addition := range additions {
				entry := make(map[string]any, len(addition))
				for key, value := range addition {
					entry[key] = value
				}
				if workflowPhase == 3 {
					entry["state"] = "active"
				}
				entries = append(entries, entry)
			}
			writeJSON(t, root, path, entries)
		}
	}
	return root
}

type actualBridgeResult struct {
	err                    error
	authorization          string
	candidateMarkers       string
	trustedScanFailed      bool
	candidateScanSucceeded bool
}

// Checkout/fetch/setup actions use owned local snapshots and the native Go
// compiler. All policy-root/scan/guard shell bodies and executables are real.
func executeActualBridge(t *testing.T, base, candidate, event string) actualBridgeResult {
	t.Helper()
	workspace := bridgeTempRoot(t)
	bridgeCopyTree(t, base, filepath.Join(workspace, "trusted"))
	bridgeCopyTree(t, candidate, filepath.Join(workspace, "candidate"))
	workflowData, err := secureRead(base, policyWorkflowPath)
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := decodeBridgeWorkflow(workflowData)
	if err != nil {
		t.Fatal(err)
	}
	steps := workflow.Jobs["policy"].Steps
	checkout, _, err := bridgeStepByID(steps, "trusted-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTrustedCheckout(checkout); err != nil {
		return actualBridgeResult{err: err}
	}
	if _, _, err := bridgeStepByID(steps, "candidate-toolchain"); err == nil {
		if err := validateBridgeWorkflow(workflow); err != nil {
			return actualBridgeResult{err: err}
		}
	}
	state := make(map[string]string)
	result := actualBridgeResult{}
	markerPath := filepath.Join(workspace, "candidate-execution.marker")
	for _, step := range steps {
		if step.Name == "Scan candidate workflows with trusted base policy" {
			step.ID = "trusted-policy"
		}
		if step.ID != "policy-root" && step.ID != "trusted-policy" && step.ID != "bootstrap-rejection" &&
			step.ID != "adoption-guard" && step.ID != "candidate-toolchain" && step.ID != "candidate-policy" {
			continue
		}
		if step.If != "" {
			allowed, err := evaluateBridgeCondition(step.If, state)
			if err != nil {
				t.Fatal(err)
			}
			if !allowed {
				continue
			}
		}
		if step.ID == "candidate-toolchain" || step.ID == "candidate-policy" {
			result.candidateMarkers += step.ID + "\n"
		}
		if step.Run == "" {
			if err := validatePinnedSetup(step); err != nil {
				result.err = err
				break
			}
			continue
		}
		outputPath := filepath.Join(workspace, step.ID+".outputs")
		writeFile(t, workspace, step.ID+".outputs", nil)
		source := strings.ReplaceAll(step.Run, "${{ steps.policy-root.outputs.root }}", state["steps.policy-root.outputs.root"])
		stdout, stderr, err := bridgeCommand(t, workspace, []string{
			"GITHUB_WORKSPACE=" + workspace, "GITHUB_OUTPUT=" + outputPath,
			"EVENT_NAME=" + event, "BEFORE_SHA=" + bridgePredecessorSHA,
			"BRIDGE_CANDIDATE_EXECUTION_MARKER=" + markerPath,
		}, "bash", "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", source)
		t.Logf("%s %s: exit=%v\nstdout:\n%sstderr:\n%s", event, step.ID, err, stdout, stderr)
		if step.ID == "trusted-policy" {
			result.trustedScanFailed = err != nil
		}
		if step.ID == "candidate-policy" {
			result.candidateScanSucceeded = err == nil
		}
		state["steps."+step.ID+".outcome"] = "success"
		if err != nil {
			state["steps."+step.ID+".outcome"] = "failure"
			if !step.ContinueOnError {
				result.err = err
				break
			}
		}
		outputs, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.SplitSeq(string(outputs), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if ok {
				state["steps."+step.ID+".outputs."+key] = value
			}
		}
		if step.ID == "policy-root" && state["steps.policy-root.outputs.root"] != "trusted" {
			t.Fatal("ordinary chain selected candidate instead of the immediate prior base")
		}
		if step.ID == "adoption-guard" {
			result.authorization = string(outputs)
			t.Logf("guard authorization output: %q", result.authorization)
		}
	}
	if marker, err := os.ReadFile(markerPath); err == nil {
		result.candidateMarkers += string(marker)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	t.Logf("candidate execution markers: %q", result.candidateMarkers)
	return result
}

func TestPublicWorkflowPolicyBridgeActualChain(t *testing.T) {
	fixture := newActualBridgeFixture(t)
	for _, event := range []string{"pull_request_target", "push"} {
		t.Run(event, func(t *testing.T) {
			for phase := 1; phase <= 3; phase++ {
				t.Run(fmt.Sprintf("paired-phase-%d", phase), func(t *testing.T) {
					result := executeActualBridge(t, fixture.root(t, phase-1, phase-1), fixture.root(t, phase, phase), event)
					if result.err != nil || result.authorization != "" || result.candidateMarkers != "" {
						t.Fatalf("paired transition must use only trusted analyzer: %+v", result)
					}
				})
				for _, bundleAhead := range []bool{false, true} {
					t.Run(fmt.Sprintf("skew-phase-%d-bundle-ahead-%t", phase, bundleAhead), func(t *testing.T) {
						bundle, workflow := phase-1, phase
						if bundleAhead {
							bundle, workflow = workflow, bundle
						}
						base := fixture.root(t, bundle, workflow)
						catchup := executeActualBridge(t, base, fixture.root(t, phase, phase), event)
						if catchup.err != nil || catchup.authorization != "" || catchup.candidateMarkers != "" {
							t.Fatalf("matching catch-up must remain trusted-only: %+v", catchup)
						}
						next := phase + 1
						if phase == 3 {
							next = 2
						}
						premature := executeActualBridge(t, base, fixture.root(t, next, next), event)
						if premature.err == nil || premature.authorization != "" || premature.candidateMarkers != "" {
							t.Fatalf("skew cannot authorize premature advance or rollback: %+v", premature)
						}
					})
				}
			}
			t.Run("same-PR-stage-and-adopt", func(t *testing.T) {
				result := executeActualBridge(t, fixture.root(t, 0, 0), fixture.root(t, 2, 2), event)
				if result.err == nil || result.authorization != "" || result.candidateMarkers != "" {
					t.Fatalf("unstaged candidate must stay inert: %+v", result)
				}
			})
		})
	}
	active := fixture.root(t, 3, 3)
	if !bytes.Equal(mustBridgeRead(t, active, policyWrapperPath), mustBridgeRead(t, fixture.initial, policyWrapperPath)) {
		t.Fatal("fallback base must execute the real original historical wrapper")
	}
	for _, harnessChanged := range []bool{false, true} {
		t.Run(fmt.Sprintf("executable-adoption-harness-changed-%t", harnessChanged), func(t *testing.T) {
			testActualExecutableAdoption(t, active, harnessChanged)
		})
	}
}

func testActualExecutableAdoption(t *testing.T, active string, harnessChanged bool) {
	t.Helper()
	stagedWrapper := append([]byte("#!/usr/bin/env bash\nprintf 'wrapper\\n' >> \"${BRIDGE_CANDIDATE_EXECUTION_MARKER:?}\"\n"), mustBridgeRead(t, active, policyWrapperPath)...)
	stagedFiles := map[string][]byte{policyWrapperPath: stagedWrapper}
	if harnessChanged {
		stagedFiles[testPolicyHarnessPath] = append(mustBridgeRead(t, active, testPolicyHarnessPath), []byte("\n# Benign staged-byte change for the real guard chain; not the B2 repair.\n")...)
	}
	base := bridgeTempRoot(t)
	bridgeCopyTree(t, active, base)
	manifest := readJSON[testAuthorityManifest](t, base, authorityManifestPath)
	staged := manifest.Bundles[0]
	staged.State = "staged"
	staged.Files = append([]testAuthorityFile(nil), staged.Files...)
	for index := range staged.Files {
		if data, ok := stagedFiles[staged.Files[index].Path]; ok {
			staged.Files[index].SHA256 = digest(data)
		}
	}
	manifest.Bundles = append(manifest.Bundles, staged)
	writeJSON(t, base, authorityManifestPath, manifest)
	adopt := bridgeTempRoot(t)
	bridgeCopyTree(t, base, adopt)
	for path, data := range stagedFiles {
		writeFileMode(t, adopt, path, data, 0o755)
	}
	entries := readJSON[[]testExecutableEntry](t, adopt, executableManifestPath)
	updated := make(map[string]int)
	for index := range entries {
		if data, ok := stagedFiles[entries[index].Path]; ok {
			entries[index].SHA256 = digest(data)
			updated[entries[index].Path+"/"+entries[index].State]++
		}
	}
	for path := range stagedFiles {
		if updated[path+"/active"] == 0 || updated[path+"/staged"] == 0 {
			t.Fatalf("real adoption must update all existing active and unused staged rows for %s: %v", path, updated)
		}
	}
	t.Logf("real executable digest replacements: %v", updated)
	writeJSON(t, adopt, executableManifestPath, entries)
	for _, event := range []string{"pull_request_target", "push"} {
		t.Run(event+"/authorized-real-wrapper-fallback", func(t *testing.T) {
			result := executeActualBridge(t, base, adopt, event)
			if result.err != nil || !result.trustedScanFailed || !result.candidateScanSucceeded || result.authorization != expectedAuthorizationMarker+"\n" ||
				result.candidateMarkers != "candidate-toolchain\ncandidate-policy\nwrapper\n" {
				t.Fatalf("real fallback must require exact authorization and execute real candidate scanner: %+v", result)
			}
		})
		mutations := []string{"extra-doc", "partial-wrapper", "unrelated-digest", "duplicate", "metadata", "order", "cardinality", "mixed-inventory", "symlink", "non-executable-wrapper", "workflow-edit", "unknown-root"}
		if harnessChanged {
			mutations = append(mutations, "partial-active-harness", "partial-staged-harness", "missing-harness-rows", "wrong-harness", "missing-harness-file", "harness-mode")
		}
		for _, mutation := range mutations {
			t.Run(event+"/unauthorized-"+mutation, func(t *testing.T) {
				candidate := bridgeTempRoot(t)
				bridgeCopyTree(t, adopt, candidate)
				trusted := base
				switch mutation {
				case "extra-doc":
					writeFile(t, candidate, "docs/unapproved.md", []byte("unauthorized delta\n"))
				case "partial-wrapper":
					entries := readJSON[[]testExecutableEntry](t, candidate, executableManifestPath)
					for index := range entries {
						if entries[index].Path == policyWrapperPath {
							entries[index].SHA256 = digest(mustBridgeRead(t, base, policyWrapperPath))
							break
						}
					}
					writeJSON(t, candidate, executableManifestPath, entries)
				case "partial-active-harness", "partial-staged-harness", "missing-harness-rows", "wrong-harness":
					entries := readJSON[[]testExecutableEntry](t, candidate, executableManifestPath)
					if mutation == "missing-harness-rows" {
						trusted = bridgeTempRoot(t)
						bridgeCopyTree(t, base, trusted)
						baseEntries := readJSON[[]testExecutableEntry](t, trusted, executableManifestPath)
						baseEntries = slices.DeleteFunc(baseEntries, func(row testExecutableEntry) bool { return row.Path == testPolicyHarnessPath })
						entries = slices.DeleteFunc(entries, func(row testExecutableEntry) bool { return row.Path == testPolicyHarnessPath })
						writeJSON(t, trusted, executableManifestPath, baseEntries)
					} else {
						for index := range entries {
							if entries[index].Path != testPolicyHarnessPath ||
								(mutation == "partial-active-harness" && entries[index].State != "active") ||
								(mutation == "partial-staged-harness" && entries[index].State != "staged") {
								continue
							}
							entries[index].SHA256 = digest(mustBridgeRead(t, base, testPolicyHarnessPath))
							if mutation == "wrong-harness" {
								entries[index].SHA256 = strings.Repeat("d", 64)
							}
							break
						}
					}
					writeJSON(t, candidate, executableManifestPath, entries)
				case "unrelated-digest", "duplicate", "metadata", "order", "cardinality":
					entries := readJSON[[]testExecutableEntry](t, candidate, executableManifestPath)
					switch mutation {
					case "unrelated-digest":
						found := false
						for index := range entries {
							if entries[index].Path != policyWrapperPath && entries[index].Path != testPolicyHarnessPath {
								entries[index].SHA256 = strings.Repeat("d", 64)
								found = true
								break
							}
						}
						if !found {
							t.Fatal("real fixture must retain unrelated executable authority")
						}
					case "duplicate":
						entries[1] = entries[0]
					case "metadata":
						entries[0].Rationale = "unapproved metadata"
					case "order":
						entries[0], entries[1] = entries[1], entries[0]
					case "cardinality":
						entries = entries[1:]
					}
					writeJSON(t, candidate, executableManifestPath, entries)
				case "missing-harness-file":
					if err := os.Remove(filepath.Join(candidate, testPolicyHarnessPath)); err != nil {
						t.Fatal(err)
					}
				case "harness-mode":
					if err := os.Chmod(filepath.Join(candidate, testPolicyHarnessPath), 0o644); err != nil {
						t.Fatal(err)
					}
				case "mixed-inventory":
					writeFile(t, candidate, ".github/workflows/policytool/main_test.go", []byte("wrong inventory\n"))
				case "symlink":
					if err := os.Symlink("public-workflow-policy.md", filepath.Join(candidate, "docs", "unapproved.md")); err != nil {
						t.Fatal(err)
					}
				case "non-executable-wrapper":
					if err := os.Chmod(filepath.Join(candidate, policyWrapperPath), 0o644); err != nil {
						t.Fatal(err)
					}
				case "workflow-edit":
					writeFile(t, candidate, policyWorkflowPath, append(mustBridgeRead(t, candidate, policyWorkflowPath), []byte("\nconcurrency: unapproved\n")...))
				case "unknown-root":
					trusted = bridgeTempRoot(t)
					bridgeCopyTree(t, base, trusted)
					if err := os.Remove(filepath.Join(trusted, policyWrapperPath)); err != nil {
						t.Fatal(err)
					}
				}
				result := executeActualBridge(t, trusted, candidate, event)
				if result.err == nil || result.authorization != "" || result.candidateMarkers != "" {
					t.Fatalf("unauthorized candidate executed or emitted marker: %+v", result)
				}
			})
		}
	}
}

func TestPublicWorkflowPolicyBridgeActualChainWithoutHistory(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, ordinaryGit := range []bool{true, false} {
		t.Run(fmt.Sprintf("ordinary-git-%t", ordinaryGit), func(t *testing.T) {
			root := bridgeTempRoot(t)
			for _, relative := range append(append([]string{}, immutableGuardPaths...), policyWorkflowPath,
				"docs/public-workflow-policy.md", ".github/workflows/policytool/go.mod", ".github/workflows/policytool/go.sum") {
				bridgeCopyFile(t, repo, root, relative)
			}
			if ordinaryGit {
				stdout, stderr, err := bridgeCommand(t, root, nil, "git", "init", "--quiet", root)
				if err != nil {
					t.Fatalf("initialize owned no-history ordinary Git root: %v\n%s\n%s", err, stdout, stderr)
				}
				if _, _, err := bridgeCommand(t, root, nil, "git", "cat-file", "-e", bridgePredecessorSHA+"^{commit}"); err == nil {
					t.Fatal("no-history fixture unexpectedly contains the predecessor object")
				}
			}
			stdout, stderr, err := bridgeCommand(t, filepath.Join(root, ".github/workflows/policytool"), nil,
				"go", "test", "./adoptionguard", "-run", "^TestPublicWorkflowPolicyBridgeActualChain$", "-count=1", "-v")
			t.Logf("no-history actual-chain stdout:\n%sstderr:\n%s", stdout, stderr)
			if err != nil {
				t.Fatalf("actual-chain must not require Git history: %v", err)
			}
		})
	}
}

func mustBridgeRead(t *testing.T, root, relative string) []byte {
	t.Helper()
	data, err := secureRead(root, relative)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPublicWorkflowPolicyUsesGovernedTrustedGuardLauncher(t *testing.T) {
	workflow := readBridgeWorkflow(t)
	guard, _, err := bridgeStepByID(workflow.Jobs["policy"].Steps, "adoption-guard")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(guard.Run, "GOFLAGS") || regexp.MustCompile(`\bgo[[:space:]]+run\b`).MatchString(guard.Run) {
		t.Fatalf("adoption guard workflow step invokes Go directly instead of the governed trusted launcher:\n%s", guard.Run)
	}
	if !strings.Contains(guard.Run, "./.github/workflows/policytool/adoptionguard/run.sh") {
		t.Fatalf("adoption guard workflow step does not invoke the governed trusted launcher:\n%s", guard.Run)
	}
}

func TestAdoptionGuardLauncherContract(t *testing.T) {
	data, err := os.ReadFile("run.sh")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != expectedPolicyLauncher {
		t.Fatalf("adoption guard launcher bytes changed:\ngot:\n%s\nwant:\n%s", data, expectedPolicyLauncher)
	}
	info, err := os.Stat("run.sh")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("adoption guard launcher mode = %#o, want 0755", info.Mode().Perm())
	}
}

func TestPublicWorkflowPolicyBridgeUnknownExecutionMetadataIsRejected(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "public-workflow-policy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "        id: policy-root\n"
	mutated := bytes.Replace(data, []byte(anchor), []byte(anchor+"        timeout-minutes: 5\n"), 1)
	if bytes.Equal(mutated, data) {
		t.Fatal("test mutation anchor was not found")
	}
	workflow, err := decodeBridgeWorkflow(mutated)
	if err == nil {
		err = validateBridgeWorkflow(workflow)
	}
	if err == nil {
		t.Fatal("unknown step execution metadata was accepted")
	}
}

func TestPublicWorkflowPolicyBridgeMutationsAreRejected(t *testing.T) {
	mutations := map[string]func([]bridgeStep){
		"weaken guard condition to conclusion": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "adoption-guard" {
					steps[index].If = "steps.policy-root.outputs.root == 'trusted' && steps.trusted-policy.conclusion == 'failure'"
				}
			}
		},
		"accept wrong marker": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "candidate-toolchain" {
					steps[index].If = "steps.adoption-guard.outputs.candidate_policy_authorized != ''"
				}
			}
		},
		"run candidate setup before guard": func(steps []bridgeStep) {
			guardIndex, setupIndex := -1, -1
			for index := range steps {
				switch steps[index].ID {
				case "adoption-guard":
					guardIndex = index
				case "candidate-toolchain":
					setupIndex = index
				}
			}
			steps[guardIndex], steps[setupIndex] = steps[setupIndex], steps[guardIndex]
		},
		"trusted setup continues on error": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-toolchain" {
					steps[index].ContinueOnError = true
				}
			}
		},
		"candidate setup continues on error": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "candidate-toolchain" {
					steps[index].ContinueOnError = true
				}
			}
		},
		"trusted setup becomes conditional": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-toolchain" {
					steps[index].If = "steps.policy-root.outputs.root == 'trusted'"
				}
			}
		},
		"trusted scan gains extra command": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-policy" {
					steps[index].Run += "\necho unexpected\n"
				}
			}
		},
		"candidate scan runs tests": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "candidate-policy" {
					steps[index].Run += "\nGOWORK=off go test ./...\n"
				}
			}
		},
		"candidate fetch executes appended command": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "candidate-fetch" {
					steps[index].Run += "\ncd candidate && ./malicious\n"
				}
			}
		},
		"candidate fetch accepts wrong push ref": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "candidate-fetch" {
					steps[index].Run = strings.Replace(
						steps[index].Run,
						"push::refs/heads/main",
						"push::*",
						1,
					)
				}
			}
		},
		"trusted checkout uses pull request head": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-checkout" {
					steps[index].With["ref"] = "${{ github.event.pull_request.head.sha }}"
				}
			}
		},
		"trusted checkout action becomes unpinned": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-checkout" {
					steps[index].Uses = "actions/checkout@main"
				}
			}
		},
		"trusted checkout path changes": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-checkout" {
					steps[index].With["path"] = "candidate"
				}
			}
		},
		"trusted checkout persists credentials": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-checkout" {
					steps[index].With["persist-credentials"] = true
				}
			}
		},
		"trusted checkout stops tolerating missing base": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-checkout" {
					steps[index].ContinueOnError = false
				}
			}
		},
		"trusted checkout gains extra input": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-checkout" {
					steps[index].With["fetch-depth"] = 0
				}
			}
		},
		"policy root executes candidate before selection": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "policy-root" {
					steps[index].Run = "cd candidate && ./malicious\n" + steps[index].Run
				}
			}
		},
		"policy root defaults to candidate": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "policy-root" {
					steps[index].Run = strings.Replace(steps[index].Run, "root=trusted", "root=candidate", 1)
				}
			}
		},
		"bootstrap rejection appends candidate execution": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "bootstrap-rejection" {
					steps[index].Run += "\ncd candidate && ./malicious\n"
				}
			}
		},
		"adoption guard prepends candidate execution": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "adoption-guard" {
					steps[index].Run = "cd candidate && ./malicious\n" + steps[index].Run
				}
			}
		},
		"adoption guard appends candidate execution": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "adoption-guard" {
					steps[index].Run += "\ncd candidate && ./malicious\n"
				}
			}
		},
		"adoption guard invokes Go directly": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "adoption-guard" {
					steps[index].Run = strings.Replace(
						steps[index].Run,
						"./.github/workflows/policytool/adoptionguard/run.sh",
						"env GOWORK=off GOFLAGS=-mod=readonly go run ./.github/workflows/policytool/adoptionguard",
						1,
					)
				}
			}
		},
		"trusted scan uses expression placeholder collision": func(steps []bridgeStep) {
			const expression = "${{ steps.policy-root.outputs.root }}"
			placeholder := githubExpressionPlaceholderPrefix + digest([]byte(expression))
			for index := range steps {
				if steps[index].ID == "trusted-policy" {
					steps[index].Run = strings.Replace(steps[index].Run, expression, placeholder, 1)
				}
			}
		},
		"trusted scan gains expression-bearing comment": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-policy" {
					steps[index].Run += "\n# ${{ github.event.pull_request.body }}\n"
				}
			}
		},
		"policy root uses candidate shell": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "policy-root" {
					steps[index].Shell = "candidate/malicious {0}"
				}
			}
		},
		"policy root uses candidate working directory": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "policy-root" {
					steps[index].WorkingDirectory = "candidate"
				}
			}
		},
		"trusted checkout is conditional": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-checkout" {
					steps[index].If = "github.event_name == 'push'"
				}
			}
		},
		"trusted checkout gains environment": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-checkout" {
					steps[index].Env = map[string]string{}
					steps[index].Env["BASH_ENV"] = "candidate/malicious"
				}
			}
		},
		"policy root spoofs event environment": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "policy-root" {
					steps[index].Env["EVENT_NAME"] = "push"
				}
			}
		},
		"adoption guard gains bash environment": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "adoption-guard" {
					steps[index].Env = map[string]string{}
					steps[index].Env["BASH_ENV"] = "candidate/malicious"
				}
			}
		},
		"trusted policy scan is skipped": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-policy" {
					steps[index].If = "false"
				}
			}
		},
		"trusted setup selects older compiler": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-toolchain" {
					steps[index].With["go-version"] = "1.26.5"
				}
			}
		},
		"candidate setup selects older compiler": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "candidate-toolchain" {
					steps[index].With["go-version"] = "1.26.5"
				}
			}
		},
		"trusted setup adds module-floor selection": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "trusted-toolchain" {
					steps[index].With["go-version-file"] = "trusted/.github/workflows/policytool/go.mod"
				}
			}
		},
		"candidate setup adds module-floor selection": func(steps []bridgeStep) {
			for index := range steps {
				if steps[index].ID == "candidate-toolchain" {
					steps[index].With["go-version-file"] = "candidate/.github/workflows/policytool/go.mod"
				}
			}
		},
	}
	if err := validateBridgeWorkflow(readBridgeWorkflow(t)); err != nil {
		t.Fatalf("real workflow must be valid before mutation checks: %v", err)
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			workflow := readBridgeWorkflow(t)
			job := workflow.Jobs["policy"]
			mutate(job.Steps)
			workflow.Jobs["policy"] = job
			if err := validateBridgeWorkflow(workflow); err == nil {
				t.Fatal("unsafe workflow mutation was accepted")
			}
		})
	}

	workflowMutations := map[string]func(*bridgeWorkflow){
		"workflow name changes": func(workflow *bridgeWorkflow) {
			workflow.Name = "Renamed Public Workflow Policy"
		},
		"pull request target gains another branch": func(workflow *bridgeWorkflow) {
			workflow.On.PullRequestTarget.Branches = append(workflow.On.PullRequestTarget.Branches, "develop")
		},
		"pull request target loses synchronize activity": func(workflow *bridgeWorkflow) {
			activities := workflow.On.PullRequestTarget.Types[:0]
			for _, activity := range workflow.On.PullRequestTarget.Types {
				if activity != "synchronize" {
					activities = append(activities, activity)
				}
			}
			workflow.On.PullRequestTarget.Types = activities
		},
		"pull request target gains closed activity": func(workflow *bridgeWorkflow) {
			workflow.On.PullRequestTarget.Types = append(workflow.On.PullRequestTarget.Types, "closed")
		},
		"push target loses main branch": func(workflow *bridgeWorkflow) {
			workflow.On.Push.Branches = nil
		},
		"push target gains another branch": func(workflow *bridgeWorkflow) {
			workflow.On.Push.Branches = append(workflow.On.Push.Branches, "develop")
		},
		"workflow permissions expand": func(workflow *bridgeWorkflow) {
			workflow.Permissions["actions"] = "read"
		},
		"job permissions expand": func(workflow *bridgeWorkflow) {
			job := workflow.Jobs["policy"]
			job.Permissions["actions"] = "read"
			workflow.Jobs["policy"] = job
		},
		"runner becomes self-hosted": func(workflow *bridgeWorkflow) {
			job := workflow.Jobs["policy"]
			job.RunsOn = "self-hosted"
			workflow.Jobs["policy"] = job
		},
		"base ref context changes": func(workflow *bridgeWorkflow) {
			job := workflow.Jobs["policy"]
			for index := range job.Steps {
				if job.Steps[index].ID == "candidate-fetch" {
					job.Steps[index].Env["BASE_REF"] = "${{ github.ref_name }}"
				}
			}
			workflow.Jobs["policy"] = job
		},
		"event ref context changes": func(workflow *bridgeWorkflow) {
			job := workflow.Jobs["policy"]
			for index := range job.Steps {
				if job.Steps[index].ID == "candidate-fetch" {
					job.Steps[index].Env["EVENT_REF"] = "${{ github.ref_name }}"
				}
			}
			workflow.Jobs["policy"] = job
		},
		"candidate environment loses prompt guard": func(workflow *bridgeWorkflow) {
			job := workflow.Jobs["policy"]
			for index := range job.Steps {
				if job.Steps[index].ID == "candidate-fetch" {
					delete(job.Steps[index].Env, "GIT_ASKPASS")
				}
			}
			workflow.Jobs["policy"] = job
		},
		"candidate environment gains credential input": func(workflow *bridgeWorkflow) {
			job := workflow.Jobs["policy"]
			for index := range job.Steps {
				if job.Steps[index].ID == "candidate-fetch" {
					job.Steps[index].Env["CANDIDATE_TOKEN"] = "${{ secrets.GITHUB_TOKEN }}"
				}
			}
			workflow.Jobs["policy"] = job
		},
		"candidate environment enables terminal prompt": func(workflow *bridgeWorkflow) {
			job := workflow.Jobs["policy"]
			for index := range job.Steps {
				if job.Steps[index].ID == "candidate-fetch" {
					job.Steps[index].Env["GIT_TERMINAL_PROMPT"] = "1"
				}
			}
			workflow.Jobs["policy"] = job
		},
		"default shell executes candidate": func(workflow *bridgeWorkflow) {
			workflow.Defaults.Run.Shell = "candidate/malicious {0}"
		},
		"extra candidate job": func(workflow *bridgeWorkflow) {
			workflow.Jobs["candidate"] = bridgeJob{
				RunsOn: "ubuntu-latest",
				Steps: []bridgeStep{{
					Name: "Execute candidate",
					Run:  "cd candidate && ./malicious",
				}},
			}
		},
	}
	for name, mutate := range workflowMutations {
		t.Run(name, func(t *testing.T) {
			workflow := readBridgeWorkflow(t)
			mutate(&workflow)
			if err := validateBridgeWorkflow(workflow); err == nil {
				t.Fatal("unsafe workflow mutation was accepted")
			}
		})
	}
}
