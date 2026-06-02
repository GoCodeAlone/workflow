package module

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
	"gopkg.in/yaml.v3"
)

// ─── step.iac_provider_reconcile ─────────────────────────────────────────────

// IaCProviderReconcileStep performs a drift → import → approximate-YAML →
// DRAFT commit cycle. It is explicitly approximate: the YAML it emits is a
// cloud snapshot, NOT a faithful reconstruction of any authored spec (no
// SpecToYAML call on authored data). The draft PR/commit body carries a
// mandatory disclaimer warning the reviewer that secret:// refs are absent
// and the YAML must be reviewed before merge.
//
// Output shape:
//
//	{
//	  "draft":          bool   — true iff a draft commit/PR was actually produced
//	  "ref":            string — optional (branch/PR ref when draft=true)
//	  "warning":        string — the disclaimer
//	  "count":          int    — number of drifted resources imported
//	  "state_diverged": bool   — true when drift was found but git failed (no PR produced)
//	  "reason":         string — set when state_diverged=true
//	}
//
// On git failure draft is FALSE (no commit/PR exists); the caller maps
// state_diverged to HTTP 207.
const reconcileWarning = "imported from cloud; approximate; does NOT reconstruct your secret:// refs — review before merge"

// IaCProviderReconcileStep implements step.iac_provider_reconcile.
type IaCProviderReconcileStep struct {
	name     string
	provider string
	branch   string
	target   string // "branch-push" (default) or "gh-pr"
	repoDir  string
	gitFn    GitExecFn
	app      modular.Application
}

// NewIaCProviderReconcileStepFactory returns a StepFactory for
// step.iac_provider_reconcile. gitFn is the git executor (same pattern as
// NewIaCCommitBackStepFactory). The factory panics if gitFn is nil.
func NewIaCProviderReconcileStepFactory(gitFn GitExecFn) StepFactory {
	if gitFn == nil {
		panic("NewIaCProviderReconcileStepFactory: gitFn must not be nil")
	}
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		providerName, _ := cfg["provider"].(string)
		if providerName == "" {
			return nil, fmt.Errorf("iac_provider_reconcile step %q: 'provider' is required", name)
		}
		branch, _ := cfg["branch"].(string)
		if branch == "" {
			branch = "infra/reconcile"
		}
		target, _ := cfg["target"].(string)
		if target == "" {
			target = defaultTarget
		}
		repoDir, _ := cfg["repo_dir"].(string)
		if repoDir == "" {
			return nil, fmt.Errorf("iac_provider_reconcile step %q: 'repo_dir' is required", name)
		}
		return &IaCProviderReconcileStep{
			name:     name,
			provider: providerName,
			branch:   branch,
			target:   target,
			repoDir:  repoDir,
			gitFn:    gitFn,
			app:      app,
		}, nil
	}
}

func (s *IaCProviderReconcileStep) Name() string { return s.name }

