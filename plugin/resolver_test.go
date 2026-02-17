package plugin

import (
	"reflect"
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestResolveWorkflowDependencies_NilRequires(t *testing.T) {
	mgr := NewEnginePluginManager(capability.NewRegistry(), schema.NewModuleSchemaRegistry())

	cfg := &config.WorkflowConfig{}
	if err := mgr.ResolveWorkflowDependencies(cfg); err != nil {
		t.Fatalf("expected nil error for nil Requires, got: %v", err)
	}
}

func TestResolveWorkflowDependencies_AllSatisfied(t *testing.T) {
	capReg := capability.NewRegistry()

	// Register a contract and a provider.
	_ = capReg.RegisterContract(capability.Contract{
		Name:          "http-server",
		Description:   "HTTP server capability",
		InterfaceType: reflect.TypeOf((*any)(nil)).Elem(),
	})
	_ = capReg.RegisterProvider("http-server", "http-plugin", 10, reflect.TypeOf((*any)(nil)).Elem())

	mgr := NewEnginePluginManager(capReg, schema.NewModuleSchemaRegistry())

	cfg := &config.WorkflowConfig{
		Requires: &config.RequiresConfig{
			Capabilities: []string{"http-server"},
		},
	}
	if err := mgr.ResolveWorkflowDependencies(cfg); err != nil {
		t.Fatalf("expected no error when all capabilities satisfied, got: %v", err)
	}
}

func TestResolveWorkflowDependencies_MissingCapabilities(t *testing.T) {
	capReg := capability.NewRegistry()
	mgr := NewEnginePluginManager(capReg, schema.NewModuleSchemaRegistry())

	cfg := &config.WorkflowConfig{
		Requires: &config.RequiresConfig{
			Capabilities: []string{"http-server", "message-broker"},
		},
	}

	err := mgr.ResolveWorkflowDependencies(cfg)
	if err == nil {
		t.Fatal("expected error for missing capabilities")
	}

	mce, ok := err.(*MissingCapabilitiesError)
	if !ok {
		t.Fatalf("expected *MissingCapabilitiesError, got %T", err)
	}
	if len(mce.Missing) != 2 {
		t.Errorf("expected 2 missing capabilities, got %d", len(mce.Missing))
	}
}

func TestResolveWorkflowDependencies_EmptyCapabilities(t *testing.T) {
	mgr := NewEnginePluginManager(capability.NewRegistry(), schema.NewModuleSchemaRegistry())

	cfg := &config.WorkflowConfig{
		Requires: &config.RequiresConfig{
			Capabilities: []string{},
		},
	}
	if err := mgr.ResolveWorkflowDependencies(cfg); err != nil {
		t.Fatalf("expected nil error for empty capabilities, got: %v", err)
	}
}
