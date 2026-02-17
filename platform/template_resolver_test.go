package platform

import (
	"testing"
)

func TestResolverStringSubstitution(t *testing.T) {
	resolver := NewTemplateResolver()

	tmpl := &WorkflowTemplate{
		Name:    "test",
		Version: "1.0.0",
		Parameters: []TemplateParameter{
			{Name: "app_name", Type: "string", Required: true},
			{Name: "image", Type: "string", Required: true},
		},
		Capabilities: []CapabilityDeclaration{
			{
				Name: "${app_name}",
				Type: "container_runtime",
				Properties: map[string]any{
					"image": "${image}",
				},
			},
		},
	}

	caps, err := resolver.Resolve(tmpl, map[string]any{
		"app_name": "my-api",
		"image":    "myrepo/my-api:v1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(caps) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(caps))
	}
	if caps[0].Name != "my-api" {
		t.Errorf("expected name 'my-api', got %q", caps[0].Name)
	}
	if caps[0].Properties["image"] != "myrepo/my-api:v1" {
		t.Errorf("expected image 'myrepo/my-api:v1', got %v", caps[0].Properties["image"])
	}
}

func TestResolverIntBoolSubstitution(t *testing.T) {
	resolver := NewTemplateResolver()

	tmpl := &WorkflowTemplate{
		Name:    "test",
		Version: "1.0.0",
		Parameters: []TemplateParameter{
			{Name: "replicas", Type: "int", Required: true},
			{Name: "debug", Type: "bool", Required: true},
		},
		Capabilities: []CapabilityDeclaration{
			{
				Name: "svc",
				Type: "container_runtime",
				Properties: map[string]any{
					"replicas": "${replicas}",
					"debug":    "${debug}",
				},
			},
		},
	}

	caps, err := resolver.Resolve(tmpl, map[string]any{
		"replicas": 5,
		"debug":    true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// When the entire value is a placeholder, the native type is preserved.
	if caps[0].Properties["replicas"] != 5 {
		t.Errorf("expected replicas=5 (int), got %v (%T)", caps[0].Properties["replicas"], caps[0].Properties["replicas"])
	}
	if caps[0].Properties["debug"] != true {
		t.Errorf("expected debug=true (bool), got %v (%T)", caps[0].Properties["debug"], caps[0].Properties["debug"])
	}
}

func TestResolverListMapSubstitution(t *testing.T) {
	resolver := NewTemplateResolver()

	tmpl := &WorkflowTemplate{
		Name:    "test",
		Version: "1.0.0",
		Parameters: []TemplateParameter{
			{Name: "ports", Type: "list", Required: true},
			{Name: "labels", Type: "map", Required: true},
		},
		Capabilities: []CapabilityDeclaration{
			{
				Name: "svc",
				Type: "container_runtime",
				Properties: map[string]any{
					"ports":  "${ports}",
					"labels": "${labels}",
				},
			},
		},
	}

	portList := []any{8080, 8443}
	labelMap := map[string]any{"env": "prod", "team": "platform"}

	caps, err := resolver.Resolve(tmpl, map[string]any{
		"ports":  portList,
		"labels": labelMap,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ports, ok := caps[0].Properties["ports"].([]any)
	if !ok {
		t.Fatalf("expected ports to be []any, got %T", caps[0].Properties["ports"])
	}
	if len(ports) != 2 {
		t.Errorf("expected 2 ports, got %d", len(ports))
	}

	labels, ok := caps[0].Properties["labels"].(map[string]any)
	if !ok {
		t.Fatalf("expected labels to be map[string]any, got %T", caps[0].Properties["labels"])
	}
	if labels["env"] != "prod" {
		t.Errorf("expected labels.env='prod', got %v", labels["env"])
	}
}

func TestResolverMissingRequiredParameter(t *testing.T) {
	resolver := NewTemplateResolver()

	tmpl := &WorkflowTemplate{
		Name:    "test",
		Version: "1.0.0",
		Parameters: []TemplateParameter{
			{Name: "app_name", Type: "string", Required: true},
		},
		Capabilities: []CapabilityDeclaration{
			{Name: "${app_name}", Type: "container_runtime"},
		},
	}

	_, err := resolver.Resolve(tmpl, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing required parameter")
	}
	if got := err.Error(); got != `required parameter "app_name" is missing` {
		t.Errorf("unexpected error message: %s", got)
	}
}

func TestResolverDefaultValues(t *testing.T) {
	resolver := NewTemplateResolver()

	tmpl := &WorkflowTemplate{
		Name:    "test",
		Version: "1.0.0",
		Parameters: []TemplateParameter{
			{Name: "replicas", Type: "int", Default: 3},
			{Name: "memory", Type: "string", Default: "512Mi"},
		},
		Capabilities: []CapabilityDeclaration{
			{
				Name: "svc",
				Type: "container_runtime",
				Properties: map[string]any{
					"replicas": "${replicas}",
					"memory":   "${memory}",
				},
			},
		},
	}

	caps, err := resolver.Resolve(tmpl, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if caps[0].Properties["replicas"] != 3 {
		t.Errorf("expected default replicas=3, got %v", caps[0].Properties["replicas"])
	}
	if caps[0].Properties["memory"] != "512Mi" {
		t.Errorf("expected default memory='512Mi', got %v", caps[0].Properties["memory"])
	}
}

func TestResolverNestedMapSubstitution(t *testing.T) {
	resolver := NewTemplateResolver()

	tmpl := &WorkflowTemplate{
		Name:    "test",
		Version: "1.0.0",
		Parameters: []TemplateParameter{
			{Name: "app_name", Type: "string", Required: true},
			{Name: "port", Type: "int", Required: true},
		},
		Capabilities: []CapabilityDeclaration{
			{
				Name: "${app_name}",
				Type: "container_runtime",
				Properties: map[string]any{
					"health_check": map[string]any{
						"path": "/health",
						"port": "${port}",
					},
					"ports": []any{
						map[string]any{
							"container_port": "${port}",
							"protocol":       "tcp",
						},
					},
				},
			},
		},
	}

	caps, err := resolver.Resolve(tmpl, map[string]any{
		"app_name": "api",
		"port":     8080,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hc, ok := caps[0].Properties["health_check"].(map[string]any)
	if !ok {
		t.Fatalf("expected health_check to be map, got %T", caps[0].Properties["health_check"])
	}
	if hc["port"] != 8080 {
		t.Errorf("expected health_check.port=8080, got %v", hc["port"])
	}

	ports, ok := caps[0].Properties["ports"].([]any)
	if !ok {
		t.Fatalf("expected ports to be []any, got %T", caps[0].Properties["ports"])
	}
	portMap, ok := ports[0].(map[string]any)
	if !ok {
		t.Fatalf("expected ports[0] to be map, got %T", ports[0])
	}
	if portMap["container_port"] != 8080 {
		t.Errorf("expected container_port=8080, got %v", portMap["container_port"])
	}
}

func TestResolverValidationConstraints(t *testing.T) {
	resolver := NewTemplateResolver()

	tmpl := &WorkflowTemplate{
		Name:    "test",
		Version: "1.0.0",
		Parameters: []TemplateParameter{
			{Name: "name", Type: "string", Required: true, Validation: `^[a-z][a-z0-9-]*$`},
		},
		Capabilities: []CapabilityDeclaration{
			{Name: "${name}", Type: "container_runtime"},
		},
	}

	// Valid name.
	caps, err := resolver.Resolve(tmpl, map[string]any{"name": "my-service"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if caps[0].Name != "my-service" {
		t.Errorf("expected 'my-service', got %q", caps[0].Name)
	}

	// Invalid name.
	_, err = resolver.Resolve(tmpl, map[string]any{"name": "UPPER_CASE"})
	if err == nil {
		t.Fatal("expected error for validation failure")
	}
}

func TestResolverNilTemplate(t *testing.T) {
	resolver := NewTemplateResolver()

	_, err := resolver.Resolve(nil, map[string]any{})
	if err == nil {
		t.Fatal("expected error for nil template")
	}
}

func TestResolverMixedStringInterpolation(t *testing.T) {
	resolver := NewTemplateResolver()

	tmpl := &WorkflowTemplate{
		Name:    "test",
		Version: "1.0.0",
		Parameters: []TemplateParameter{
			{Name: "name", Type: "string", Required: true},
			{Name: "version", Type: "string", Required: true},
		},
		Capabilities: []CapabilityDeclaration{
			{
				Name: "svc",
				Type: "container_runtime",
				Properties: map[string]any{
					"image": "myrepo/${name}:${version}",
				},
			},
		},
	}

	caps, err := resolver.Resolve(tmpl, map[string]any{
		"name":    "api",
		"version": "v2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if caps[0].Properties["image"] != "myrepo/api:v2" {
		t.Errorf("expected 'myrepo/api:v2', got %v", caps[0].Properties["image"])
	}
}
