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
