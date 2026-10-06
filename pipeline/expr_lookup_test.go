package pipeline

import (
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestScopedConfigLookupExpr(t *testing.T) {
	previous := ConfigLookup
	t.Cleanup(func() { ConfigLookup = previous })
	ConfigLookup = func(string) (string, bool) { return "global-b", true }
	lookup := func(key string) (string, bool) { return "private-a", key == "url" }
	pc := &interfaces.PipelineContext{Current: map[string]any{}}
	for _, engine := range []*ExprEngine{NewExprEngine(), {}} {
		got, err := engine.Evaluate(`config("url")`, pc)
		if err != nil || got != "global-b" {
			t.Fatal("global/zero expression behavior changed")
		}
	}
	pc.Current["config"] = func(string) string { return "spoof" }
	got, err := NewExprEngine().Evaluate(`config("url")`, pc)
	if err != nil || got != "spoof" {
		t.Fatal("legacy Current function precedence changed")
	}
	for _, spoof := range []any{"spoof", func(string) string { return "spoof" }, nil} {
		pc.Current["config"] = spoof
		got, err := NewExprEngineWithConfigLookup(lookup).Evaluate(`config("url") + "/tasks"`, pc)
		if err != nil || got != "private-a/tasks" {
			t.Fatal("Current shadowed private config helper")
		}
	}
	for _, engine := range []*ExprEngine{NewExprEngineWithConfigLookup(lookup), NewExprEngineWithConfigLookup(nil)} {
		got, err := engine.Evaluate(`config("missing")`, pc)
		if err != nil || got != "" {
			t.Fatal("missing private key fell back globally")
		}
	}
}
