package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type registrySyncTargetFixture struct {
	root     string
	manifest string
	mutate   func(string, int, map[string]any) string
	latest   atomic.Int32
	apiURL   string
	handler  http.Handler
}

func newRegistrySyncTargetFixture(t *testing.T) *registrySyncTargetFixture {
	t.Helper()
	f := &registrySyncTargetFixture{root: t.TempDir()}
	f.manifest = filepath.Join(f.root, "plugins", "foo", "manifest.json")
	mustWrite(t, f.manifest, `{"name":"workflow-plugin-foo","version":"1.0.0","repository":"https://github.com/owner/repo","type":"external","description":"preserved"}`+"\n")
	counts := make(map[string]int)
	var countsMu sync.Mutex
	f.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		countsMu.Lock()
		counts[r.URL.Path]++
		n := counts[r.URL.Path]
		countsMu.Unlock()
		var out map[string]any
		switch {
		case r.URL.Path == "/repos/owner/repo/releases/latest":
			f.latest.Add(1)
			out = map[string]any{"tag_name": "v2.0.0"}
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/releases/tags/"):
			tag := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/releases/tags/")
			out = map[string]any{
				"id": 100, "tag_name": tag, "target_commitish": strings.Repeat("a", 40),
				"draft": false, "prerelease": false, "immutable": true,
				"assets": []any{
					map[string]any{"id": 200, "name": "checksums.txt", "size": 105, "browser_download_url": f.apiURL + "/checksums/" + tag},
					map[string]any{"id": 201, "name": "workflow-plugin-foo-linux-amd64.tar.gz", "size": 42, "browser_download_url": f.apiURL + "/assets/" + tag + "/workflow-plugin-foo-linux-amd64.tar.gz"},
				},
			}
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/ref/tags/"):
			out = map[string]any{"object": map[string]any{"type": "tag", "sha": strings.Repeat("b", 40)}}
		case r.URL.Path == "/repos/owner/repo/git/tags/"+strings.Repeat("b", 40):
			out = map[string]any{"sha": strings.Repeat("b", 40), "object": map[string]any{"type": "commit", "sha": strings.Repeat("a", 40)}}
		case strings.HasPrefix(r.URL.Path, "/checksums/"):
			out = map[string]any{}
		case r.URL.Path == "/repos/owner/repo/contents/plugin.json":
			if r.URL.Query().Get("ref") != strings.Repeat("a", 40) {
				http.NotFound(w, r)
				return
			}
			out = map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(`{"name":"workflow-plugin-foo","type":"external","capabilities":{"services":["foo"]}}`))}
		default:
			http.NotFound(w, r)
			return
		}
		if f.mutate != nil {
			if body := f.mutate(r.URL.Path, n, out); body != "" {
				fmt.Fprint(w, body)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/checksums/") {
			fmt.Fprintln(w, strings.Repeat("c", 64)+"  workflow-plugin-foo-linux-amd64.tar.gz")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(out); err != nil {
			t.Error(err)
		}
	})
	srv := httptest.NewServer(f.handler)
	t.Cleanup(srv.Close)
	f.apiURL = srv.URL
	oldURL, oldClient := gitHubAPIBaseURL, gitHubAPIClient
	gitHubAPIBaseURL, gitHubAPIClient = srv.URL, srv.Client()
	t.Cleanup(func() { gitHubAPIBaseURL, gitHubAPIClient = oldURL, oldClient })
	return f
}

func (f *registrySyncTargetFixture) run(target string, extra ...string) error {
	args := []string{"--registry-dir", f.root, "--plugin", "foo", "--target-version", target, "--fix"}
	return runPluginRegistrySync(append(args, extra...))
}

