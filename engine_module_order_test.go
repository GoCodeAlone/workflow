package workflow

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func mods(names ...string) []config.ModuleConfig {
	out := make([]config.ModuleConfig, len(names))
	for i, n := range names {
		out[i] = config.ModuleConfig{Name: n, Type: "test.noop"}
	}
	return out
}

func withDeps(m config.ModuleConfig, deps ...string) config.ModuleConfig {
	m.DependsOn = append(m.DependsOn, deps...)
	return m
}

func order(modules []config.ModuleConfig) []string {
	out := make([]string, len(modules))
	for i, m := range modules {
		out[i] = m.Name
	}
	return out
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTopoSortModules_NoDeps_PreservesOrder(t *testing.T) {
	in := mods("c", "a", "b")
	out, err := topoSortModules(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := order(out), []string{"c", "a", "b"}; !equalSlice(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestTopoSortModules_SingleChain(t *testing.T) {
	// Declared C, B, A but B depends on A, C depends on B.
	// Engine must init A, then B, then C.
	in := []config.ModuleConfig{
		withDeps(config.ModuleConfig{Name: "c", Type: "x"}, "b"),
		withDeps(config.ModuleConfig{Name: "b", Type: "x"}, "a"),
		{Name: "a", Type: "x"},
	}
	out, err := topoSortModules(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := order(out), []string{"a", "b", "c"}; !equalSlice(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestTopoSortModules_BMWShape(t *testing.T) {
	// Mirrors the BMW PR 279 shape: broker first, stream depends on broker,
	// six consumers depend on broker + stream. Original declared order had
	// the consumers BEFORE the broker (which is exactly why BMW had to
	// rename the broker to `aaa-` to force alphabetical init).
	in := []config.ModuleConfig{
		withDeps(config.ModuleConfig{Name: "bmw-consumer-audit", Type: "eventbus.consumer"}, "bmw-eventbus", "bmw-stream"),
		withDeps(config.ModuleConfig{Name: "bmw-consumer-settle", Type: "eventbus.consumer"}, "bmw-eventbus", "bmw-stream"),
		{Name: "bmw-eventbus", Type: "eventbus.broker"},
		withDeps(config.ModuleConfig{Name: "bmw-stream", Type: "eventbus.stream"}, "bmw-eventbus"),
	}
	out, err := topoSortModules(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := order(out)
	// Broker first, then stream, then consumers (in declared order).
	want := []string{"bmw-eventbus", "bmw-stream", "bmw-consumer-audit", "bmw-consumer-settle"}
	if !equalSlice(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestTopoSortModules_ParallelChains(t *testing.T) {
	// Two independent chains: a→b and x→y. Declared order is mixed; siblings
	// should keep declared order via stable tie-break.
	in := []config.ModuleConfig{
		withDeps(config.ModuleConfig{Name: "b", Type: "x"}, "a"),
		withDeps(config.ModuleConfig{Name: "y", Type: "x"}, "x"),
		{Name: "a", Type: "x"},
		{Name: "x", Type: "x"},
	}
	out, err := topoSortModules(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := order(out)
	// Stable tie-break by original declared index. After roots a (idx 2) and
	// x (idx 3) are processed, b (idx 0) becomes ready before y (idx 1) is
	// promoted (b's predecessor a was popped first). So the ready frontier
	// after popping a is [x, b] — but b's declared index (0) wins the next
	// pop. The output is the declared order with dependencies hoisted just
	// far enough to satisfy each edge.
	want := []string{"a", "b", "x", "y"}
	if !equalSlice(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestTopoSortModules_AlreadyCorrect_IsNoOp(t *testing.T) {
	in := []config.ModuleConfig{
		{Name: "a", Type: "x"},
		withDeps(config.ModuleConfig{Name: "b", Type: "x"}, "a"),
		withDeps(config.ModuleConfig{Name: "c", Type: "x"}, "b"),
	}
	out, err := topoSortModules(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := order(out), []string{"a", "b", "c"}; !equalSlice(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestTopoSortModules_MissingDepIsTolerated(t *testing.T) {
	// schema.ValidateConfig catches missing-dep refs separately. topoSortModules
	// should not panic or stall when DependsOn points at something not in the
	// list — it just ignores the unresolvable edge.
	in := []config.ModuleConfig{
		withDeps(config.ModuleConfig{Name: "b", Type: "x"}, "ghost"),
		{Name: "a", Type: "x"},
	}
	out, err := topoSortModules(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := order(out)
	want := []string{"b", "a"}
	if !equalSlice(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestTopoSortModules_DirectCycleDetected(t *testing.T) {
	in := []config.ModuleConfig{
		withDeps(config.ModuleConfig{Name: "a", Type: "x"}, "b"),
		withDeps(config.ModuleConfig{Name: "b", Type: "x"}, "a"),
	}
	_, err := topoSortModules(in)
	if err == nil {
		t.Fatalf("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error %q does not mention cycle", err.Error())
	}
	if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
		t.Errorf("error %q does not include unordered modules", err.Error())
	}
}

func TestTopoSortModules_CycleErrorListsDependentsToo(t *testing.T) {
	// a→b, b→a forms a 2-cycle; c depends on a (so c is a downstream dependent
	// of the cycle, not itself a member). Kahn's algorithm cannot distinguish
	// strict cycle members from dependents using inDegree alone, so the error
	// names everything that could not be ordered — and the docstring + commit
	// message both flag this. Pin the behaviour so a future "tighten to true
	// cycle members" refactor (e.g., Tarjan SCC) is a conscious decision and
	// not a silent regression.
	in := []config.ModuleConfig{
		withDeps(config.ModuleConfig{Name: "a", Type: "x"}, "b"),
		withDeps(config.ModuleConfig{Name: "b", Type: "x"}, "a"),
		withDeps(config.ModuleConfig{Name: "c", Type: "x"}, "a"),
	}
	_, err := topoSortModules(in)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	msg := err.Error()
	for _, name := range []string{"a", "b", "c"} {
		if !strings.Contains(msg, name) {
			t.Errorf("error %q missing unordered module %q", msg, name)
		}
	}
}

func TestTopoSortModules_IndirectCycleDetected(t *testing.T) {
	in := []config.ModuleConfig{
		withDeps(config.ModuleConfig{Name: "a", Type: "x"}, "c"),
		withDeps(config.ModuleConfig{Name: "b", Type: "x"}, "a"),
		withDeps(config.ModuleConfig{Name: "c", Type: "x"}, "b"),
	}
	_, err := topoSortModules(in)
	if err == nil {
		t.Fatalf("expected cycle error, got nil")
	}
	for _, want := range []string{"a", "b", "c"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("cycle error %q missing module %q", err.Error(), want)
		}
	}
}

func TestTopoSortModules_EmptyAndSingleton(t *testing.T) {
	out, err := topoSortModules(nil)
	if err != nil {
		t.Fatalf("nil input: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("nil input produced %d modules", len(out))
	}

	one := mods("solo")
	out, err = topoSortModules(one)
	if err != nil {
		t.Fatalf("singleton input: %v", err)
	}
	if got, want := order(out), []string{"solo"}; !equalSlice(got, want) {
		t.Errorf("singleton order = %v, want %v", got, want)
	}
}

func TestTopoSortModules_DuplicateNames_FirstWins(t *testing.T) {
	// Schema rejects duplicates, but ConfigTransformHooks may merge fragments
	// that re-declare a name. The dependent should resolve against the first
	// declared instance (index 0) — not silently shadow to the last (which
	// would compute a different inDegree for the duplicate and could mis-order
	// downstream modules).
	in := []config.ModuleConfig{
		{Name: "a", Type: "x"},
		withDeps(config.ModuleConfig{Name: "b", Type: "x"}, "a"),
		{Name: "a", Type: "x"}, // duplicate; should be treated as already-resolved root
	}
	out, err := topoSortModules(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := order(out)
	// Both 'a' entries are roots (inDegree 0). 'b' depends on the *first*
	// occurrence of 'a'. Output should preserve the declared order of roots
	// + put 'b' after the first 'a'.
	want := []string{"a", "b", "a"}
	if !equalSlice(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestFilterResolvableDeps_DropsEmptyAndUnknown(t *testing.T) {
	names := map[string]struct{}{"a": {}, "b": {}, "c": {}}
	got := filterResolvableDeps([]string{"a", "", "ghost", "b", "phantom", "c"}, names)
	want := []string{"a", "b", "c"}
	if !equalSlice(got, want) {
		t.Errorf("filterResolvableDeps = %v, want %v", got, want)
	}
}

func TestFilterResolvableDeps_AllUnknownReturnsEmpty(t *testing.T) {
	names := map[string]struct{}{"only-this": {}}
	got := filterResolvableDeps([]string{"x", "y", "z", ""}, names)
	if len(got) != 0 {
		t.Errorf("filterResolvableDeps all-unknown = %v, want empty", got)
	}
}

func TestFilterResolvableDeps_EmptyInputEmptyOutput(t *testing.T) {
	got := filterResolvableDeps(nil, map[string]struct{}{"a": {}})
	if len(got) != 0 {
		t.Errorf("filterResolvableDeps(nil) = %v, want empty", got)
	}
}

func TestFilterResolvableDeps_PreservesOrder(t *testing.T) {
	// Order in input must be preserved in output.
	names := map[string]struct{}{"a": {}, "b": {}, "c": {}}
	got := filterResolvableDeps([]string{"c", "a", "b"}, names)
	want := []string{"c", "a", "b"}
	if !equalSlice(got, want) {
		t.Errorf("filterResolvableDeps = %v, want %v (order preserved)", got, want)
	}
}

func TestTopoSortModules_EmptyDependencyStringIgnored(t *testing.T) {
	// Schema validation rejects "" entries, but defensively the sort should
	// not blow up if one slips through (e.g., from a hand-built ModuleConfig
	// in a test or programmatic construction).
	in := []config.ModuleConfig{
		withDeps(config.ModuleConfig{Name: "b", Type: "x"}, "", "a"),
		{Name: "a", Type: "x"},
	}
	out, err := topoSortModules(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := order(out), []string{"a", "b"}; !equalSlice(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
