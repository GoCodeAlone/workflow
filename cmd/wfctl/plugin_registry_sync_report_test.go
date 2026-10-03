package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func registryReportGit(t *testing.T, root string, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func commitRegistryReportFixture(t *testing.T, f *registrySyncTargetFixture) {
	t.Helper()
	registryReportGit(t, f.root, "init", "-q")
	mustWrite(t, filepath.Join(f.root, "README.md"), "Fixture registry\n")
	registryReportGit(t, f.root, "add", "--", "plugins", "README.md")
	registryReportGit(t, f.root, "-c", "user.name=Registry Fixture", "-c", "user.email=registry@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath="+os.DevNull, "commit", "-q", "-m", "fixture")
}

func readRegistryReport(t *testing.T, path string) (map[string]any, []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&report); err != nil {
		t.Fatal(err)
	}
	canonical, err := workflowCanonicalJSONV1(report)
	if err != nil || !bytes.Equal(canonical, data) {
		t.Fatalf("report is not number-free canonical JSON: %q, %v", data, err)
	}
	return report, data
}

func registryReportFixtureTree(t *testing.T, f *registrySyncTargetFixture, reportPath string) string {
	t.Helper()
	index := filepath.Join(t.TempDir(), "index")
	git := func(args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", f.root, "--literal-pathspecs"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+index)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture tree git %v: %v\n%s", args, err, out)
		}
		return out
	}
	git("read-tree", "HEAD")
	if relative, err := filepath.Rel(f.root, reportPath); err == nil && filepath.IsLocal(relative) {
		git("rm", "--cached", "--ignore-unmatch", "--", filepath.ToSlash(relative))
	}
	git("add", "-u", "--", ".")
	return strings.TrimSpace(string(git("write-tree")))
}

func assertRegistryReportBinding(t *testing.T, f *registrySyncTargetFixture, path, baseTree, oldVersion, newVersion string, changed bool) []byte {
	t.Helper()
	report, data := readRegistryReport(t, path)
	result := registryReportFixtureTree(t, f, path)
	if report["schema"] != "registry-sync-operation.v1" || report["plugin"] != "foo" || report["repository"] != "owner/repo" || report["old_version"] != oldVersion || report["new_version"] != newVersion || report["base_tree"] != baseTree || report["result_tree"] != result {
		t.Fatalf("report bindings disagree with actual fixture: %s", data)
	}
	diff := registryReportGit(t, f.root, "diff-tree", "--no-commit-id", "-r", "-p", "--binary", "--full-index", "--no-renames", "--no-ext-diff", "--no-textconv", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", baseTree, result, "--")
	if report["diff_digest"] != "sha256:"+registrySyncSHA256(diff) {
		t.Fatalf("report diff digest does not bind Git diff bytes: %s", data)
	}
	paths := report["changed_paths"].([]any)
	if changed {
		if len(paths) != 1 || paths[0] != "plugins/foo/manifest.json" || baseTree == result {
			t.Fatalf("wrong changed-path set: %s", data)
		}
	} else if len(paths) != 0 || baseTree != result || len(diff) != 0 {
		t.Fatalf("no-op must bind equal trees and the empty diff: %s", data)
	}
	return data
}

func TestPluginRegistrySyncReport_ForwardNoopRollback(t *testing.T) {
	f := newRegistrySyncTargetFixture(t)
	commitRegistryReportFixture(t, f)
	path := filepath.Join(f.root, ".github", "registry-operations", "operation.json")
	indexBefore := registryReportGit(t, f.root, "ls-files", "--stage", "-z")
	headBefore := registryReportGit(t, f.root, "rev-parse", "HEAD")
	old := "1.0.0"
	var priorNoop []byte
	for _, target := range []string{"2.0.0", "2.0.0", "2.0.0", "1.0.0", "1.0.0", "1.0.0"} {
		base := registryReportFixtureTree(t, f, path)
		if err := f.run(target, "--report", path); err != nil {
			t.Fatalf("report target %s: %v", target, err)
		}
		changed := old != target
		data := assertRegistryReportBinding(t, f, path, base, old, target, changed)
		if !changed {
			if priorNoop != nil && !bytes.Equal(priorNoop, data) {
				t.Fatal("repeated no-op report changed bytes")
			}
			priorNoop = data
		} else {
			priorNoop = nil
		}
		old = target
	}
	if !bytes.Equal(indexBefore, registryReportGit(t, f.root, "ls-files", "--stage", "-z")) || !bytes.Equal(headBefore, registryReportGit(t, f.root, "rev-parse", "HEAD")) {
		t.Fatal("report generation changed the checkout index or HEAD")
	}
}

func TestPluginRegistrySyncReport_CRLFNoop(t *testing.T) {
	f := newRegistrySyncTargetFixture(t)
	registryReportGit(t, f.root, "init", "-q")
	registryReportGit(t, f.root, "config", "core.autocrlf", "false")
	if err := f.run("2.0.0"); err != nil {
		t.Fatal(err)
	}
	commitRegistryReportFixture(t, f)
	lf := registryReportGit(t, f.root, "show", "HEAD:plugins/foo/manifest.json")
	if !bytes.Contains(lf, []byte("\n")) || bytes.Contains(lf, []byte("\r")) {
		t.Fatal("fixture did not commit an LF manifest")
	}
	registryReportGit(t, f.root, "config", "core.autocrlf", "true")
	crlf := bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))
	mustWrite(t, f.manifest, string(crlf))
	info, err := os.Stat(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.root, "operation.json")
	base := registryReportFixtureTree(t, f, path)
	if base != strings.TrimSpace(string(registryReportGit(t, f.root, "rev-parse", "HEAD^{tree}"))) {
		t.Fatal("CRLF fixture was not unchanged under Git's EOL rules")
	}
	var prior []byte
	for range 2 {
		if err := f.run("2.0.0", "--report", path); err != nil {
			t.Fatal(err)
		}
		data := assertRegistryReportBinding(t, f, path, base, "2.0.0", "2.0.0", false)
		if prior != nil && !bytes.Equal(prior, data) {
			t.Fatal("repeated CRLF no-op changed report bytes")
		}
		prior = data
		after, err := os.ReadFile(f.manifest)
		if err != nil {
			t.Fatal(err)
		}
		afterInfo, err := os.Stat(f.manifest)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(crlf, after) || !os.SameFile(info, afterInfo) {
			t.Fatal("CRLF no-op rewrote the working manifest")
		}
	}
}

