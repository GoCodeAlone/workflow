package platform

import (
	"context"
	"testing"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewStdTemplateRegistry()
	ctx := context.Background()

	tmpl := &WorkflowTemplate{
		Name:        "microservice",
		Version:     "1.0.0",
		Description: "Standard microservice",
		Parameters: []TemplateParameter{
			{Name: "app_name", Type: "string", Required: true},
		},
		Capabilities: []CapabilityDeclaration{
			{Name: "${app_name}", Type: "container_runtime"},
		},
	}

	if err := reg.Register(ctx, tmpl); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	got, err := reg.Get(ctx, "microservice", "1.0.0")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Name != "microservice" || got.Version != "1.0.0" {
		t.Errorf("unexpected template: name=%q version=%q", got.Name, got.Version)
	}
}

func TestRegistryRegisterDuplicateReturnsError(t *testing.T) {
	reg := NewStdTemplateRegistry()
	ctx := context.Background()

	tmpl := &WorkflowTemplate{
		Name:    "microservice",
		Version: "1.0.0",
	}

	if err := reg.Register(ctx, tmpl); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	err := reg.Register(ctx, tmpl)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestRegistryGetEmptyVersionReturnsLatest(t *testing.T) {
	reg := NewStdTemplateRegistry()
	ctx := context.Background()

	for _, ver := range []string{"1.0.0", "2.0.0", "1.5.0"} {
		tmpl := &WorkflowTemplate{
			Name:    "microservice",
			Version: ver,
		}
		if err := reg.Register(ctx, tmpl); err != nil {
			t.Fatalf("Register %s failed: %v", ver, err)
		}
	}

	got, err := reg.Get(ctx, "microservice", "")
	if err != nil {
		t.Fatalf("Get with empty version failed: %v", err)
	}
	if got.Version != "2.0.0" {
		t.Errorf("expected latest version '2.0.0', got %q", got.Version)
	}
}

func TestRegistryGetLatest(t *testing.T) {
	reg := NewStdTemplateRegistry()
	ctx := context.Background()

	for _, ver := range []string{"1.0.0", "3.1.0", "2.5.0"} {
		tmpl := &WorkflowTemplate{
			Name:    "service",
			Version: ver,
		}
		if err := reg.Register(ctx, tmpl); err != nil {
			t.Fatalf("Register %s failed: %v", ver, err)
		}
	}

	got, err := reg.GetLatest(ctx, "service")
	if err != nil {
		t.Fatalf("GetLatest failed: %v", err)
	}
	if got.Version != "3.1.0" {
		t.Errorf("expected latest version '3.1.0', got %q", got.Version)
	}
}

func TestRegistryListReturnsAllTemplates(t *testing.T) {
	reg := NewStdTemplateRegistry()
	ctx := context.Background()

	templates := []*WorkflowTemplate{
		{Name: "microservice", Version: "1.0.0", Parameters: []TemplateParameter{{Name: "name"}}},
		{Name: "microservice", Version: "2.0.0", Parameters: []TemplateParameter{{Name: "name"}}},
		{Name: "database", Version: "1.0.0", Parameters: []TemplateParameter{{Name: "engine"}}},
	}

	for _, tmpl := range templates {
		if err := reg.Register(ctx, tmpl); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
	}

	summaries, err := reg.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(summaries) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(summaries))
	}

	// Sorted by name then version.
	if summaries[0].Name != "database" {
		t.Errorf("expected first entry to be 'database', got %q", summaries[0].Name)
	}
	if summaries[1].Name != "microservice" || summaries[1].Version != "1.0.0" {
		t.Errorf("expected second entry 'microservice' v1.0.0, got %q v%s", summaries[1].Name, summaries[1].Version)
	}
}

func TestRegistryResolveWithAllParameterTypes(t *testing.T) {
	reg := NewStdTemplateRegistry()
	ctx := context.Background()

	tmpl := &WorkflowTemplate{
		Name:    "full-test",
		Version: "1.0.0",
		Parameters: []TemplateParameter{
			{Name: "name", Type: "string", Required: true},
			{Name: "replicas", Type: "int", Default: 3},
			{Name: "debug", Type: "bool", Default: false},
		},
		Capabilities: []CapabilityDeclaration{
			{
				Name: "${name}",
				Type: "container_runtime",
				Properties: map[string]any{
					"replicas": "${replicas}",
					"debug":    "${debug}",
				},
			},
		},
	}

	if err := reg.Register(ctx, tmpl); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	caps, err := reg.Resolve(ctx, "full-test", "1.0.0", map[string]any{
		"name":     "api-svc",
		"replicas": 5,
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if len(caps) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(caps))
	}
	if caps[0].Name != "api-svc" {
		t.Errorf("expected name 'api-svc', got %q", caps[0].Name)
	}
	if caps[0].Properties["replicas"] != 5 {
		t.Errorf("expected replicas=5, got %v", caps[0].Properties["replicas"])
	}
	if caps[0].Properties["debug"] != false {
		t.Errorf("expected debug=false (default), got %v", caps[0].Properties["debug"])
	}
}

func TestRegistryResolveMissingRequiredParam(t *testing.T) {
	reg := NewStdTemplateRegistry()
	ctx := context.Background()

	tmpl := &WorkflowTemplate{
		Name:    "test",
		Version: "1.0.0",
		Parameters: []TemplateParameter{
			{Name: "name", Type: "string", Required: true},
		},
		Capabilities: []CapabilityDeclaration{
			{Name: "${name}", Type: "container_runtime"},
		},
	}

	if err := reg.Register(ctx, tmpl); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	_, err := reg.Resolve(ctx, "test", "1.0.0", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing required parameter")
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	reg := NewStdTemplateRegistry()
	ctx := context.Background()

	_, err := reg.Get(ctx, "nonexistent", "1.0.0")
	if err == nil {
		t.Fatal("expected error for nonexistent template")
	}
}

func TestRegistryRegisterNilTemplate(t *testing.T) {
	reg := NewStdTemplateRegistry()
	ctx := context.Background()

	err := reg.Register(ctx, nil)
	if err == nil {
		t.Fatal("expected error for nil template")
	}
}

func TestRegistryRegisterMissingName(t *testing.T) {
	reg := NewStdTemplateRegistry()
	ctx := context.Background()

	err := reg.Register(ctx, &WorkflowTemplate{Version: "1.0.0"})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestRegistryRegisterMissingVersion(t *testing.T) {
	reg := NewStdTemplateRegistry()
	ctx := context.Background()

	err := reg.Register(ctx, &WorkflowTemplate{Name: "test"})
	if err == nil {
		t.Fatal("expected error for missing version")
	}
}

func TestRegistryResolveWithEmptyVersion(t *testing.T) {
	reg := NewStdTemplateRegistry()
	ctx := context.Background()

	for _, ver := range []string{"1.0.0", "2.0.0"} {
		tmpl := &WorkflowTemplate{
			Name:    "svc",
			Version: ver,
			Parameters: []TemplateParameter{
				{Name: "name", Type: "string", Required: true},
			},
			Capabilities: []CapabilityDeclaration{
				{Name: "${name}", Type: "container_runtime"},
			},
		}
		if err := reg.Register(ctx, tmpl); err != nil {
			t.Fatalf("Register %s failed: %v", ver, err)
		}
	}

	caps, err := reg.Resolve(ctx, "svc", "", map[string]any{"name": "test"})
	if err != nil {
		t.Fatalf("Resolve with empty version failed: %v", err)
	}
	if len(caps) != 1 || caps[0].Name != "test" {
		t.Errorf("unexpected resolve result: %+v", caps)
	}
}
