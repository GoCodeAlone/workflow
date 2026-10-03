package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type registrySyncHostFileState struct {
	info os.FileInfo
	data []byte
}

type registrySyncHostRegistryState struct {
	files               map[string]registrySyncHostFileState
	head, index, status []byte
}

func registrySyncHostReadState(t *testing.T, f *registrySyncTargetFixture, watched ...string) registrySyncHostRegistryState {
	t.Helper()
	state := registrySyncHostRegistryState{files: make(map[string]registrySyncHostFileState)}
	read := func(path string) {
		t.Helper()
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			state.files[path] = registrySyncHostFileState{}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		file := registrySyncHostFileState{info: info}
		switch {
		case info.Mode().IsRegular():
			file.data, err = os.ReadFile(path)
		case info.Mode()&os.ModeSymlink != 0:
			var target string
			target, err = os.Readlink(path)
			file.data = []byte(target)
		}
		if err != nil {
			t.Fatal(err)
		}
		state.files[path] = file
	}
	if err := filepath.WalkDir(f.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == filepath.Join(f.root, ".git") {
			return filepath.SkipDir
		}
		read(path)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range watched {
		if !filepath.IsAbs(path) {
			path = filepath.Join(f.root, path)
		}
		read(path)
	}
	state.head = registryReportGit(t, f.root, "rev-parse", "HEAD")
	state.index = registryReportGit(t, f.root, "ls-files", "--stage", "-z")
	state.status = registryReportGit(t, f.root, "status", "--porcelain=v1", "--untracked-files=all", "-z")
	return state
}

func registrySyncHostAssertUnchanged(t *testing.T, before, after registrySyncHostRegistryState) {
	t.Helper()
	if len(before.files) != len(after.files) {
		t.Errorf("denied CLI changed the registry/report path set: before=%d after=%d", len(before.files), len(after.files))
	}
	for path, old := range before.files {
		current, ok := after.files[path]
		if !ok || (old.info == nil) != (current.info == nil) {
			t.Errorf("denied CLI created or removed %s", path)
			continue
		}
		if old.info != nil && (old.info.Mode() != current.info.Mode() || !os.SameFile(old.info, current.info) || !bytes.Equal(old.data, current.data)) {
			t.Errorf("denied CLI changed bytes, mode or file identity: %s", path)
		}
	}
	for _, field := range []struct {
		name          string
		before, after []byte
	}{
		{"HEAD", before.head, after.head},
		{"index entries", before.index, after.index},
		{"worktree status", before.status, after.status},
	} {
		if !bytes.Equal(field.before, field.after) {
			t.Errorf("denied CLI changed registry %s", field.name)
		}
	}
}

func TestPluginRegistrySyncHostNegatives(t *testing.T) {
	// One transport and binary serve independent, sequential registry fixtures.
	var active atomic.Pointer[registrySyncTargetFixture]
	var requests atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture := active.Load()
		if fixture == nil {
			http.Error(w, "no active registry fixture", http.StatusServiceUnavailable)
			return
		}
		requests.Add(1)
		fixture.handler.ServeHTTP(w, r)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	binary := registrySyncActualCLIBinary(t, runtime.GOOS, server.URL)
	env := registrySyncHostEnvironment(os.Environ())
	var proxy *registrySyncReleaseProxy
	if runtime.GOOS == "linux" {
		proxy = newRegistrySyncReleaseProxy(t, handler)
		env = append(env, "HTTPS_PROXY="+proxy.url, "NO_PROXY=127.0.0.1,localhost", "SSL_CERT_FILE="+proxy.caFile, "SSL_CERT_DIR="+proxy.caDirectory)
	}
	fixture := func(t *testing.T) (*registrySyncTargetFixture, string) {
		t.Helper()
		f := newRegistrySyncTargetFixture(t)
		commitRegistryReportFixture(t, f)
		return f, filepath.Join(f.root, "operation.json")
	}
	run := func(t *testing.T, f *registrySyncTargetFixture, target, plugin, report string) ([]byte, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, "plugin", "registry-sync", "--registry-dir", f.root, "--plugin", plugin, "--target-version", target, "--fix", "--report", report)
		cmd.Dir, cmd.Env = f.root, env
		cmd.WaitDelay = time.Second
		active.Store(f)
		out, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("CLI deadline is not a validation denial: %v\n%s", ctx.Err(), out)
		}
		if errors.Is(err, exec.ErrWaitDelay) {
			t.Fatalf("CLI capture drain did not finish: %v\n%s", err, out)
		}
		return out, err
	}
	seedReport := func(t *testing.T, f *registrySyncTargetFixture, report string) {
		t.Helper()
		base := registryReportFixtureTree(t, f, report)
		if out, err := run(t, f, "2.0.0", "foo", report); err != nil {
			t.Fatalf("actual CLI control failed before negative cases: %v\n%s", err, out)
		}
		assertRegistryReportBinding(t, f, report, base, "1.0.0", "2.0.0", true)
	}
	control, controlReport := fixture(t)
	seedReport(t, control, controlReport)
	if proxy != nil && (proxy.connects.Load() == 0 || proxy.requests.Load() == 0) {
		t.Fatal("actual Linux control bypassed the default-api.github.com CONNECT/TLS fixture")
	}
	reject := func(t *testing.T, f *registrySyncTargetFixture, target, plugin, report, want string, network bool, watched ...string) {
		t.Helper()
		watched = append(watched, report)
		before := registrySyncHostReadState(t, f, watched...)
		beforeRequests := requests.Load()
		out, err := run(t, f, target, plugin, report)
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(out), want) {
			t.Errorf("expected actual CLI denial containing %q, got %v\n%s", want, err, out)
		}
		if gotNetwork := requests.Load() > beforeRequests; gotNetwork != network {
			t.Errorf("denial reached incorrect validation phase: GitHub requests=%t, want=%t", gotNetwork, network)
		}
		if f.latest.Load() != 0 {
			t.Error("exact target denial consulted floating latest")
		}
		registrySyncHostAssertUnchanged(t, before, registrySyncHostReadState(t, f, watched...))
	}

	t.Run("mutation", func(t *testing.T) {
		for _, kind := range []string{"tag", "source", "release", "asset", "checksum", "metadata"} {
			t.Run(kind, func(t *testing.T) {
				f, report := fixture(t)
				var changed atomic.Int32
				f.mutate = func(path string, n int, out map[string]any) string {
					if n != 2 {
						return ""
					}
					switch {
					case kind == "tag" && strings.Contains(path, "/git/ref/"):
						out["object"].(map[string]any)["sha"] = strings.Repeat("d", 40)
					case kind == "source" && strings.Contains(path, "/git/tags/"):
						out["object"].(map[string]any)["sha"] = strings.Repeat("d", 40)
					case kind == "release" && strings.Contains(path, "/releases/tags/"):
						out["id"] = 101
					case kind == "asset" && strings.Contains(path, "/releases/tags/"):
						out["assets"].([]any)[1].(map[string]any)["id"] = 202
					case kind == "checksum" && strings.HasPrefix(path, "/checksums/"):
						changed.Add(1)
						return strings.Repeat("e", 64) + "  workflow-plugin-foo-linux-amd64.tar.gz\n"
					case kind == "metadata" && strings.HasSuffix(path, "/plugin.json"):
						out["content"] = base64.StdEncoding.EncodeToString([]byte(`{"type":"external","capabilities":{"services":["changed"]}}`))
					default:
						return ""
					}
					changed.Add(1)
					return ""
				}
				// Keep changed objects readable so rejection requires snapshot comparison.
				original := f.handler
				f.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if kind == "tag" && r.URL.Path == "/repos/owner/repo/git/tags/"+strings.Repeat("d", 40) {
						w.Header().Set("Content-Type", "application/json")
						if err := json.NewEncoder(w).Encode(map[string]any{
							"sha": strings.Repeat("d", 40), "object": map[string]any{"type": "commit", "sha": strings.Repeat("a", 40)},
						}); err != nil {
							t.Error(err)
						}
						return
					}
					if kind == "source" && r.URL.Path == "/repos/owner/repo/contents/plugin.json" && r.URL.Query().Get("ref") == strings.Repeat("d", 40) {
						r = r.Clone(r.Context())
						r.URL.RawQuery = "ref=" + strings.Repeat("a", 40)
					}
					original.ServeHTTP(w, r)
				})
				reject(t, f, "2.0.0", "foo", report, "changed", true)
				if changed.Load() != 1 {
					t.Fatalf("actual CLI did not consume the mutated second %s response: %d", kind, changed.Load())
				}
			})
		}
	})
	t.Run("mutable-release", func(t *testing.T) {
		f, report := fixture(t)
		var changed atomic.Int32
		f.mutate = func(path string, _ int, out map[string]any) string {
			if strings.Contains(path, "/releases/tags/") {
				out["immutable"] = false
				changed.Add(1)
			}
			return ""
		}
		reject(t, f, "2.0.0", "foo", report, "immutable", true)
		if changed.Load() != 1 {
			t.Fatal("actual CLI did not consume the mutable release")
		}
	})
	t.Run("invalid-target", func(t *testing.T) {
		for _, tc := range []struct{ name, target string }{
			{"empty", ""}, {"latest", "latest"}, {"incomplete", "1.2"}, {"prerelease", "1.2.3-rc.1"},
			{"build-metadata", "1.2.3+build"}, {"leading-zero", "01.2.3"}, {"escape", "../1.2.3"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f, report := fixture(t)
				reject(t, f, tc.target, "foo", report, "exact stable", false)
			})
		}
	})
	t.Run("path", func(t *testing.T) {
		for _, kind := range []string{"plugin-escape", "plugin-symlink", "report-escape", "report-manifest", "report-parent-symlink", "report-blocked-parent"} {
			t.Run(kind, func(t *testing.T) {
				f, report := fixture(t)
				plugin, want := "foo", ""
				var watched []string
				switch kind {
				case "plugin-escape":
					plugin, want = "../foo", "--plugin must name one plugin directory"
				case "plugin-symlink":
					outside := t.TempDir()
					manifest := filepath.Join(outside, "manifest.json")
					mustWrite(t, manifest, "outside manifest sentinel\n")
					if err := os.Symlink(outside, filepath.Join(f.root, "plugins", "escape")); err != nil {
						t.Fatal(err)
					}
					watched = append(watched, manifest)
					plugin, want = "escape", "registry plugin path contains symlink"
				case "report-escape":
					report, want = "../escaped-operation.json", "invalid plugin or report path"
				case "report-manifest":
					report, want = f.manifest, "report path overlaps governed registry paths"
				case "report-parent-symlink":
					outside := t.TempDir()
					sentinel := filepath.Join(outside, "sentinel")
					mustWrite(t, sentinel, "outside report sentinel\n")
					if err := os.Symlink(outside, filepath.Join(f.root, "linked")); err != nil {
						t.Fatal(err)
					}
					watched = append(watched, sentinel)
					report, want = filepath.Join(f.root, "linked", "operation.json"), "report path contains a symlink"
				case "report-blocked-parent":
					mustWrite(t, filepath.Join(f.root, "blocked"), "file, not directory\n")
					report, want = filepath.Join(f.root, "blocked", "operation.json"), "not a directory"
				}
				reject(t, f, "2.0.0", plugin, report, want, false, watched...)
			})
		}
	})
	t.Run("extra-generated-path", func(t *testing.T) {
		for _, path := range []string{"README.md", "v1/evil.json"} {
			t.Run(path, func(t *testing.T) {
				f, report := fixture(t)
				mustWrite(t, filepath.Join(f.root, path), "unexpected\n")
				reject(t, f, "2.0.0", "foo", report, "extra generated or modified registry path: "+path, false)
			})
		}
	})
	t.Run("invalid-report", func(t *testing.T) {
		for _, kind := range []string{"unknown", "nested-unknown", "schema", "number", "escape", "extra-path", "missing", "noncanonical"} {
			t.Run(kind, func(t *testing.T) {
				f, reportPath := fixture(t)
				seedReport(t, f, reportPath)
				report, data := readRegistryReport(t, reportPath)
				want := "invalid registry-sync-operation.v1 report bindings"
				switch kind {
				case "unknown":
					report["unknown"] = true
					want = `json: unknown field "unknown"`
				case "nested-unknown":
					report["release"].(map[string]any)["unknown"] = true
					want = `json: unknown field "unknown"`
				case "schema":
					report["schema"] = "registry-sync-operation.v99"
				case "number":
					report["release"].(map[string]any)["id"] = 100
					want = "cannot unmarshal number"
				case "escape":
					report["changed_paths"] = []any{"../escape"}
				case "extra-path":
					report["changed_paths"] = []any{"README.md"}
				case "missing":
					delete(report, "source_sha")
				case "noncanonical":
					want = "report must contain exact number-free canonical JSON"
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
				mustWrite(t, reportPath, string(data))
				reject(t, f, "1.0.0", "foo", reportPath, want, false)
			})
		}
	})
}
