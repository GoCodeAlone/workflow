package main

import (
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// captureResourceDriver extends fakeResourceDriver to record the full spec.Config
// passed to both Update and Create, so tests can assert on any field (not just image).
type captureResourceDriver struct {
	fakeResourceDriver
	updateCfg map[string]any
	createCfg map[string]any
}

func (d *captureResourceDriver) Update(_ context.Context, _ interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.updateCfg = spec.Config
	d.updateImage, _ = spec.Config["image"].(string)
	if d.updateErr != nil {
		return nil, d.updateErr
	}
	return &interfaces.ResourceOutput{}, nil
}

func (d *captureResourceDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.createCalled = true
	d.createCfg = spec.Config
	d.createSpec = spec
	if d.createErr != nil {
		return nil, d.createErr
	}
	return &interfaces.ResourceOutput{}, nil
}

// ── TestPluginDeployProvider_ProviderConfigExpanded ─────────────────────────────

func TestPluginDeployProvider_ProviderConfigExpanded(t *testing.T) {
	t.Setenv("TEST_DO_TOKEN", "tok_LIVE123")

	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "do-provider",
				Type: "iac.provider",
				Config: map[string]any{
					"provider": "digitalocean",
					"token":    "${TEST_DO_TOKEN}",
				},
			},
			{
				Name: "my-app",
				Type: "infra.container_service",
				Config: map[string]any{
					"provider": "do-provider",
					"region":   "nyc3",
				},
			},
		},
	}

	p, err := newPluginDeployProvider("digitalocean", wfCfg)
	if err != nil {
		t.Fatalf("newPluginDeployProvider: %v", err)
	}
	pdp, ok := p.(*pluginDeployProvider)
	if !ok {
		t.Fatalf("expected *pluginDeployProvider, got %T", p)
	}

	// The provider config stored on the struct should have the token expanded.
	got, _ := pdp.providerCfg["token"].(string)
	if got != "tok_LIVE123" {
		t.Errorf("providerCfg[token]: want %q, got %q", "tok_LIVE123", got)
	}

	// Original module config in wfCfg must NOT be mutated.
	orig, _ := wfCfg.Modules[0].Config["token"].(string)
	if orig != "${TEST_DO_TOKEN}" {
		t.Errorf("original wfCfg module config was mutated: got %q", orig)
	}
}

// ── TestPluginDeployProvider_ResourceConfigExpanded ─────────────────────────────

func TestPluginDeployProvider_ResourceConfigExpanded(t *testing.T) {
	t.Setenv("TEST_REGION", "sfo3")

	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "do-provider",
				Type: "iac.provider",
				Config: map[string]any{
					"provider": "digitalocean",
					"token":    "static-token",
				},
			},
			{
				Name: "my-app",
				Type: "infra.container_service",
				Config: map[string]any{
					"provider": "do-provider",
					"region":   "${TEST_REGION}",
				},
			},
		},
	}

	p, err := newPluginDeployProvider("digitalocean", wfCfg)
	if err != nil {
		t.Fatalf("newPluginDeployProvider: %v", err)
	}
	pdp := p.(*pluginDeployProvider)

	// Resource config stored on the struct should have region expanded.
	got, _ := pdp.resourceCfg["region"].(string)
	if got != "sfo3" {
		t.Errorf("resourceCfg[region]: want %q, got %q", "sfo3", got)
	}
}

// ── TestPluginDeployProvider_DeployPassesExpandedConfigToDriver ──────────────

func TestPluginDeployProvider_DeployPassesExpandedConfigToDriver(t *testing.T) {
	t.Setenv("TEST_HTTP_PORT_STR", "9090")

	driver := &captureResourceDriver{}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	// Simulate a resourceCfg that already had its env vars expanded at construction.
	p := &pluginDeployProvider{
		provider:     fake,
		resourceName: "my-app",
		resourceType: "infra.container_service",
		resourceCfg:  map[string]any{"label": "${TEST_HTTP_PORT_STR}"},
	}
	cfg := DeployConfig{
		AppName:  "my-app",
		ImageTag: "registry.example.com/myapp:v1",
		Env:      &config.CIDeployEnvironment{},
	}
	if err := p.Deploy(context.Background(), cfg); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	// The merged config passed to driver.Update should have the label expanded.
	got, _ := driver.updateCfg["label"].(string)
	if got != "9090" {
		t.Errorf("updateCfg[label]: want %q, got %q", "9090", got)
	}
}

// ── TestPluginDeployProvider_Deploy_SecretsExpandedViaEnv ────────────────────

