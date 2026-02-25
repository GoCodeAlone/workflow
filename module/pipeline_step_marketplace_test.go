package module

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// ─── mock registry ────────────────────────────────────────────────────────

// mockMarketplaceRegistry is a simple in-memory registry for testing.
type mockMarketplaceRegistry struct {
	entries   []MarketplaceEntry
	installed map[string]bool
}

func newMockRegistry(entries []MarketplaceEntry) *mockMarketplaceRegistry {
	installed := make(map[string]bool)
	for _, e := range entries {
		if e.Installed {
			installed[e.Name] = true
		}
	}
	return &mockMarketplaceRegistry{entries: entries, installed: installed}
}

func (r *mockMarketplaceRegistry) Search(query, category string, tags []string) ([]MarketplaceEntry, error) {
	var results []MarketplaceEntry
	for _, e := range r.entries {
		if query != "" && !strings.Contains(strings.ToLower(e.Name), strings.ToLower(query)) &&
			!strings.Contains(strings.ToLower(e.Description), strings.ToLower(query)) {
			continue
		}
		if category != "" && e.Category != category {
			continue
		}
		if len(tags) > 0 {
			matched := false
			for _, want := range tags {
				for _, have := range e.Tags {
					if have == want {
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}
			if !matched {
				continue
			}
		}
		results = append(results, e)
	}
	return results, nil
}

func (r *mockMarketplaceRegistry) Detail(name string) (*MarketplaceEntry, error) {
	for _, e := range r.entries {
		if e.Name == name {
			e.Installed = r.installed[name]
			return &e, nil
		}
	}
	return nil, fmt.Errorf("plugin %q not found", name)
}

func (r *mockMarketplaceRegistry) Install(name string) error {
	for _, e := range r.entries {
		if e.Name == name {
			r.installed[name] = true
			return nil
		}
	}
	return fmt.Errorf("plugin %q not found in registry", name)
}

func (r *mockMarketplaceRegistry) Uninstall(name string) error {
	if !r.installed[name] {
		return fmt.Errorf("plugin %q is not installed", name)
	}
	delete(r.installed, name)
	return nil
}

func (r *mockMarketplaceRegistry) Update(name string) (*MarketplaceEntry, error) {
	if !r.installed[name] {
		return nil, fmt.Errorf("plugin %q is not installed", name)
	}
	for i, e := range r.entries {
		if e.Name == name {
			r.entries[i].Version = "99.0.0"
			updated := r.entries[i]
			updated.Installed = true
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("plugin %q not found", name)
}

func (r *mockMarketplaceRegistry) ListInstalled() ([]MarketplaceEntry, error) {
	var result []MarketplaceEntry
	for _, e := range r.entries {
		if r.installed[e.Name] {
			e.Installed = true
			result = append(result, e)
		}
	}
	return result, nil
}

// ─── seed data ────────────────────────────────────────────────────────────

func seedEntries() []MarketplaceEntry {
	return []MarketplaceEntry{
		{
			Name:        "auth-oidc",
			Version:     "1.2.0",
			Description: "OpenID Connect authentication provider",
			Author:      "GoCodeAlone",
			Category:    "auth",
			Tags:        []string{"auth", "oidc", "sso"},
			Downloads:   4200,
			Rating:      4.8,
		},
		{
			Name:        "storage-s3",
			Version:     "2.0.1",
			Description: "AWS S3 blob storage backend",
			Author:      "GoCodeAlone",
			Category:    "storage",
			Tags:        []string{"storage", "aws", "s3"},
			Downloads:   8900,
			Rating:      4.9,
			Installed:   true,
		},
		{
			Name:        "messaging-kafka",
			Version:     "1.0.3",
			Description: "Apache Kafka messaging integration",
			Author:      "GoCodeAlone",
			Category:    "messaging",
			Tags:        []string{"messaging", "kafka"},
			Downloads:   3100,
			Rating:      4.6,
		},
		{
			Name:        "observability-otel",
			Version:     "0.9.0",
			Description: "OpenTelemetry tracing and metrics",
			Author:      "GoCodeAlone",
			Category:    "observability",
			Tags:        []string{"observability", "otel", "tracing"},
			Downloads:   2700,
			Rating:      4.5,
		},
	}
}

// ─── search tests ─────────────────────────────────────────────────────────

func TestMarketplaceSearch_NoFilters(t *testing.T) {
	reg := newMockRegistry(seedEntries())
	factory := NewMarketplaceSearchStepFactory(reg)

	step, err := factory("search-all", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	if step.Name() != "search-all" {
		t.Errorf("expected name 'search-all', got %q", step.Name())
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	count, _ := result.Output["count"].(int)
	if count != 4 {
		t.Errorf("expected 4 results, got %d", count)
	}
}

func TestMarketplaceSearch_ByQuery(t *testing.T) {
	reg := newMockRegistry(seedEntries())
	factory := NewMarketplaceSearchStepFactory(reg)

	step, err := factory("search-kafka", map[string]any{
		"query": "kafka",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	count, _ := result.Output["count"].(int)
	if count != 1 {
		t.Errorf("expected 1 result, got %d", count)
	}
	results, _ := result.Output["results"].([]map[string]any)
	if len(results) != 1 || results[0]["name"] != "messaging-kafka" {
		t.Errorf("expected messaging-kafka in results, got %v", results)
	}
}

func TestMarketplaceSearch_ByCategory(t *testing.T) {
	reg := newMockRegistry(seedEntries())
	factory := NewMarketplaceSearchStepFactory(reg)

	step, err := factory("search-auth", map[string]any{
		"category": "auth",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	count, _ := result.Output["count"].(int)
	if count != 1 {
		t.Errorf("expected 1 result for category=auth, got %d", count)
	}
}

func TestMarketplaceSearch_ByTags(t *testing.T) {
	reg := newMockRegistry(seedEntries())
	factory := NewMarketplaceSearchStepFactory(reg)

	step, err := factory("search-by-tag", map[string]any{
		"tags": []any{"tracing"},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	count, _ := result.Output["count"].(int)
	if count != 1 {
		t.Errorf("expected 1 result for tag=tracing, got %d", count)
	}
}

func TestMarketplaceSearch_NoResults(t *testing.T) {
	reg := newMockRegistry(seedEntries())
	factory := NewMarketplaceSearchStepFactory(reg)

	step, err := factory("search-none", map[string]any{
		"query": "nonexistent-xyz",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	count, _ := result.Output["count"].(int)
	if count != 0 {
		t.Errorf("expected 0 results, got %d", count)
	}
}

// ─── detail tests ─────────────────────────────────────────────────────────

func TestMarketplaceDetail_KnownPlugin(t *testing.T) {
	reg := newMockRegistry(seedEntries())
	factory := NewMarketplaceDetailStepFactory(reg)

	step, err := factory("detail-oidc", map[string]any{
		"plugin": "auth-oidc",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	pluginMap, ok := result.Output["plugin"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'plugin' map in output, got %T", result.Output["plugin"])
	}
	if pluginMap["name"] != "auth-oidc" {
		t.Errorf("expected name='auth-oidc', got %v", pluginMap["name"])
	}
	if pluginMap["category"] != "auth" {
		t.Errorf("expected category='auth', got %v", pluginMap["category"])
	}
}

func TestMarketplaceDetail_MissingPluginConfig(t *testing.T) {
	reg := newMockRegistry(seedEntries())
	factory := NewMarketplaceDetailStepFactory(reg)

	_, err := factory("detail-bad", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing plugin config")
	}
}

func TestMarketplaceDetail_UnknownPlugin(t *testing.T) {
	reg := newMockRegistry(seedEntries())
	factory := NewMarketplaceDetailStepFactory(reg)

	step, err := factory("detail-unknown", map[string]any{
		"plugin": "does-not-exist",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, err = step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err == nil {
		t.Fatal("expected error for unknown plugin")
	}
}

// ─── install lifecycle tests ───────────────────────────────────────────────

func TestMarketplaceInstall_Lifecycle(t *testing.T) {
	reg := newMockRegistry(seedEntries())

	// 1. Search → find kafka
	searchFactory := NewMarketplaceSearchStepFactory(reg)
	searchStep, _ := searchFactory("search", map[string]any{"query": "kafka"}, nil)
	searchResult, err := searchStep.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("search error: %v", err)
	}
	if count := searchResult.Output["count"].(int); count != 1 {
		t.Fatalf("expected 1 search result, got %d", count)
	}

	// 2. Install kafka
	installFactory := NewMarketplaceInstallStepFactory(reg)
	installStep, err := installFactory("install-kafka", map[string]any{
		"plugin": "messaging-kafka",
	}, nil)
	if err != nil {
		t.Fatalf("install factory error: %v", err)
	}

	installResult, err := installStep.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("install error: %v", err)
	}
	if installResult.Output["success"] != true {
		t.Errorf("expected success=true, got %v", installResult.Output["success"])
	}
	if installResult.Output["status"] != "installed" {
		t.Errorf("expected status=installed, got %v", installResult.Output["status"])
	}

	// 3. Verify via installed list
	installedFactory := NewMarketplaceInstalledStepFactory(reg)
	installedStep, _ := installedFactory("list-installed", map[string]any{}, nil)
	installedResult, err := installedStep.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("installed list error: %v", err)
	}
	count, _ := installedResult.Output["count"].(int)
	if count != 2 {
		t.Errorf("expected 2 installed plugins (storage-s3 + messaging-kafka), got %d", count)
	}

	// 4. Uninstall kafka
	uninstallFactory := NewMarketplaceUninstallStepFactory(reg)
	uninstallStep, err := uninstallFactory("uninstall-kafka", map[string]any{
		"plugin": "messaging-kafka",
	}, nil)
	if err != nil {
		t.Fatalf("uninstall factory error: %v", err)
	}

	uninstallResult, err := uninstallStep.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("uninstall error: %v", err)
	}
	if uninstallResult.Output["status"] != "uninstalled" {
		t.Errorf("expected status=uninstalled, got %v", uninstallResult.Output["status"])
	}

	// 5. Verify only storage-s3 remains
	installedResult2, err := installedStep.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("installed list error after uninstall: %v", err)
	}
	if count := installedResult2.Output["count"].(int); count != 1 {
		t.Errorf("expected 1 installed plugin after uninstall, got %d", count)
	}
}

func TestMarketplaceInstall_MissingConfig(t *testing.T) {
	reg := newMockRegistry(seedEntries())
	factory := NewMarketplaceInstallStepFactory(reg)

	_, err := factory("bad", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing plugin config")
	}
}

func TestMarketplaceInstall_UnknownPlugin(t *testing.T) {
	reg := newMockRegistry(seedEntries())
	factory := NewMarketplaceInstallStepFactory(reg)

	step, err := factory("install-unknown", map[string]any{
		"plugin": "ghost-plugin",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, err = step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err == nil {
		t.Fatal("expected error for unknown plugin")
	}
}

func TestMarketplaceUninstall_NotInstalled(t *testing.T) {
	reg := newMockRegistry(seedEntries())
	factory := NewMarketplaceUninstallStepFactory(reg)

	step, err := factory("uninstall-not-installed", map[string]any{
		"plugin": "auth-oidc", // not installed
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, err = step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err == nil {
		t.Fatal("expected error for uninstalling non-installed plugin")
	}
}

// ─── update tests ─────────────────────────────────────────────────────────

func TestMarketplaceUpdate_InstalledPlugin(t *testing.T) {
	reg := newMockRegistry(seedEntries())
	factory := NewMarketplaceUpdateStepFactory(reg)

	step, err := factory("update-s3", map[string]any{
		"plugin": "storage-s3",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("update error: %v", err)
	}

	if result.Output["success"] != true {
		t.Errorf("expected success=true, got %v", result.Output["success"])
	}
	if result.Output["status"] != "updated" {
		t.Errorf("expected status=updated, got %v", result.Output["status"])
	}
	pluginMap, ok := result.Output["plugin"].(map[string]any)
	if !ok {
		t.Fatalf("expected plugin map in output")
	}
	if pluginMap["version"] != "99.0.0" {
		t.Errorf("expected updated version 99.0.0, got %v", pluginMap["version"])
	}
}

func TestMarketplaceUpdate_NotInstalled(t *testing.T) {
	reg := newMockRegistry(seedEntries())
	factory := NewMarketplaceUpdateStepFactory(reg)

	step, err := factory("update-not-installed", map[string]any{
		"plugin": "auth-oidc",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, err = step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err == nil {
		t.Fatal("expected error for updating non-installed plugin")
	}
}

func TestMarketplaceUpdate_MissingConfig(t *testing.T) {
	reg := newMockRegistry(seedEntries())
	factory := NewMarketplaceUpdateStepFactory(reg)

	_, err := factory("update-bad", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing plugin config")
	}
}
