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
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// These fixtures model the catalog contract, not real release checksums. Mixed
// releases deliberately place standalone binaries before the plugin archives.
type catalogRoleFixture struct {
	root, manifest, catalog, repo, family string
	assets                                []releaseAsset
	atlasVersion                          string
	atlasName                             string
	missingAtlas                          bool
	mutateSibling                         bool
	apiURL                                string
	handler                               http.Handler
	mu                                    sync.Mutex
	requests                              []string
	releaseReads                          int
}

func newCatalogRoleFixture(t *testing.T, catalog string) *catalogRoleFixture {
	t.Helper()
	restoreRegistrySyncTestHooks(t)
	f := &catalogRoleFixture{root: t.TempDir(), catalog: catalog, atlasVersion: "0.4.1", atlasName: "workflow-plugin-atlas-migrate"}
	f.repo, f.family = "GoCodeAlone/workflow-plugin-migrations", catalog
	families := []string{"db-migrate", "workflow-plugin-migrations", "workflow-plugin-atlas-migrate"}
	if catalog == "github" {
		f.repo, f.family = "GoCodeAlone/workflow-plugin-github", "workflow-plugin-github"
		families = []string{"github", "workflow-plugin-github"}
	}
	for _, family := range families {
		for _, goos := range []string{"darwin", "linux", "windows"} {
			if family == "workflow-plugin-atlas-migrate" && goos == "windows" {
				continue
			}
			for _, arch := range []string{"amd64", "arm64"} {
				name := family + "-" + goos + "-" + arch + ".tar.gz"
				f.assets = append(f.assets, releaseAsset{Name: name, OS: goos, Arch: arch,
					URL:    "https://github.com/" + f.repo + "/releases/download/v0.4.1/" + name,
					SHA256: fmt.Sprintf("%064x", len(f.assets)+1)})
			}
		}
	}
	f.manifest = filepath.Join(f.root, "plugins", catalog, "manifest.json")
	f.writeManifest(t, "0.4.1", f.selected())
	f.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, r.URL.RequestURI())
		prefix := "/repos/" + f.repo
		var out any
		switch {
		case r.URL.Path == prefix+"/releases/latest":
			out = map[string]any{"tag_name": "v0.4.1"}
		case strings.HasPrefix(r.URL.Path, prefix+"/releases/tags/"):
			f.releaseReads++
			assets := []any{map[string]any{"id": 100, "name": "checksums.txt", "size": 42, "browser_download_url": f.apiURL + "/checksums"}}
			for i, asset := range f.assets {
				id := 200 + i
				if f.mutateSibling && f.releaseReads > 1 && i == 0 {
					id += 1000
				}
				assets = append(assets, map[string]any{"id": id, "name": asset.Name, "size": 42,
					"browser_download_url": asset.URL, "digest": "sha256:" + asset.SHA256})
			}
			out = map[string]any{"id": 1, "tag_name": strings.TrimPrefix(r.URL.Path, prefix+"/releases/tags/"),
				"draft": false, "prerelease": false, "immutable": true, "assets": assets}
		case strings.HasPrefix(r.URL.Path, prefix+"/git/ref/tags/"):
			out = map[string]any{"object": map[string]any{"type": "commit", "sha": strings.Repeat("a", 40)}}
		case r.URL.Path == "/checksums":
			for _, asset := range f.assets {
				fmt.Fprintf(w, "%s  %s\n", asset.SHA256, asset.Name)
			}
			return
		case r.URL.Path == prefix+"/contents/plugin.json" || r.URL.Path == prefix+"/contents/plugin.atlas.json":
			if r.URL.Query().Get("ref") != strings.Repeat("a", 40) {
				http.Error(w, "source must be pinned to the tag commit", http.StatusBadRequest)
				return
			}
			atlas := strings.HasSuffix(r.URL.Path, "/plugin.atlas.json")
			if atlas && f.missingAtlas {
				http.NotFound(w, r)
				return
			}
			out = map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString(f.metadata(atlas))}
		default:
			http.NotFound(w, r)
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

	// Exercise the actual legacy contents-path selection without invoking a real
	// GitHub CLI or contacting GitHub. Every unsupported command fails closed.
	bin := t.TempDir()
	script := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  'api repos/'*'/contents/plugin.atlas.json?ref='*) [ \"$CATALOG_TEST_MISSING_ATLAS\" != 1 ] || exit 1; printf '%s\\n' \"$CATALOG_TEST_ATLAS\" ;;\n" +
		"  'api repos/'*'/contents/plugin.json?ref='*) printf '%s\\n' \"$CATALOG_TEST_MAIN\" ;;\n" +
		"  *) exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CATALOG_TEST_MAIN", base64.StdEncoding.EncodeToString(f.metadata(false)))
	t.Setenv("CATALOG_TEST_ATLAS", base64.StdEncoding.EncodeToString(f.metadata(true)))
	t.Setenv("CATALOG_TEST_MISSING_ATLAS", "0")
	return f
}

