package admincore

import (
	"net/http"
	"testing"

	"github.com/GoCodeAlone/workflow/plugin"
)

func pluginContext() plugin.PluginContext {
	return plugin.PluginContext{}
}

func TestPlugin_Metadata(t *testing.T) {
	t.Parallel()

	p := &Plugin{}

	if p.Name() != "admin-core" {
		t.Errorf("expected name 'admin-core', got %q", p.Name())
	}
	if p.Version() != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", p.Version())
	}
	if p.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestPlugin_Dependencies(t *testing.T) {
	t.Parallel()

	p := &Plugin{}
	deps := p.Dependencies()
	if deps != nil {
		t.Errorf("expected nil dependencies, got %v", deps)
	}
}

func TestPlugin_Lifecycle(t *testing.T) {
	t.Parallel()

	p := &Plugin{}

	// OnEnable and OnDisable should be no-ops
	if err := p.OnEnable(pluginContext()); err != nil {
		t.Errorf("OnEnable error: %v", err)
	}
	if err := p.OnDisable(pluginContext()); err != nil {
		t.Errorf("OnDisable error: %v", err)
	}
}

func TestPlugin_RegisterRoutes(t *testing.T) {
	t.Parallel()

	p := &Plugin{}
	mux := http.NewServeMux()

	// RegisterRoutes should not panic even though it's a no-op
	p.RegisterRoutes(mux)
}

func TestPlugin_UIPages(t *testing.T) {
	t.Parallel()

	p := &Plugin{}
	pages := p.UIPages()

	if len(pages) == 0 {
		t.Fatal("expected UI pages to be returned")
	}

	// Verify expected pages exist
	expectedIDs := map[string]bool{
		"dashboard":    false,
		"editor":       false,
		"marketplace":  false,
		"templates":    false,
		"environments": false,
		"settings":     false,
		"executions":   false,
		"logs":         false,
		"events":       false,
	}

	for _, page := range pages {
		if _, ok := expectedIDs[page.ID]; ok {
			expectedIDs[page.ID] = true
		} else {
			t.Errorf("unexpected page ID: %q", page.ID)
		}

		if page.Label == "" {
			t.Errorf("page %q has empty label", page.ID)
		}
		if page.Icon == "" {
			t.Errorf("page %q has empty icon", page.ID)
		}
		if page.Category == "" {
			t.Errorf("page %q has empty category", page.ID)
		}
		if page.Category != "global" && page.Category != "workflow" {
			t.Errorf("page %q has unexpected category %q", page.ID, page.Category)
		}
	}

	for id, found := range expectedIDs {
		if !found {
			t.Errorf("expected page %q not found", id)
		}
	}
}

func TestPlugin_UIPages_GlobalVsWorkflow(t *testing.T) {
	t.Parallel()

	p := &Plugin{}
	pages := p.UIPages()

	globalCount := 0
	workflowCount := 0
	for _, page := range pages {
		switch page.Category {
		case "global":
			globalCount++
		case "workflow":
			workflowCount++
		}
	}

	if globalCount == 0 {
		t.Error("expected global pages")
	}
	if workflowCount == 0 {
		t.Error("expected workflow-scoped pages")
	}
}
