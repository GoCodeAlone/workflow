package admin_test

import (
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin"
)

// TestAssetFS_AllExpectedFilesEmbedded pins the file list the host
// module serves via http.FileServerFS. If the //go:embed directive
// misses a file (typo in glob, file deleted, etc.) the host module's
// GET routes return 404 silently; this test catches the omission at
// build time.
//
// Per plan §Task 13 (T12 additions: actions.html, actions.js).
func TestAssetFS_AllExpectedFilesEmbedded(t *testing.T) {
	expected := []string{
		"ui_dist/resources.html",
		"ui_dist/resources.js",
		"ui_dist/resource.html",
		"ui_dist/resource.js",
		"ui_dist/new.html",
		"ui_dist/new.js",
		"ui_dist/styles.css",
		// T12: audit-viewer assets.
		"ui_dist/actions.html",
		"ui_dist/actions.js",
	}
	for _, path := range expected {
		t.Run(path, func(t *testing.T) {
			f, err := admin.AssetFS.Open(path)
			if err != nil {
				t.Fatalf("AssetFS.Open(%q): %v", path, err)
			}
			defer f.Close()
			stat, err := f.Stat()
			if err != nil {
				t.Fatalf("AssetFS.Stat(%q): %v", path, err)
			}
			if stat.Size() == 0 {
				t.Errorf("%s is empty — embed glob matched but the file is empty", path)
			}
		})
	}
}

// TestAssetFS_ListsAllAndOnlyExpected catches accidental inclusion
// of non-asset files (test fixtures, sourcemaps, .DS_Store) AND
// drift in the embed glob coverage.
func TestAssetFS_ListsAllAndOnlyExpected(t *testing.T) {
	var got []string
	err := fs.WalkDir(admin.AssetFS, "ui_dist", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		got = append(got, path)
		return nil
	})
	if err != nil {
		t.Fatalf("fs.WalkDir: %v", err)
	}
	// Every embedded file must be .html, .js, or .css — the embed
	// directive's glob shape. A future addition (e.g. .png assets)
	// requires updating BOTH the //go:embed line + this test.
	for _, path := range got {
		switch {
		case strings.HasSuffix(path, ".html"),
			strings.HasSuffix(path, ".js"),
			strings.HasSuffix(path, ".css"):
			// allowed
		default:
			t.Errorf("AssetFS contains non-html/js/css file %q — update //go:embed glob OR remove the file", path)
		}
	}
	if len(got) == 0 {
		t.Fatal("AssetFS empty — //go:embed glob did not match any files")
	}
}

// --- T13: mutation panel + audit-viewer content assertions ----------------

