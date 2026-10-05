package integration_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLinkedVersionOverridesBuildInfo(t *testing.T) {
	t.Setenv("WFCTL_DIFFCACHE", "disabled")
	exeName := "wfctl"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	exe := filepath.Join(t.TempDir(), exeName)
	build := exec.Command("go", "build", "-o", exe, "-ldflags", "-X main.version=v9.9.9", ".")
	build.Dir = ".."
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build wfctl: %v\n%s", err, out)
	}

	run := exec.Command(exe, "--version")
	run.Dir = ".."
	run.Env = append(os.Environ(), "WFCTL_NO_UPDATE_CHECK=1", "CI=true")
	var stderr bytes.Buffer
	run.Stderr = &stderr
	out, err := run.Output()
	if err != nil {
		t.Fatalf("wfctl --version: %v\nstdout: %s\nstderr: %s", err, out, stderr.String())
	}
	if len(out) != 0 {
		t.Fatalf("wfctl --version unexpectedly wrote stdout: %s", out)
	}
	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	if len(lines) > 1 {
		t.Logf("wfctl --version diagnostics: %s", strings.Join(lines[:len(lines)-1], "\n"))
	}
	if got := lines[len(lines)-1]; got != "v9.9.9" {
		t.Fatalf("linked version = %q, want v9.9.9", got)
	}
}
