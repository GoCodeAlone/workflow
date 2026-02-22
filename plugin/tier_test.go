package plugin

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/schema"
)

// -- tier constant tests --

func TestPluginTierConstants(t *testing.T) {
	if TierCore != "core" {
		t.Errorf("TierCore = %q, want %q", TierCore, "core")
	}
	if TierCommunity != "community" {
		t.Errorf("TierCommunity = %q, want %q", TierCommunity, "community")
	}
	if TierPremium != "premium" {
		t.Errorf("TierPremium = %q, want %q", TierPremium, "premium")
	}
}

func TestPluginTierString(t *testing.T) {
	tier := TierCore
	if string(tier) != "core" {
		t.Errorf("string(TierCore) = %q, want %q", string(tier), "core")
	}
}

// -- tier validation tests --

// mockLicenseValidator is a test double for LicenseValidator.
type mockLicenseValidator struct {
	allowed map[string]bool
}

func (m *mockLicenseValidator) ValidatePlugin(name string) error {
	if m.allowed[name] {
		return nil
	}
	return errors.New("license not valid for plugin: " + name)
}

func newTierLoader() *PluginLoader {
	return NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
}

func makeTierPlugin(name string, tier PluginTier) *BaseEnginePlugin {
	return &BaseEnginePlugin{
		BaseNativePlugin: BaseNativePlugin{
			PluginName:        name,
			PluginVersion:     "1.0.0",
			PluginDescription: "tier test plugin " + name,
		},
		Manifest: PluginManifest{
			Name:        name,
			Version:     "1.0.0",
			Author:      "test",
			Description: "tier test plugin " + name,
			Tier:        tier,
		},
	}
}

func TestValidateTier_CoreAlwaysAllowed(t *testing.T) {
	l := newTierLoader()
	m := &PluginManifest{
		Name: "core-plugin", Version: "1.0.0", Author: "a", Description: "d",
		Tier: TierCore,
	}
	if err := l.ValidateTier(m); err != nil {
		t.Errorf("core tier should always be allowed, got error: %v", err)
	}
}

func TestValidateTier_CommunityAlwaysAllowed(t *testing.T) {
	l := newTierLoader()
	m := &PluginManifest{
		Name: "community-plugin", Version: "1.0.0", Author: "a", Description: "d",
		Tier: TierCommunity,
	}
	if err := l.ValidateTier(m); err != nil {
		t.Errorf("community tier should always be allowed, got error: %v", err)
	}
}

func TestValidateTier_EmptyTierAllowed(t *testing.T) {
	l := newTierLoader()
	m := &PluginManifest{
		Name: "no-tier-plugin", Version: "1.0.0", Author: "a", Description: "d",
	}
	if err := l.ValidateTier(m); err != nil {
		t.Errorf("empty tier should be allowed, got error: %v", err)
	}
}

func TestValidateTier_PremiumNoValidator_AllowedWithWarning(t *testing.T) {
	// No license validator set — premium plugins should be allowed with a warning
	// (graceful degradation for self-hosted).
	l := newTierLoader()
	m := &PluginManifest{
		Name: "premium-plugin", Version: "1.0.0", Author: "a", Description: "d",
		Tier: TierPremium,
	}
	if err := l.ValidateTier(m); err != nil {
		t.Errorf("premium tier without validator should be allowed (graceful degradation), got error: %v", err)
	}
}

func TestValidateTier_PremiumWithValidatorApproved(t *testing.T) {
	l := newTierLoader()
	l.SetLicenseValidator(&mockLicenseValidator{allowed: map[string]bool{"premium-ok": true}})
	m := &PluginManifest{
		Name: "premium-ok", Version: "1.0.0", Author: "a", Description: "d",
		Tier: TierPremium,
	}
	if err := l.ValidateTier(m); err != nil {
		t.Errorf("premium tier with approval should succeed, got error: %v", err)
	}
}

func TestValidateTier_PremiumWithValidatorDenied(t *testing.T) {
	l := newTierLoader()
	l.SetLicenseValidator(&mockLicenseValidator{allowed: map[string]bool{}})
	m := &PluginManifest{
		Name: "premium-denied", Version: "1.0.0", Author: "a", Description: "d",
		Tier: TierPremium,
	}
	err := l.ValidateTier(m)
	if err == nil {
		t.Error("premium tier with denied license should return error")
	}
	if !strings.Contains(err.Error(), "premium-denied") {
		t.Errorf("error should mention plugin name, got: %v", err)
	}
}

func TestValidateTier_UnknownTierRejected(t *testing.T) {
	l := newTierLoader()
	m := &PluginManifest{
		Name: "weird-plugin", Version: "1.0.0", Author: "a", Description: "d",
		Tier: PluginTier("enterprise"),
	}
	if err := l.ValidateTier(m); err == nil {
		t.Error("unknown tier should return error")
	}
}

func TestLoadPlugin_TierValidationCalledOnLoad(t *testing.T) {
	l := newTierLoader()
	l.SetLicenseValidator(&mockLicenseValidator{allowed: map[string]bool{}})

	p := makeTierPlugin("premium-blocked", TierPremium)
	err := l.LoadPlugin(p)
	if err == nil {
		t.Error("LoadPlugin should fail for unlicensed premium plugin")
	}
	if !strings.Contains(err.Error(), "license") {
		t.Errorf("error should mention license, got: %v", err)
	}
}

func TestLoadPlugin_CoreTierAlwaysLoads(t *testing.T) {
	l := newTierLoader()
	// Even with a restrictive validator, core plugins should load.
	l.SetLicenseValidator(&mockLicenseValidator{allowed: map[string]bool{}})

	p := makeTierPlugin("core-plugin", TierCore)
	if err := l.LoadPlugin(p); err != nil {
		t.Errorf("core tier plugin should load regardless of validator, got error: %v", err)
	}
}

// -- manifest serialization tests --

func TestPluginManifest_TierSerializesInJSON(t *testing.T) {
	m := &PluginManifest{
		Name:        "test-plugin",
		Version:     "1.0.0",
		Author:      "Author",
		Description: "A plugin with tier",
		Tier:        TierCore,
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if !strings.Contains(string(data), `"tier":"core"`) {
		t.Errorf("expected tier field in JSON, got: %s", string(data))
	}
}

func TestPluginManifest_TierRoundTrip(t *testing.T) {
	for _, tier := range []PluginTier{TierCore, TierCommunity, TierPremium} {
		t.Run(string(tier), func(t *testing.T) {
			m := &PluginManifest{
				Name:        "tier-plugin",
				Version:     "1.0.0",
				Author:      "Author",
				Description: "Tier test",
				Tier:        tier,
			}
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}
			var loaded PluginManifest
			if err := json.Unmarshal(data, &loaded); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if loaded.Tier != tier {
				t.Errorf("Tier = %q, want %q", loaded.Tier, tier)
			}
		})
	}
}

func TestPluginManifest_TierOmittedWhenEmpty(t *testing.T) {
	m := &PluginManifest{
		Name:        "no-tier-plugin",
		Version:     "1.0.0",
		Author:      "Author",
		Description: "No tier",
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if strings.Contains(string(data), `"tier"`) {
		t.Errorf("tier field should be omitted when empty, got: %s", string(data))
	}
}
