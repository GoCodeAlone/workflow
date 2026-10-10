package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/plugin"
)

func discoveryWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func discoveryPlugin(t *testing.T, root, directory, owner string, types ...string) {
	t.Helper()
	dir := filepath.Join(root, directory)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(plugin.PluginManifest{
		Name: directory, Version: "0.1.0",
		IaCProvider: plugin.IaCProviderCapability{Name: owner, ResourceTypes: types, ComputePlanVersion: "v2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	discoveryWrite(t, filepath.Join(dir, "plugin.json"), string(data))
}

func discoveryFixture(t *testing.T, resources string) (string, string) {
	t.Helper()
	root := t.TempDir()
	plugins := filepath.Join(root, "plugins")
	discoveryPlugin(t, plugins, "fixture-alpha", "alpha", "alpha.widget", "shared.widget")
	discoveryPlugin(t, plugins, "fixture-beta", "beta", "beta.widget", "shared.widget")
	prev := currentInfraPluginDir
	currentInfraPluginDir = plugins
	t.Cleanup(func() { currentInfraPluginDir = prev })
	path := filepath.Join(root, "infra.yaml")
	discoveryWrite(t, path, "modules:\n"+
		"  - name: provider-a\n    type: iac.provider\n    config: {provider: alpha}\n"+
		"  - name: provider-b\n    type: iac.provider\n    config: {provider: beta}\n"+resources)
	return path, plugins
}

const discoveryCustomResources = `  - name: custom-a
    type: alpha.widget
    config: {iac_provider: provider-a}
  - name: custom-b
    type: beta.widget
    config: {provider: provider-b}
`

func discoverySpecNames(specs []interfaces.ResourceSpec) []string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}

func TestIaCResourceDiscovery_Extraction(t *testing.T) {
	for _, tt := range []struct {
		name, resources string
		want            []string
	}{
		{"custom-only", discoveryCustomResources, []string{"custom-a", "custom-b"}},
		{"mixed", "  - name: canonical\n    type: infra.database\n    config: {provider: provider-a}\n" + discoveryCustomResources, []string{"canonical", "custom-a", "custom-b"}},
		{"ordinary-provider-key", "  - name: application\n    type: http.server\n    config: {provider: application-backend}\n", []string{}},
		{"shared-type-selected", "  - name: shared\n    type: shared.widget\n    config: {iac_provider: provider-b}\n", []string{"shared"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path, _ := discoveryFixture(t, tt.resources)
			for _, env := range []string{"", "fixture"} {
				specs, err := parseInfraResourceSpecsForEnv(path, env)
				if err != nil {
					t.Fatal(err)
				}
				if got := discoverySpecNames(specs); !reflect.DeepEqual(got, tt.want) {
					t.Errorf("env %q resource names = %v; want %v", env, got, tt.want)
				}
			}
		})
	}
}

func TestIaCResourceDiscovery_BindingDenials(t *testing.T) {
	for _, tt := range []struct{ name, typ, cfg string }{
		{"wrong-owner", "alpha.widget", "{provider: provider-b}"},
		{"missing-reference", "alpha.widget", "{}"},
		{"undeclared-reference", "alpha.widget", "{iac_provider: absent}"},
		{"unknown-explicit-type", "unknown.widget", "{iac_provider: provider-a}"},
		{"unknown-declared-provider-type", "unknown.widget", "{provider: provider-a}"},
		{"invalid-reference-type", "alpha.widget", "{iac_provider: [provider-a]}"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path, _ := discoveryFixture(t, "  - name: denied\n    type: "+tt.typ+"\n    config: "+tt.cfg+"\n")
			if _, err := parseInfraResourceSpecsForEnv(path, "fixture"); err == nil {
				t.Fatal("explicit custom IaC intent was silently omitted or accepted")
			}
			if _, err := collectInfraEnvVarRefs(path, "fixture"); err == nil {
				t.Fatal("input classification must return the same binding denial")
			}
		})
	}
}

func TestIaCResourceDiscovery_EnvironmentAndImports(t *testing.T) {
	path, _ := discoveryFixture(t, `  - name: disabled
    type: alpha.widget
    environments: {fixture: null}
  - name: canonical
    type: infra.database
    config: {provider: provider-a}
    environments:
      fixture:
        config: {name: canonical-fixture}
`)
	importPath := filepath.Join(filepath.Dir(path), "custom.yaml")
	discoveryWrite(t, importPath, `modules:
  - name: custom
    type: alpha.widget
    config:
      name: driver-name
      env_vars: {A: "${TASK26_A}"}
      env_vars_secret: {B: "$TASK26_B"}
      secret_env_vars: {C: "${TASK26_A}"}
    environments:
      fixture:
        config:
          name: driver-env-name
          env_vars: {D: "${TASK26_D}"}
`)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	discoveryWrite(t, path, "imports: [custom.yaml]\nenvironments:\n  fixture:\n    provider: provider-a\n    region: fixture-region\n    envVars: {DEFAULT_ONLY: '$TASK26_DEFAULT_ONLY'}\n"+string(data))
	specs, err := parseInfraResourceSpecsForEnv(path, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("resolved/imported resource count = %d; want 2", len(specs))
	}
	for _, spec := range specs {
		if spec.Type == "infra.database" && spec.Name != "canonical-fixture" {
			t.Errorf("non-container canonical name lift lost: %q", spec.Name)
		}
		if spec.Type == "alpha.widget" {
			if spec.Name != "custom" || spec.Config["name"] != "driver-env-name" {
				t.Errorf("custom driver config.name must not become resource identity: %+v", spec)
			}
			if resolveIaCProviderRef(spec.Config) != "provider-a" || spec.Config["region"] != "fixture-region" {
				t.Errorf("custom resource lost effective defaults: %+v", spec)
			}
			ev := spec.Config["env_vars"].(map[string]any)
			if ev["A"] != "${TASK26_A}" || ev["DEFAULT_ONLY"] != nil {
				t.Errorf("preserved refs/container-only defaults changed: %v", ev)
			}
		}
	}
	refs, err := collectInfraEnvVarRefs(path, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"TASK26_A", "TASK26_B", "TASK26_D"}; !reflect.DeepEqual(refs, want) {
		t.Errorf("raw custom input refs = %v; want %v", refs, want)
	}
	tmp, err := writeEnvResolvedConfig(path, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(tmp) })
	resolved, err := config.LoadFromFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, mod := range resolved.Modules {
		if mod.Type == "alpha.widget" && (mod.Config["provider"] != "provider-a" || mod.Config["region"] != "fixture-region" || mod.Config["name"] != "driver-env-name") {
			t.Errorf("CLI env writer disagrees with extraction: %+v", mod)
		}
	}
}

