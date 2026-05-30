package cigen

import (
	"fmt"
	"strings"
)

// RenderGitLabCI generates a GitLab CI YAML configuration from a CIPlan.
// It returns a map with a single key ".gitlab-ci.yml".
func RenderGitLabCI(p *CIPlan) (map[string]string, error) {
	if p == nil {
		return nil, fmt.Errorf("RenderGitLabCI: plan is nil")
	}

	content, err := renderGitLabWorkflow(p)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		".gitlab-ci.yml": content,
	}, nil
}

// renderGitLabWorkflow produces the full .gitlab-ci.yml content.
func renderGitLabWorkflow(p *CIPlan) (string, error) {
	branch := p.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	version := p.WfctlVersion
	if version == "" {
		version = "latest"
	}

	var b strings.Builder

	// Stages
	stages := []string{"plan", "apply"}
	if p.Smoke != nil {
		stages = append(stages, "smoke")
	}
	b.WriteString("stages:\n")
	for _, s := range stages {
		fmt.Fprintf(&b, "  - %s\n", s)
	}
	b.WriteString("\n")

	// Global variables
	b.WriteString("variables:\n")
	fmt.Fprintf(&b, "  WFCTL_VERSION: %q\n", version)
	for _, s := range p.Secrets {
		// Secret refs in GitLab CI are just $NAME CI variables
		fmt.Fprintf(&b, "  %s: $%s\n", s.Name, s.Name)
	}
	b.WriteString("\n")

	// before_script
	b.WriteString("before_script:\n")
	b.WriteString("  - go install \"github.com/GoCodeAlone/workflow/cmd/wfctl@${WFCTL_VERSION}\"\n")
	b.WriteString("  - export PATH=\"$(go env GOPATH)/bin:$PATH\"\n")
	if p.PluginInstall {
		for _, phase := range p.Phases {
			fmt.Fprintf(&b, "  - wfctl plugin install --config '%s'\n", phase.ConfigPath)
		}
	}
	b.WriteString("\n")

	// Plan jobs
	for _, phase := range p.Phases {
		jobName := "plan"
		if len(p.Phases) > 1 {
			jobName = fmt.Sprintf("plan-%s", phase.Name)
		}
		fmt.Fprintf(&b, "%s:\n", jobName)
		b.WriteString("  stage: plan\n")
		b.WriteString("  script:\n")
		fmt.Fprintf(&b, "    - wfctl infra plan --config '%s' --format markdown > plan.md\n", phase.ConfigPath)
		b.WriteString("  artifacts:\n")
		b.WriteString("    paths:\n")
		b.WriteString("      - plan.md\n")
		b.WriteString("    expire_in: 1 hour\n")
		b.WriteString("  rules:\n")
		b.WriteString("    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n")
		b.WriteString("      changes:\n")
		fmt.Fprintf(&b, "        - %q\n", phase.ConfigPath)
		b.WriteString("\n")
	}

	// Apply jobs
	prevJob := ""
	for i, phase := range p.Phases {
		jobName := "apply"
		if len(p.Phases) > 1 {
			jobName = fmt.Sprintf("apply-%s", phase.Name)
		}
		fmt.Fprintf(&b, "%s:\n", jobName)
		b.WriteString("  stage: apply\n")
		if prevJob != "" {
			b.WriteString("  needs:\n")
			fmt.Fprintf(&b, "    - job: %s\n", prevJob)
			b.WriteString("      artifacts: false\n")
		}
		b.WriteString("  script:\n")
		isLastPhase := i == len(p.Phases)-1
		if isLastPhase && p.Migrations != nil {
			fmt.Fprintf(&b, "    - wfctl ci run --config '%s' --phase migrate\n", phase.ConfigPath)
		}
		fmt.Fprintf(&b, "    - wfctl infra apply --config '%s' --auto-approve\n", phase.ConfigPath)
		b.WriteString("  environment:\n")
		b.WriteString("    name: production\n")
		b.WriteString("  rules:\n")
		fmt.Fprintf(&b, "    - if: $CI_COMMIT_BRANCH == %q && $CI_PIPELINE_SOURCE == \"push\"\n", branch)
		if p.Triggers.Dispatch {
			b.WriteString("    - if: $CI_PIPELINE_SOURCE == \"web\"\n")
		}
		b.WriteString("\n")
		prevJob = jobName
	}

	// Smoke job
	if p.Smoke != nil {
		lastApplyJob := "apply"
		if len(p.Phases) > 1 {
			lastPhase := p.Phases[len(p.Phases)-1]
			lastApplyJob = fmt.Sprintf("apply-%s", lastPhase.Name)
		}
		b.WriteString("smoke:\n")
		b.WriteString("  stage: smoke\n")
		b.WriteString("  needs:\n")
		fmt.Fprintf(&b, "    - job: %s\n", lastApplyJob)
		b.WriteString("  script:\n")
		fmt.Fprintf(&b, "    - curl --fail --max-time 30 '%s'\n", p.Smoke.URL)
		b.WriteString("  rules:\n")
		fmt.Fprintf(&b, "    - if: $CI_COMMIT_BRANCH == %q && $CI_PIPELINE_SOURCE == \"push\"\n", branch)
		b.WriteString("\n")
	}

	return b.String(), nil
}
