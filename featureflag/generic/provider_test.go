package generic_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/GoCodeAlone/workflow/featureflag"
	"github.com/GoCodeAlone/workflow/featureflag/generic"
	_ "modernc.org/sqlite"
)

func setupTestStore(t *testing.T) *generic.Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := generic.NewStoreFromDB(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func setupProvider(t *testing.T) (*generic.Provider, *generic.Store) {
	t.Helper()
	store := setupTestStore(t)
	p := generic.NewProvider(store, nil)
	return p, store
}

func TestEvaluate_DisabledFlag(t *testing.T) {
	p, store := setupProvider(t)
	ctx := context.Background()

	err := store.UpsertFlag(ctx, &generic.FlagRow{
		Key:        "dark-mode",
		Type:       "boolean",
		Enabled:    false,
		DefaultVal: "true",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	val, err := p.Evaluate(ctx, "dark-mode", featureflag.EvaluationContext{UserKey: "user-1"})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if val.Value != true {
		t.Fatalf("expected true (default), got %v", val.Value)
	}
	if val.Reason != "disabled" {
		t.Fatalf("expected reason 'disabled', got %q", val.Reason)
	}
}

func TestEvaluate_UserOverride(t *testing.T) {
	p, store := setupProvider(t)
	ctx := context.Background()

	_ = store.UpsertFlag(ctx, &generic.FlagRow{
		Key: "beta", Type: "boolean", Enabled: true, DefaultVal: "false",
	})
	_ = store.UpsertOverride(ctx, &generic.OverrideRow{
		FlagKey: "beta", Scope: "user", ScopeKey: "alice", Value: "true",
	})

	val, err := p.Evaluate(ctx, "beta", featureflag.EvaluationContext{UserKey: "alice"})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if val.Value != true {
		t.Fatalf("expected true (override), got %v", val.Value)
	}
	if val.Reason != "user_override" {
		t.Fatalf("expected reason 'user_override', got %q", val.Reason)
	}

	// Other user should get default.
	val2, err := p.Evaluate(ctx, "beta", featureflag.EvaluationContext{UserKey: "bob"})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if val2.Value != false {
		t.Fatalf("expected false (default), got %v", val2.Value)
	}
}

func TestEvaluate_GroupOverride(t *testing.T) {
	p, store := setupProvider(t)
	ctx := context.Background()

	_ = store.UpsertFlag(ctx, &generic.FlagRow{
		Key: "new-ui", Type: "boolean", Enabled: true, DefaultVal: "false",
	})
	_ = store.UpsertOverride(ctx, &generic.OverrideRow{
		FlagKey: "new-ui", Scope: "group", ScopeKey: "internal", Value: "true",
	})

	val, err := p.Evaluate(ctx, "new-ui", featureflag.EvaluationContext{
		UserKey:    "user-1",
		Attributes: map[string]string{"groups": "internal,beta"},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if val.Value != true {
		t.Fatalf("expected true (group override), got %v", val.Value)
	}
	if val.Reason != "group_override" {
		t.Fatalf("expected reason 'group_override', got %q", val.Reason)
	}
}

func TestEvaluate_TargetingRule(t *testing.T) {
	p, store := setupProvider(t)
	ctx := context.Background()

	_ = store.UpsertFlag(ctx, &generic.FlagRow{
		Key: "region-feature", Type: "string", Enabled: true, DefaultVal: "standard",
	})
	_ = store.AddRule(ctx, &generic.RuleRow{
		FlagKey: "region-feature", Priority: 1, Attribute: "region", Operator: "eq", Value: "eu", ServeValue: "eu_experience",
	})
	_ = store.AddRule(ctx, &generic.RuleRow{
		FlagKey: "region-feature", Priority: 2, Attribute: "plan", Operator: "in", Value: "pro,enterprise", ServeValue: "premium",
	})

	// Rule 1 matches.
	val, err := p.Evaluate(ctx, "region-feature", featureflag.EvaluationContext{
		UserKey:    "user-eu",
		Attributes: map[string]string{"region": "eu"},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if val.Value != "eu_experience" {
		t.Fatalf("expected 'eu_experience', got %v", val.Value)
	}
	if val.Reason != "targeting_rule" {
		t.Fatalf("expected reason 'targeting_rule', got %q", val.Reason)
	}

	// Rule 2 matches (in operator).
	val2, err := p.Evaluate(ctx, "region-feature", featureflag.EvaluationContext{
		UserKey:    "user-pro",
		Attributes: map[string]string{"plan": "pro"},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if val2.Value != "premium" {
		t.Fatalf("expected 'premium', got %v", val2.Value)
	}

	// No rule matches -> default.
	val3, err := p.Evaluate(ctx, "region-feature", featureflag.EvaluationContext{
		UserKey:    "user-us",
		Attributes: map[string]string{"region": "us", "plan": "free"},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if val3.Value != "standard" {
		t.Fatalf("expected 'standard', got %v", val3.Value)
	}
}

func TestEvaluate_ConditionOperators(t *testing.T) {
	p, store := setupProvider(t)
	ctx := context.Background()

	_ = store.UpsertFlag(ctx, &generic.FlagRow{
		Key: "op-test", Type: "boolean", Enabled: true, DefaultVal: "false",
	})

	tests := []struct {
		name     string
		op       string
		ruleVal  string
		attrKey  string
		attrVal  string
		expected bool
	}{
		{"eq match", "eq", "yes", "x", "yes", true},
		{"eq no match", "eq", "yes", "x", "no", false},
		{"neq match", "neq", "yes", "x", "no", true},
		{"neq no match", "neq", "yes", "x", "yes", false},
		{"in match", "in", "a,b,c", "x", "b", true},
		{"in no match", "in", "a,b,c", "x", "d", false},
		{"contains match", "contains", "ello", "x", "hello", true},
		{"contains no match", "contains", "xyz", "x", "hello", false},
		{"startsWith match", "startsWith", "hel", "x", "hello", true},
		{"startsWith no match", "startsWith", "xyz", "x", "hello", false},
		{"gt match", "gt", "10", "x", "15", true},
		{"gt no match", "gt", "10", "x", "5", false},
		{"lt match", "lt", "10", "x", "5", true},
		{"lt no match", "lt", "10", "x", "15", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Remove existing rules.
			rules, _ := store.GetRules(ctx, "op-test")
			for _, r := range rules {
				_ = store.DeleteRule(ctx, r.ID)
			}

			_ = store.AddRule(ctx, &generic.RuleRow{
				FlagKey: "op-test", Priority: 1, Attribute: tt.attrKey, Operator: tt.op, Value: tt.ruleVal, ServeValue: "true",
			})

			val, err := p.Evaluate(ctx, "op-test", featureflag.EvaluationContext{
				UserKey:    "user",
				Attributes: map[string]string{tt.attrKey: tt.attrVal},
			})
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			got := val.Value.(bool)
			if got != tt.expected {
				t.Fatalf("expected %v, got %v (reason=%s)", tt.expected, got, val.Reason)
			}
		})
	}
}

func TestEvaluate_PercentageRollout(t *testing.T) {
	p, store := setupProvider(t)
	ctx := context.Background()

	_ = store.UpsertFlag(ctx, &generic.FlagRow{
		Key: "rollout", Type: "boolean", Enabled: true, DefaultVal: "true", Percentage: 50,
	})

	// Determinism: same user should always get the same result.
	val1, _ := p.Evaluate(ctx, "rollout", featureflag.EvaluationContext{UserKey: "stable-user"})
	for i := 0; i < 20; i++ {
		val, _ := p.Evaluate(ctx, "rollout", featureflag.EvaluationContext{UserKey: "stable-user"})
		if val.Reason != val1.Reason {
			t.Fatalf("rollout not deterministic: got %q then %q", val1.Reason, val.Reason)
		}
	}

	// Distribution: over many users we should see a mix of in/out.
	inCount := 0
	total := 1000
	for i := 0; i < total; i++ {
		val, _ := p.Evaluate(ctx, "rollout", featureflag.EvaluationContext{
			UserKey: "user-" + string(rune('A'+i%26)) + "-" + itoa(i),
		})
		if val.Reason == "percentage_rollout_in" {
			inCount++
		}
	}
	// Expect roughly 50% +-15% of users to be in the rollout.
	pct := float64(inCount) / float64(total) * 100
	if pct < 35 || pct > 65 {
		t.Fatalf("percentage rollout distribution out of range: %.1f%% (expected ~50%%)", pct)
	}
}

func TestEvaluate_DefaultValue(t *testing.T) {
	p, store := setupProvider(t)
	ctx := context.Background()

	_ = store.UpsertFlag(ctx, &generic.FlagRow{
		Key: "simple", Type: "string", Enabled: true, DefaultVal: "hello",
	})

	val, err := p.Evaluate(ctx, "simple", featureflag.EvaluationContext{UserKey: "anyone"})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if val.Value != "hello" {
		t.Fatalf("expected 'hello', got %v", val.Value)
	}
	if val.Reason != "default" {
		t.Fatalf("expected reason 'default', got %q", val.Reason)
	}
}

func TestEvaluate_FlagTypes(t *testing.T) {
	p, store := setupProvider(t)
	ctx := context.Background()
	evalCtx := featureflag.EvaluationContext{UserKey: "u"}

	tests := []struct {
		flagType   string
		defaultVal string
		expected   any
	}{
		{"boolean", "true", true},
		{"boolean", "false", false},
		{"string", "hello", "hello"},
		{"integer", "42", int64(42)},
		{"float", "3.14", 3.14},
		{"json", `{"a":1}`, map[string]any{"a": float64(1)}},
	}

	for _, tt := range tests {
		t.Run(tt.flagType+"_"+tt.defaultVal, func(t *testing.T) {
			key := "type-" + tt.flagType + "-" + tt.defaultVal
			_ = store.UpsertFlag(ctx, &generic.FlagRow{
				Key: key, Type: tt.flagType, Enabled: true, DefaultVal: tt.defaultVal,
			})

			val, err := p.Evaluate(ctx, key, evalCtx)
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}

			// Compare with type assertion for maps.
			switch expected := tt.expected.(type) {
			case map[string]any:
				got, ok := val.Value.(map[string]any)
				if !ok {
					t.Fatalf("expected map, got %T", val.Value)
				}
				for k, v := range expected {
					if got[k] != v {
						t.Fatalf("map key %q: expected %v, got %v", k, v, got[k])
					}
				}
			default:
				if val.Value != expected {
					t.Fatalf("expected %v (%T), got %v (%T)", expected, expected, val.Value, val.Value)
				}
			}
		})
	}
}

func TestAllFlags(t *testing.T) {
	p, store := setupProvider(t)
	ctx := context.Background()

	_ = store.UpsertFlag(ctx, &generic.FlagRow{Key: "a", Type: "boolean", Enabled: true, DefaultVal: "true"})
	_ = store.UpsertFlag(ctx, &generic.FlagRow{Key: "b", Type: "string", Enabled: true, DefaultVal: "hi"})
	_ = store.UpsertFlag(ctx, &generic.FlagRow{Key: "c", Type: "boolean", Enabled: false, DefaultVal: "false"})

	flags, err := p.AllFlags(ctx, featureflag.EvaluationContext{UserKey: "u"})
	if err != nil {
		t.Fatalf("all flags: %v", err)
	}
	if len(flags) != 3 {
		t.Fatalf("expected 3 flags, got %d", len(flags))
	}
}

func TestProviderName(t *testing.T) {
	p, _ := setupProvider(t)
	if p.Name() != "generic" {
		t.Fatalf("expected name 'generic', got %q", p.Name())
	}
}

func TestSubscribeAndNotify(t *testing.T) {
	p, _ := setupProvider(t)

	var received featureflag.FlagChangeEvent
	cancel := p.Subscribe(func(evt featureflag.FlagChangeEvent) {
		received = evt
	})
	defer cancel()

	p.NotifyChange("test-flag", true, featureflag.FlagTypeBoolean)

	if received.Key != "test-flag" {
		t.Fatalf("expected key 'test-flag', got %q", received.Key)
	}
	if received.Source != "generic" {
		t.Fatalf("expected source 'generic', got %q", received.Source)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create flag.
	err := store.UpsertFlag(ctx, &generic.FlagRow{
		Key: "test", Type: "boolean", Description: "a test flag", Enabled: true,
		DefaultVal: "true", Tags: []string{"testing"}, Percentage: 25,
	})
	if err != nil {
		t.Fatalf("upsert flag: %v", err)
	}

	// Read it back.
	f, err := store.GetFlag(ctx, "test")
	if err != nil {
		t.Fatalf("get flag: %v", err)
	}
	if f.Key != "test" || f.Type != "boolean" || f.Description != "a test flag" {
		t.Fatalf("unexpected flag: %+v", f)
	}
	if !f.Enabled || f.DefaultVal != "true" || f.Percentage != 25 {
		t.Fatalf("unexpected flag values: %+v", f)
	}
	if len(f.Tags) != 1 || f.Tags[0] != "testing" {
		t.Fatalf("unexpected tags: %v", f.Tags)
	}

	// Add a rule.
	rule := &generic.RuleRow{
		FlagKey: "test", Priority: 1, Attribute: "env", Operator: "eq", Value: "prod", ServeValue: "false",
	}
	if err := store.AddRule(ctx, rule); err != nil {
		t.Fatalf("add rule: %v", err)
	}
	rules, err := store.GetRules(ctx, "test")
	if err != nil {
		t.Fatalf("get rules: %v", err)
	}
	if len(rules) != 1 || rules[0].Operator != "eq" {
		t.Fatalf("unexpected rules: %+v", rules)
	}

	// Add an override.
	if err := store.UpsertOverride(ctx, &generic.OverrideRow{
		FlagKey: "test", Scope: "user", ScopeKey: "admin", Value: "false",
	}); err != nil {
		t.Fatalf("upsert override: %v", err)
	}
	overrides, err := store.GetOverrides(ctx, "test")
	if err != nil {
		t.Fatalf("get overrides: %v", err)
	}
	if len(overrides) != 1 || overrides[0].ScopeKey != "admin" {
		t.Fatalf("unexpected overrides: %+v", overrides)
	}

	// Delete flag (cascade should remove rules and overrides).
	if err := store.DeleteFlag(ctx, "test"); err != nil {
		t.Fatalf("delete flag: %v", err)
	}
	_, err = store.GetFlag(ctx, "test")
	if err == nil {
		t.Fatalf("expected error after delete, got nil")
	}
}

// itoa converts an int to a string without importing strconv in the test.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}