func TestIaCResourceDiscovery_ProviderOverrideAndDisabled(t *testing.T) {
	path, _ := discoveryFixture(t, `  - name: custom
    type: beta.widget
    config: {provider: provider-a}
    environments:
      fixture: {provider: provider-b}
`)
	specs, err := parseInfraResourceSpecsForEnv(path, "fixture")
	if err != nil || len(specs) != 1 {
		t.Fatalf("env provider override: specs=%v err=%v", specs, err)
	}
	path, _ = discoveryFixture(t, discoveryCustomResources)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	discoveryWrite(t, path, strings.Replace(string(data), "config: {provider: alpha}", "config: {provider: alpha}\n    environments: {fixture: null}", 1))
	if _, err := parseInfraResourceSpecsForEnv(path, "fixture"); err == nil {
		t.Fatal("active custom resource must not silently reference disabled provider")
	}
}

func TestIaCResourceDiscovery_RawOwnerConflict(t *testing.T) {
	path, plugins := discoveryFixture(t, "  - name: custom\n    type: beta.widget\n    config: {iac_provider: provider-a}\n")
	discoveryWrite(t, filepath.Join(plugins, "fixture-alpha", "plugin.json"), `{
  "name":"fixture-alpha","version":"0.1.0",
  "iacProvider":{"name":"alpha","resourceTypes":["alpha.widget"],"computePlanVersion":"v2"},
  "capabilities":{"iacProvider":{"name":"beta","resourceTypes":["beta.widget"]}}
}`)
	for _, owner := range []string{"alpha", "beta"} {
		if _, _, _, err := findIaCPluginDir(plugins, owner); err == nil {
			t.Errorf("raw unequal owners must fail before normalization for selection %q", owner)
		}
	}
	if _, err := parseInfraResourceSpecsForEnv(path, ""); err == nil {
		t.Fatal("raw beta types must not become alpha-owned through normalized merging")
	}
}