func (f *catalogRoleFixture) metadata(atlas bool) []byte {
	if atlas {
		return []byte(fmt.Sprintf(`{"name":%q,"version":%q,"type":"external","minEngineVersion":"0.80.24","capabilities":{"moduleTypes":["database.migrations","database.migration_driver"],"migrationDrivers":["atlas"],"stepTypes":[],"cliCommands":[],"triggerTypes":[],"buildHooks":[],"configProvider":false}}`, f.atlasName, f.atlasVersion))
	}
	if f.catalog == "github" {
		return []byte(`{"name":"workflow-plugin-github","version":"0.4.1","type":"external","capabilities":{"moduleTypes":["github.app"],"stepTypes":["step.gh_pr_create"]}}`)
	}
	return []byte(`{"name":"workflow-plugin-migrations","version":"0.4.1","type":"external","minEngineVersion":"0.80.24","capabilities":{"moduleTypes":["database.migrations","database.migration_driver"],"migrationDrivers":["golang-migrate","goose"],"stepTypes":["step.migrate_up"],"cliCommands":[{"name":"db-migrate"}],"triggerTypes":[],"buildHooks":[],"configProvider":false}}`)
}

func (f *catalogRoleFixture) selected() []releaseAsset {
	var selected []releaseAsset
	for _, asset := range f.assets {
		if assetBinaryName(asset.Name) == f.family {
			selected = append(selected, asset)
		}
	}
	return selected
}

func (f *catalogRoleFixture) writeManifest(t *testing.T, version string, assets []releaseAsset) {
	t.Helper()
	var metadata map[string]any
	if err := json.Unmarshal(f.metadata(f.catalog == "workflow-plugin-atlas-migrate"), &metadata); err != nil {
		t.Fatal(err)
	}
	raw := map[string]any{"name": f.catalog, "version": version, "type": "external", "repository": "https://github.com/" + f.repo,
		"description": "preserved", "capabilities": metadata["capabilities"]}
	if value, ok := metadata["minEngineVersion"]; ok {
		raw["minEngineVersion"] = value
	}
	downloads := []any{}
	for _, asset := range assets {
		downloads = append(downloads, map[string]any{"os": asset.OS, "arch": asset.Arch, "url": strings.ReplaceAll(asset.URL, "/v0.4.1/", "/v"+version+"/"), "sha256": asset.SHA256})
	}
	raw["downloads"] = downloads
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, f.manifest, string(data)+"\n")
}

