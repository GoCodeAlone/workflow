package module

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/iactest"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type recordingIaCProvider struct {
	iactest.NoopProvider
	initCfg map[string]any
	initErr error
}

func (p *recordingIaCProvider) Initialize(_ context.Context, cfg map[string]any) error {
	p.initCfg = cfg
	return p.initErr
}

func TestIaCProviderModule_InitAliasesAndInitializesExternalProvider(t *testing.T) {
	provider := &recordingIaCProvider{}
	app := NewMockApplication()
	if err := app.RegisterService("workflow-plugin-aws", provider); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	m := NewIaCProviderModule("aws-provider", map[string]any{
		"plugin":      "workflow-plugin-aws",
		"mode":        "mock",
		"region":      "us-east-1",
		"_config_dir": "/tmp/app",
	})

	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	var got interfaces.IaCProvider
	if err := app.GetService("aws-provider", &got); err != nil {
		t.Fatalf("GetService(alias): %v", err)
	}
	if got != provider {
		t.Fatalf("alias service = %T %p, want provider %p", got, got, provider)
	}

	wantCfg := map[string]any{"mode": "mock", "region": "us-east-1"}
	if !reflect.DeepEqual(provider.initCfg, wantCfg) {
		t.Fatalf("Initialize config = %#v, want %#v", provider.initCfg, wantCfg)
	}
}

func TestIaCProviderModule_InitAcceptsProviderShorthand(t *testing.T) {
	provider := &recordingIaCProvider{}
	app := NewMockApplication()
	if err := app.RegisterService("workflow-plugin-aws", provider); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	m := NewIaCProviderModule("aws-provider", map[string]any{
		"provider": "aws",
		"mode":     "mock",
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if provider.initCfg["provider"] != "aws" || provider.initCfg["mode"] != "mock" {
		t.Fatalf("Initialize config = %#v", provider.initCfg)
	}
}

func TestIaCProviderModule_InitAcceptsServiceAlias(t *testing.T) {
	provider := &recordingIaCProvider{}
	app := NewMockApplication()
	if err := app.RegisterService("workflow-plugin-aws", provider); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	m := NewIaCProviderModule("aws-provider", map[string]any{
		"service": "workflow-plugin-aws",
		"mode":    "mock",
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, ok := provider.initCfg["service"]; ok {
		t.Fatalf("Initialize config leaked service alias: %#v", provider.initCfg)
	}
	if provider.initCfg["mode"] != "mock" {
		t.Fatalf("Initialize config = %#v", provider.initCfg)
	}
}

func TestIaCProviderModule_InitRequiresPluginService(t *testing.T) {
	m := NewIaCProviderModule("aws-provider", map[string]any{"mode": "mock"})
	err := m.Init(NewMockApplication())
	if err == nil {
		t.Fatal("Init must fail without plugin config")
	}
	if !strings.Contains(err.Error(), "plugin") {
		t.Fatalf("Init error = %v, want plugin guidance", err)
	}
}

func TestIaCProviderModule_InitRejectsNonProviderService(t *testing.T) {
	app := NewMockApplication()
	if err := app.RegisterService("not-provider", "nope"); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	m := NewIaCProviderModule("aws-provider", map[string]any{"plugin": "not-provider"})
	err := m.Init(app)
	if err == nil {
		t.Fatal("Init must fail when plugin service is not an IaCProvider")
	}
	if !strings.Contains(err.Error(), "does not implement IaCProvider") {
		t.Fatalf("Init error = %v", err)
	}
}

func TestIaCProviderModule_InitWrapsProviderInitializeError(t *testing.T) {
	sentinel := errors.New("bad provider config")
	provider := &recordingIaCProvider{initErr: sentinel}
	app := NewMockApplication()
	if err := app.RegisterService("workflow-plugin-aws", provider); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	m := NewIaCProviderModule("aws-provider", map[string]any{"plugin": "workflow-plugin-aws"})
	err := m.Init(app)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Init error = %v, want wrapping %v", err, sentinel)
	}
}