func TestPluginRegistrySyncTarget_ForwardNoopRollback(t *testing.T) {
	f := newRegistrySyncTargetFixture(t)
	previous := map[string][]byte{}
	for _, target := range []string{"2.0.0", "2.0.0", "v1.0.0", "1.0.0"} {
		beforeInfo, err := os.Stat(f.manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.run(target); err != nil {
			t.Fatalf("target %s: %v", target, err)
		}
		after, err := os.ReadFile(f.manifest)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(after, &got); err != nil {
			t.Fatal(err)
		}
		if got["version"] != strings.TrimPrefix(target, "v") || got["description"] != "preserved" {
			t.Fatalf("manifest = %s", after)
		}
		version := strings.TrimPrefix(target, "v")
		if prior := previous[version]; prior != nil {
			if !bytes.Equal(prior, after) {
				t.Fatal("repeated sync changed manifest bytes")
			}
			afterInfo, err := os.Stat(f.manifest)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(beforeInfo, afterInfo) {
				t.Fatal("no-op replaced manifest instead of preserving it")
			}
		}
		previous[version] = after
	}
	if f.latest.Load() != 0 {
		t.Fatal("explicit target consulted the floating latest release")
	}
}

func TestPluginRegistrySyncTarget_ActualCLI(t *testing.T) {
	f := newRegistrySyncTargetFixture(t)
	commitRegistryReportFixture(t, f)
	reportPath := filepath.Join(f.root, ".github", "registry-operations", "operation.json")
	binary := registrySyncActualCLIBinary(t, runtime.GOOS, f.apiURL)
	env := registrySyncHostEnvironment(os.Environ())
	var proxy *registrySyncReleaseProxy
	if runtime.GOOS == "linux" {
		proxy = newRegistrySyncReleaseProxy(t, f.handler)
		env = append(env, "HTTPS_PROXY="+proxy.url, "NO_PROXY=127.0.0.1,localhost", "SSL_CERT_FILE="+proxy.caFile, "SSL_CERT_DIR="+proxy.caDirectory)
	}
	oldVersion := "1.0.0"
	var priorNoop []byte
	for _, version := range []string{"2.0.0", "2.0.0", "2.0.0", "1.0.0", "1.0.0", "1.0.0"} {
		base := registryReportFixtureTree(t, f, reportPath)
		cmd := exec.CommandContext(t.Context(), binary, "plugin", "registry-sync", "--registry-dir", f.root, "--plugin", "foo", "--target-version", version, "--fix", "--report", reportPath)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("actual CLI target %s: %v\n%s", version, err, out)
		}
		data, err := os.ReadFile(f.manifest)
		if err != nil {
			t.Fatal(err)
		}
		var manifest map[string]any
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		if manifest["version"] != version {
			t.Fatalf("CLI result = %s", data)
		}
		changed := oldVersion != version
		report := assertRegistryReportBinding(t, f, reportPath, base, oldVersion, version, changed)
		if !changed {
			if priorNoop != nil && !bytes.Equal(priorNoop, report) {
				t.Fatal("actual CLI repeated no-op report changed bytes")
			}
			priorNoop = report
		} else {
			priorNoop = nil
		}
		oldVersion = version
	}
	if f.latest.Load() != 0 {
		t.Fatal("actual CLI used floating latest for an exact target")
	}
	if proxy != nil && (proxy.connects.Load() == 0 || proxy.requests.Load() == 0) {
		t.Fatal("actual Linux CLI bypassed the default-api.github.com CONNECT/TLS fixture")
	}
}

func TestPluginRegistrySyncTarget_RejectsMutationBeforeWrite(t *testing.T) {
	for _, kind := range []string{"tag", "source", "release", "asset", "checksum", "metadata"} {
		t.Run(kind, func(t *testing.T) {
			f := newRegistrySyncTargetFixture(t)
			before, err := os.ReadFile(f.manifest)
			if err != nil {
				t.Fatal(err)
			}
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
					return strings.Repeat("e", 64) + "  workflow-plugin-foo-linux-amd64.tar.gz\n"
				case kind == "metadata" && strings.HasSuffix(path, "/plugin.json"):
					out["content"] = base64.StdEncoding.EncodeToString([]byte(`{"type":"external","capabilities":{"services":["changed"]}}`))
				}
				return ""
			}
			if err := f.run("2.0.0"); err == nil || !strings.Contains(err.Error(), "changed") {
				t.Fatalf("expected mutation rejection, got %v", err)
			}
			after, err := os.ReadFile(f.manifest)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("failed sync mutated manifest")
			}
		})
	}
}

func TestPluginRegistrySyncTarget_RejectsInvalidSelection(t *testing.T) {
	for _, target := range []string{"", "latest", "1.2", "1.2.3-rc.1", "1.2.3+build", "01.2.3", "../1.2.3"} {
		t.Run(target, func(t *testing.T) {
			f := newRegistrySyncTargetFixture(t)
			if err := f.run(target); err == nil || !strings.Contains(err.Error(), "stable") {
				t.Fatalf("expected stable-version rejection, got %v", err)
			}
		})
	}
}

func TestPluginRegistrySyncTarget_RejectsPluginEscape(t *testing.T) {
	f := newRegistrySyncTargetFixture(t)
	if err := f.run("2.0.0", "--plugin", "../foo"); err == nil || !strings.Contains(err.Error(), "plugin") || strings.Contains(err.Error(), "flag provided") {
		t.Fatalf("expected plugin-path rejection, got %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(f.root, "plugins", "escape")); err != nil {
		t.Fatal(err)
	}
	if err := f.run("2.0.0", "--plugin", "escape"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func TestPluginRegistrySyncTarget_PreservesExactManifestNumbers(t *testing.T) {
	f := newRegistrySyncTargetFixture(t)
	mustWrite(t, f.manifest, `{"name":"workflow-plugin-foo","version":"1.0.0","repository":"https://github.com/owner/repo","type":"external","opaque_counter":9007199254740993}`)
	if err := f.run("2.0.0"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"opaque_counter": 9007199254740993`)) {
		t.Fatalf("sync changed an unrelated exact JSON integer: %s", data)
	}
}

func TestPluginRegistrySyncTarget_DryRunDoesNotWrite(t *testing.T) {
	f := newRegistrySyncTargetFixture(t)
	before, err := os.ReadFile(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	err = runPluginRegistrySync([]string{"--registry-dir", f.root, "--plugin", "foo", "--target-version", "2.0.0"})
	if err == nil || !strings.Contains(err.Error(), "--fix") {
		t.Fatalf("dry-run should report drift, got %v", err)
	}
	after, err := os.ReadFile(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("dry-run wrote manifest")
	}
}
