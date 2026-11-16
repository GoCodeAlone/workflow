package module

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func credentialGrant() interfaces.StepCredentialGrant {
	return interfaces.StepCredentialGrant{Plugin: "test-plugin", StepType: "step.capture", StepName: "capture", Field: "auth_token_ref", Ref: "config:compute.token", Scope: "application"}
}

func TestStepCredentialGrants(t *testing.T) {
	t.Cleanup(GetConfigRegistry().Reset)
	GetConfigRegistry().Reset()
	_ = GetConfigRegistry().Set("compute.token", "dummy-global-fallback", true)
	_ = GetConfigRegistry().Set("missing", "dummy-global-fallback", true)
	GetConfigRegistry().Freeze()
	g := credentialGrant()
	t.Run("strict decode", func(t *testing.T) {
		valid := map[string]any{"plugin": g.Plugin, "step_type": g.StepType, "step_name": g.StepName, "field": g.Field, "ref": g.Ref, "scope": g.Scope}
		if grants, err := DecodeStepCredentialGrants([]any{valid}); err != nil || len(grants) != 1 || grants[0] != g {
			t.Fatal("valid grant rejected")
		}
		valid["sensitive"] = true
		if _, err := DecodeStepCredentialGrants([]any{valid}); err == nil {
			t.Fatal("unknown field accepted")
		}
		for _, raw := range []any{nil, "not a list", map[string]any{}, []any{nil}, []any{"bad"}, []any{map[string]any{"plugin": 1}}, []any{g, g}} {
			if _, err := DecodeStepCredentialGrants(raw); err == nil {
				t.Fatal("malformed grant accepted")
			}
		}
		if grants, err := DecodeStepCredentialGrants([]any{}); err != nil || len(grants) != 0 {
			t.Fatal("empty grants rejected")
		}
	})
	t.Run("invalid authority", func(t *testing.T) {
		for _, change := range []func(*interfaces.StepCredentialGrant){
			func(g *interfaces.StepCredentialGrant) { g.Plugin = "" },
			func(g *interfaces.StepCredentialGrant) { g.StepType = "" },
			func(g *interfaces.StepCredentialGrant) { g.StepName = "" },
			func(g *interfaces.StepCredentialGrant) { g.Scope = "tenant" },
			func(g *interfaces.StepCredentialGrant) { g.Scope = "" },
			func(g *interfaces.StepCredentialGrant) { g.Field = "nested.ref" },
			func(g *interfaces.StepCredentialGrant) { g.Ref = "secret:key" },
			func(g *interfaces.StepCredentialGrant) { g.Ref = "" },
			func(g *interfaces.StepCredentialGrant) { g.Ref = "config:" },
			func(g *interfaces.StepCredentialGrant) { g.Ref = "config:bad..key" },
			func(g *interfaces.StepCredentialGrant) { g.Ref = "config: key" },
			func(g *interfaces.StepCredentialGrant) { g.Ref = `{{config "alias"}}` },
			func(g *interfaces.StepCredentialGrant) { g.Ref = `${ config("alias") }` },
		} {
			bad := g
			change(&bad)
			if _, err := DecodeStepCredentialGrants([]interfaces.StepCredentialGrant{bad}); err == nil {
				t.Fatal("invalid authority accepted")
			}
		}
		for _, change := range []func(*interfaces.StepCredentialGrant){
			func(g *interfaces.StepCredentialGrant) { g.Ref = "config:other" },
			func(g *interfaces.StepCredentialGrant) { g.Plugin = "other-plugin" },
			func(g *interfaces.StepCredentialGrant) { g.Field = "another_ref" },
		} {
			bad := g
			change(&bad)
			if _, err := DecodeStepCredentialGrants([]interfaces.StepCredentialGrant{g, bad}); err == nil {
				t.Fatal("ambiguous grants accepted")
			}
		}
	})
	r := NewConfigRegistry()
	_ = r.Set("compute.token", "dummy-private", false)
	_ = r.Set("server_url", "http://app-a", false)
	_ = r.Set("ungranted_secret", "dummy-other", true)
	r.Freeze()
	grants := []interfaces.StepCredentialGrant{g}
	binder, err := NewStepCredentialBinder(r, grants)
	if err != nil {
		t.Fatal(err)
	}
	grants[0].Ref = "config:ungranted_secret"
	target := interfaces.StepCredentialTarget{Plugin: g.Plugin, StepType: g.StepType, StepName: g.StepName}
	t.Run("exact target copied ref", func(t *testing.T) {
		cfg := map[string]any{g.Field: g.Ref}
		bound, err := binder.BindStep(target, cfg)
		if err != nil || bound == nil {
			t.Fatal("binding failed")
		}
		cfg[g.Field] = "config:ungranted_secret"
		if err := bound.ValidateConfig(cfg); err == nil {
			t.Fatal("raw mutation accepted by validator")
		}
		resolved, err := bound.Resolve(context.Background())
		if err != nil || len(resolved) != 1 || resolved[0].Ref != g.Ref || resolved[0].Value != "dummy-private" {
			t.Fatal("binding did not retain exact original grant")
		}
		resolved[0].Value = "mutated"
		again, err := bound.Resolve(context.Background())
		if err != nil || again[0].Value != "dummy-private" {
			t.Fatal("result mutation affected binding")
		}
		if value, ok := bound.ConfigLookup("server_url"); !ok || value != "http://app-a" {
			t.Fatal("wrong config lookup")
		}
		if _, ok := bound.ConfigLookup("missing"); ok {
			t.Fatal("unexpected missing value")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if values, err := bound.Resolve(ctx); !errors.Is(err, context.Canceled) || values != nil {
			t.Fatal("cancellation did not fail closed")
		}
		if values, err := bound.Resolve(nil); err == nil || values != nil {
			t.Fatal("nil context accepted")
		}
	})
	t.Run("foreign targets", func(t *testing.T) {
		for _, foreign := range []interfaces.StepCredentialTarget{
			{Plugin: "other", StepType: g.StepType, StepName: g.StepName},
			{Plugin: g.Plugin, StepType: "other", StepName: g.StepName},
			{Plugin: g.Plugin, StepType: g.StepType, StepName: "other"},
		} {
			if bound, err := binder.BindStep(foreign, nil); err != nil || bound != nil {
				t.Fatal("foreign target got binding")
			}
		}
	})
	t.Run("wrong construction refs", func(t *testing.T) {
		for _, value := range []any{nil, 1, "", "config:other", `{{config "alias"}}`, `${ config("alias") }`} {
			if bound, err := binder.BindStep(target, map[string]any{g.Field: value}); err == nil || bound != nil {
				t.Fatal("wrong ref accepted")
			}
		}
	})
	t.Run("validate all bound literals", func(t *testing.T) {
		second := g
		second.Field, second.Ref = "other_ref", "config:other.token"
		b, err := NewStepCredentialBinder(r, []interfaces.StepCredentialGrant{g, second})
		if err != nil {
			t.Fatal(err)
		}
		cfg := map[string]any{g.Field: g.Ref, second.Field: second.Ref}
		bound, err := b.BindStep(target, cfg)
		if err != nil || bound == nil || bound.ValidateConfig == nil {
			t.Fatal("binding must expose literal validator")
		}
		if err := bound.ValidateConfig(cfg); err != nil {
			t.Fatal("original literals rejected")
		}
		for _, field := range []string{g.Field, second.Field} {
			for _, bad := range []any{nil, 1, "", "config:ungranted_secret", `{{config "alias"}}`, `${ config("alias") }`} {
				changed := map[string]any{g.Field: g.Ref, second.Field: second.Ref}
				changed[field] = bad
				if err := bound.ValidateConfig(changed); err == nil || err.Error() != "step credential literal does not match grant" {
					t.Fatal("changed literal did not fail with safe error")
				}
			}
		}
		if err := bound.ValidateConfig(nil); err == nil {
			t.Fatal("missing fields accepted")
		}
		delete(cfg, g.Field)
		if err := bound.ValidateConfig(cfg); err == nil {
			t.Fatal("deleted field accepted")
		}
	})
	t.Run("missing and empty source", func(t *testing.T) {
		for _, empty := range []bool{false, true} {
			source := NewConfigRegistry()
			if empty {
				_ = source.Set("compute.token", "", false)
			}
			source.Freeze()
			b, err := NewStepCredentialBinder(source, []interfaces.StepCredentialGrant{g})
			if err != nil {
				t.Fatal(err)
			}
			bound, err := b.BindStep(target, map[string]any{g.Field: g.Ref})
			if err != nil {
				t.Fatal(err)
			}
			if values, err := bound.Resolve(context.Background()); err == nil || values != nil {
				t.Fatal("missing/empty source accepted")
			}
		}
	})
	t.Run("private frozen source required", func(t *testing.T) {
		for _, source := range []*ConfigRegistry{nil, GetConfigRegistry(), NewConfigRegistry()} {
			if _, err := NewStepCredentialBinder(source, []interfaces.StepCredentialGrant{g}); err == nil {
				t.Fatal("invalid source accepted")
			}
		}
	})
}