func TestIaCResourceDiscovery_ManifestLookup(t *testing.T) {
	t.Run("top-level-and-legacy", func(t *testing.T) {
		_, plugins := discoveryFixture(t, "")
		name, ver, _, err := findIaCPluginDir(plugins, "alpha")
		if err != nil || name != "fixture-alpha" || ver != "v2" {
			t.Fatalf("top-level minimal manifest lookup: %q %q %v", name, ver, err)
		}
		discoveryWrite(t, filepath.Join(plugins, "fixture-alpha", "plugin.json"), `{"capabilities":{"iacProvider":{"name":"alpha","resourceTypes":["alpha.widget"]}},"iacProvider":{"computePlanVersion":"v2"}}`)
		name, _, _, err = findIaCPluginDir(plugins, "alpha")
		if err != nil || name != "fixture-alpha" {
			t.Fatalf("legacy minimal manifest lookup: %q %v", name, err)
		}
	})
	t.Run("duplicate-owner", func(t *testing.T) {
		_, plugins := discoveryFixture(t, "")
		discoveryPlugin(t, plugins, "another-alpha", "alpha", "alpha.widget")
		if _, _, _, err := findIaCPluginDir(plugins, "alpha"); err == nil {
			t.Fatal("multiple installed owners must not select the first directory")
		}
	})
	t.Run("malformed-needed", func(t *testing.T) {
		path, plugins := discoveryFixture(t, discoveryCustomResources)
		discoveryWrite(t, filepath.Join(plugins, "fixture-alpha", "plugin.json"), `{"iacProvider":{"name":"alpha","resourceTypes":[41]}}`)
		if _, err := parseInfraResourceSpecsForEnv(path, ""); err == nil {
			t.Fatal("malformed needed metadata must not silently omit custom resources")
		}
	})
	t.Run("duplicate-provider-module", func(t *testing.T) {
		path, _ := discoveryFixture(t, "  - name: provider-a\n    type: iac.provider\n    config: {provider: beta}\n"+discoveryCustomResources)
		if _, err := parseInfraResourceSpecsForEnv(path, ""); err == nil {
			t.Fatal("duplicate provider identity must fail rather than overwrite")
		}
	})
	t.Run("missing-selected-owner", func(t *testing.T) {
		path, plugins := discoveryFixture(t, discoveryCustomResources)
		if err := os.Remove(filepath.Join(plugins, "fixture-alpha", "plugin.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := parseInfraResourceSpecs(path); err == nil || !strings.Contains(err.Error(), "no installed plugin") {
			t.Fatalf("missing selected declaration must not omit custom intent: %v", err)
		}
	})
	t.Run("malformed-no-payload", func(t *testing.T) {
		path, plugins := discoveryFixture(t, discoveryCustomResources)
		discoveryWrite(t, filepath.Join(plugins, "fixture-alpha", "plugin.json"), `{"iacProvider":{"name":"alpha","resourceTypes":"TASK26_PRIVATE_METADATA"}}`)
		_, err := parseInfraResourceSpecs(path)
		if err == nil || strings.Contains(err.Error(), "TASK26_PRIVATE_METADATA") {
			t.Fatalf("invalid metadata must fail without payload bytes: %v", err)
		}
	})
	t.Run("duplicate-types-are-not-ambiguous", func(t *testing.T) {
		path, plugins := discoveryFixture(t, discoveryCustomResources)
		discoveryPlugin(t, plugins, "fixture-alpha", "alpha", "alpha.widget", "alpha.widget")
		specs, err := parseInfraResourceSpecs(path)
		if err != nil || len(specs) != 2 {
			t.Fatalf("duplicate type strings normalize for classification: %v %v", specs, err)
		}
	})
}

func TestIaCResourceDiscovery_LegacyAndPrecedence(t *testing.T) {
	path, _ := discoveryFixture(t, "  - name: custom\n    type: alpha.widget\n    config: {iac_provider: provider-a, provider: implementation-name}\n")
	specs, err := parseInfraResourceSpecs(path)
	if err != nil || len(specs) != 1 || resolveIaCProviderRef(specs[0].Config) != "provider-a" {
		t.Fatalf("iac_provider precedence: specs=%v err=%v", specs, err)
	}
	path = filepath.Join(t.TempDir(), "legacy.yaml")
	discoveryWrite(t, path, "modules:\n  - {name: canonical, type: infra.database}\n  - {name: legacy, type: platform.kubernetes}\n  - {name: ordinary, type: http.server}\n")
	specs, err = parseInfraResourceSpecs(path)
	if err != nil || !reflect.DeepEqual(discoverySpecNames(specs), []string{"canonical", "legacy"}) {
		t.Fatalf("canonical no-provider compatibility: %v %v", specs, err)
	}
}

func TestIaCResourceDiscovery_InputRefs(t *testing.T) {
	path, _ := discoveryFixture(t, `  - name: custom
    type: alpha.widget
    config:
      iac_provider: provider-a
      env_vars: {A: "${TASK26_A}"}
      env_vars_secret: {B: "$TASK26_B"}
      secret_env_vars: {C: "${TASK26_A}"}
`)
	refs, err := collectInfraEnvVarRefs(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"TASK26_A", "TASK26_B"}; !reflect.DeepEqual(refs, want) {
		t.Fatalf("custom input refs = %v; want %v", refs, want)
	}
}

func TestIaCResourceDiscovery_EnvWriter(t *testing.T) {
	path, _ := discoveryFixture(t, "  - name: custom\n    type: alpha.widget\n    config: {iac_provider: provider-a}\n")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	discoveryWrite(t, path, "environments:\n  fixture: {provider: provider-a, region: local}\n"+string(data))
	tmp, err := writeEnvResolvedConfig(path, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(tmp) })
	cfg, err := config.LoadFromFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, mod := range cfg.Modules {
		if mod.Type == "alpha.widget" && mod.Config["region"] != "local" {
			t.Fatal("custom IaC defaults missing from CLI environment writer")
		}
	}
}

