package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldDockerfile_Generic(t *testing.T) {
	dir := t.TempDir()
	err := scaffoldDockerfile(scaffoldDockerfileArgs{
		mode:      "generic",
		outputDir: dir,
	})
	if err != nil {
		t.Fatalf("scaffoldDockerfile generic: %v", err)
	}

	out := filepath.Join(dir, "Dockerfile.prebuilt")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	content := string(data)

	// Must use distroless base
	if !strings.Contains(content, "distroless") {
		t.Error("generic Dockerfile should use distroless base image")
	}
	// Must have COPY workflow-server
	if !strings.Contains(content, "workflow-server") {
		t.Error("generic Dockerfile should reference workflow-server binary")
	}
	// Must have ENTRYPOINT or CMD
	if !strings.Contains(content, "ENTRYPOINT") && !strings.Contains(content, "CMD") {
		t.Error("generic Dockerfile should have ENTRYPOINT or CMD")
	}
}

func TestScaffoldDockerfile_Library(t *testing.T) {
	dir := t.TempDir()
	err := scaffoldDockerfile(scaffoldDockerfileArgs{
		mode:      "library",
		binary:    "bmw-server",
		outputDir: dir,
	})
	if err != nil {
		t.Fatalf("scaffoldDockerfile library: %v", err)
	}

	out := filepath.Join(dir, "Dockerfile.prebuilt")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "bmw-server") {
		t.Error("library Dockerfile should reference the --binary name")
	}
	if !strings.Contains(content, "distroless") {
		t.Error("library Dockerfile should use distroless base image")
	}
}

func TestScaffoldDockerfile_LibraryRequiresBinary(t *testing.T) {
	err := scaffoldDockerfile(scaffoldDockerfileArgs{
		mode:      "library",
		outputDir: t.TempDir(),
	})
	if err == nil {
		t.Error("library mode without --binary should return error")
	}
	if !strings.Contains(err.Error(), "binary") {
		t.Errorf("error should mention --binary, got: %v", err)
	}
}

func TestScaffoldDockerfile_BaseImageOverride(t *testing.T) {
	dir := t.TempDir()
	customBase := "gcr.io/distroless/static:nonroot"
	err := scaffoldDockerfile(scaffoldDockerfileArgs{
		mode:      "generic",
		baseImage: customBase,
		outputDir: dir,
	})
	if err != nil {
		t.Fatalf("scaffoldDockerfile with base-image: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "Dockerfile.prebuilt"))
	if !strings.Contains(string(data), customBase) {
		t.Errorf("custom base image %q not found in output", customBase)
	}
}

func TestScaffoldDockerfile_WarnOnAlpineBase(t *testing.T) {
	err := scaffoldDockerfile(scaffoldDockerfileArgs{
		mode:      "generic",
		baseImage: "alpine:3.20",
		outputDir: t.TempDir(),
	})
	// Should warn but NOT fail (alpine is just a warning)
	if err != nil {
		t.Errorf("alpine base should warn not fail, got error: %v", err)
	}
}

func TestScaffoldDockerfile_BlockShellBase(t *testing.T) {
	err := scaffoldDockerfile(scaffoldDockerfileArgs{
		mode:      "generic",
		baseImage: "ubuntu:22.04",
		outputDir: t.TempDir(),
	})
	// Should fail because shell-containing base without --allow-shell
	if err == nil {
		t.Error("ubuntu base should be blocked without --allow-shell")
	}
}

func TestScaffoldDockerfile_AllowShellOverride(t *testing.T) {
	dir := t.TempDir()
	err := scaffoldDockerfile(scaffoldDockerfileArgs{
		mode:       "generic",
		baseImage:  "ubuntu:22.04",
		allowShell: true,
		outputDir:  dir,
	})
	// Should succeed with --allow-shell
	if err != nil {
		t.Errorf("ubuntu base with --allow-shell should not fail: %v", err)
	}
}

func TestScaffoldDockerfile_DefaultMode(t *testing.T) {
	// Empty mode defaults to generic
	dir := t.TempDir()
	err := scaffoldDockerfile(scaffoldDockerfileArgs{
		mode:      "",
		outputDir: dir,
	})
	if err != nil {
		t.Fatalf("default mode: %v", err)
	}
	_, err = os.Stat(filepath.Join(dir, "Dockerfile.prebuilt"))
	if err != nil {
		t.Error("Dockerfile.prebuilt should be created")
	}
}

func TestScaffoldDockerfile_InvalidMode(t *testing.T) {
	err := scaffoldDockerfile(scaffoldDockerfileArgs{
		mode:      "bad-mode",
		outputDir: t.TempDir(),
	})
	if err == nil {
		t.Error("invalid mode should return error")
	}
}

func TestValidateBaseImage_FullyQualifiedRefs(t *testing.T) {
	// Fully-qualified and edge-case image refs tested via subtests.
	cases := []struct {
		image      string
		allowShell bool
		wantErr    bool
	}{
		// Fully-qualified ubuntu refs blocked same as short "ubuntu:22.04".
		{"docker.io/library/ubuntu:22.04", false, true},
		{"docker.io/library/ubuntu:22.04", true, false},
		{"ghcr.io/org/ubuntu:22.04", false, true},
		{"ghcr.io/org/ubuntu:22.04", true, false},
		// Alpine: warning only, no error.
		{"docker.io/library/alpine:3.20", false, false},
		// Distroless: always allowed.
		{"gcr.io/distroless/base-debian12:nonroot", false, false},
		// Digest-only form: base name resolves to "ubuntu" → blocked.
		{"ubuntu@sha256:aaaabbbbccccdddd0000111122223333", false, true},
		// Registry with port: base name resolves to "ubuntu" → blocked.
		{"registry.internal:5000/library/ubuntu:22.04", false, true},
		// Tag+digest combined: base name resolves to "ubuntu" → blocked.
		{"ubuntu:22.04@sha256:aaaabbbb11112222", false, true},
		// Empty string: no match against any blocked or warning base → no error, no panic.
		{"", false, false},
		// Busybox via fully-qualified path: shell-containing → blocked without --allow-shell.
		{"docker.io/library/busybox:1.36", false, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.image, func(t *testing.T) {
			t.Helper()
			err := validateBaseImage(tc.image, tc.allowShell)
			if tc.wantErr && err == nil {
				t.Errorf("validateBaseImage(%q, allowShell=%v): expected error, got nil", tc.image, tc.allowShell)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateBaseImage(%q, allowShell=%v): unexpected error: %v", tc.image, tc.allowShell, err)
			}
		})
	}
}
