package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/GoCodeAlone/workflow/secrets"
)

func TestParseNameMappings(t *testing.T) {
	got, err := parseNameMappings([]string{"NAMECHEAP_API_KEY=GCA_NC_API_KEY", "CLOUDFLARE_ACCOUNT_ID=GCA_CF_ACCOUNT_ID"})
	if err != nil {
		t.Fatalf("parseNameMappings: %v", err)
	}
	want := map[string]string{
		"NAMECHEAP_API_KEY":     "GCA_NC_API_KEY",
		"CLOUDFLARE_ACCOUNT_ID": "GCA_CF_ACCOUNT_ID",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mappings = %+v, want %+v", got, want)
	}
	if _, err := parseNameMappings([]string{"NAMECHEAP_API_KEY"}); err == nil {
		t.Fatal("parseNameMappings accepted missing stored name")
	}
}

func TestApplyManifestNameMappingsUsesStoredNameForStatus(t *testing.T) {
	inputs := applyManifestNameMappings([]manifestDiscoveredSecret{{
		PluginRequiredSecret: PluginRequiredSecret{Name: "NAMECHEAP_API_KEY", Sensitive: true},
		Kind:                 envSetupInputSecret,
	}}, map[string]string{"NAMECHEAP_API_KEY": "GCA_NC_API_KEY"})

	provider := newEngineTestProvider(map[string]string{"GCA_NC_API_KEY": "set"})
	target := manifestSecretTargetProvider{Store: "github-org", Provider: provider}
	status := manifestTargetCheck(context.Background(), target, inputs[0])
	if !status.IsSet || status.Name != "NAMECHEAP_API_KEY" {
		t.Fatalf("status = %+v, want logical name set via mapped storage", status)
	}
	if provider.setCnt["NAMECHEAP_API_KEY"] != 0 {
		t.Fatal("logical name should not be used for provider checks")
	}
}

func TestManifestTargetSetRoutesSecretsAndVariables(t *testing.T) {
	provider := &mixedSetupProvider{
		engineTestProvider: newEngineTestProvider(nil),
		vars:               map[string]string{},
	}
	targets := []manifestSecretTarget{
		{
			Secret: manifestDiscoveredSecret{
				PluginRequiredSecret: PluginRequiredSecret{Name: "NAMECHEAP_API_KEY", Sensitive: true},
				Kind:                 envSetupInputSecret,
				StorageName:          "GCA_NC_API_KEY",
			},
			Provider: provider,
		},
		{
			Secret: manifestDiscoveredSecret{
				PluginRequiredSecret: PluginRequiredSecret{Name: "NAMECHEAP_CLIENT_IP"},
				Kind:                 envSetupInputVar,
				StorageName:          "GCA_NC_CLIENT_IP",
			},
			Provider: provider,
		},
	}
	report, err := runManifestSecretTargetSetup(context.Background(), targets, func(input manifestDiscoveredSecret) (string, bool, error) {
		return "value-" + input.Kind.String(), true, nil
	}, nil, true)
	if err != nil {
		t.Fatalf("runManifestSecretTargetSetup: %v", err)
	}
	if !reflect.DeepEqual(report.Set, []string{"NAMECHEAP_API_KEY -> GCA_NC_API_KEY", "NAMECHEAP_CLIENT_IP -> GCA_NC_CLIENT_IP"}) {
		t.Fatalf("report.Set = %v", report.Set)
	}
	if provider.data["GCA_NC_API_KEY"] != "value-secret" {
		t.Fatalf("secret stored under mapped name = %q", provider.data["GCA_NC_API_KEY"])
	}
	if provider.vars["GCA_NC_CLIENT_IP"] != "value-var" {
		t.Fatalf("var stored under mapped name = %q", provider.vars["GCA_NC_CLIENT_IP"])
	}
}

func TestManifestInputValueLookupPrefersStoredName(t *testing.T) {
	input := manifestDiscoveredSecret{
		PluginRequiredSecret: PluginRequiredSecret{Name: "NAMECHEAP_API_KEY"},
		Kind:                 envSetupInputSecret,
		StorageName:          "GCA_NC_API_KEY",
	}
	value, provided, err := manifestSecretValue(input, manifestSecretValueOptions{
		secretMap: map[string]string{
			"NAMECHEAP_API_KEY": "logical",
			"GCA_NC_API_KEY":    "stored",
		},
	})
	if err != nil {
		t.Fatalf("manifestSecretValue: %v", err)
	}
	if !provided || value != "stored" {
		t.Fatalf("value=%q provided=%v, want stored mapping value", value, provided)
	}
}

func TestPluginConfigValuePrefersMappedStoredName(t *testing.T) {
	t.Setenv("NAMECHEAP_API_USER", "logical")
	t.Setenv("GCA_NC_API_USER", "stored")
	value, provided, err := pluginConfigValue(PluginRequiredConfig{Name: "NAMECHEAP_API_USER"}, "GCA_NC_API_USER", nil, true, false)
	if err != nil {
		t.Fatalf("pluginConfigValue: %v", err)
	}
	if !provided || value != "stored" {
		t.Fatalf("value=%q provided=%v, want stored env value", value, provided)
	}

	value, provided, err = pluginConfigValue(PluginRequiredConfig{Name: "NAMECHEAP_API_USER"}, "GCA_NC_API_USER", map[string]string{
		"NAMECHEAP_API_USER": "literal-logical",
		"GCA_NC_API_USER":    "literal-stored",
	}, false, false)
	if err != nil {
		t.Fatalf("pluginConfigValue literal: %v", err)
	}
	if !provided || value != "literal-stored" {
		t.Fatalf("literal value=%q provided=%v, want stored literal", value, provided)
	}
}

type mixedSetupProvider struct {
	*engineTestProvider
	vars map[string]string
}

func (p *mixedSetupProvider) Name() string { return "github" }

func (p *mixedSetupProvider) SetVariable(_ context.Context, key, value string) error {
	p.vars[key] = value
	return nil
}

func (p *mixedSetupProvider) DeleteVariable(_ context.Context, key string) error {
	delete(p.vars, key)
	return nil
}

func (p *mixedSetupProvider) ListVariables(context.Context) ([]secrets.VariableMeta, error) {
	out := make([]secrets.VariableMeta, 0, len(p.vars))
	for name := range p.vars {
		out = append(out, secrets.VariableMeta{Name: name, Exists: true})
	}
	return out, nil
}

func (p *mixedSetupProvider) CheckVariable(_ context.Context, key string) (secrets.VariableMeta, error) {
	_, ok := p.vars[key]
	return secrets.VariableMeta{Name: key, Exists: ok}, nil
}