// R1: classification uses effective routing, but payloads and fingerprints
// must still come from the unexpanded configuration.
func TestIaCResourceDiscovery_R1EffectiveRouting(t *testing.T) {
	for _, tc := range []struct {
		name, routing, overrides, defaults string
		rawKey, rawValue                   string
		wantRefs                           []string
	}{
		{"iac-braced", `iac_provider: "${TASK26_R1_REF}"`, "", "", "iac_provider", "${TASK26_R1_REF}", []string{"TASK26_R1_PAYLOAD", "TASK26_R1_REF"}},
		{"iac-bare", `iac_provider: "$TASK26_R1_REF"`, "", "", "iac_provider", "$TASK26_R1_REF", []string{"TASK26_R1_PAYLOAD", "TASK26_R1_REF"}},
		{"provider-braced", `provider: "${TASK26_R1_REF}"`, "", "", "provider", "${TASK26_R1_REF}", []string{"TASK26_R1_PAYLOAD", "TASK26_R1_REF"}},
		{"provider-bare", `provider: "$TASK26_R1_REF"`, "", "", "provider", "$TASK26_R1_REF", []string{"TASK26_R1_PAYLOAD", "TASK26_R1_REF"}},
		{"iac-precedence", `iac_provider: "${TASK26_R1_REF}", provider: provider-b`, "", "", "iac_provider", "${TASK26_R1_REF}", []string{"TASK26_R1_PAYLOAD", "TASK26_R1_REF"}},
		{"env-iac-override", "iac_provider: provider-b", "    environments:\n      fixture:\n        config: {iac_provider: \"$TASK26_R1_REF\"}\n", "", "iac_provider", "$TASK26_R1_REF", []string{"TASK26_R1_PAYLOAD", "TASK26_R1_REF"}},
		{"env-provider-override", "provider: provider-b", "    environments:\n      fixture: {provider: \"${TASK26_R1_REF}\"}\n", "", "provider", "${TASK26_R1_REF}", []string{"TASK26_R1_PAYLOAD", "TASK26_R1_REF"}},
		{"default-braced", "", "", "environments:\n  fixture: {provider: \"${TASK26_R1_REF}\", region: local}\n", "provider", "${TASK26_R1_REF}", []string{"TASK26_R1_PAYLOAD"}},
		{"default-bare", "", "", "environments:\n  fixture: {provider: \"$TASK26_R1_REF\", region: local}\n", "provider", "$TASK26_R1_REF", []string{"TASK26_R1_PAYLOAD"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TASK26_R1_REF", "provider-a")
			t.Setenv("TASK26_R1_TYPE", "alpha")
			t.Setenv("TASK26_R1_PAYLOAD", "task26-routing-private-canary")
			path, _ := discoveryFixture(t, "")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			header := strings.Replace(string(data), "provider: alpha", `provider: "${TASK26_R1_TYPE}"`, 1)
			discoveryWrite(t, path, tc.defaults+"imports: [resource.yaml]\n"+header)
			routing := tc.routing
			if routing != "" {
				routing += ", "
			}
			discoveryWrite(t, filepath.Join(filepath.Dir(path), "resource.yaml"), "modules:\n  - name: routed\n    type: alpha.widget\n    config: {"+routing+`env_vars: {A: "${TASK26_R1_PAYLOAD}"}, env_vars_secret: {B: "$TASK26_R1_PAYLOAD"}, secret_env_vars: {C: "${TASK26_R1_PAYLOAD}"}}`+"\n"+tc.overrides)
			for _, entry := range []string{"extraction", "writer", "inputs"} {
				t.Run(entry, func(t *testing.T) {
					switch entry {
					case "extraction":
						specs, err := parseInfraResourceSpecsForEnv(path, "fixture")
						if err != nil || len(specs) != 1 {
							t.Fatalf("effective routing must extract imported resource: count=%d err=%v", len(specs), err)
						}
						if resolveIaCProviderRef(specs[0].Config) != "provider-a" || specs[0].Config["env_vars"].(map[string]any)["A"] != "${TASK26_R1_PAYLOAD}" {
							t.Fatal("effective routing or preserved payload differs")
						}
					case "writer":
						tmp, err := writeEnvResolvedConfig(path, "fixture")
						if err != nil {
							t.Fatalf("effective routing must allow env writer: %v", err)
						}
						defer os.Remove(tmp)
						cfg, err := config.LoadFromFile(tmp)
						if err != nil {
							t.Fatal(err)
						}
						found := false
						for _, mod := range cfg.Modules {
							if mod.Name == "routed" {
								found = true
								if mod.Config[tc.rawKey] != tc.rawValue || mod.Config["env_vars"].(map[string]any)["A"] != "${TASK26_R1_PAYLOAD}" {
									t.Fatal("classification expanded raw writer configuration")
								}
							}
						}
						if !found {
							t.Fatal("writer omitted imported custom resource")
						}
					case "inputs":
						refs, err := collectInfraEnvVarRefs(path, "fixture")
						if err != nil || !reflect.DeepEqual(refs, tc.wantRefs) {
							t.Fatalf("raw routing/payload fingerprints: refs=%v err=%v", refs, err)
						}
						// Defaults retain the existing raw-module-only input scan.
						snapshot, err := computeInfraInputSnapshot(path, "fixture")
						if err != nil || len(snapshot) != len(tc.wantRefs) || snapshot["TASK26_R1_PAYLOAD"] == "" {
							t.Fatalf("raw input snapshot: %v", err)
						}
						encoded, err := json.Marshal(snapshot)
						if err != nil || strings.Contains(string(encoded), "task26-routing-private-canary") {
							t.Fatal("snapshot contains payload instead of fingerprint")
						}
					}
				})
			}
		})
	}
}

