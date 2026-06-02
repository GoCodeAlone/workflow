package module_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

// stubDriftImportProvider extends stubIaCProvider with controllable Import and
// DetectDrift responses for reconcile tests.
type stubDriftImportProvider struct {
	stubIaCProvider
	importResult *interfaces.ResourceState
	importErr    error
}

func (s *stubDriftImportProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return s.importResult, s.importErr
}

// compile-time check
var _ interfaces.IaCProvider = (*stubDriftImportProvider)(nil)

// ─── step.iac_provider_reconcile tests ───────────────────────────────────────

// TestIaCProviderReconcile_DriftedProducesDraftCommit verifies that when the
// provider reports drifted resources the step produces a draft commit/PR with
// draft:true and a non-empty warning string containing the required disclaimer.
func TestIaCProviderReconcile_DriftedProducesDraftCommit(t *testing.T) {
	dir := t.TempDir()

	var calls [][]string
	gitFn := stubGitExecFn(t, 0, &calls)

	app := module.NewMockApplication()
	provider := &stubDriftImportProvider{
		stubIaCProvider: stubIaCProvider{
			driftResult: []interfaces.DriftResult{
				{
					Name:    "db",
					Type:    "infra.database",
					Drifted: true,
					Class:   interfaces.DriftClassConfig,
				},
			},
			statusResult: []interfaces.ResourceStatus{
				{Name: "db", Type: "infra.database", ProviderID: "pid-1"},
			},
		},
		importResult: &interfaces.ResourceState{
			Name:       "db",
			Type:       "infra.database",
			ProviderID: "pid-1",
		},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderReconcileStepFactory(gitFn)
	step, err := factory("reconcile", map[string]any{
		"provider": "my-provider",
		"branch":   "infra/reconcile-drift",
		"repo_dir": dir,
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Must be a draft
	if draft, _ := result.Output["draft"].(bool); !draft {
		t.Errorf("expected draft:true, got %v", result.Output["draft"])
	}

	// Warning must contain the required disclaimer
	warning, _ := result.Output["warning"].(string)
	if warning == "" {
		t.Error("expected non-empty warning string")
	}
	if !containsString(warning, "approximate") {
		t.Errorf("warning must contain 'approximate', got: %q", warning)
	}
	if !containsString(warning, "secret://") {
		t.Errorf("warning must mention secret:// refs, got: %q", warning)
	}

	// Git must have been called (branch + commit)
	if len(calls) == 0 {
		t.Error("expected git calls for draft commit")
	}

	// Every invocation must carry the FULL argv (binary as args[0]).
	for i, call := range calls {
		if len(call) == 0 {
			t.Fatalf("call %d is empty", i)
		}
		if call[0] != "git" && call[0] != "gh" {
			t.Errorf("call %d must start with the binary name (git/gh), got: %v", i, call)
		}
	}
	// Default target is branch-push → must push the branch, must NOT call gh.
	assertCallPresent(t, calls, []string{"git", "checkout", "-b", "infra/reconcile-drift"})
	assertCallPresent(t, calls, []string{"git", "add", "-A"})
	assertCallPresent(t, calls, []string{"git", "push", "--set-upstream", "origin", "infra/reconcile-drift"})
	for _, call := range calls {
		if len(call) > 0 && call[0] == "gh" {
			t.Errorf("default branch-push target must NOT call gh, got call: %v", call)
		}
	}
}

// TestIaCProviderReconcile_GhPRTarget verifies that target=gh-pr opens a DRAFT
// PR (gh pr create --draft) instead of a plain branch push.
func TestIaCProviderReconcile_GhPRTarget(t *testing.T) {
	dir := t.TempDir()

	var calls [][]string
	gitFn := stubGitExecFn(t, 0, &calls)

	app := module.NewMockApplication()
	provider := &stubDriftImportProvider{
		stubIaCProvider: stubIaCProvider{
			driftResult: []interfaces.DriftResult{
				{Name: "db", Type: "infra.database", Drifted: true},
			},
			statusResult: []interfaces.ResourceStatus{
				{Name: "db", Type: "infra.database", ProviderID: "pid-1"},
			},
		},
		importResult: &interfaces.ResourceState{Name: "db", Type: "infra.database", ProviderID: "pid-1"},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderReconcileStepFactory(gitFn)
	step, err := factory("reconcile", map[string]any{
		"provider": "my-provider",
		"branch":   "infra/reconcile",
		"repo_dir": dir,
		"target":   "gh-pr",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if draft, _ := result.Output["draft"].(bool); !draft {
		t.Errorf("expected draft:true, got %v", result.Output)
	}

	foundGhDraft := false
	for _, call := range calls {
		if len(call) >= 3 && call[0] == "gh" && call[1] == "pr" && call[2] == "create" {
			foundGhDraft = true
			if !argvContains(call, "--draft") {
				t.Errorf("reconcile gh pr create must include --draft, got: %v", call)
			}
		}
	}
	if !foundGhDraft {
		t.Errorf("expected 'gh pr create --draft' for target=gh-pr, got calls: %v", calls)
	}
}

// TestIaCProviderReconcile_NoDrift_NothingCommitted verifies that when there is
// no drift the step does NOT produce a commit and returns draft:false with
// count:0.
func TestIaCProviderReconcile_NoDrift_NothingCommitted(t *testing.T) {
	dir := t.TempDir()

	var calls [][]string
	gitFn := stubGitExecFn(t, 0, &calls)

	app := module.NewMockApplication()
	provider := &stubDriftImportProvider{
		stubIaCProvider: stubIaCProvider{
			driftResult: []interfaces.DriftResult{
				{Name: "db", Type: "infra.database", Drifted: false},
			},
			statusResult: []interfaces.ResourceStatus{
				{Name: "db", Type: "infra.database", ProviderID: "pid-1"},
			},
		},
		importResult: &interfaces.ResourceState{
			Name: "db", Type: "infra.database", ProviderID: "pid-1",
		},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderReconcileStepFactory(gitFn)
	step, err := factory("reconcile", map[string]any{
		"provider": "my-provider",
		"branch":   "infra/reconcile-drift",
		"repo_dir": dir,
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if draft, _ := result.Output["draft"].(bool); draft {
		t.Error("expected draft:false when no drift")
	}
	if len(calls) > 0 {
		t.Errorf("git must not be called when no drift, got %d calls", len(calls))
	}
}

// TestIaCProviderReconcile_OutputIsApproximate verifies that the reconcile step
// does NOT claim to faithfully reconstruct authored specs — the YAML output is
// labeled as approximate / cloud-snapshot, NOT produced via iac/specgen's
// SpecToYAML on authored specs.
func TestIaCProviderReconcile_OutputIsApproximate(t *testing.T) {
	dir := t.TempDir()

	var calls [][]string
	gitFn := stubGitExecFn(t, 0, &calls)

	app := module.NewMockApplication()
	provider := &stubDriftImportProvider{
		stubIaCProvider: stubIaCProvider{
			driftResult: []interfaces.DriftResult{
				{Name: "db", Type: "infra.database", Drifted: true},
			},
			statusResult: []interfaces.ResourceStatus{
				{Name: "db", Type: "infra.database", ProviderID: "pid-1"},
			},
		},
		importResult: &interfaces.ResourceState{
			Name: "db", Type: "infra.database", ProviderID: "pid-1",
		},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderReconcileStepFactory(gitFn)
	step, err := factory("reconcile", map[string]any{
		"provider": "my-provider",
		"branch":   "infra/reconcile",
		"repo_dir": dir,
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// The warning must include the standard disclaimer text.
	warning, _ := result.Output["warning"].(string)
	requiredPhrases := []string{
		"imported from cloud",
		"approximate",
		"secret://",
		"review before merge",
	}
	for _, phrase := range requiredPhrases {
		if !containsString(warning, phrase) {
			t.Errorf("warning missing required phrase %q; full warning: %q", phrase, warning)
		}
	}
}

// TestIaCProviderReconcile_GitFails_StateDiverged verifies that when drift was
// detected but the git executor fails, the step returns draft:false (NO commit
// was produced) + state_diverged:true + a reason — NOT draft:true (which would
// falsely claim a PR exists).
func TestIaCProviderReconcile_GitFails_StateDiverged(t *testing.T) {
	dir := t.TempDir()

	var calls [][]string
	// Fail on the first git call (e.g. git checkout -b).
	gitFn := stubGitExecFn(t, 1, &calls)

	app := module.NewMockApplication()
	provider := &stubDriftImportProvider{
		stubIaCProvider: stubIaCProvider{
			driftResult: []interfaces.DriftResult{
				{Name: "db", Type: "infra.database", Drifted: true},
			},
			statusResult: []interfaces.ResourceStatus{
				{Name: "db", Type: "infra.database", ProviderID: "pid-1"},
			},
		},
		importResult: &interfaces.ResourceState{Name: "db", Type: "infra.database", ProviderID: "pid-1"},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderReconcileStepFactory(gitFn)
	step, err := factory("reconcile", map[string]any{
		"provider": "my-provider",
		"branch":   "infra/reconcile",
		"repo_dir": dir,
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	// Git failure must NOT be a hard pipeline error.
	if err != nil {
		t.Fatalf("Execute must not hard-error on git failure, got: %v", err)
	}

	// draft MUST be false — no commit/PR was produced.
	if draft, _ := result.Output["draft"].(bool); draft {
		t.Errorf("expected draft:false on git failure (no PR produced), got %v", result.Output)
	}
	if sd, _ := result.Output["state_diverged"].(bool); !sd {
		t.Errorf("expected state_diverged:true on git failure, got %v", result.Output)
	}
	if _, ok := result.Output["reason"]; !ok {
		t.Error("expected reason field when state_diverged")
	}
	// Must not claim a ref.
	if _, ok := result.Output["ref"]; ok {
		t.Errorf("must not claim a ref on git failure, got %v", result.Output["ref"])
	}
}

// TestIaCProviderReconcile_Factory_RequiresProvider verifies factory rejects missing provider.
func TestIaCProviderReconcile_Factory_RequiresProvider(t *testing.T) {
	gitFn := stubGitExecFn(t, 0, nil)
	factory := module.NewIaCProviderReconcileStepFactory(gitFn)
	_, err := factory("reconcile", map[string]any{
		"branch":   "infra/auto",
		"repo_dir": "/tmp",
	}, nil)
	if err == nil {
		t.Fatal("expected error when 'provider' missing")
	}
}

// TestIaCProviderReconcile_Factory_PanicOnNilGitFn verifies the factory panics when
// gitFn is nil.
func TestIaCProviderReconcile_Factory_PanicOnNilGitFn(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when gitFn is nil")
		}
	}()
	module.NewIaCProviderReconcileStepFactory(nil) //nolint:staticcheck // intentional panic test
}