func TestPluginRegistrySyncReport_CleanFilterResultTree(t *testing.T) {
	if _, err := exec.LookPath("sed"); err != nil {
		t.Skip("Git clean-filter fixture requires sed")
	}
	f := newRegistrySyncTargetFixture(t)
	registryReportGit(t, f.root, "init", "-q")
	registryReportGit(t, f.root, "config", "core.autocrlf", "false")
	registryReportGit(t, f.root, "config", "filter.registry-report.clean", "sed 's/preserved/filtered/g'")
	registryReportGit(t, f.root, "config", "filter.registry-report.required", "true")
	mustWrite(t, filepath.Join(f.root, ".gitattributes"), "plugins/foo/manifest.json filter=registry-report\n")
	registryReportGit(t, f.root, "add", "--", ".gitattributes")
	commitRegistryReportFixture(t, f)
	committed := registryReportGit(t, f.root, "show", "HEAD:plugins/foo/manifest.json")
	if !bytes.Contains(committed, []byte(`"description":"filtered"`)) {
		t.Fatal("fixture did not apply its clean filter to the committed manifest")
	}
	indexBefore := registryReportGit(t, f.root, "ls-files", "--stage", "-z")
	headBefore := registryReportGit(t, f.root, "rev-parse", "HEAD")
	path := filepath.Join(f.root, "operation.json")
	old := "1.0.0"
	for _, target := range []string{"2.0.0", "2.0.0", "1.0.0"} {
		base := registryReportFixtureTree(t, f, path)
		if err := f.run(target, "--report", path); err != nil {
			t.Fatal(err)
		}
		assertRegistryReportBinding(t, f, path, base, old, target, old != target)
		report, _ := readRegistryReport(t, path)
		result := registryReportGit(t, f.root, "show", report["result_tree"].(string)+":plugins/foo/manifest.json")
		working, err := os.ReadFile(f.manifest)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(working, []byte("preserved")) || !bytes.Equal(result, bytes.ReplaceAll(working, []byte("preserved"), []byte("filtered"))) {
			t.Fatal("report result did not bind the locally clean-filtered manifest")
		}
		old = target
	}
	if !bytes.Equal(indexBefore, registryReportGit(t, f.root, "ls-files", "--stage", "-z")) || !bytes.Equal(headBefore, registryReportGit(t, f.root, "rev-parse", "HEAD")) {
		t.Fatal("filtered report generation changed the checkout index or HEAD")
	}
}