func (f *catalogRoleFixture) assertSelectedManifest(t *testing.T) {
	t.Helper()
	raw, err := readJSONFile(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	downloads, _ := raw["downloads"].([]any)
	want := map[string]releaseAsset{}
	for _, asset := range f.selected() {
		want[asset.URL] = asset
	}
	if len(downloads) != len(want) {
		t.Errorf("%s selected %d downloads, want only %d full-plugin archives", f.catalog, len(downloads), len(want))
	}
	for _, value := range downloads {
		entry, _ := value.(map[string]any)
		url, _ := entry["url"].(string)
		asset, ok := want[url]
		if !ok {
			t.Errorf("unexpected sibling/standalone download %s", url)
			continue
		}
		if entry["sha256"] != asset.SHA256 || entry["os"] != asset.OS || entry["arch"] != asset.Arch {
			t.Errorf("selected archive lost platform/checksum identity: %v", entry)
		}
		delete(want, url)
	}
	if len(want) != 0 {
		t.Errorf("missing selected archives: %v", want)
	}
	var metadata map[string]any
	if err := json.Unmarshal(f.metadata(f.catalog == "workflow-plugin-atlas-migrate"), &metadata); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw["capabilities"], metadata["capabilities"]) {
		t.Errorf("capabilities came from the wrong release role: got %v, want %v", raw["capabilities"], metadata["capabilities"])
	}
	if raw["description"] != "preserved" {
		t.Error("unrelated manifest metadata changed")
	}
}

