package cigen_test

import (
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
      - uses: actions/checkout@9f698171ed81b15d1823a05fc7211befd50c8ae0 # v6.0.3
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
