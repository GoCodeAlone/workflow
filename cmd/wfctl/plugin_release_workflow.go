package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type releaseWorkflowFinding struct {
	Code    string
	Message string
}

func runPluginReleaseWorkflow(args []string) error {
	fs := flag.NewFlagSet("plugin release-workflow", flag.ContinueOnError)
	path := fs.String("path", ".github/workflows/release.yml", "release workflow path")
	fix := fs.Bool("fix", false, "rewrite known stale wfctl install patterns")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl plugin release-workflow [options]

Audit a plugin release workflow for stale wfctl installation patterns. The
preferred release gate path is the SHA-pinned GoCodeAlone/setup-wfctl action
with its default latest wfctl resolution.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	data, err := os.ReadFile(*path)
	if err != nil {
		return fmt.Errorf("read release workflow: %w", err)
	}
	updated, findings, changed := auditReleaseWorkflow(string(data), *fix)
	if len(findings) == 0 {
		fmt.Printf("release workflow OK: %s\n", *path)
		return nil
	}
	for _, finding := range findings {
		fmt.Printf("%s: %s\n", finding.Code, finding.Message)
	}
	if !*fix {
		return fmt.Errorf("release workflow has %d issue(s); rerun with --fix", len(findings))
	}
	if changed {
		if err := os.WriteFile(*path, []byte(updated), 0o600); err != nil {
			return fmt.Errorf("write release workflow: %w", err)
		}
		fmt.Printf("release workflow updated: %s\n", *path)
	} else {
		fmt.Printf("release workflow unchanged: %s\n", *path)
	}
	return nil
}

func auditReleaseWorkflow(content string, fix bool) (string, []releaseWorkflowFinding, bool) {
	findings := releaseWorkflowFindings(content)
	if !fix || len(findings) == 0 {
		return content, findings, false
	}
	updated := normalizeSetupWfctlAction(content)
	updated = replaceManualWfctlInstallSteps(updated)
	updated = normalizeWfctlBinaryInvocations(updated)
	updated = normalizeSetupWfctlVersionInput(updated)
	return updated, findings, updated != content
}

func releaseWorkflowFindings(content string) []releaseWorkflowFinding {
	var findings []releaseWorkflowFinding
	if strings.Contains(content, "workflow/releases/download/v") || strings.Contains(content, "/wfctl-bin/wfctl") {
		findings = append(findings, releaseWorkflowFinding{
			Code:    "manual-wfctl-install",
			Message: "release workflow installs a hardcoded wfctl binary instead of using setup-wfctl",
		})
	}
	if regexp.MustCompile(`GoCodeAlone/setup-wfctl@v[0-9]+`).MatchString(content) {
		findings = append(findings, releaseWorkflowFinding{
			Code:    "unpinned-setup-wfctl",
			Message: "setup-wfctl action uses a mutable major ref instead of a pinned SHA",
		})
	}
	if regexp.MustCompile(`version:\s*['"]?v[0-9]+\.[0-9]+\.[0-9]+`).MatchString(content) ||
		regexp.MustCompile(`version:\s*v[0-9]+\.[0-9]+\.[0-9]+`).MatchString(content) {
		findings = append(findings, releaseWorkflowFinding{
			Code:    "pinned-wfctl-version",
			Message: "setup-wfctl pins an explicit wfctl release; use latest unless a workflow documents a compatibility reason",
		})
	}
	if !strings.Contains(content, "GoCodeAlone/setup-wfctl@") {
		findings = append(findings, releaseWorkflowFinding{
			Code:    "missing-setup-wfctl",
			Message: "release workflow does not use the setup-wfctl action",
		})
	}
	return findings
}

func normalizeSetupWfctlAction(content string) string {
	re := regexp.MustCompile(`GoCodeAlone/setup-wfctl@[^\s#]+(?:\s*#\s*v1)?`)
	return re.ReplaceAllString(content, githubActionsSetupWfctlRef)
}

func replaceManualWfctlInstallSteps(content string) string {
	lines := strings.SplitAfter(content, "\n")
	var out []string
	for i := 0; i < len(lines); {
		line := lines[i]
		if !strings.Contains(line, "- name: Install wfctl ") {
			out = append(out, line)
			i++
			continue
		}
		indent := line[:strings.Index(line, "- name:")]
		out = append(out,
			indent+"- name: Setup wfctl\n",
			indent+"  uses: "+githubActionsSetupWfctlRef+"\n",
		)
		i++
		for i < len(lines) {
			next := lines[i]
			if strings.HasPrefix(next, indent+"- name:") || strings.HasPrefix(next, indent+"- uses:") {
				break
			}
			i++
		}
	}
	return strings.Join(out, "")
}

func normalizeWfctlBinaryInvocations(content string) string {
	replacements := map[string]string{
		`"${{ runner.temp }}/wfctl-bin/wfctl"`: `wfctl`,
		`${{ runner.temp }}/wfctl-bin/wfctl`:   "wfctl",
		`"${RUNNER_TEMP}/wfctl-bin/wfctl"`:     `wfctl`,
		`${RUNNER_TEMP}/wfctl-bin/wfctl`:       "wfctl",
	}
	for old, replacement := range replacements {
		content = strings.ReplaceAll(content, old, replacement)
	}
	content = regexp.MustCompile(`run:\s*"?wfctl ([^"\n]+)"`).ReplaceAllString(content, `run: wfctl $1`)
	return content
}

func normalizeSetupWfctlVersionInput(content string) string {
	reInline := regexp.MustCompile(`with:\s*\{\s*version:\s*['"]?v[0-9]+\.[0-9]+\.[0-9]+['"]?\s*\}`)
	content = reInline.ReplaceAllString(content, "with: { version: latest }")
	reBlock := regexp.MustCompile(`version:\s*['"]?v[0-9]+\.[0-9]+\.[0-9]+['"]?`)
	return reBlock.ReplaceAllString(content, "version: latest")
}