// TestPluginDeployProvider_Deploy_SecretsExpandedViaEnv verifies that secrets
// carried in DeployConfig.Secrets are available for ${VAR} expansion in the
// resource config during Deploy. The deploy path temporarily exports each
// secret as an env var before calling ExpandEnvInMap, then restores the previous
// value so sibling tests are not affected.
func TestPluginDeployProvider_Deploy_SecretsExpandedViaEnv(t *testing.T) {
	// Do NOT pre-set the env var — it should come from cfg.Secrets.
	// t.Setenv cleans up after the test; use a unique name to avoid collisions.
	t.Setenv("DEPLOY_SECRET_TOKEN_UNIQUE", "") // ensure clean state; t.Setenv restores

	driver := &captureResourceDriver{}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	p := &pluginDeployProvider{
		provider:     fake,
		resourceName: "my-app",
		resourceType: "infra.container_service",
		// token references a secret injected via cfg.Secrets
		resourceCfg: map[string]any{"token": "${DEPLOY_SECRET_TOKEN_UNIQUE}"},
	}
	cfg := DeployConfig{
		AppName:  "my-app",
		ImageTag: "myapp:v1",
		Env:      &config.CIDeployEnvironment{},
		Secrets:  map[string]string{"DEPLOY_SECRET_TOKEN_UNIQUE": "vault_secret_abc"},
	}
	if err := p.Deploy(context.Background(), cfg); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	got, _ := driver.updateCfg["token"].(string)
	if got != "vault_secret_abc" {
		t.Errorf("updateCfg[token]: want %q, got %q", "vault_secret_abc", got)
	}
}

// ── TestPluginDeployProvider_Deploy_EmptyImageTagPreservesConfig ──────────────

// TestPluginDeployProvider_Deploy_EmptyImageTagPreservesConfig verifies that when
// cfg.ImageTag is empty the spec.Config["image"] set in the YAML (post-substitution)
// is passed through to the driver unchanged. This is the BMW scenario where the
// image ref is encoded in the YAML and IMAGE_TAG is not set.
func TestPluginDeployProvider_Deploy_EmptyImageTagPreservesConfig(t *testing.T) {
	driver := &captureResourceDriver{}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	p := &pluginDeployProvider{
		provider:     fake,
		resourceName: "my-app",
		resourceType: "infra.container_service",
		resourceCfg:  map[string]any{"image": "registry/org/app:sha256abc"},
	}
	cfg := DeployConfig{
		AppName:  "my-app",
		ImageTag: "", // not set — e.g. IMAGE_TAG env var absent
		Env:      &config.CIDeployEnvironment{},
	}
	if err := p.Deploy(context.Background(), cfg); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	got, _ := driver.updateCfg["image"].(string)
	if got != "registry/org/app:sha256abc" {
		t.Errorf("updateCfg[image]: want %q, got %q", "registry/org/app:sha256abc", got)
	}
}

// ── TestPluginDeployProvider_Deploy_NonEmptyImageTagOverrides ────────────────

// TestPluginDeployProvider_Deploy_NonEmptyImageTagOverrides verifies that when
// cfg.ImageTag is non-empty it overrides whatever the YAML config provided,
// preserving the existing IMAGE_TAG env-override CI path.
func TestPluginDeployProvider_Deploy_NonEmptyImageTagOverrides(t *testing.T) {
	driver := &captureResourceDriver{}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	p := &pluginDeployProvider{
		provider:     fake,
		resourceName: "my-app",
		resourceType: "infra.container_service",
		resourceCfg:  map[string]any{"image": "old:tag"},
	}
	cfg := DeployConfig{
		AppName:  "my-app",
		ImageTag: "new:tag",
		Env:      &config.CIDeployEnvironment{},
	}
	if err := p.Deploy(context.Background(), cfg); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	got, _ := driver.updateCfg["image"].(string)
	if got != "new:tag" {
		t.Errorf("updateCfg[image]: want %q, got %q", "new:tag", got)
	}
}

// ── TestPluginDeployProvider_Deploy_BMWScenario ───────────────────────────────

// TestPluginDeployProvider_Deploy_BMWScenario mirrors BMW's deploy.yml setup:
// the image field in YAML contains ${IMAGE_SHA}, IMAGE_SHA is set in the
// environment, and IMAGE_TAG (cfg.ImageTag) is absent. The driver must receive
// the fully-substituted image reference.
func TestPluginDeployProvider_Deploy_BMWScenario(t *testing.T) {
	t.Setenv("IMAGE_SHA_DEPLOY_BMW_TEST", "sha256deadbeef")

	driver := &captureResourceDriver{}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	p := &pluginDeployProvider{
		provider:     fake,
		resourceName: "buymywishlist",
		resourceType: "infra.container_service",
		resourceCfg: map[string]any{
			"image": "registry.digitalocean.com/bmw-registry/buymywishlist:${IMAGE_SHA_DEPLOY_BMW_TEST}",
		},
	}
	cfg := DeployConfig{
		AppName:  "buymywishlist",
		ImageTag: "", // IMAGE_TAG not set
		Env:      &config.CIDeployEnvironment{},
	}
	if err := p.Deploy(context.Background(), cfg); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	want := "registry.digitalocean.com/bmw-registry/buymywishlist:sha256deadbeef"
	got, _ := driver.updateCfg["image"].(string)
	if got != want {
		t.Errorf("updateCfg[image]: want %q, got %q", want, got)
	}
}

