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
  - name: missing-sources
    type: config.provider
    config:
      schema:
        not_provider_var:
          env: MISSING_SOURCES
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

func TestCollectConfigVariablesFromTopLevelVars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deploy.yaml")
	if err := os.WriteFile(path, []byte(`
vars:
  entries:
    - name: PUBLIC_CLIENT_ID
      description: OAuth public client ID.
      required: true
    - name: OPTIONAL_FLAG
      default: "false"
variables:
  entries:
    - name: FACEBOOK_APP_ID
      description: Facebook public app ID.
    - name: PUBLIC_CLIENT_ID
      description: Duplicate alias entry should not win.
modules:
  - name: app-config
    type: config.provider
    config:
      sources:
        - type: env
      schema:
        google_client_id:
          env: PUBLIC_CLIENT_ID
          desc: Schema duplicate should not win over explicit vars entry.
        secret:
          env: PRIVATE_SECRET
          sensitive: true
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	vars, skipped, err := collectConfigVariablesFromFile(path)
	if err != nil {
		t.Fatalf("collectConfigVariablesFromFile: %v", err)
	}
	byName := map[string]configVariableEntry{}
	for _, entry := range vars {
		byName[entry.Name] = entry
	}
	for _, name := range []string{"FACEBOOK_APP_ID", "OPTIONAL_FLAG", "PUBLIC_CLIENT_ID"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("missing variable %s in %+v", name, vars)
		}
	}
	if got := byName["PUBLIC_CLIENT_ID"]; !got.Required || got.Key != "vars.PUBLIC_CLIENT_ID" || got.Description != "OAuth public client ID." {
		t.Fatalf("PUBLIC_CLIENT_ID = %+v", got)
	}
	if got := byName["OPTIONAL_FLAG"]; got.Default != "false" {
		t.Fatalf("OPTIONAL_FLAG = %+v", got)
	}
	if len(skipped) != 1 || skipped[0] != "PRIVATE_SECRET" {
		t.Fatalf("skipped = %+v, want PRIVATE_SECRET", skipped)
	}
}

func TestCollectConfigVariablesErrorsOnMalformedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.yaml")
	if err := os.WriteFile(path, []byte(`
modules:
  - name: bad-config
    type: config.provider
    config:
      sources:
        - type: env
      schema:
        bad_entry: not-a-map
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, err := collectConfigVariablesFromFile(path)
	if err == nil {
		t.Fatal("expected malformed schema error")
	}
}

func TestValuesFromFlagsAndReaderTrimsAndSkipsBlankKeys(t *testing.T) {
	values, err := valuesFromFlagsAndReader([]string{" FLAG = literal "}, bytes.NewBufferString(`
 =ignored
 READER = value
NO_EQUALS
`))
	if err != nil {
		t.Fatalf("valuesFromFlagsAndReader: %v", err)
	}
	if values["FLAG"] != " literal " {
		t.Fatalf("FLAG = %q", values["FLAG"])
	}
	if values["READER"] != "value" {
		t.Fatalf("READER = %q", values["READER"])
	}
	if _, ok := values[""]; ok {
		t.Fatal("blank key must be skipped")
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