// readAsset is a test helper that reads the full content of an embedded
// asset as a string, failing the test on any error.
func readAsset(t *testing.T, path string) string {
	t.Helper()
	f, err := admin.AssetFS.Open(path)
	if err != nil {
		t.Fatalf("AssetFS.Open(%q): %v", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("io.ReadAll(%q): %v", path, err)
	}
	return string(data)
}

// TestResourceHTML_MutationPanelMarkup pins key mutation-panel markup
// elements in resource.html so a future accidental deletion is caught
// at build time rather than at Playwright runtime.
func TestResourceHTML_MutationPanelMarkup(t *testing.T) {
	content := readAsset(t, "ui_dist/resource.html")
	must := []struct {
		id   string
		what string
	}{
		{`id="mutations"`, "mutations section"},
		{`id="bearer-token"`, "bearer token input"},
		{`id="btn-plan"`, "Plan button"},
		{`id="btn-drift"`, "Check Drift button"},
		{`id="plan-result"`, "plan result panel"},
		{`id="plan-actions-table"`, "plan actions table"},
		{`id="apply-confirm"`, "Apply confirm checkbox"},
		{`id="btn-apply"`, "Apply button"},
		{`id="destroy-confirm"`, "Destroy confirm checkbox"},
		{`id="btn-destroy"`, "Destroy button"},
		{`id="drift-result"`, "drift result panel"},
		{`id="mutation-error"`, "mutation error div"},
	}
	for _, m := range must {
		if !strings.Contains(content, m.id) {
			t.Errorf("resource.html missing %s: expected to contain %q", m.what, m.id)
		}
	}
}

// TestResourceJS_MutationPanelEndpoints pins that resource.js references
// all four mutation endpoint paths and sends the Authorization header.
func TestResourceJS_MutationPanelEndpoints(t *testing.T) {
	content := readAsset(t, "ui_dist/resource.js")
	must := []string{
		`${API}/plan`,
		`${API}/apply`,
		`${API}/destroy`,
		`${API}/drift`,
		`Authorization`,
		`Bearer`,
		`PLAN_STATE`,
		`allow-replace-cb`,
	}
	for _, s := range must {
		if !strings.Contains(content, s) {
			t.Errorf("resource.js missing expected string %q", s)
		}
	}
}

// TestActionsHTML_AuditViewerMarkup pins key audit-viewer markup elements
// in actions.html.
func TestActionsHTML_AuditViewerMarkup(t *testing.T) {
	content := readAsset(t, "ui_dist/actions.html")
	must := []struct {
		id   string
		what string
	}{
		{`id="bearer-token"`, "bearer token input"},
		{`id="filter-action"`, "action filter select"},
		{`id="filter-result"`, "result filter select"},
		{`id="filter-limit"`, "limit filter select"},
		{`id="btn-refresh"`, "Refresh button"},
		{`id="auto-refresh"`, "auto-refresh checkbox"},
		{`id="audit-table"`, "audit table"},
		{`id="error"`, "error div"},
	}
	for _, m := range must {
		if !strings.Contains(content, m.id) {
			t.Errorf("actions.html missing %s: expected to contain %q", m.what, m.id)
		}
	}
	// result filter must offer the three canonical result values (no free-text).
	for _, v := range []string{`value="ok"`, `value="denied"`, `value="error"`} {
		if !strings.Contains(content, v) {
			t.Errorf("actions.html result filter missing option %q (selectable only)", v)
		}
	}
}

// TestActionsJS_AuditEndpoint pins that actions.js fetches the correct
// audit endpoint with Authorization header and handles ndjson parsing.
// setInterval is pinned to the fetchAndCache call specifically — the
// T12 bug was setInterval(fetchAudit,...) which would pass a bare
// "setInterval" check but leave the cache stale after auto-refresh.
func TestActionsJS_AuditEndpoint(t *testing.T) {
	content := readAsset(t, "ui_dist/actions.js")
	must := []string{
		`${API}/audit`,
		`Authorization`,
		`Bearer`,
		`parseNdjson`,
		`renderEntries`,
		`audit-ok`,
		`audit-denied`,
		`audit-error`,
		`setInterval(fetchAndCache,`, // pin the correct callee, not just presence
		`sessionStorage`,
	}
	for _, s := range must {
		if !strings.Contains(content, s) {
			t.Errorf("actions.js missing expected string %q", s)
		}
	}
}

// TestAssetPrefix_FilesAccessibleViaSubFS verifies that the asset files
// are reachable when the embed.FS is Sub'd to "ui_dist" — matching the
// http.FileServer pattern in module/infra_admin.go (fs.Sub strips the
// leading "ui_dist/" so a request for /admin/infra-admin/actions.html
// resolves to actions.html inside the sub-FS).
func TestAssetPrefix_FilesAccessibleViaSubFS(t *testing.T) {
	sub, err := fs.Sub(admin.AssetFS, "ui_dist")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	for _, name := range []string{
		"actions.html",
		"actions.js",
		"resource.html",
		"resource.js",
	} {
		f, err := sub.Open(name)
		if err != nil {
			t.Errorf("sub.Open(%q): %v — asset not reachable via FileServer path", name, err)
			continue
		}
		f.Close()
	}
}