// ── TestPluginDeployProvider_Deploy_ImageTagWinsOverYAMLSHA ──────────────────

// TestPluginDeployProvider_Deploy_ImageTagWinsOverYAMLSHA verifies that a
// non-empty cfg.ImageTag (IMAGE_TAG env) wins over the ${IMAGE_SHA}-encoded
// image in the YAML, even when IMAGE_SHA is also set.
func TestPluginDeployProvider_Deploy_ImageTagWinsOverYAMLSHA(t *testing.T) {
	t.Setenv("IMAGE_SHA_DEPLOY_BMW_OVERRIDE_TEST", "sha256deadbeef")

	driver := &captureResourceDriver{}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	p := &pluginDeployProvider{
		provider:     fake,
		resourceName: "buymywishlist",
		resourceType: "infra.container_service",
		resourceCfg: map[string]any{
			"image": "registry.digitalocean.com/bmw-registry/buymywishlist:${IMAGE_SHA_DEPLOY_BMW_OVERRIDE_TEST}",
		},
	}
	cfg := DeployConfig{
		AppName:  "buymywishlist",
		ImageTag: "explicit-override:latest",
		Env:      &config.CIDeployEnvironment{},
	}
	if err := p.Deploy(context.Background(), cfg); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	got, _ := driver.updateCfg["image"].(string)
	if got != "explicit-override:latest" {
		t.Errorf("updateCfg[image]: want %q, got %q", "explicit-override:latest", got)
	}
}

// ── TestPluginDeployProvider_Deploy_EmptyImageBothLayers ─────────────────────

// TestPluginDeployProvider_Deploy_EmptyImageBothLayers verifies that Deploy
// returns a clear, actionable error when both IMAGE_TAG (cfg.ImageTag) and the
// YAML module config have no image — rather than sending an empty image to the
// remote API and getting an opaque provider error back.
func TestPluginDeployProvider_Deploy_EmptyImageBothLayers(t *testing.T) {
	driver := &captureResourceDriver{}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	p := &pluginDeployProvider{
		provider:     fake,
		resourceName: "my-app",
		resourceType: "infra.container_service",
		resourceCfg:  map[string]any{"region": "nyc3"}, // no "image" key
	}
	cfg := DeployConfig{
		AppName:  "my-app",
		ImageTag: "", // IMAGE_TAG also absent
		Env:      &config.CIDeployEnvironment{},
	}
	err := p.Deploy(context.Background(), cfg)
	if err == nil {
		t.Fatal("Deploy: expected error for empty image, got nil")
	}
	if !strings.Contains(err.Error(), "image") {
		t.Errorf("Deploy error should mention 'image', got: %v", err)
	}
}

// ── TestPluginDeployProvider_Deploy_SecretImageSubstitution ──────────────────

// TestPluginDeployProvider_Deploy_SecretImageSubstitution verifies that when
// the YAML image field contains a ${SECRET_VAR} reference, and that secret is
// carried in cfg.Secrets (not the OS env), Deploy fully substitutes the image
// before calling the driver.
func TestPluginDeployProvider_Deploy_SecretImageSubstitution(t *testing.T) {
	// Ensure the secret key is not already in the environment.
	t.Setenv("SECRET_IMAGE_DEPLOY_TEST_UNIQUE", "")

	driver := &captureResourceDriver{}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	p := &pluginDeployProvider{
		provider:     fake,
		resourceName: "my-app",
		resourceType: "infra.container_service",
		resourceCfg:  map[string]any{"image": "${SECRET_IMAGE_DEPLOY_TEST_UNIQUE}"},
	}
	cfg := DeployConfig{
		AppName:  "my-app",
		ImageTag: "",
		Env:      &config.CIDeployEnvironment{},
		Secrets:  map[string]string{"SECRET_IMAGE_DEPLOY_TEST_UNIQUE": "foo:bar"},
	}
	if err := p.Deploy(context.Background(), cfg); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	got, _ := driver.updateCfg["image"].(string)
	if got != "foo:bar" {
		t.Errorf("updateCfg[image]: want %q, got %q", "foo:bar", got)
	}
}