func (s *IaCProviderReconcileStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	provider, err := resolveIaCProvider(s.app, s.provider, s.name, "iac_provider_reconcile")
	if err != nil {
		return nil, err
	}

	// Step 1: detect drift using DetectDrift (existence-only; no authored specs
	// available at reconcile time).
	statuses, err := provider.Status(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("iac_provider_reconcile step %q: Status: %w", s.name, err)
	}

	// Build refs from current statuses.
	refs := make([]statusRef, 0, len(statuses))
	for _, st := range statuses {
		refs = append(refs, statusRef{
			Name:       st.Name,
			Type:       st.Type,
			ProviderID: st.ProviderID,
		})
	}

	drifts, err := provider.DetectDrift(ctx, statusRefsToResourceRefs(refs))
	if err != nil {
		return nil, fmt.Errorf("iac_provider_reconcile step %q: DetectDrift: %w", s.name, err)
	}

	// Step 2: collect drifted resources.
	var drifted []statusRef
	for i, d := range drifts {
		if d.Drifted {
			drifted = append(drifted, refs[i])
		}
	}

	if len(drifted) == 0 {
		// No drift — nothing to reconcile.
		return &StepResult{Output: map[string]any{
			"draft":   false,
			"warning": "",
			"count":   0,
		}}, nil
	}

	// Step 3: for each drifted resource, call Import to get a cloud snapshot.
	snapshots := make([]map[string]any, 0, len(drifted))
	for _, r := range drifted {
		state, importErr := provider.Import(ctx, r.ProviderID, r.Type)
		if importErr != nil {
			// Import errors are non-fatal for the reconcile step — record what we
			// can and skip this resource rather than aborting the whole run.
			snapshots = append(snapshots, map[string]any{
				"name":         r.Name,
				"type":         r.Type,
				"provider_id":  r.ProviderID,
				"import_error": importErr.Error(),
			})
			continue
		}
		entry := map[string]any{
			"name":        r.Name,
			"type":        r.Type,
			"provider_id": r.ProviderID,
		}
		if state != nil {
			if state.Outputs != nil {
				entry["outputs"] = state.Outputs
			}
			if state.AppliedConfig != nil {
				entry["config"] = state.AppliedConfig
			}
		}
		snapshots = append(snapshots, entry)
	}

	// Step 4: build an APPROXIMATE YAML cloud-snapshot. This is NOT SpecToYAML
	// (which is for authored specs). We emit a plainly-labeled cloud-snapshot
	// YAML prefixed with a comment block carrying the mandatory disclaimer.
	approxYAML, err := buildApproximateYAML(snapshots)
	if err != nil {
		return nil, fmt.Errorf("iac_provider_reconcile step %q: build approximate YAML: %w", s.name, err)
	}

	// Write the approximate YAML to repo_dir.
	outPath := filepath.Join(s.repoDir, "reconcile-snapshot.yaml")
	if err := os.WriteFile(outPath, approxYAML, 0o600); err != nil {
		return nil, fmt.Errorf("iac_provider_reconcile step %q: write snapshot: %w", s.name, err)
	}

	// Step 5: create a draft commit. Each command is a COMPLETE argv (binary as
	// argv[0]) run host-native in repo_dir.
	commitMessage := fmt.Sprintf("chore(reconcile): import drift snapshot — %s", reconcileWarning)

	var gitErr error
	var ref string

	_, gitErr = s.gitFn(ctx, []string{"git", "checkout", "-b", s.branch}, nil, s.repoDir)
	if gitErr == nil {
		_, gitErr = s.gitFn(ctx, []string{"git", "add", "-A"}, nil, s.repoDir)
	}
	if gitErr == nil {
		_, gitErr = s.gitFn(ctx, []string{"git", "commit", "-m", commitMessage}, nil, s.repoDir)
	}
	if gitErr == nil {
		switch s.target {
		case "gh-pr":
			ref, gitErr = s.gitFn(ctx, []string{"gh", "pr", "create",
				"--head", s.branch,
				"--title", "reconcile: drift snapshot (approximate; review required)",
				"--body", reconcileWarning,
				"--draft",
			}, nil, s.repoDir)
		default: // "branch-push"
			ref, gitErr = s.gitFn(ctx, []string{"git", "push", "--set-upstream", "origin", s.branch}, nil, s.repoDir)
		}
	}

	if gitErr != nil {
		// Git failure on the reconcile path — NO commit/PR was produced, so
		// draft MUST be false (claiming draft:true would be a lie). state_diverged
		// signals the caller to surface a 207.
		return &StepResult{Output: map[string]any{
			"draft":          false,
			"state_diverged": true,
			"warning":        reconcileWarning,
			"count":          len(drifted),
			"reason":         fmt.Sprintf("git executor error: %v", gitErr),
		}}, nil
	}

	out := map[string]any{
		"draft":   true,
		"warning": reconcileWarning,
		"count":   len(drifted),
	}
	if ref != "" {
		out["ref"] = ref
	}
	return &StepResult{Output: out}, nil
}

// statusRef is a minimal struct holding drift-detection identifiers for a
// resource. Using a bespoke type avoids importing the full ResourceRef struct
// while still carrying the ProviderID needed for Import.
type statusRef struct {
	Name       string
	Type       string
	ProviderID string
}

// statusRefsToResourceRefs converts []statusRef to []interfaces.ResourceRef.
func statusRefsToResourceRefs(refs []statusRef) []interfaces.ResourceRef {
	out := make([]interfaces.ResourceRef, len(refs))
	for i, r := range refs {
		out[i] = interfaces.ResourceRef{
			Name:       r.Name,
			Type:       r.Type,
			ProviderID: r.ProviderID,
		}
	}
	return out
}

// buildApproximateYAML produces a YAML document from cloud-import snapshots.
// The result is clearly labeled as approximate via a header comment; it does
// NOT follow the SpecToYAML authoring schema.
func buildApproximateYAML(snapshots []map[string]any) ([]byte, error) {
	header := "# APPROXIMATE CLOUD SNAPSHOT — imported from cloud state\n" +
		"# " + reconcileWarning + "\n" +
		"# This file was auto-generated by step.iac_provider_reconcile.\n" +
		"# Do NOT use this as a source of truth without review.\n\n"

	body, err := yaml.Marshal(snapshots)
	if err != nil {
		return nil, err
	}
	return append([]byte(header), body...), nil
}

// Ensure IaCProviderReconcileStep satisfies PipelineStep at compile time.
var _ PipelineStep = (*IaCProviderReconcileStep)(nil)
