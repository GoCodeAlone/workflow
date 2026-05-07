package main

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ── parseIncludeFlag ──────────────────────────────────────────────────────────

func TestParseIncludeFlag_Empty_ReturnsNil(t *testing.T) {
	if got := parseIncludeFlag(""); got != nil {
		t.Errorf("empty include should yield nil; got %v", got)
	}
}

func TestParseIncludeFlag_SingleName(t *testing.T) {
	got := parseIncludeFlag("res-A")
	if len(got) != 1 {
		t.Fatalf("expected 1 entry; got %v", got)
	}
	if _, ok := got["res-A"]; !ok {
		t.Errorf("expected 'res-A' in set; got %v", got)
	}
}

func TestParseIncludeFlag_MultipleNames(t *testing.T) {
	got := parseIncludeFlag("res-A,res-B,res-C")
	if len(got) != 3 {
		t.Fatalf("expected 3 entries; got %v", got)
	}
	for _, name := range []string{"res-A", "res-B", "res-C"} {
		if _, ok := got[name]; !ok {
			t.Errorf("expected %q in set; got %v", name, got)
		}
	}
}

func TestParseIncludeFlag_TrimsWhitespace(t *testing.T) {
	got := parseIncludeFlag(" res-A , res-B ")
	if len(got) != 2 {
		t.Fatalf("expected 2 entries; got %v", got)
	}
	if _, ok := got["res-A"]; !ok {
		t.Errorf("expected trimmed 'res-A'; got %v", got)
	}
}

func TestParseIncludeFlag_AllWhitespace_ReturnsNil(t *testing.T) {
	if got := parseIncludeFlag("  , ,  "); got != nil {
		t.Errorf("all-whitespace include should yield nil; got %v", got)
	}
}

// ── validateIncludeSet ────────────────────────────────────────────────────────

func TestValidateIncludeSet_NilInclude_NoError(t *testing.T) {
	if err := validateIncludeSet(nil, nil, nil); err != nil {
		t.Errorf("nil include should not error; got %v", err)
	}
}

func TestValidateIncludeSet_KnownSpec_NoError(t *testing.T) {
	specs := []interfaces.ResourceSpec{{Name: "res-A"}}
	include := parseIncludeFlag("res-A")
	if err := validateIncludeSet(include, specs, nil); err != nil {
		t.Errorf("known spec should not error; got %v", err)
	}
}

func TestValidateIncludeSet_KnownState_NoError(t *testing.T) {
	states := []interfaces.ResourceState{{Name: "res-B"}}
	include := parseIncludeFlag("res-B")
	if err := validateIncludeSet(include, nil, states); err != nil {
		t.Errorf("known state-only resource should not error; got %v", err)
	}
}

func TestValidateIncludeSet_UnknownName_FailsFast(t *testing.T) {
	specs := []interfaces.ResourceSpec{{Name: "res-A"}}
	include := parseIncludeFlag("res-X")
	err := validateIncludeSet(include, specs, nil)
	if err == nil {
		t.Fatal("expected error for unknown include name")
	}
	if !strings.Contains(err.Error(), "res-X") {
		t.Errorf("error should name the unknown resource; got %v", err)
	}
}

func TestValidateIncludeSet_MultipleUnknown_ListsAll(t *testing.T) {
	include := parseIncludeFlag("res-X,res-Y")
	err := validateIncludeSet(include, nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown names")
	}
	if !strings.Contains(err.Error(), "res-X") || !strings.Contains(err.Error(), "res-Y") {
		t.Errorf("error should list all unknown names; got %v", err)
	}
}

// ── filterSpecsByInclude ──────────────────────────────────────────────────────

func TestFilterSpecsByInclude_NilInclude_ReturnsAll(t *testing.T) {
	specs := []interfaces.ResourceSpec{{Name: "res-A"}, {Name: "res-B"}}
	got := filterSpecsByInclude(specs, nil)
	if len(got) != 2 {
		t.Errorf("nil include should return all specs; got %v", got)
	}
}

