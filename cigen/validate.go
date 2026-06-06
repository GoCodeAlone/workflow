package cigen

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/rhysd/actionlint"
	"gopkg.in/yaml.v3"
)

// ValidationFinding describes one CI artifact validation problem.
type ValidationFinding struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidationMessages formats validation findings for CLI output and tests.
func ValidationMessages(findings []ValidationFinding) []string {
	messages := make([]string, 0, len(findings))
	for _, finding := range findings {
		if finding.Path != "" {
			messages = append(messages, fmt.Sprintf("%s: %s", finding.Path, finding.Message))
			continue
		}
		messages = append(messages, finding.Message)
	}
	return messages
}

// ValidateRenderedFiles validates rendered CI provider artifacts for a platform.
func ValidateRenderedFiles(platform string, files map[string]string) []ValidationFinding {
	if len(files) == 0 {
		return []ValidationFinding{{Code: "missing_ci_artifact", Message: "no CI artifact files provided"}}
	}
	var findings []ValidationFinding
	for _, path := range sortedFileKeys(files) {
		content := files[path]
		switch platform {
		case "github_actions":
			findings = append(findings, validateGitHubActions(path, content)...)
		case "gitlab_ci":
			findings = append(findings, validateGitLabCI(path, content)...)
		case "jenkins":
			findings = append(findings, validateJenkins(path, content)...)
		case "circleci":
			findings = append(findings, validateCircleCI(path, content)...)
		default:
			return []ValidationFinding{{
				Code:    "unsupported_ci_platform",
				Message: fmt.Sprintf("unsupported platform %q (supported: github_actions, gitlab_ci, jenkins, circleci)", platform),
			}}
		}
	}
	return findings
}

func validateGitHubActions(path, content string) []ValidationFinding {
	findings := lintGitHubActions(path, content)
	node, yamlFindings := parseYAMLArtifact(path, content)
	if len(yamlFindings) > 0 {
		return append(findings, yamlFindings...)
	}
	for _, key := range []string{"on", "jobs"} {
		if !yamlMapHasKey(node, key) {
			findings = append(findings, ciFinding(path, "missing_github_actions_"+key, "GitHub Actions workflow missing top-level "+key))
		}
	}
	if jobs := yamlMapValue(node, "jobs"); jobs != nil && len(jobs.Content) == 0 {
		findings = append(findings, ciFinding(path, "empty_github_actions_jobs", "GitHub Actions workflow jobs must not be empty"))
	}
	return findings
}

func lintGitHubActions(path, content string) []ValidationFinding {
	linter, err := actionlint.NewLinter(io.Discard, &actionlint.LinterOptions{
		Oneline:    true,
		Shellcheck: "",
		Pyflakes:   "",
	})
	if err != nil {
		return []ValidationFinding{ciFinding(path, "github_actions_linter_unavailable", fmt.Sprintf("GitHub Actions linter unavailable: %v", err))}
	}
	lintErrors, err := linter.Lint(path, []byte(content), nil)
	if err != nil {
		return []ValidationFinding{ciFinding(path, "github_actions_lint_error", fmt.Sprintf("GitHub Actions lint failed: %v", err))}
	}
	findings := make([]ValidationFinding, 0, len(lintErrors))
	for _, lintError := range lintErrors {
		code := "actionlint"
		if lintError.Kind != "" {
			code = "actionlint_" + lintError.Kind
		}
		findings = append(findings, ciFinding(path, code, lintError.Error()))
	}
	return findings
}

func validateGitLabCI(path, content string) []ValidationFinding {
	node, findings := parseYAMLArtifact(path, content)
	if len(findings) > 0 {
		return findings
	}
	if !yamlMapHasKey(node, "stages") {
		findings = append(findings, ciFinding(path, "missing_gitlab_stages", "GitLab CI config missing top-level stages"))
	}
	if !yamlMapHasAnyJob(node, "stages", "variables", "before_script", "default", "include", "workflow") {
		findings = append(findings, ciFinding(path, "missing_gitlab_jobs", "GitLab CI config missing at least one job"))
	}
	if strings.Contains(content, "\nonly:") || strings.Contains(content, "\n  only:") {
		findings = append(findings, ciFinding(path, "deprecated_gitlab_only", "GitLab CI config uses deprecated only: syntax; use rules:"))
	}
	return findings
}

func validateJenkins(path, content string) []ValidationFinding {
	var findings []ValidationFinding
	for _, want := range []string{"pipeline", "stages"} {
		if !strings.Contains(content, want) {
			findings = append(findings, ciFinding(path, "missing_jenkins_"+want, "Jenkinsfile missing "+want+" block"))
		}
	}
	if strings.Contains(content, "wfctl deploy --image") || strings.Contains(content, "docker build") {
		findings = append(findings, ciFinding(path, "legacy_jenkins_deploy", "Jenkinsfile contains retired template deploy/build stages"))
	}
	return findings
}

func validateCircleCI(path, content string) []ValidationFinding {
	node, findings := parseYAMLArtifact(path, content)
	if len(findings) > 0 {
		return findings
	}
	for _, key := range []string{"version", "jobs", "workflows"} {
		if !yamlMapHasKey(node, key) {
			findings = append(findings, ciFinding(path, "missing_circleci_"+key, "CircleCI config missing top-level "+key))
		}
	}
	if strings.Contains(content, "\nneeds:") || strings.Contains(content, "\n  needs:") {
		findings = append(findings, ciFinding(path, "github_actions_needs_in_circleci", "CircleCI config must use requires:, not GitHub Actions needs:"))
	}
	return findings
}

func parseYAMLArtifact(path, content string) (*yaml.Node, []ValidationFinding) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(content), &node); err != nil {
		return nil, []ValidationFinding{ciFinding(path, "invalid_yaml", fmt.Sprintf("invalid YAML: %v", err))}
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return nil, []ValidationFinding{ciFinding(path, "invalid_yaml_document", "CI config must be a YAML mapping")}
	}
	return node.Content[0], nil
}

func yamlMapHasKey(node *yaml.Node, key string) bool {
	return yamlMapValue(node, key) != nil
}

func yamlMapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func yamlMapHasAnyJob(node *yaml.Node, reserved ...string) bool {
	reservedSet := make(map[string]struct{}, len(reserved))
	for _, key := range reserved {
		reservedSet[key] = struct{}{}
	}
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if _, ok := reservedSet[key]; ok {
			continue
		}
		if node.Content[i+1].Kind == yaml.MappingNode {
			return true
		}
	}
	return false
}

func sortedFileKeys(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func ciFinding(path, code, message string) ValidationFinding {
	return ValidationFinding{Path: path, Code: code, Message: message}
}
