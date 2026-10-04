package wfctlhelpers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func envClassifierFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "infra.yaml")
	data := `environments:
  fixture:
    provider: selected
    region: local
    envVars: {DEFAULT: "${TOP_ONLY}"}
modules:
  - name: canonical
    type: infra.database
    environments:
      fixture:
        config: {name: canonical-fixture}
  - name: container
    type: infra.container_service
  - name: custom
    type: example.widget
    config: {name: driver-name, env_vars: {VALUE: "${PRESERVED}"}}
    environments:
      fixture:
        config: {name: driver-fixture-name}
  - name: ordinary
    type: http.server
  - name: disabled
    type: example.widget
    environments: {fixture: null}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWriteEnvResolvedConfigWithClassifier(t *testing.T) {
	path := envClassifierFixture(t)
	calls := 0
	tmp, err := WriteEnvResolvedConfigWithClassifier(path, "fixture", func(mod *config.ResolvedModule) (bool, error) {
		calls++
		return IsInfraType(mod.Type) || mod.Type == "example.widget", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(tmp) })
	cfg, err := config.LoadFromFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 4 || len(cfg.Modules) != 4 {
		t.Fatalf("disabled resolution/callbacks: calls=%d modules=%d", calls, len(cfg.Modules))
	}
	for _, mod := range cfg.Modules {
		switch mod.Type {
		case "example.widget":
			if mod.Name != "custom" || mod.Config["name"] != "driver-fixture-name" {
				t.Errorf("custom config.name changed identity: %+v", mod)
			}
			if mod.Config["provider"] != "selected" || mod.Config["region"] != "local" {
				t.Errorf("classified defaults missing: %+v", mod)
			}
			ev := mod.Config["env_vars"].(map[string]any)
			if ev["VALUE"] != "${PRESERVED}" || ev["DEFAULT"] != nil {
				t.Errorf("custom refs or container-only defaults changed: %v", ev)
			}
		case "infra.database":
			if mod.Name != "canonical-fixture" || mod.Config["name"] != nil {
				t.Errorf("non-container infra-prefix name lift lost: %+v", mod)
			}
		case "infra.container_service":
			if mod.Config["env_vars"].(map[string]any)["DEFAULT"] != "${TOP_ONLY}" {
				t.Fatal("container defaults lost")
			}
		case "http.server":
			if mod.Config["provider"] != nil || mod.Config["region"] != nil {
				t.Fatal("ordinary module received IaC defaults")
			}
		}
	}
}

func TestWriteEnvResolvedConfig_LegacyClassifier(t *testing.T) {
	path := envClassifierFixture(t)
	tmp, err := WriteEnvResolvedConfig(path, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(tmp) })
	cfg, err := config.LoadFromFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, mod := range cfg.Modules {
		if mod.Type == "example.widget" && (mod.Config["provider"] != nil || mod.Config["region"] != nil) {
			t.Fatal("public legacy helper must not discover custom plugin types")
		}
	}
	if !IsInfraType("infra.database") || !IsInfraType("platform.kubernetes") || IsInfraType("example.widget") {
		t.Fatal("public prefix contract changed")
	}
}

func TestWriteEnvResolvedConfigWithClassifier_Error(t *testing.T) {
	path := envClassifierFixture(t)
	sentinel := errors.New("selected provider denied")
	tmp, err := WriteEnvResolvedConfigWithClassifier(path, "fixture", func(*config.ResolvedModule) (bool, error) {
		return false, sentinel
	})
	if tmp != "" || !errors.Is(err, sentinel) {
		t.Fatalf("callback failure must propagate without a temp config: path=%q err=%v", tmp, err)
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".wfctl-env-resolved-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("failed classification leaked temp files: %v %v", files, err)
	}
	if _, err := WriteEnvResolvedConfigWithClassifier(path, "fixture", nil); err == nil {
		t.Fatal("nil classifier must fail")
	}
}
