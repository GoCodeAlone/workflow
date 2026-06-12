package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const controlPlaneModulePath = "github.com/GoCodeAlone/workflow-plugin-control-plane"

func TestControlPlaneReleasedModuleFixture(t *testing.T) {
	moduleDir := controlPlaneReleasedModuleDir(t)

	goMod, err := os.ReadFile(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		t.Fatalf("read released module go.mod: %v", err)
	}
	if !strings.Contains(string(goMod), "module "+controlPlaneModulePath) {
		t.Fatalf("released module go.mod missing module path %q", controlPlaneModulePath)
	}
	for _, rel := range []string{
		"plugin.json",
		"plugin.contracts.json",
		"descriptorsets/control_plane.binpb",
	} {
		if _, err := os.Stat(filepath.Join(moduleDir, rel)); err != nil {
			t.Fatalf("released module missing %s: %v", rel, err)
		}
	}
}

func controlPlaneReleasedModuleDir(t *testing.T) string {
	t.Helper()

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", controlPlaneModulePath)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("resolve released control-plane module: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	moduleDir := strings.TrimSpace(string(out))
	if moduleDir == "" {
		t.Fatal("released control-plane module dir is empty")
	}
	return moduleDir
}
