package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/cigen"
	"github.com/GoCodeAlone/workflow/cmd/wfctl/internal/prompt"
)

// wizardChoices holds the user's selections from the interactive wizard.
// It is populated by runWizard (TTY path) and consumed by applyWizardOverrides.
// Tests inject it directly without touching a TTY.
type wizardChoices struct {
	// Platform is one of "github_actions" or "gitlab_ci".
	Platform string
	// Runner is the CI runner label (e.g. "ubuntu-latest", "self-hosted").
	Runner string
	// Smoke indicates whether to emit a smoke job.
	Smoke bool
	// Migrations indicates whether to emit a migrations step.
	Migrations bool
	// PlanGuard indicates whether to enforce plan-before-apply.
	PlanGuard bool
	// Write is true when the user confirmed writing the output.
	Write bool
}

// applyWizardOverrides mutates plan with the operator's wizard choices.
// It is a pure function: no I/O, no TTY dependency — designed for unit testing.
// Note: choices.Platform is used by the caller to pick the renderer; it is not
// stored in CIPlan (there is no platform field on the plan struct).
func applyWizardOverrides(plan *cigen.CIPlan, choices wizardChoices) {
	if choices.Runner != "" {
		plan.Runner = choices.Runner
	}

	if !choices.Smoke {
		plan.Smoke = nil
	}

	if !choices.Migrations {
		plan.Migrations = nil
	}

	plan.PlanGuard = choices.PlanGuard
}

// platformOptions is the ordered list of CI platforms for the wizard.
var platformOptions = []string{"github_actions", "gitlab_ci", "jenkins", "circleci"}

// runnerOptions is the ordered list of common runner labels for the wizard.
var runnerOptions = []string{"ubuntu-latest", "self-hosted", "other (type below)"}

// runCIWizard drives the interactive wizard over a pre-analyzed plan.
// It prints warnings before asking the user to confirm writing.
// Returns wizardChoices populated from TTY input, or an error.
func runCIWizard(plan *cigen.CIPlan) (wizardChoices, error) {
	choices := wizardChoices{
		// Seed defaults from what Analyze derived.
		Smoke:      plan.Smoke != nil,
		Migrations: plan.Migrations != nil,
		PlanGuard:  plan.PlanGuard,
	}

	// 1. Platform
	platIdx, err := prompt.Select("Select CI platform", platformOptions)
	if err != nil {
		if errors.Is(err, prompt.ErrNotInteractive) {
			return choices, fmt.Errorf("stdin is not a terminal; specify --platform for non-interactive generation")
		}
		return choices, fmt.Errorf("wizard: platform selection: %w", err)
	}
	choices.Platform = platformOptions[platIdx]

	// 2. Runner
	runnerIdx, err := prompt.Select(
		fmt.Sprintf("Select runner label (default: %s)", plan.Runner),
		runnerOptions,
	)
	if err != nil {
		if errors.Is(err, prompt.ErrNotInteractive) {
			return choices, fmt.Errorf("stdin is not a terminal")
		}
		return choices, fmt.Errorf("wizard: runner selection: %w", err)
	}

	switch runnerIdx {
	case 0:
		choices.Runner = "ubuntu-latest"
	case 1:
		choices.Runner = "self-hosted"
	default:
		// "other" — ask for free-form input
		customRunner, inputErr := prompt.Input("Runner label", false)
		if inputErr != nil {
			return choices, fmt.Errorf("wizard: runner input: %w", inputErr)
		}
		customRunner = strings.TrimSpace(customRunner)
		if customRunner == "" {
			customRunner = plan.Runner // keep the analyzed default
		}
		choices.Runner = customRunner
	}

	// 3. Toggle: include smoke job?
	smoke, err := prompt.Confirm("Include smoke job?", choices.Smoke)
	if err != nil {
		return choices, fmt.Errorf("wizard: smoke confirm: %w", err)
	}
	choices.Smoke = smoke

	// 4. Toggle: include migrations step?
	mig, err := prompt.Confirm("Include migrations step?", choices.Migrations)
	if err != nil {
		return choices, fmt.Errorf("wizard: migrations confirm: %w", err)
	}
	choices.Migrations = mig

	// 5. Toggle: enable plan-guard?
	pg, err := prompt.Confirm("Enable plan-guard (refuse apply on replace/destroy)?", choices.PlanGuard)
	if err != nil {
		return choices, fmt.Errorf("wizard: plan-guard confirm: %w", err)
	}
	choices.PlanGuard = pg

	// 6. Surface warnings
	if len(plan.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range plan.Warnings {
			fmt.Printf("  ! %s\n", w)
		}
		fmt.Println()
	}

	// 7. Confirm write
	write, err := prompt.Confirm("Write generated files?", true)
	if err != nil {
		return choices, fmt.Errorf("wizard: write confirm: %w", err)
	}
	choices.Write = write

	return choices, nil
}