func TestPluginRegistrySyncReport_AutomaticSelectionExactIDs(t *testing.T) {
	f := newRegistrySyncTargetFixture(t)
	commitRegistryReportFixture(t, f)
	f.mutate = func(path string, _ int, out map[string]any) string {
		if strings.Contains(path, "/releases/tags/") {
			out["id"] = int64(9007199254740993)
			out["target_commitish"] = "<>&"
			asset := out["assets"].([]any)[1].(map[string]any)
			asset["id"], asset["size"] = int64(9007199254740995), int64(9007199254740997)
		}
		return ""
	}
	path := filepath.Join(t.TempDir(), "operation.json")
	if err := runPluginRegistrySync([]string{"--registry-dir", f.root, "--plugin", "foo", "--fix", "--report", path}); err != nil {
		t.Fatal(err)
	}
	report, data := readRegistryReport(t, path)
	release := report["release"].(map[string]any)
	if release["id"] != "9007199254740993" || !bytes.Contains(data, []byte(`"target_commitish":"<>&"`)) {
		t.Fatalf("IDs rounded or strings HTML-escaped: %s", data)
	}
	asset := report["assets"].([]any)[1].(map[string]any)
	if asset["id"] != "9007199254740995" || asset["size"] != "9007199254740997" || f.latest.Load() != 1 {
		t.Fatalf("asset ID/size or automatic selection incorrect: %s", data)
	}
}

func TestPluginRegistrySyncReport_RejectsExistingInvalidReport(t *testing.T) {
	for _, kind := range []string{"unknown", "nested unknown", "schema", "number", "escape", "extra path", "missing", "noncanonical"} {
		t.Run(kind, func(t *testing.T) {
			f := newRegistrySyncTargetFixture(t)
			commitRegistryReportFixture(t, f)
			path := filepath.Join(f.root, "operation.json")
			if err := f.run("2.0.0", "--report", path); err != nil {
				t.Fatal(err)
			}
			report, data := readRegistryReport(t, path)
			switch kind {
			case "unknown":
				report["unknown"] = true
			case "nested unknown":
				report["release"].(map[string]any)["unknown"] = true
			case "schema":
				report["schema"] = "registry-sync-operation.v99"
			case "number":
				report["release"].(map[string]any)["id"] = 100
			case "escape":
				report["changed_paths"] = []any{"../escape"}
			case "extra path":
				report["changed_paths"] = []any{"README.md"}
			case "missing":
				delete(report, "source_sha")
			}
			if kind == "noncanonical" {
				data = append(data, '\n')
			} else {
				var err error
				data, err = json.Marshal(report)
				if err != nil {
					t.Fatal(err)
				}
			}
			mustWrite(t, path, string(data))
			before, err := os.ReadFile(f.manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.run("1.0.0", "--report", path); err == nil {
				t.Fatal("accepted invalid existing report")
			}
			after, err := os.ReadFile(f.manifest)
			if err != nil {
				t.Fatal(err)
			}
			unchanged, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) || !bytes.Equal(data, unchanged) {
				t.Fatal("failed report validation changed manifest or report")
			}
		})
	}
}

func TestPluginRegistrySyncReport_RejectsExtraGeneratedPath(t *testing.T) {
	for _, path := range []string{"README.md", "v1/evil.json"} {
		t.Run(path, func(t *testing.T) {
			f := newRegistrySyncTargetFixture(t)
			commitRegistryReportFixture(t, f)
			mustWrite(t, filepath.Join(f.root, path), "unexpected\n")
			if err := f.run("2.0.0", "--report", filepath.Join(t.TempDir(), "operation.json")); err == nil {
				t.Fatal("accepted extra generated path")
			}
		})
	}
}

func TestPluginRegistrySyncTarget_RejectsMutableRelease(t *testing.T) {
	f := newRegistrySyncTargetFixture(t)
	f.mutate = func(path string, _ int, out map[string]any) string {
		if strings.Contains(path, "/releases/tags/") {
			out["immutable"] = false
		}
		return ""
	}
	if err := f.run("2.0.0"); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("expected mutable release rejection, got %v", err)
	}
}

