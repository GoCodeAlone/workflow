package cigen

import (
	"fmt"
	"strings"
)

// RenderCircleCI generates a CircleCI 2.1 configuration from a CIPlan.
// It returns a map with a single key ".circleci/config.yml".
//
// The output mirrors the GitHub Actions renderer's job set — plan / per-phase
// apply (plan-guard + last-phase migrations) / smoke — as CircleCI jobs wired by
// a `workflows:` graph (plan jobs on PR branches, apply jobs on the default
// branch chained via `requires:`). CircleCI auto-injects project-level env vars
// into every job, so secrets are referenced, not re-declared. It deliberately
// emits no docker-build/deploy stage (ADR 0044).
func RenderCircleCI(p *CIPlan) (map[string]string, error) {
	if p == nil {
		return nil, fmt.Errorf("RenderCircleCI: plan is nil")
	}
	content, err := renderCircleCIConfig(p)
	if err != nil {
		return nil, err
	}
	return map[string]string{".circleci/config.yml": content}, nil
}

func renderCircleCIConfig(p *CIPlan) (string, error) {
	branch := p.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	version := p.WfctlVersion
	if version == "" {
		version = "latest"
	}

	var b strings.Builder
	b.WriteString("version: 2.1\n")
	// Project-level env vars (secrets) are auto-injected into every job by
	// CircleCI; set these in the project settings. They are referenced, never
	// re-declared as NAME: $NAME no-ops.
	if creds := jenkinsCredentialUnion(p); len(creds) > 0 {
		fmt.Fprintf(&b, "# Required project environment variables: %s\n", strings.Join(creds, ", "))
	}
	b.WriteString("\n")

	// Jobs
	b.WriteString("jobs:\n")
	for _, phase := range p.Phases {
		writeCirclePlanJob(&b, circleJobName("plan", phase, p), phase, p, version)
	}
	for i, phase := range p.Phases {
		writeCircleApplyJob(&b, circleJobName("apply", phase, p), phase, p, version, i == len(p.Phases)-1)
	}
	if p.Smoke != nil {
		b.WriteString("  smoke:\n")
		b.WriteString("    docker:\n      - image: cimg/base:current\n")
		b.WriteString("    steps:\n")
		fmt.Fprintf(&b, "      - run: curl --fail --max-time 30 '%s'\n", p.Smoke.URL)
	}

	// Workflow graph
	b.WriteString("\nworkflows:\n")
	b.WriteString("  infra:\n")
	b.WriteString("    jobs:\n")
	prevApply := ""
	for _, phase := range p.Phases {
		planJob := circleJobName("plan", phase, p)
		applyJob := circleJobName("apply", phase, p)
		// Plan jobs run on non-default branches (i.e. PRs).
		fmt.Fprintf(&b, "      - %s:\n", planJob)
		b.WriteString("          filters:\n            branches:\n")
		fmt.Fprintf(&b, "              ignore:\n                - %s\n", branch)
		// Apply jobs run on the default branch, chained via requires:.
		fmt.Fprintf(&b, "      - %s:\n", applyJob)
		if prevApply != "" {
			fmt.Fprintf(&b, "          requires:\n            - %s\n", prevApply)
		}
		b.WriteString("          filters:\n            branches:\n")
		fmt.Fprintf(&b, "              only:\n                - %s\n", branch)
		prevApply = applyJob
	}
	if p.Smoke != nil {
		b.WriteString("      - smoke:\n")
		fmt.Fprintf(&b, "          requires:\n            - %s\n", prevApply)
		b.WriteString("          filters:\n            branches:\n")
		fmt.Fprintf(&b, "              only:\n                - %s\n", branch)
	}

	return b.String(), nil
}

// circleJobName returns the phase-suffixed job name for multi-phase plans, or the
// bare prefix for single-phase plans (mirrors the GitLab renderer's naming).
func circleJobName(prefix string, phase DeployPhase, p *CIPlan) string {
	if len(p.Phases) > 1 {
		return fmt.Sprintf("%s-%s", prefix, phase.Name)
	}
	return prefix
}

func writeCirclePlanJob(b *strings.Builder, jobName string, phase DeployPhase, p *CIPlan, version string) {
	fmt.Fprintf(b, "  %s:\n", jobName)
	b.WriteString("    docker:\n      - image: cimg/go:1.26\n")
	b.WriteString("    steps:\n")
	b.WriteString("      - checkout\n")
	writeCircleSetup(b, p, phase, version)
	fmt.Fprintf(b, "      - run: wfctl infra plan --config '%s' --format markdown\n", phase.ConfigPath)
}

func writeCircleApplyJob(b *strings.Builder, jobName string, phase DeployPhase, p *CIPlan, version string, isLast bool) {
	fmt.Fprintf(b, "  %s:\n", jobName)
	b.WriteString("    docker:\n      - image: cimg/go:1.26\n")
	b.WriteString("    steps:\n")
	b.WriteString("      - checkout\n")
	writeCircleSetup(b, p, phase, version)
	if p.PlanGuard {
		writeCirclePlanGuard(b, phase.ConfigPath)
	}
	// Migrations run only in the last phase, via the shared `wfctl migrations up`
	// runner (never `wfctl ci run --phase migrate`).
	if isLast && p.Migrations != nil {
		fmt.Fprintf(b, "      - run: %s\n", migrationsUpCommand(phase.ConfigPath, p.Migrations.Env))
	}
	fmt.Fprintf(b, "      - run: wfctl infra apply --config '%s' --auto-approve\n", phase.ConfigPath)
}

// writeCircleSetup installs wfctl (and plugins when needed) for the job.
func writeCircleSetup(b *strings.Builder, p *CIPlan, phase DeployPhase, version string) {
	fmt.Fprintf(b, "      - run: go install github.com/GoCodeAlone/workflow/cmd/wfctl@%s\n", version)
	if p.PluginInstall {
		fmt.Fprintf(b, "      - run: wfctl plugin install --config '%s'\n", phase.ConfigPath)
	}
}

// writeCirclePlanGuard refuses to apply when the plan includes a replace or
// destroy of a protected resource (exit 1, no `|| true`).
func writeCirclePlanGuard(b *strings.Builder, configPath string) {
	b.WriteString("      - run:\n")
	b.WriteString("          name: Plan guard\n")
	b.WriteString("          command: |\n")
	fmt.Fprintf(b, "            wfctl infra plan --config '%s' | tee plan-guard.txt\n", configPath)
	b.WriteString("            if grep -Eq -- '^[[:space:]]*(- delete|-/\\+ replace)[[:space:]]' plan-guard.txt || \\\n")
	b.WriteString("               grep -Eq -- 'Plan: .*([1-9][0-9]* to replace|[1-9][0-9]* to destroy)' plan-guard.txt; then\n")
	b.WriteString("              echo 'Refusing apply: plan includes replace or destroy of a protected resource.' >&2\n")
	b.WriteString("              exit 1\n")
	b.WriteString("            fi\n")
}