func TestPluginRegistrySync_CatalogRoles_ApplyFix(t *testing.T) {
	for _, catalog := range []string{"github", "workflow-plugin-migrations", "workflow-plugin-atlas-migrate"} {
		t.Run(catalog, func(t *testing.T) {
			f := newCatalogRoleFixture(t, catalog)
			raw, err := readJSONFile(f.manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := applyFix(f.manifest, raw, f.repo, "v0.4.1", "0.4.1"); err != nil {
				t.Fatal(err)
			}
			f.assertSelectedManifest(t)
		})
	}
}

func TestPluginRegistrySync_CatalogRoles_SameVersionDrift(t *testing.T) {
	f := newCatalogRoleFixture(t, "github")
	f.writeManifest(t, "0.4.1", f.assets)
	before, _ := os.ReadFile(f.manifest)
	if err := syncDefault(f.root, false, f.catalog, false); err == nil {
		t.Error("same-version standalone downloads were incorrectly reported as current")
	}
	if after, _ := os.ReadFile(f.manifest); !bytes.Equal(before, after) {
		t.Fatal("check-only sync changed manifest bytes")
	}
	if err := syncDefault(f.root, true, f.catalog, false); err != nil {
		t.Fatal(err)
	}
	f.assertSelectedManifest(t)
}

func TestPluginRegistrySync_CatalogRoles_RejectInvalidFamilyWithoutWrites(t *testing.T) {
	for _, scenario := range []string{"missing-family", "missing-platform", "duplicate-platform", "missing-atlas-metadata"} {
		t.Run(scenario, func(t *testing.T) {
			f := newCatalogRoleFixture(t, "workflow-plugin-atlas-migrate")
			f.writeManifest(t, "0.4.0", f.selected())
			switch scenario {
			case "missing-family":
				var assets []releaseAsset
				for _, asset := range f.assets {
					if assetBinaryName(asset.Name) != f.family {
						assets = append(assets, asset)
					}
				}
				f.assets = assets
			case "missing-platform":
				f.assets = f.assets[:len(f.assets)-1]
			case "duplicate-platform":
				f.assets = append(f.assets, f.selected()[0])
			case "missing-atlas-metadata":
				f.missingAtlas = true
				t.Setenv("CATALOG_TEST_MISSING_ATLAS", "1")
			}
			before, _ := os.ReadFile(f.manifest)
			if err := syncDefault(f.root, true, f.catalog, false); err == nil {
				t.Errorf("%s must return an actionable error from fix mode", scenario)
			}
			if after, _ := os.ReadFile(f.manifest); !bytes.Equal(before, after) {
				t.Errorf("%s changed manifest bytes despite invalid role evidence", scenario)
			}
		})
	}
}

func TestPluginRegistrySync_CatalogRoles_VerifyCapabilities(t *testing.T) {
	for _, catalog := range []string{"github", "workflow-plugin-migrations", "workflow-plugin-atlas-migrate"} {
		t.Run(catalog, func(t *testing.T) {
			f := newCatalogRoleFixture(t, catalog)
			if runtime.GOOS == "windows" && catalog == "workflow-plugin-atlas-migrate" {
				t.Skip("Atlas has no Windows release role")
			}
			var downloaded string
			registrySyncDownloadReleaseAsset = func(_, _, name, dir string) (string, error) {
				downloaded = name
				path := filepath.Join(dir, assetBinaryName(name))
				return path, os.WriteFile(path, []byte("synthetic executable; verification is stubbed"), 0o755)
			}
			verified := false
			registrySyncVerifyManifest = func(_, manifest string, opts manifestCompareOptions) error {
				verified = true
				if manifest != f.manifest || !opts.SkipName {
					t.Error("capability verification contract changed")
				}
				return nil
			}
			if err := verifyRegistryPluginCapabilities(catalog, f.manifest, f.repo, "v0.4.1"); err != nil {
				t.Fatal(err)
			}
			want := f.family + "-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
			if !verified || downloaded != want {
				t.Errorf("verified=%v archive=%s, want selected role %s", verified, downloaded, want)
			}
		})
	}
}

func TestRegistrySyncTarget_CatalogRoles_SelectPinnedMetadata(t *testing.T) {
	for _, catalog := range []string{"github", "workflow-plugin-migrations", "workflow-plugin-atlas-migrate"} {
		t.Run(catalog, func(t *testing.T) {
			f := newCatalogRoleFixture(t, catalog)
			f.writeManifest(t, "0.4.0", f.selected())
			if err := syncRegistryTarget(f.root, catalog, "v0.4.1", true, false); err != nil {
				t.Fatalf("valid mixed release must select one full-plugin family: %v", err)
			}
			f.assertSelectedManifest(t)
			filename := "plugin.json"
			if catalog == "workflow-plugin-atlas-migrate" {
				filename = "plugin.atlas.json"
			}
			f.mu.Lock()
			requests := strings.Join(f.requests, "\n")
			f.mu.Unlock()
			if !strings.Contains(requests, "/contents/"+filename+"?ref="+strings.Repeat("a", 40)) {
				t.Errorf("selected metadata was not read at pinned source commit: %s", requests)
			}
		})
	}
}

func TestRegistrySyncTarget_CatalogRoles_PreserveSourceVersionGate(t *testing.T) {
	f := newCatalogRoleFixture(t, "workflow-plugin-atlas-migrate")
	// The real v0.4.1 Atlas source currently retains this placeholder. Role
	// selection must not silently relax strict target source-version policy.
	f.atlasVersion = "0.0.0"
	before, _ := os.ReadFile(f.manifest)
	err := syncRegistryTarget(f.root, f.catalog, "v0.4.1", true, false)
	if err == nil || !strings.Contains(err.Error(), "version does not match target release") {
		t.Errorf("want pinned Atlas source-version rejection, got %v", err)
	}
	if after, _ := os.ReadFile(f.manifest); !bytes.Equal(before, after) {
		t.Fatal("source-version rejection changed manifest bytes")
	}
}

func TestRegistrySyncTarget_CatalogRoles_PreserveWholeReleaseBinding(t *testing.T) {
	f := newCatalogRoleFixture(t, "workflow-plugin-atlas-migrate")
	f.mutateSibling = true
	before, _ := os.ReadFile(f.manifest)
	err := syncRegistryTarget(f.root, f.catalog, "v0.4.1", true, false)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("want mutation rejection for nonselected sibling asset identity, got %v", err)
	}
	if after, _ := os.ReadFile(f.manifest); !bytes.Equal(before, after) {
		t.Fatal("release mutation rejection changed manifest bytes")
	}
}

