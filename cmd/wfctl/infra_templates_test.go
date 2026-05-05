package main

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

func TestResolveOutputTemplates_OutputRef(t *testing.T) {
	spec := &interfaces.ResourceSpec{
		Name: "app",
		Config: map[string]any{
			"db_url": "{{ outputs.database.uri }}",
			"region": "us-east-1",
		},
	}
	outputs := map[string]*interfaces.ResourceOutput{
		"database": {
			Name:    "database",
			Outputs: map[string]any{"uri": "postgres://user:pass@host:5432/db"},
		},
	}
	if err := resolveOutputTemplates(spec, outputs, nil); err != nil {
		t.Fatalf("resolveOutputTemplates: %v", err)
	}
	if spec.Config["db_url"] != "postgres://user:pass@host:5432/db" {
		t.Errorf("db_url = %q", spec.Config["db_url"])
	}
	if spec.Config["region"] != "us-east-1" {
		t.Error("non-template value should be unchanged")
	}
}

func TestResolveOutputTemplates_SecretsRef(t *testing.T) {
	spec := &interfaces.ResourceSpec{
		Name:   "app",
		Config: map[string]any{"api_key": "{{ secrets.MY_API_KEY }}"},
	}
	provider := &mockSecretsProvider{values: map[string]string{"MY_API_KEY": "secret-value-123"}}
	if err := resolveOutputTemplates(spec, nil, provider); err != nil {
		t.Fatalf("resolveOutputTemplates secrets: %v", err)
	}
	if spec.Config["api_key"] != "secret-value-123" {
		t.Errorf("api_key = %q", spec.Config["api_key"])
	}
}

func TestResolveOutputTemplates_NestedMap(t *testing.T) {
	spec := &interfaces.ResourceSpec{
		Name: "app",
		Config: map[string]any{
			"database": map[string]any{
				"host": "{{ outputs.db.host }}",
				"port": 5432,
			},
		},
	}
	outputs := map[string]*interfaces.ResourceOutput{
		"db": {Outputs: map[string]any{"host": "db.internal"}},
	}
	if err := resolveOutputTemplates(spec, outputs, nil); err != nil {
		t.Fatalf("resolveOutputTemplates nested: %v", err)
	}
	nested := spec.Config["database"].(map[string]any)
	if nested["host"] != "db.internal" {
		t.Errorf("nested host = %q", nested["host"])
	}
}

func TestResolveOutputTemplates_MissingResource(t *testing.T) {
	spec := &interfaces.ResourceSpec{
		Name:   "app",
		Config: map[string]any{"url": "{{ outputs.missing.endpoint }}"},
	}
	err := resolveOutputTemplates(spec, map[string]*interfaces.ResourceOutput{}, nil)
	if err == nil {
		t.Error("expected error for missing resource")
	}
}

func TestInferDependencies_AddsImplicit(t *testing.T) {
	specs := []interfaces.ResourceSpec{
		{Name: "db", Type: "infra.database"},
		{Name: "app", Type: "infra.service", Config: map[string]any{
			"db_url": "{{ outputs.db.uri }}",
		}},
	}
	result := inferDependencies(specs)
	if len(result[1].DependsOn) == 0 {
		t.Fatal("expected inferred dependency on 'db'")
	}
	if result[1].DependsOn[0] != "db" {
		t.Errorf("DependsOn[0] = %q, want %q", result[1].DependsOn[0], "db")
	}
}

func TestInferDependencies_NoDuplication(t *testing.T) {
	specs := []interfaces.ResourceSpec{
		{Name: "db"},
		{
			Name:      "app",
			DependsOn: []string{"db"},
			Config:    map[string]any{"url": "{{ outputs.db.uri }}"},
		},
	}
	result := inferDependencies(specs)
	count := 0
	for _, dep := range result[1].DependsOn {
		if dep == "db" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("'db' appears %d times in DependsOn, want 1", count)
	}
}

func TestTopoSort_BasicOrder(t *testing.T) {
	specs := []interfaces.ResourceSpec{
		{Name: "app", DependsOn: []string{"db"}},
		{Name: "db"},
	}
	sorted, err := topoSort(specs)
	if err != nil {
		t.Fatalf("topoSort: %v", err)
	}
	if sorted[0].Name != "db" || sorted[1].Name != "app" {
		t.Errorf("order = [%s, %s], want [db, app]", sorted[0].Name, sorted[1].Name)
	}
}

func TestTopoSort_CircularDependency(t *testing.T) {
	specs := []interfaces.ResourceSpec{
		{Name: "a", DependsOn: []string{"b"}},
		{Name: "b", DependsOn: []string{"a"}},
	}
	_, err := topoSort(specs)
	if err == nil {
		t.Error("expected error for circular dependency")
	}
}

func TestTopoSort_NoDependencies(t *testing.T) {
	specs := []interfaces.ResourceSpec{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}
	sorted, err := topoSort(specs)
	if err != nil {
		t.Fatalf("topoSort: %v", err)
	}
	if len(sorted) != 3 {
		t.Errorf("expected 3 specs, got %d", len(sorted))
	}
}

// mockSecretsProvider is a simple in-memory secrets.Provider for tests.
type mockSecretsProvider struct {
	values map[string]string
}

func (m *mockSecretsProvider) Name() string { return "mock" }
func (m *mockSecretsProvider) Get(_ context.Context, key string) (string, error) {
	v, ok := m.values[key]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}
func (m *mockSecretsProvider) Set(_ context.Context, key, value string) error {
	m.values[key] = value
	return nil
}
func (m *mockSecretsProvider) Delete(_ context.Context, key string) error {
	delete(m.values, key)
	return nil
}
func (m *mockSecretsProvider) List(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(m.values))
	for k := range m.values {
		keys = append(keys, k)
	}
	return keys, nil
}
