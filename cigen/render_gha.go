package cigen

import (
	"fmt"
	"strings"
)

// RenderGitHubActions generates GitHub Actions workflow YAML files from a CIPlan.
// It returns a map of relative paths to YAML content.
func RenderGitHubActions(p *CIPlan) (map[string]string, error) {
	if p == nil {
		return nil, fmt.Errorf("RenderGitHubActions: plan is nil")
	}

	name := p.Project
	if name == "" {
		name = "deploy"
	}

	content, err := renderGHAWorkflow(p, name)
	if err != nil {
		return nil, err
	}

	filename := fmt.Sprintf(".github/workflows/%s.yml", sanitizeFilename(name))
	return map[string]string{
		filename: content,
	}, nil
}

// sanitizeFilename replaces characters invalid in filenames with dashes.
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// renderGHAWorkflow produces the full workflow YAML content.
func renderGHAWorkflow(p *CIPlan, name string) (string, error) {
	branch := p.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	runner := p.Runner
	if runner == "" {
		runner = "ubuntu-latest"
	}
	version := p.WfctlVersion
	if version == "" {
		version = "latest"
	}

	var b strings.Builder

	// Header + triggers
	fmt.Fprintf(&b, "name: %s\n", name)
	b.WriteString("on:\n")
	if p.Triggers.PR {
		b.WriteString("  pull_request:\n")
		writePhasePaths(&b, p)
	}
	if p.Triggers.PushMain {
		b.WriteString("  push:\n")
		fmt.Fprintf(&b, "    branches: [%s]\n", branch)
		writePhasePaths(&b, p)
	}
	if p.Triggers.Dispatch {
		b.WriteString("  workflow_dispatch:\n")
	}

	b.WriteString("permissions:\n")
	b.WriteString("  contents: read\n")
	b.WriteString("  pull-requests: write\n")
	b.WriteString("jobs:\n")

	// Plan job (PR only)
	b.WriteString("  plan:\n")
	b.WriteString("    if: github.event_name == 'pull_request'\n")
	fmt.Fprintf(&b, "    runs-on: %s\n", runner)
	b.WriteString("    steps:\n")
	writeCheckoutStep(&b)
	writeSetupWfctlStep(&b, version)
	if p.PluginInstall {
		writePluginInstallStep(&b, p)
	}
	for _, phase := range p.Phases {
		fmt.Fprintf(&b, "      - name: Plan %s\n", phase.Name)
		fmt.Fprintf(&b, "        run: wfctl infra plan --config '%s' --format markdown >> plan.md\n", phase.ConfigPath)
	}
	b.WriteString("      - name: Post plan comment\n")
	b.WriteString("        uses: actions/github-script@v7\n")
	b.WriteString("        with:\n")
	b.WriteString("          script: |\n")
	b.WriteString("            const fs = require('fs');\n")
	b.WriteString("            const plan = fs.readFileSync('plan.md', 'utf8');\n")
	b.WriteString("            github.rest.issues.createComment({\n")
	b.WriteString("              issue_number: context.issue.number,\n")
	b.WriteString("              owner: context.repo.owner,\n")
	b.WriteString("              repo: context.repo.repo,\n")
	b.WriteString("              body: plan\n")
	b.WriteString("            });\n")

	// Apply jobs
	if len(p.Phases) == 1 {
		writeApplyJob(&b, "apply", p.Phases[0], nil, p, runner, version, branch)
	} else {
		// Multi-phase: apply-prereq then apply-deploy with needs
		prevJob := ""
		for i, phase := range p.Phases {
			jobName := fmt.Sprintf("apply-%s", phase.Name)
			var needs *string
			if i > 0 && prevJob != "" {
				needs = &prevJob
			}
			writeApplyJob(&b, jobName, phase, needs, p, runner, version, branch)
			prevJob = jobName
		}
	}

	// Smoke job
	if p.Smoke != nil {
		lastApplyJob := "apply"
		if len(p.Phases) > 1 {
			lastPhase := p.Phases[len(p.Phases)-1]
			lastApplyJob = fmt.Sprintf("apply-%s", lastPhase.Name)
		}
		b.WriteString("  smoke:\n")
		fmt.Fprintf(&b, "    needs: %s\n", lastApplyJob)
		fmt.Fprintf(&b, "    if: github.event_name == 'push' && github.ref == 'refs/heads/%s'\n", branch)
		fmt.Fprintf(&b, "    runs-on: %s\n", runner)
		b.WriteString("    steps:\n")
		b.WriteString("      - name: Smoke test\n")
		b.WriteString("        run: |\n")
		fmt.Fprintf(&b, "          curl --fail --max-time 30 '%s'\n", p.Smoke.URL)
	}

	return b.String(), nil
}

// writeCheckoutStep emits the checkout step.
func writeCheckoutStep(b *strings.Builder) {
	b.WriteString("      - uses: actions/checkout@v4\n")
}