func TestPluginRegistrySyncReport_Schema(t *testing.T) {
	f := newRegistrySyncTargetFixture(t)
	commitRegistryReportFixture(t, f)
	path := filepath.Join(t.TempDir(), "operation.json")
	if err := f.run("2.0.0", "--report", path); err != nil {
		t.Fatal(err)
	}
	report, _ := readRegistryReport(t, path)
	data, err := os.ReadFile("../../docs/schemas/registry-sync-operation-v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	const schemaURL = "https://gocodealone.com/schemas/registry-sync-operation-v1.schema.json"
	if err := compiler.AddResource(schemaURL, doc); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(report); err != nil {
		t.Fatal(err)
	}
	report["release"].(map[string]any)["unexpected"] = true
	if err := schema.Validate(report); err == nil {
		t.Fatal("schema accepted an unknown nested field")
	}
}

func TestPluginRegistrySyncReport_TrackedReportExcluded(t *testing.T) {
	f := newRegistrySyncTargetFixture(t)
	commitRegistryReportFixture(t, f)
	path := filepath.Join(f.root, "operation.json")
	if err := f.run("2.0.0", "--report", path); err != nil {
		t.Fatal(err)
	}
	registryReportGit(t, f.root, "add", "--", "plugins/foo/manifest.json", "operation.json")
	registryReportGit(t, f.root, "-c", "user.name=Registry Fixture", "-c", "user.email=registry@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath="+os.DevNull, "commit", "-q", "-m", "tracked operation fixture")
	base := registryReportFixtureTree(t, f, path)
	if base == strings.TrimSpace(string(registryReportGit(t, f.root, "rev-parse", "HEAD^{tree}"))) {
		t.Fatal("fixture did not exclude the tracked report")
	}
	if err := f.run("2.0.0", "--report", path); err != nil {
		t.Fatal(err)
	}
	first := assertRegistryReportBinding(t, f, path, base, "2.0.0", "2.0.0", false)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.run("2.0.0", "--report", path); err != nil {
		t.Fatal(err)
	}
	second := assertRegistryReportBinding(t, f, path, base, "2.0.0", "2.0.0", false)
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || !os.SameFile(info, afterInfo) {
		t.Fatal("no-op report changed bytes or file identity")
	}
}

func TestPluginRegistrySyncReport_AtomicReplacement(t *testing.T) {
	f := newRegistrySyncTargetFixture(t)
	commitRegistryReportFixture(t, f)
	path := filepath.Join(t.TempDir(), "operation.json")
	if err := f.run("2.0.0", "--report", path); err != nil {
		t.Fatal(err)
	}
	for _, original := range []string{path, f.manifest} {
		link := filepath.Join(t.TempDir(), filepath.Base(original))
		if err := os.Link(original, link); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(link)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			after, err := os.ReadFile(link)
			if err != nil || !bytes.Equal(before, after) {
				t.Errorf("sync overwrote an existing inode in place: %s, %v", original, err)
			}
		})
	}
	if err := f.run("1.0.0", "--report", path); err != nil {
		t.Fatal(err)
	}
}

func TestPluginRegistrySyncReport_RejectsMutationWithoutWrites(t *testing.T) {
	for _, kind := range []string{"release", "asset", "checksum", "mutable"} {
		t.Run(kind, func(t *testing.T) {
			f := newRegistrySyncTargetFixture(t)
			commitRegistryReportFixture(t, f)
			before, err := os.ReadFile(f.manifest)
			if err != nil {
				t.Fatal(err)
			}
			f.mutate = func(path string, n int, out map[string]any) string {
				if n != 2 {
					return ""
				}
				if strings.Contains(path, "/releases/tags/") {
					switch kind {
					case "release":
						out["id"] = 999
					case "asset":
						out["assets"].([]any)[1].(map[string]any)["size"] = 999
					case "mutable":
						out["immutable"] = false
					}
				}
				if kind == "checksum" && strings.HasPrefix(path, "/checksums/") {
					return strings.Repeat("d", 64) + "  workflow-plugin-foo-linux-amd64.tar.gz\n"
				}
				return ""
			}
			path := filepath.Join(t.TempDir(), "operation.json")
			if err := f.run("2.0.0", "--report", path); err == nil {
				t.Fatal("accepted a changed release")
			}
			after, err := os.ReadFile(f.manifest)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("failed reread changed manifest: %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("failed reread wrote a report: %v", err)
			}
		})
	}
}

func TestPluginRegistrySyncReport_RejectsOutputEscapeAndWriteFailure(t *testing.T) {
	for _, kind := range []string{"relative escape", "manifest", "parent symlink", "blocked parent"} {
		t.Run(kind, func(t *testing.T) {
			f := newRegistrySyncTargetFixture(t)
			commitRegistryReportFixture(t, f)
			path := "../operation.json"
			switch kind {
			case "manifest":
				path = f.manifest
			case "parent symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(f.root, "linked")); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(f.root, "linked", "operation.json")
			case "blocked parent":
				mustWrite(t, filepath.Join(f.root, "blocked"), "file, not directory")
				path = filepath.Join(f.root, "blocked", "operation.json")
			}
			before, err := os.ReadFile(f.manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.run("2.0.0", "--report", path); err == nil {
				t.Fatal("accepted invalid report destination")
			}
			after, err := os.ReadFile(f.manifest)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("invalid report destination changed manifest: %v", err)
			}
		})
	}
}
