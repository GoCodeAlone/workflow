package cigen_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/cigen"
)

func TestValidateRenderedFilesAcceptsGeneratedPlatforms(t *testing.T) {
	plan := richCIPlan()
	cases := []struct {
		platform string
		render   func(*cigen.CIPlan) (map[string]string, error)
	}{
		{platform: "github_actions", render: cigen.RenderGitHubActions},
		{platform: "gitlab_ci", render: cigen.RenderGitLabCI},
		{platform: "jenkins", render: cigen.RenderJenkins},
		{platform: "circleci", render: cigen.RenderCircleCI},
	}

	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			files, err := tc.render(plan)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if findings := cigen.ValidateRenderedFiles(tc.platform, files); len(findings) > 0 {
				t.Fatalf("expected generated %s files to validate, got %v", tc.platform, findings)
			}
		})
	}
}

func TestValidateRenderedFilesRejectsWrongProviderShape(t *testing.T) {
	files := map[string]string{
		".github/workflows/deploy.yml": "stages:\n  - plan\n",
	}
	findings := cigen.ValidateRenderedFiles("github_actions", files)
	if len(findings) == 0 {
		t.Fatal("expected GitHub Actions validation findings")
	}
	if got := strings.Join(cigen.ValidationMessages(findings), "\n"); !strings.Contains(got, "jobs") {
		t.Fatalf("expected findings to mention missing jobs, got:\n%s", got)
	}
}

func TestValidateRenderedFilesUsesActionlintForGitHubActions(t *testing.T) {
	files := map[string]string{
		".github/workflows/deploy.yml": `name: deploy
on:
  pull_request:
jobs:
  plan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3
        run: echo invalid
`,
	}
	findings := cigen.ValidateRenderedFiles("github_actions", files)
	if len(findings) == 0 {
		t.Fatal("expected GitHub Actions validation findings")
	}
	for _, finding := range findings {
		if strings.HasPrefix(finding.Code, "actionlint") {
			return
		}
	}
	t.Fatalf("expected actionlint finding, got %#v", findings)
}

func TestValidateRenderedFilesReportsMalformedGitHubActionsYAML(t *testing.T) {
	const path = ".github/workflows/deploy.yml"
	files := map[string]string{
		path: "name: deploy\non: push\njobs: [\n",
	}
	findings := cigen.ValidateRenderedFiles("github_actions", files)
	location := regexp.MustCompile(regexp.QuoteMeta(path) + `:[1-9][0-9]*:[0-9]+:.*could not parse as YAML`)
	for _, finding := range findings {
		if finding.Code != "actionlint_syntax-check" {
			continue
		}
		if finding.Path != path || !location.MatchString(finding.Message) {
			t.Fatalf("expected located actionlint YAML diagnostic, got %#v", finding)
		}
		return
	}
	t.Fatalf("expected actionlint YAML syntax finding, got %#v", findings)
}
