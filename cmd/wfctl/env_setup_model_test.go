package main

import (
	"context"
	"reflect"
	"strings"
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
	if _, err := parseNameMappings([]string{"A=B", "A=C"}); err == nil {
		t.Fatal("parseNameMappings accepted duplicate logical name")
	}
	if _, err := parseNameMappings([]string{"A=C", "B=C"}); err == nil {
		t.Fatal("parseNameMappings accepted stored-name collision")
	}
}

func TestApplyManifestNameMappingsUsesStoredNameForStatus(t *testing.T) {
	inputs, err := applyManifestNameMappings([]manifestDiscoveredSecret{{
		PluginRequiredSecret: PluginRequiredSecret{Name: "NAMECHEAP_API_KEY", Sensitive: true},
		Kind:                 envSetupInputSecret,
	}}, map[string]string{"NAMECHEAP_API_KEY": "GCA_NC_API_KEY"})
	if err != nil {
		t.Fatalf("applyManifestNameMappings: %v", err)
	}

	provider := newEngineTestProvider(map[string]string{"GCA_NC_API_KEY": "set"})
	target := manifestSecretTargetProvider{Store: "github-org", Provider: provider}
	status := manifestTargetCheck(context.Background(), target, inputs[0])
	if !status.IsSet || status.Name != "NAMECHEAP_API_KEY" {
		t.Fatalf("status = %+v, want logical name set via mapped storage", status)
	}
	if provider.checkCnt["GCA_NC_API_KEY"] != 1 {
		t.Fatalf("provider Check(GCA_NC_API_KEY) called %d times, want 1", provider.checkCnt["GCA_NC_API_KEY"])
	}
	if provider.checkCnt["NAMECHEAP_API_KEY"] != 0 {
		t.Fatal("logical name should not be used for provider checks")
	}
}

func TestApplyManifestNameMappingsRejectsDiscoveredInputCollision(t *testing.T) {
	_, err := applyManifestNameMappings([]manifestDiscoveredSecret{
		{
			PluginRequiredSecret: PluginRequiredSecret{Name: "NAMECHEAP_API_KEY", Sensitive: true},
			Kind:                 envSetupInputSecret,
		},
		{
			PluginRequiredSecret: PluginRequiredSecret{Name: "GCA_NC_API_KEY", Sensitive: true},
			Kind:                 envSetupInputSecret,
		},
	}, map[string]string{"NAMECHEAP_API_KEY": "GCA_NC_API_KEY"})
	if err == nil {
		t.Fatal("applyManifestNameMappings accepted storage-name collision with discovered logical input")
	}
	if !strings.Contains(err.Error(), "GCA_NC_API_KEY") {
		t.Fatalf("error = %q, want colliding storage name", err)
	}
}

func TestManifestSecretTargetAllowedRequiresVariableProviderForVars(t *testing.T) {
	input := manifestDiscoveredSecret{
		PluginRequiredSecret: PluginRequiredSecret{Name: "NAMECHEAP_CLIENT_IP"},
		Kind:                 envSetupInputVar,
	}
	secretOnly := manifestSecretTargetProvider{
		Store:    "github:repo",
		Provider: newEngineTestProvider(nil),
	}
	if manifestSecretTargetAllowed(input, secretOnly) {
		t.Fatal("var input should be disallowed when provider lacks VariableProvider support")
	}
	withVars := manifestSecretTargetProvider{
		Store: "github:repo",
		Provider: &mixedSetupProvider{
			engineTestProvider: newEngineTestProvider(nil),
			vars:               map[string]string{},
		},
	}
	if !manifestSecretTargetAllowed(input, withVars) {
		t.Fatal("var input should be allowed when provider supports VariableProvider")
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