// R2: metadata that cannot claim the needed owner is not valid authority and
// must not veto unrelated modules or another valid selected owner.
func TestIaCResourceDiscovery_R2UnneededMetadata(t *testing.T) {
	for _, damage := range []string{"malformed", "unreadable", "other-owner-invalid-types", "unneeded-duplicate-beta"} {
		t.Run(damage, func(t *testing.T) {
			path, plugins := discoveryFixture(t, "")
			dir := filepath.Join(plugins, "unrelated")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(dir, "plugin.json")
			switch damage {
			case "malformed":
				discoveryWrite(t, manifest, "{broken")
			case "unreadable":
				if err := os.Mkdir(manifest, 0o700); err != nil {
					t.Fatal(err)
				}
			case "other-owner-invalid-types":
				discoveryWrite(t, manifest, `{"iacProvider":{"name":"unrelated","resourceTypes":[42]}}`)
			case "unneeded-duplicate-beta":
				discoveryPlugin(t, plugins, "duplicate-beta", "beta", "beta.widget")
			}
			for _, selected := range []bool{false, true} {
				t.Run(fmt.Sprintf("selected=%t", selected), func(t *testing.T) {
					resources := "modules:\n  - {name: canonical, type: infra.database}\n  - {name: application, type: http.server, config: {provider: app-backend}}\n"
					want := []string{"canonical"}
					if selected {
						resources = "modules:\n  - {name: provider-a, type: iac.provider, config: {provider: alpha}}\n  - {name: custom, type: alpha.widget, config: {iac_provider: provider-a}}\n  - {name: application, type: http.server}\n"
						want = []string{"custom"}
					}
					discoveryWrite(t, path, resources)
					for _, entry := range []string{"extraction", "writer", "inputs", "lookup"} {
						t.Run(entry, func(t *testing.T) {
							switch entry {
							case "extraction":
								for _, env := range []string{"", "fixture"} {
									specs, err := parseInfraResourceSpecsForEnv(path, env)
									if err != nil || !reflect.DeepEqual(discoverySpecNames(specs), want) {
										t.Fatalf("unneeded metadata vetoed resources: count=%d err=%v", len(specs), err)
									}
								}
							case "writer":
								tmp, err := writeEnvResolvedConfig(path, "fixture")
								if err != nil {
									t.Fatalf("unneeded metadata vetoed writer: %v", err)
								}
								defer os.Remove(tmp)
							case "inputs":
								if _, err := collectInfraEnvVarRefs(path, "fixture"); err != nil {
									t.Fatalf("unneeded metadata vetoed inputs: %v", err)
								}
							case "lookup":
								name, _, _, err := findIaCPluginDir(plugins, "alpha")
								if err != nil || name != "fixture-alpha" {
									t.Fatalf("unneeded metadata vetoed valid alpha lookup: name=%q err=%v", name, err)
								}
							}
						})
					}
				})
			}
		})
	}
}

