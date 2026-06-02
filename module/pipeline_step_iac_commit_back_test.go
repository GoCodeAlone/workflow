package module_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// buildFullApplyResult returns an apply_result any value (as it appears in the
// pipeline context) representing a FULL success: no errors + action_count
// actions all succeeded.
func buildFullApplyResult(t *testing.T, actionCount int) any {
	t.Helper()
	actions := make([]interfaces.ActionOutcome, actionCount)
	for i := range actions {
		actions[i] = interfaces.ActionOutcome{Status: interfaces.ActionStatusSuccess}
	}
	ar := interfaces.ApplyResult{
		PlanID:  "plan-test",
		Actions: actions,
		// Errors is nil → full success
	}
	b, err := json.Marshal(ar)
	if err != nil {
		t.Fatalf("buildFullApplyResult: marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("buildFullApplyResult: unmarshal: %v", err)
	}
	return out
}

// buildPartialApplyResult returns an apply_result with errors (partial failure).
func buildPartialApplyResult(t *testing.T) any {
	t.Helper()
	ar := interfaces.ApplyResult{
		PlanID: "plan-partial",
		Errors: []interfaces.ActionError{
			{Resource: "db", Action: "create", Error: "timeout"},
		},
		Actions: []interfaces.ActionOutcome{
			{Status: interfaces.ActionStatusSuccess},
			{Status: interfaces.ActionStatusError},
		},
	}
	b, err := json.Marshal(ar)
	if err != nil {
		t.Fatalf("buildPartialApplyResult: marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("buildPartialApplyResult: unmarshal: %v", err)
	}
	return out
}

// commitBackPCWithApplyResult builds a PipelineContext that holds an apply
// result at the default path "steps.apply.apply_result" with the given
// action_count.
func commitBackPCWithApplyResult(applyResult any, actionCount int) *module.PipelineContext {
	return &module.PipelineContext{
		StepOutputs: map[string]map[string]any{
			"apply": {
				"apply_result": applyResult,
				"action_count": float64(actionCount),
			},
		},
	}
}

// stubGitExecFn returns a GitExecFn stub that records every git invocation and
// returns the specified error on the nth call (1-indexed, 0 means never error).
func stubGitExecFn(t *testing.T, failOnCall int, captured *[][]string) module.GitExecFn {
	t.Helper()
	call := 0
	return func(_ context.Context, args []string, _ map[string]string, _ string) (string, error) {
		call++
		if captured != nil {
			*captured = append(*captured, append([]string{}, args...))
		}
		if failOnCall > 0 && call == failOnCall {
			return "", errors.New("git push: remote rejected")
		}
		return "ok", nil
	}
}

// ─── step.iac_commit_back tests ──────────────────────────────────────────────

// TestIaCCommitBack_FullSuccess_Commits verifies that on a full-success apply
// (no errors + action_count matches) the step serialises the specs with
// SpecToYAML, writes into repo_dir, calls git and reports committed:true.
func TestIaCCommitBack_FullSuccess_Commits(t *testing.T) {
	dir := t.TempDir()

	var calls [][]string
	gitFn := stubGitExecFn(t, 0, &calls)

	factory := module.NewIaCCommitBackStepFactory(gitFn)
	step, err := factory("cb", map[string]any{
		"repo_dir": dir,
		"branch":   "infra/auto-commit",
		"message":  "chore: commit back applied specs",
		"target":   "branch-push",
		"specs": []any{
			map[string]any{"name": "db", "type": "infra.database"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	applyResult := buildFullApplyResult(t, 1)
	pc := commitBackPCWithApplyResult(applyResult, 1)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Must report committed:true
	if committed, _ := result.Output["committed"].(bool); !committed {
		t.Errorf("expected committed:true, got %v", result.Output["committed"])
	}

	// Git commands must have been invoked
	if len(calls) == 0 {
		t.Error("expected at least one git command call")
	}

	// Every git/gh invocation must carry the FULL argv (binary as args[0]) so
	// host-native exec runs args[0] directly — no entrypoint double-prefix.
	for i, call := range calls {
		if len(call) == 0 {
			t.Fatalf("call %d is empty", i)
		}
		if call[0] != "git" && call[0] != "gh" {
			t.Errorf("call %d must start with the binary name (git/gh), got: %v", i, call)
		}
	}
	// The branch-push path must include the canonical command sequence.
	assertCallPresent(t, calls, []string{"git", "checkout", "-b", "infra/auto-commit"})
	assertCallPresent(t, calls, []string{"git", "add", "-A"})
	assertCallPresent(t, calls, []string{"git", "commit", "-m", "chore: commit back applied specs"})
	assertCallPresent(t, calls, []string{"git", "push", "--set-upstream", "origin", "infra/auto-commit"})

	// The specs YAML file must exist in repo_dir
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected SpecToYAML file written to repo_dir")
	}
}

// assertCallPresent fails the test if no recorded call equals want exactly.
func assertCallPresent(t *testing.T, calls [][]string, want []string) {
	t.Helper()
	for _, c := range calls {
		if equalArgs(c, want) {
			return
		}
	}
	t.Errorf("expected git call %v, got calls: %v", want, calls)
}

func equalArgs(a, b []string) bool {
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

// TestIaCCommitBack_WorkDirForwarded verifies that the step passes repo_dir as
// the workDir argument to the git executor on every call.
func TestIaCCommitBack_WorkDirForwarded(t *testing.T) {
	dir := t.TempDir()

	var workDirs []string
	gitFn := func(_ context.Context, _ []string, _ map[string]string, workDir string) (string, error) {
		workDirs = append(workDirs, workDir)
		return "ok", nil
	}

	factory := module.NewIaCCommitBackStepFactory(gitFn)
	step, err := factory("cb", map[string]any{
		"repo_dir": dir,
		"branch":   "infra/auto",
		"message":  "chore: commit back",
		"specs": []any{
			map[string]any{"name": "db", "type": "infra.database"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	applyResult := buildFullApplyResult(t, 0)
	pc := commitBackPCWithApplyResult(applyResult, 0)

	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if len(workDirs) == 0 {
		t.Fatal("expected at least one git call carrying a workDir")
	}
	for i, wd := range workDirs {
		if wd != dir {
			t.Errorf("call %d workDir = %q, want %q", i, wd, dir)
		}
	}
}

// TestIaCCommitBack_PartialApplyByCount_NoCommit verifies that an apply_result
// with empty errors but action_count > len(actions) is treated as a partial
// apply (NOT full success) — no commit is produced.
func TestIaCCommitBack_PartialApplyByCount_NoCommit(t *testing.T) {
	dir := t.TempDir()

	var calls [][]string
	gitFn := stubGitExecFn(t, 0, &calls)

	factory := module.NewIaCCommitBackStepFactory(gitFn)
	step, err := factory("cb", map[string]any{
		"repo_dir": dir,
		"branch":   "infra/auto",
		"message":  "chore: commit back",
		"specs": []any{
			map[string]any{"name": "db", "type": "infra.database"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Apply result has NO errors but only 1 action outcome recorded while the
	// plan had 3 actions (action_count=3) → not full success.
	applyResult := buildFullApplyResult(t, 1)
	pc := commitBackPCWithApplyResult(applyResult, 3)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute must not error on partial-by-count, got: %v", err)
	}

	if committed, _ := result.Output["committed"].(bool); committed {
		t.Error("expected committed:false when action_count > len(actions)")
	}
	reason, _ := result.Output["reason"].(string)
	if reason != "partial-apply" {
		t.Errorf("expected reason 'partial-apply', got %q", reason)
	}
	if len(calls) > 0 {
		t.Errorf("git must not be called on partial-by-count, got %d calls", len(calls))
	}
}

// TestIaCCommitBack_PartialApply_NoCommit verifies that a partial apply
// (Errors non-empty) causes the step to skip committing and return
// committed:false with reason:"partial-apply".
func TestIaCCommitBack_PartialApply_NoCommit(t *testing.T) {
	dir := t.TempDir()

	var calls [][]string
	gitFn := stubGitExecFn(t, 0, &calls)

	factory := module.NewIaCCommitBackStepFactory(gitFn)
	step, err := factory("cb", map[string]any{
		"repo_dir": dir,
		"branch":   "infra/auto",
		"message":  "chore: commit back",
		"specs": []any{
			map[string]any{"name": "db", "type": "infra.database"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	applyResult := buildPartialApplyResult(t)
	// action_count is 2 but Errors is non-empty → partial
	pc := commitBackPCWithApplyResult(applyResult, 2)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute must not error on partial-apply, got: %v", err)
	}

	if committed, _ := result.Output["committed"].(bool); committed {
		t.Error("expected committed:false on partial apply")
	}
	reason, _ := result.Output["reason"].(string)
	if reason != "partial-apply" {
		t.Errorf("expected reason 'partial-apply', got %q", reason)
	}
	if len(calls) > 0 {
		t.Errorf("git must not be called on partial apply, got %d calls", len(calls))
	}
}

// TestIaCCommitBack_GitFails_StateDiverged verifies that when the apply
// succeeded but the git executor errors, the step returns
// state_diverged:true (not a hard error) so callers can map to HTTP 207.
func TestIaCCommitBack_GitFails_StateDiverged(t *testing.T) {
	dir := t.TempDir()

	var calls [][]string
	// Fail on the very first git call (e.g., git checkout -b)
	gitFn := stubGitExecFn(t, 1, &calls)

	factory := module.NewIaCCommitBackStepFactory(gitFn)
	step, err := factory("cb", map[string]any{
		"repo_dir": dir,
		"branch":   "infra/auto",
		"message":  "chore: commit back",
		"specs": []any{
			map[string]any{"name": "db", "type": "infra.database"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	applyResult := buildFullApplyResult(t, 0)
	pc := commitBackPCWithApplyResult(applyResult, 0)

	result, err := step.Execute(context.Background(), pc)
	// A git executor failure MUST NOT be a pipeline error; it returns
	// state_diverged:true so the route can map to HTTP 207.
	if err != nil {
		t.Fatalf("Execute must not hard-error on git failure, got: %v", err)
	}

	if sd, _ := result.Output["state_diverged"].(bool); !sd {
		t.Errorf("expected state_diverged:true on git failure, got %v", result.Output)
	}
	if _, ok := result.Output["reason"]; !ok {
		t.Error("expected reason field when state_diverged")
	}
}

// TestIaCCommitBack_SecretRefSurvival verifies that authored specs containing
// secret:// refs are serialised verbatim — the literal ref appears in the
// YAML, not an expanded value.
func TestIaCCommitBack_SecretRefSurvival(t *testing.T) {
	dir := t.TempDir()

	var calls [][]string
	gitFn := stubGitExecFn(t, 0, &calls)

	factory := module.NewIaCCommitBackStepFactory(gitFn)
	step, err := factory("cb", map[string]any{
		"repo_dir": dir,
		"branch":   "infra/auto",
		"message":  "chore: commit back",
		"specs": []any{
			map[string]any{
				"name": "secret-db",
				"type": "infra.database",
				"config": map[string]any{
					"password": "secret://vault/my-db-pw",
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	applyResult := buildFullApplyResult(t, 0)
	pc := commitBackPCWithApplyResult(applyResult, 0)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if committed, _ := result.Output["committed"].(bool); !committed {
		t.Errorf("expected committed:true, got %v", result.Output)
	}

	// Read the written YAML file and assert the secret:// ref is present verbatim.
	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		t.Fatal("no file written to repo_dir")
	}
	yamlBytes, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	yamlStr := string(yamlBytes)
	if !containsString(yamlStr, "secret://vault/my-db-pw") {
		t.Errorf("expected literal secret:// ref in YAML, got:\n%s", yamlStr)
	}
}

// TestIaCCommitBack_Factory_RequiresBranch verifies factory rejects missing branch.
func TestIaCCommitBack_Factory_RequiresBranch(t *testing.T) {
	gitFn := stubGitExecFn(t, 0, nil)
	factory := module.NewIaCCommitBackStepFactory(gitFn)
	_, err := factory("cb", map[string]any{
		"repo_dir": "/tmp",
		"message":  "msg",
		"specs":    []any{},
	}, nil)
	if err == nil {
		t.Fatal("expected error when 'branch' missing")
	}
}

// TestIaCCommitBack_Factory_RequiresRepoDir verifies factory rejects missing repo_dir.
func TestIaCCommitBack_Factory_RequiresRepoDir(t *testing.T) {
	gitFn := stubGitExecFn(t, 0, nil)
	factory := module.NewIaCCommitBackStepFactory(gitFn)
	_, err := factory("cb", map[string]any{
		"branch":  "infra/auto",
		"message": "msg",
		"specs":   []any{},
	}, nil)
	if err == nil {
		t.Fatal("expected error when 'repo_dir' missing")
	}
}

// TestIaCCommitBack_Factory_PanicOnNilGitFn verifies the factory panics when
// gitFn is nil (mirrors the apply step pattern).
func TestIaCCommitBack_Factory_PanicOnNilGitFn(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when gitFn is nil")
		}
	}()
	module.NewIaCCommitBackStepFactory(nil) //nolint:staticcheck // intentional panic test
}

// TestIaCCommitBack_GhPR_Target verifies that target=gh-pr calls gh instead of git push.
func TestIaCCommitBack_GhPR_Target(t *testing.T) {
	dir := t.TempDir()

	var calls [][]string
	gitFn := stubGitExecFn(t, 0, &calls)

	factory := module.NewIaCCommitBackStepFactory(gitFn)
	step, err := factory("cb", map[string]any{
		"repo_dir": dir,
		"branch":   "infra/auto",
		"message":  "chore: commit back",
		"target":   "gh-pr",
		"specs": []any{
			map[string]any{"name": "db", "type": "infra.database"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	applyResult := buildFullApplyResult(t, 0)
	pc := commitBackPCWithApplyResult(applyResult, 0)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if committed, _ := result.Output["committed"].(bool); !committed {
		t.Errorf("expected committed:true, got %v", result.Output)
	}

	// The gh-pr path must NOT push and MUST invoke gh pr create with full argv.
	foundGh := false
	for _, call := range calls {
		if len(call) > 0 && call[0] == "gh" {
			foundGh = true
			// gh pr create must carry --head <branch> and --fill (no --draft for commit_back).
			if len(call) < 3 || call[1] != "pr" || call[2] != "create" {
				t.Errorf("gh call must be 'gh pr create ...', got: %v", call)
			}
			if !argvContains(call, "--head") || !argvContains(call, "infra/auto") {
				t.Errorf("gh call must include '--head infra/auto', got: %v", call)
			}
			if !argvContains(call, "--fill") {
				t.Errorf("gh call must include '--fill', got: %v", call)
			}
		}
		if len(call) > 0 && argvContains(call, "push") {
			t.Errorf("gh-pr target must not git push, got call: %v", call)
		}
	}
	if !foundGh {
		t.Errorf("expected a 'gh' call for target=gh-pr, got calls: %v", calls)
	}
}

func argvContains(argv []string, want string) bool {
	for _, a := range argv {
		if a == want {
			return true
		}
	}
	return false
}
