package pipeline

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestScopedConfigLookupTemplate(t *testing.T) {
	previous := ConfigLookup
	t.Cleanup(func() { ConfigLookup = previous })
	ConfigLookup = func(string) (string, bool) { return "global-b", true }
	lookup := func(key string) (string, bool) { return "private-a", key == "url" }
	pc := &interfaces.PipelineContext{Current: map[string]any{"config": "spoof"}}
	for _, engine := range []*TemplateEngine{NewTemplateEngine(), {}} {
		got, err := engine.Resolve(`{{ config "url" }}`, pc)
		if err != nil || got != "global-b" {
			t.Fatal("global/zero template behavior changed")
		}
	}
	for _, source := range []string{`{{ config "url" | default "" }}/tasks`, `${ config("url") }/tasks`, `{{ upper (config "url") }}`} {
		want := "private-a/tasks"
		if strings.Contains(source, "upper") {
			want = "PRIVATE-A"
		}
		got, err := NewTemplateEngineWithConfigLookup(lookup).Resolve(source, pc)
		if err != nil || got != want {
			t.Fatal("private template source not used")
		}
	}
	for _, engine := range []*TemplateEngine{NewTemplateEngineWithConfigLookup(lookup), NewTemplateEngineWithConfigLookup(nil)} {
		for _, source := range []string{`{{ config "missing" }}`, `${ config("missing") }`} {
			got, err := engine.Resolve(source, pc)
			if err != nil || got != "" {
				t.Fatal("missing private key fell back globally")
			}
		}
	}
}

func TestScopedConfigLookupTemplateWarnings(t *testing.T) {
	previous := ConfigLookup
	t.Cleanup(func() { ConfigLookup = previous })
	ConfigLookup = func(string) (string, bool) { return "dummy_private_a", true }
	var logs bytes.Buffer
	pc := &interfaces.PipelineContext{Current: map[string]any{}, Metadata: map[string]any{"pipeline": "dummy_private_a"}, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	source := `{{ .${ config("token") } }}`
	if _, err := NewTemplateEngineWithConfigLookup(ConfigLookup).Resolve(source, pc); err != nil {
		t.Fatal("scoped warning fixture failed")
	}
	if !strings.Contains(logs.String(), "WARN") || strings.Contains(logs.String(), "dummy_private_a") || strings.Contains(logs.String(), `"error"`) || strings.Contains(logs.String(), `"pipeline"`) {
		t.Fatal("scoped warning leaked derived data")
	}
	logs.Reset()
	if _, err := NewTemplateEngine().Resolve(source, pc); err != nil {
		t.Fatal("legacy warning fixture failed")
	}
	if !strings.Contains(logs.String(), "WARN") || !strings.Contains(logs.String(), "dummy_private_a") {
		t.Fatal("legacy diagnostics changed")
	}
}
