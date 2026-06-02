package module

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/iac/specgen"
	"github.com/GoCodeAlone/workflow/iac/specparse"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// GitExecFn executes a git/gh command and returns its combined output.
//
// argv is the COMPLETE argument vector including the binary as argv[0] (e.g.
// {"git","commit","-m","msg"} or {"gh","pr","create","--fill",...}); the prod
// implementation runs argv[0] directly with no shell and no entrypoint prefix.
// env carries extra environment variables (merged over the host environment so
// GH_TOKEN/GITHUB_TOKEN are forwarded automatically). workDir is the git
// working directory the command runs in (the step's repo_dir).
//
// The prod implementation in plugins/platform/plugin.go runs host-native via
// os/exec — the engine committing to its own repo is not untrusted-code
// execution. Tests inject a stub.
type GitExecFn func(ctx context.Context, argv []string, env map[string]string, workDir string) (string, error)

// ─── step.iac_commit_back ────────────────────────────────────────────────────

// IaCCommitBackStep serialises the authored specs to YAML and commits the
// result back to a git branch — but ONLY when the preceding apply step
// completed with full success (no errors + action_count matches the plan).
//
// Partial apply → {committed:false, reason:"partial-apply"} (no commit).
// Full success but git failure → {state_diverged:true, reason:...}
// (route maps to HTTP 207; the apply already happened).
type IaCCommitBackStep struct {
	name            string
	specs           []interfaces.ResourceSpec
	specsFrom       string // dotted context path; mutually exclusive with specs
	applyResultFrom string // dotted context path to the upstream apply_result
	branch          string
	message         string
	target          string // "branch-push" (default) or "gh-pr"
	repoDir         string // git working directory / sandbox mount root
	gitFn           GitExecFn
}

const (
	defaultApplyResultFrom = "steps.apply.apply_result"
	defaultTarget          = "branch-push"
	specsYAMLFilename      = "resources.yaml"
)

// NewIaCCommitBackStepFactory returns a StepFactory for step.iac_commit_back.
// gitFn is the git executor — pass the prod impl from plugins/platform/plugin.go
// or inject a stub in tests. The factory panics if gitFn is nil (mirrors
// NewIaCProviderApplyStepFactory).
func NewIaCCommitBackStepFactory(gitFn GitExecFn) StepFactory {
	if gitFn == nil {
		panic("NewIaCCommitBackStepFactory: gitFn must not be nil")
	}
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		branch, _ := cfg["branch"].(string)
		if branch == "" {
			return nil, fmt.Errorf("iac_commit_back step %q: 'branch' is required", name)
		}
		repoDir, _ := cfg["repo_dir"].(string)
		if repoDir == "" {
			return nil, fmt.Errorf("iac_commit_back step %q: 'repo_dir' is required", name)
		}
		message, _ := cfg["message"].(string)
		if message == "" {
			message = "chore: commit back applied infrastructure specs"
		}
		target, _ := cfg["target"].(string)
		if target == "" {
			target = defaultTarget
		}
		applyResultFrom, _ := cfg["apply_result_from"].(string)
		if applyResultFrom == "" {
			applyResultFrom = defaultApplyResultFrom
		}

		specsFrom, _ := cfg["specs_from"].(string)
		_, hasStaticSpecs := cfg["specs"]
		if specsFrom != "" && hasStaticSpecs {
			return nil, fmt.Errorf("iac_commit_back step %q: 'specs' and 'specs_from' are mutually exclusive", name)
		}

		var specs []interfaces.ResourceSpec
		if hasStaticSpecs {
			var err error
			specs, err = parseResourceSpecs(cfg["specs"])
			if err != nil {
				return nil, fmt.Errorf("iac_commit_back step %q: parse specs: %w", name, err)
			}
		}

		return &IaCCommitBackStep{
			name:            name,
			specs:           specs,
			specsFrom:       specsFrom,
			applyResultFrom: applyResultFrom,
			branch:          branch,
			message:         message,
			target:          target,
			repoDir:         repoDir,
			gitFn:           gitFn,
		}, nil
	}
}

func (s *IaCCommitBackStep) Name() string { return s.name }