func TestIaCResourceDiscovery_R2NeededDamage(t *testing.T) {
	for _, damage := range []string{"selected-types", "duplicate-valid", "duplicate-invalid", "invalid-top-known-legacy", "raw-owner-conflict", "unreadable-needed", "malformed-needed", "missing-default-owner"} {
		t.Run(damage, func(t *testing.T) {
			path, plugins := discoveryFixture(t, "  - {name: custom, type: alpha.widget, config: {iac_provider: provider-a}}\n")
			selected := filepath.Join(plugins, "fixture-alpha", "plugin.json")
			switch damage {
			case "selected-types":
				discoveryWrite(t, selected, `{"iacProvider":{"name":"alpha","resourceTypes":[42]}}`)
			case "duplicate-valid":
				discoveryPlugin(t, plugins, "duplicate", "alpha", "alpha.widget")
			case "duplicate-invalid":
				discoveryPlugin(t, plugins, "duplicate", "alpha", "alpha.widget")
				discoveryWrite(t, filepath.Join(plugins, "duplicate", "plugin.json"), `{"iacProvider":{"name":"alpha","resourceTypes":[42]}}`)
			case "invalid-top-known-legacy":
				discoveryPlugin(t, plugins, "duplicate", "alpha", "alpha.widget")
				discoveryWrite(t, filepath.Join(plugins, "duplicate", "plugin.json"), `{"iacProvider":{"name":42},"capabilities":{"iacProvider":{"name":"alpha","resourceTypes":["alpha.widget"]}}}`)
			case "raw-owner-conflict":
				discoveryWrite(t, selected, `{"iacProvider":{"name":"beta","resourceTypes":["beta.widget"]},"capabilities":{"iacProvider":{"name":"alpha","resourceTypes":["alpha.widget"]}}}`)
			case "unreadable-needed":
				if err := os.Remove(selected); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(selected, 0o700); err != nil {
					t.Fatal(err)
				}
			case "malformed-needed":
				discoveryWrite(t, selected, "{broken")
			case "missing-default-owner":
				if err := os.Remove(selected); err != nil {
					t.Fatal(err)
				}
				discoveryWrite(t, path, "environments:\n  fixture: {provider: provider-a}\nmodules:\n  - {name: provider-a, type: iac.provider, config: {provider: alpha}}\n  - {name: custom, type: alpha.widget}\n")
			}
			if _, err := parseInfraResourceSpecsForEnv(path, "fixture"); err == nil {
				t.Fatal("needed metadata damage permitted extraction")
			}
			if _, _, _, err := findIaCPluginDir(plugins, "alpha"); err == nil && damage != "missing-default-owner" {
				t.Fatal("needed metadata damage permitted executable selection")
			}
		})
	}
}

func TestIaCResourceDiscovery_R2InvalidTypesCannotAuthorize(t *testing.T) {
	path, plugins := discoveryFixture(t, "  - {name: custom, type: malformed.widget, config: {iac_provider: provider-a}}\n")
	discoveryPlugin(t, plugins, "unrelated", "unrelated", "malformed.widget")
	discoveryWrite(t, filepath.Join(plugins, "unrelated", "plugin.json"), `{"iacProvider":{"name":"unrelated","resourceTypes":["malformed.widget",42]}}`)
	_, err := parseInfraResourceSpecs(path)
	if err == nil || !strings.Contains(err.Error(), "not declared by selected provider") {
		t.Fatalf("invalid unrelated types are not valid selected-owner authority: %v", err)
	}
}