func TestFilterSpecsByInclude_FiltersToSubset(t *testing.T) {
	specs := []interfaces.ResourceSpec{
		{Name: "res-A"}, {Name: "res-B"}, {Name: "res-C"},
	}
	include := parseIncludeFlag("res-A,res-C")
	got := filterSpecsByInclude(specs, include)
	if len(got) != 2 {
		t.Fatalf("expected 2 filtered specs; got %v", got)
	}
	names := map[string]bool{got[0].Name: true, got[1].Name: true}
	if !names["res-A"] || !names["res-C"] {
		t.Errorf("expected res-A and res-C; got %v", got)
	}
}

// ── filterStatesByInclude ─────────────────────────────────────────────────────

func TestFilterStatesByInclude_NilInclude_ReturnsAll(t *testing.T) {
	states := []interfaces.ResourceState{{Name: "res-A"}, {Name: "res-B"}}
	got := filterStatesByInclude(states, nil)
	if len(got) != 2 {
		t.Errorf("nil include should return all states; got %v", got)
	}
}

func TestFilterStatesByInclude_FiltersToSubset(t *testing.T) {
	states := []interfaces.ResourceState{
		{Name: "res-A"}, {Name: "res-B"}, {Name: "res-C"},
	}
	include := parseIncludeFlag("res-B")
	got := filterStatesByInclude(states, include)
	if len(got) != 1 || got[0].Name != "res-B" {
		t.Errorf("expected [res-B]; got %v", got)
	}
}

// ── Integration: --include flag CLI end-to-end ────────────────────────────────

// TestApplyInclude_RejectedWithPlanFlag verifies that --include + --plan is
// rejected at flag-parse time with a clear error message.
func TestApplyInclude_RejectedWithPlanFlag(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeRefreshFlagTestConfig(t, dir)

	err := runInfraApply([]string{"--auto-approve", "--include=res-A", "--plan", "plan.json", "-c", cfgPath})
	if err == nil {
		t.Fatal("expected error for --include + --plan combination")
	}
	if !strings.Contains(err.Error(), "--include cannot be combined with --plan") {
		t.Errorf("expected --include + --plan rejection message; got %v", err)
	}
}

// TestApplyInclude_EmptyFlagMeansAll verifies that --include= (empty) yields
// nil (back-compat all-resources behavior).
func TestApplyInclude_EmptyFlagMeansAll(t *testing.T) {
	includeSet := parseIncludeFlag("")
	if includeSet != nil {
		t.Errorf("empty include should yield nil; got %v", includeSet)
	}
}

// TestApplyInclude_FlagRegistered verifies the flag is registered in runInfraApply
// (not "flag provided but not defined").
func TestApplyInclude_FlagRegistered(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeRefreshFlagTestConfig(t, dir)
	seedVPCStateForRefreshFlag(t, cfgPath)

	cleanup := installFakeRefreshProvider(t, map[string]map[string]any{})
	defer cleanup()

	// --include=vpc-resource is valid (it's in config/state).
	// We accept any non-"flag not defined" outcome.
	_, err := captureStdout(t, func() error {
		return runInfraApply([]string{"--auto-approve", "--include=vpc-resource", "-c", cfgPath})
	})
	if err != nil && strings.Contains(err.Error(), "flag provided but not defined: -include") {
		t.Errorf("--include flag not registered: %v", err)
	}
}

// TestApplyInclude_UnknownResourceFails verifies that --include with a name
// not in config or state fails fast with a descriptive error.
func TestApplyInclude_UnknownResourceFails(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeRefreshFlagTestConfig(t, dir)
	seedVPCStateForRefreshFlag(t, cfgPath)

	cleanup := installFakeRefreshProvider(t, map[string]map[string]any{})
	defer cleanup()

	_, err := captureStdout(t, func() error {
		return runInfraApply([]string{"--auto-approve", "--include=does-not-exist", "-c", cfgPath})
	})
	if err == nil {
		t.Fatal("expected error for unknown --include resource name")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should name the unknown resource; got %v", err)
	}
}