func (s *IaCCommitBackStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	// 1. Resolve specs.
	specs := s.specs
	if s.specsFrom != "" {
		raw := resolveBodyFrom(s.specsFrom, pc)
		var err error
		specs, err = specparse.ParseResourceSpecs(raw)
		if err != nil {
			return nil, fmt.Errorf("iac_commit_back step %q: resolve specs_from %q: %w", s.name, s.specsFrom, err)
		}
		if len(specs) == 0 {
			return nil, fmt.Errorf("iac_commit_back step %q: specs_from %q resolved to empty/zero specs", s.name, s.specsFrom)
		}
	}

	// 2. Read apply_result from context.
	rawApplyResult := resolveBodyFrom(s.applyResultFrom, pc)
	// Also read action_count — it is a sibling of apply_result in the apply step output.
	// action_count path: replace the last segment "apply_result" with "action_count".
	actionCountFrom := replaceLastSegment(s.applyResultFrom, "action_count")
	rawActionCount := resolveBodyFrom(actionCountFrom, pc)

	// 3. Determine full success.
	if !isFullSuccess(rawApplyResult, rawActionCount) {
		return &StepResult{Output: map[string]any{
			"committed": false,
			"reason":    "partial-apply",
		}}, nil
	}

	// 4. Full success: serialise specs to YAML via specgen.SpecToYAML.
	yamlBytes, err := specgen.SpecToYAML(specs)
	if err != nil {
		return nil, fmt.Errorf("iac_commit_back step %q: SpecToYAML: %w", s.name, err)
	}

	// 5. Write YAML into repo_dir.
	outPath := filepath.Join(s.repoDir, specsYAMLFilename)
	if err := os.WriteFile(outPath, yamlBytes, 0o600); err != nil {
		return nil, fmt.Errorf("iac_commit_back step %q: write specs YAML: %w", s.name, err)
	}

	// 6. Run git commands via the injected executor. Each command is a COMPLETE
	// argv with the binary as argv[0] so the host-native executor runs it
	// directly (no entrypoint double-prefix). If ANY git operation fails →
	// state_diverged:true (the apply already happened; 207, not 5xx).
	var gitErr error
	var ref string

	_, gitErr = s.gitFn(ctx, []string{"git", "checkout", "-b", s.branch}, nil, s.repoDir)
	if gitErr == nil {
		_, gitErr = s.gitFn(ctx, []string{"git", "add", "-A"}, nil, s.repoDir)
	}
	if gitErr == nil {
		_, gitErr = s.gitFn(ctx, []string{"git", "commit", "-m", s.message}, nil, s.repoDir)
	}
	if gitErr == nil {
		switch s.target {
		case "gh-pr":
			ref, gitErr = s.gitFn(ctx, []string{"gh", "pr", "create", "--fill", "--head", s.branch}, nil, s.repoDir)
		default: // "branch-push"
			ref, gitErr = s.gitFn(ctx, []string{"git", "push", "--set-upstream", "origin", s.branch}, nil, s.repoDir)
		}
	}

	if gitErr != nil {
		return &StepResult{Output: map[string]any{
			"committed":      false,
			"state_diverged": true,
			"reason":         fmt.Sprintf("git executor error: %v", gitErr),
		}}, nil
	}

	out := map[string]any{
		"committed": true,
	}
	if ref != "" {
		out["ref"] = ref
	}
	return &StepResult{Output: out}, nil
}

// isFullSuccess returns true iff the apply result has no errors AND the number
// of recorded action outcomes matches action_count (both decoded from JSON
// so all numbers are float64).
func isFullSuccess(rawApplyResult any, rawActionCount any) bool {
	if rawApplyResult == nil {
		return false
	}
	m, ok := rawApplyResult.(map[string]any)
	if !ok {
		return false
	}
	// Check Errors field — absent or empty slice means no errors.
	if errs, ok := m["errors"]; ok && errs != nil {
		if errList, ok := errs.([]any); ok && len(errList) > 0 {
			return false
		}
	}
	// action_count is the number of planned actions; Actions slice in the result
	// should match. Both come from JSON decode so they are float64.
	actionCount := int(toFloat64(rawActionCount))
	actions, _ := m["actions"].([]any)
	return len(actions) == actionCount
}

// replaceLastSegment replaces the last dot-separated segment of path with newSeg.
// E.g. "steps.apply.apply_result" → "steps.apply.action_count".
func replaceLastSegment(path, newSeg string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[:i+1] + newSeg
		}
	}
	return newSeg
}

// toFloat64 safely converts JSON-decoded numeric values (float64 from json.Unmarshal,
// or int/int64 from direct Go construction) to float64.
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float32:
		return float64(n)
	}
	return 0
}