// writeSetupWfctlStep emits the setup-wfctl action step.
func writeSetupWfctlStep(b *strings.Builder, version string) {
	b.WriteString("      - name: Install wfctl\n")
	b.WriteString("        uses: GoCodeAlone/setup-wfctl@v1\n")
	b.WriteString("        with:\n")
	fmt.Fprintf(b, "          version: '%s'\n", version)
}

// writePluginInstallStep emits a wfctl plugin install step.
func writePluginInstallStep(b *strings.Builder, p *CIPlan) {
	for _, phase := range p.Phases {
		fmt.Fprintf(b, "      - name: Install plugins (%s)\n", phase.Name)
		fmt.Fprintf(b, "        run: wfctl plugin install --config '%s'\n", phase.ConfigPath)
	}
}

// writeApplyJob emits a single apply job.
func writeApplyJob(b *strings.Builder, jobName string, phase DeployPhase, needs *string, p *CIPlan, runner, version, branch string) {
	fmt.Fprintf(b, "  %s:\n", jobName)
	fmt.Fprintf(b, "    if: \"(github.event_name == 'push' && github.ref == 'refs/heads/%s') || github.event_name == 'workflow_dispatch'\"\n", branch)
	if needs != nil {
		fmt.Fprintf(b, "    needs: %s\n", *needs)
	}
	fmt.Fprintf(b, "    runs-on: %s\n", runner)

	// Secrets env block. Branch the SOURCE on phase.Scoped (NOT len): a scoped
	// phase uses its own subset (possibly empty → no env block); an unscoped
	// phase falls back to the plan-wide union.
	secrets := p.Secrets
	if phase.Scoped {
		secrets = phase.Secrets
	}
	if len(secrets) > 0 {
		b.WriteString("    env:\n")
		for _, s := range secrets {
			fmt.Fprintf(b, "      %s: ${{ secrets.%s }}\n", s.Name, s.Name)
		}
	}

	b.WriteString("    steps:\n")
	writeCheckoutStep(b)
	writeSetupWfctlStep(b, version)
	if p.PluginInstall {
		b.WriteString("      - name: Install plugins\n")
		fmt.Fprintf(b, "        run: wfctl plugin install --config '%s'\n", phase.ConfigPath)
	}

	// PlanGuard: a protected resource is in scope, so refuse to apply when the
	// plan includes a replace or destroy. The plan output stays visible in the
	// CI log (tee) and a destructive plan fails the job (exit 1) — no `|| true`.
	if p.PlanGuard {
		b.WriteString("      - name: Plan guard\n")
		b.WriteString("        run: |\n")
		fmt.Fprintf(b, "          wfctl infra plan --config '%s' | tee plan-guard.txt\n", phase.ConfigPath)
		b.WriteString("          if grep -Eq -- '^[[:space:]]*(- delete|-/\\+ replace)[[:space:]]' plan-guard.txt || \\\n")
		b.WriteString("             grep -Eq -- 'Plan: .*([1-9][0-9]* to replace|[1-9][0-9]* to destroy)' plan-guard.txt; then\n")
		b.WriteString("            echo \"::error::Refusing apply: plan includes replace or destroy of a protected resource.\" >&2\n")
		b.WriteString("            exit 1\n")
		b.WriteString("          fi\n")
	}

	// Migrations step (only in the last phase). Use `wfctl migrations up`,
	// the real migration runner — `wfctl ci run --phase migrate` is NOT a valid
	// phase (ci run only accepts build|test|deploy) and would fail at runtime.
	// No step-level `env:` is needed: deriveSecrets always adds the migrations
	// DBEnv to the last phase's env block above; re-declaring it here would be redundant.
	isLastPhase := phase.Name == p.Phases[len(p.Phases)-1].Name
	if isLastPhase && p.Migrations != nil {
		b.WriteString("      - name: Run migrations\n")
		fmt.Fprintf(b, "        run: %s\n", migrationsUpCommand(phase.ConfigPath, p.Migrations.Env))
	}

	fmt.Fprintf(b, "      - name: Apply %s\n", phase.Name)
	fmt.Fprintf(b, "        run: wfctl infra apply --config '%s' --auto-approve\n", phase.ConfigPath)
}

// migrationsUpCommand builds the `wfctl migrations up` invocation. `--env <env>`
// is included only when MigrationsSpec.Env is set; `--format json` is always
// appended (machine-readable output; matches the deployed multisite workflow).
func migrationsUpCommand(configPath, env string) string {
	cmd := fmt.Sprintf("wfctl migrations up --config '%s'", configPath)
	if env != "" {
		cmd += fmt.Sprintf(" --env %s", env)
	}
	cmd += " --format json"
	return cmd
}

// writePhasePaths emits the paths filter for push/pull_request triggers.
func writePhasePaths(b *strings.Builder, p *CIPlan) {
	b.WriteString("    paths:\n")
	seen := make(map[string]bool)
	for _, phase := range p.Phases {
		if !seen[phase.ConfigPath] {
			fmt.Fprintf(b, "      - '%s'\n", phase.ConfigPath)
			seen[phase.ConfigPath] = true
		}
	}
}
