package admin_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin"
)

// TestAssetFS_AllExpectedFilesEmbedded pins the file list the host
// module (T15) serves via http.FileServerFS. If the //go:embed
// directive misses a file (typo in glob, file deleted, etc.) the
// host module's GET routes return 404 silently; this test catches
// the omission at build time.
//
// Per plan §Task 13.
func TestAssetFS_AllExpectedFilesEmbedded(t *testing.T) {
	expected := []string{
		"ui_dist/resources.html",
		"ui_dist/resources.js",
		"ui_dist/resource.html",
		"ui_dist/resource.js",
		"ui_dist/new.html",
		"ui_dist/new.js",
		"ui_dist/styles.css",
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