func TestPluginRegistrySync_CatalogRoles_CanonicalCatalogOverridesMutableName(t *testing.T) {
	f := newCatalogRoleFixture(t, "workflow-plugin-atlas-migrate")
	raw, err := readJSONFile(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	// A stale display name must not redirect this catalog to its sibling role.
	raw["name"] = "workflow-plugin-migrations"
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, f.manifest, string(data)+"\n")
	if err := applyFix(f.manifest, raw, f.repo, "v0.4.1", "0.4.1"); err != nil {
		t.Fatal(err)
	}
	f.assertSelectedManifest(t)
}

func TestPluginRegistrySync_CatalogRoles_RejectWrongMetadataIdentity(t *testing.T) {
	for _, mode := range []string{"default", "target"} {
		t.Run(mode, func(t *testing.T) {
			f := newCatalogRoleFixture(t, "workflow-plugin-atlas-migrate")
			f.atlasName = "workflow-plugin-migrations"
			t.Setenv("CATALOG_TEST_ATLAS", base64.StdEncoding.EncodeToString(f.metadata(true)))
			before, _ := os.ReadFile(f.manifest)
			var err error
			if mode == "default" {
				err = syncDefault(f.root, true, f.catalog, false)
			} else {
				err = syncRegistryTarget(f.root, f.catalog, "v0.4.1", true, false)
			}
			// Default mode reports each actionable cause on stderr and returns
			// an aggregate error; target mode returns the identity error itself.
			if err == nil || (mode == "target" && !strings.Contains(err.Error(), "name")) {
				t.Errorf("want selected source name mismatch error, got %v", err)
			}
			if after, _ := os.ReadFile(f.manifest); !bytes.Equal(before, after) {
				t.Fatal("wrong metadata identity changed manifest bytes")
			}
		})
	}
}

func TestPluginRegistrySync_CatalogRoles_VerifyFailureIsNotSwallowedByFix(t *testing.T) {
	f := newCatalogRoleFixture(t, "workflow-plugin-atlas-migrate")
	// The existing manifest is valid and already at the latest version. Only
	// capability verification discovers that the release lost a required role
	// archive, so --fix must propagate that failure even without a planned edit.
	f.assets = f.assets[:len(f.assets)-1]
	registrySyncDownloadReleaseAsset = func(_, _, _, _ string) (string, error) {
		t.Fatal("incomplete role must be rejected before downloading any binary")
		return "", nil
	}
	before, _ := os.ReadFile(f.manifest)
	if err := syncDefault(f.root, true, f.catalog, true); err == nil {
		t.Error("fix mode swallowed capability verification's incomplete-role error")
	}
	if after, _ := os.ReadFile(f.manifest); !bytes.Equal(before, after) {
		t.Fatal("capability verification failure changed manifest bytes")
	}
}

func TestPluginRegistrySync_CatalogRoles_ActualDefaultCLI(t *testing.T) {
	f := newCatalogRoleFixture(t, "workflow-plugin-atlas-migrate")
	binary := registrySyncActualCLIBinary(t, runtime.GOOS, f.apiURL)
	env := registrySyncHostEnvironment(os.Environ())
	var proxy *registrySyncReleaseProxy
	if runtime.GOOS == "linux" {
		proxy = newRegistrySyncReleaseProxy(t, f.handler)
		env = append(env, "HTTPS_PROXY="+proxy.url, "NO_PROXY=127.0.0.1,localhost", "SSL_CERT_FILE="+proxy.caFile, "SSL_CERT_DIR="+proxy.caDirectory)
	}
	cmd := exec.CommandContext(t.Context(), binary, "plugin", "registry-sync", "--registry-dir", f.root, "--plugin", f.catalog, "--fix")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("actual default CLI failed: %v\n%s", err, out)
	}
	f.assertSelectedManifest(t)
	if proxy != nil && (proxy.connects.Load() == 0 || proxy.requests.Load() == 0) {
		t.Fatal("actual Linux CLI bypassed the credential-free default API fixture")
	}
}
