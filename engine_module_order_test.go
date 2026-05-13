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
		t.Errorf("error %q does not include cycle members", err.Error())
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
