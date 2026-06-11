package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectConfigVariablesFromConfigProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.yaml")
	if err := os.WriteFile(path, []byte(`
modules:
  - name: app-config
    type: config.provider
    config:
      sources:
        - type: defaults
        - type: env
          prefix: APP_
      schema:
        public_client_id:
          env: CLIENT_ID
          required: true
          desc: Public OAuth client ID.
        private_client_secret:
          env: CLIENT_SECRET
          sensitive: true
          desc: OAuth client secret.
        internal_default:
          env: INTERNAL_DEFAULT
          default: local
          desc: Has a local default.
        no_env:
          required: true
          desc: Not sourced from env.
  - name: defaults-only
    type: config.provider
    config:
      sources:
        - type: defaults
      schema:
        not_provider_var:
          env: DEFAULT_ONLY
          default: value
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	vars, skipped, err := collectConfigVariablesFromFile(path)
	if err != nil {
		t.Fatalf("collectConfigVariablesFromFile: %v", err)
	}
	if len(vars) != 2 {
		t.Fatalf("vars = %+v, want 2 non-sensitive env-backed entries", vars)
	}
	if vars[0].Name != "APP_CLIENT_ID" || vars[0].Key != "app-config.public_client_id" || !vars[0].Required {
		t.Fatalf("vars[0] = %+v", vars[0])
	}
	if vars[1].Name != "APP_INTERNAL_DEFAULT" || vars[1].Default != "local" {
		t.Fatalf("vars[1] = %+v", vars[1])
	}
	if len(skipped) != 1 || skipped[0] != "APP_CLIENT_SECRET" {
		t.Fatalf("skipped = %+v, want APP_CLIENT_SECRET", skipped)
	}
}

func TestRunVarsSetupConfigOnlySensitiveSkipsWithoutProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.yaml")
	if err := os.WriteFile(path, []byte(`
modules:
  - name: app-config
    type: config.provider
    config:
      sources:
        - type: env
      schema:
        api_token:
          env: API_TOKEN
          sensitive: true
          required: true
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	if err := runVarsSetupPluginWithIO([]string{
		"--config", path,
		"--non-interactive",
	}, nil, &out); err != nil {
		t.Fatalf("runVarsSetupPluginWithIO: %v", err)
	}
	if got := out.String(); got == "" || !bytes.Contains(out.Bytes(), []byte("declares no non-secret config variables")) {
		t.Fatalf("unexpected output: %q", got)
	}
}
