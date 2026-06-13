package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditReleaseWorkflowFindsManualInstall(t *testing.T) {
	content := staleReleaseWorkflowFixture()
	_, findings, changed := auditReleaseWorkflow(content, false)
	if changed {
		t.Fatal("audit without fix should not change content")
	}
	assertReleaseWorkflowFinding(t, findings, "manual-wfctl-install")
	assertReleaseWorkflowFinding(t, findings, "missing-setup-wfctl")
}

func TestAuditReleaseWorkflowFixesManualInstall(t *testing.T) {
	content := staleReleaseWorkflowFixture()
	updated, findings, changed := auditReleaseWorkflow(content, true)
	if len(findings) == 0 {
		t.Fatal("expected findings")
	}
	if !changed {
		t.Fatal("expected fixed content to change")
	}
	for _, forbidden := range []string{
		"workflow/releases/download/v0.63.2",
		"Install wfctl v0.63.2",
		"runner.temp }}/wfctl-bin/wfctl",
	} {
		if strings.Contains(updated, forbidden) {
			t.Fatalf("fixed workflow still contains %q:\n%s", forbidden, updated)
		}
	}
	if !strings.Contains(updated, "uses: "+githubActionsSetupWfctlRef) {
		t.Fatalf("fixed workflow missing pinned setup-wfctl action:\n%s", updated)
	}
	if !strings.Contains(updated, "run: wfctl plugin validate-contract --for-publish --tag ${{ github.ref_name }} .") {
		t.Fatalf("fixed workflow missing wfctl validation command:\n%s", updated)
	}
}

func TestAuditReleaseWorkflowNormalizesSetupAction(t *testing.T) {
	content := `name: Release
jobs:
  release:
    steps:
      - uses: GoCodeAlone/setup-wfctl@v1
        with: { version: v0.61.0 }
      - run: wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" .
`
	updated, findings, changed := auditReleaseWorkflow(content, true)
	assertReleaseWorkflowFinding(t, findings, "unpinned-setup-wfctl")
	assertReleaseWorkflowFinding(t, findings, "pinned-wfctl-version")
	if !changed {
		t.Fatal("expected fixed content to change")
	}
	if !strings.Contains(updated, githubActionsSetupWfctlRef) {
		t.Fatalf("fixed workflow missing pinned action:\n%s", updated)
	}
	if strings.Contains(updated, "version: v0.61.0") {
		t.Fatalf("fixed workflow kept stale version:\n%s", updated)
	}
}

func TestRunPluginReleaseWorkflowFixWritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "release.yml")
	if err := os.WriteFile(path, []byte(staleReleaseWorkflowFixture()), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := runPluginReleaseWorkflow([]string{"--path", path}); err == nil {
		t.Fatal("expected audit without --fix to fail")
	}
	if err := runPluginReleaseWorkflow([]string{"--path", path, "--fix"}); err != nil {
		t.Fatalf("runPluginReleaseWorkflow --fix: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixed workflow: %v", err)
	}
	if !strings.Contains(string(data), githubActionsSetupWfctlRef) {
		t.Fatalf("fixed file missing setup-wfctl action:\n%s", data)
	}
}

func assertReleaseWorkflowFinding(t *testing.T, findings []releaseWorkflowFinding, code string) {
	t.Helper()
	for _, finding := range findings {
		if finding.Code == code {
			return
		}
	}
	t.Fatalf("finding %q not found in %#v", code, findings)
}

func staleReleaseWorkflowFixture() string {
	return `name: Release
jobs:
  release:
    steps:
      - uses: actions/checkout@v6
      - name: Install wfctl v0.63.2
        run: |
          mkdir -p "${RUNNER_TEMP}/wfctl-bin"
          curl -sSfL -H "Authorization: token ${{ secrets.GITHUB_TOKEN }}" \
            -o "${RUNNER_TEMP}/wfctl-bin/wfctl" \
            "https://github.com/GoCodeAlone/workflow/releases/download/v0.63.2/wfctl-linux-amd64"
          chmod +x "${RUNNER_TEMP}/wfctl-bin/wfctl"
      - name: Validate plugin contract for publish (pre-build)
        run: "${{ runner.temp }}/wfctl-bin/wfctl plugin validate-contract --for-publish --tag ${{ github.ref_name }} ."
      - uses: goreleaser/goreleaser-action@v7
`
}
