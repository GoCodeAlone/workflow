package mcp

import (
	"strings"
	"testing"
)

// TestMCPOutputSurfacesCigen locks the cigen config-derived CI information into
// the MCP output an AI client receives: the server Instructions, the
// workflow://docs/overview + workflow://docs/setup-guide resource bodies, and
// the ci_plan / generate_github_actions tool descriptions.
func TestMCPOutputSurfacesCigen(t *testing.T) {
	// docsOverview resource: names the cigen CLI surface.
	for _, want := range []string{"cigen", "wfctl ci plan", "wfctl ci generate", "config-derived"} {
		if !strings.Contains(docsOverview, want) {
			t.Errorf("docsOverview missing %q", want)
		}
	}
	// Negative: must NOT claim Jenkins/CircleCI are template-based (all four are config-derived).
	if strings.Contains(docsOverview, "Jenkins/CircleCI template") ||
		strings.Contains(docsOverview, "Jenkins and CircleCI are template") {
		t.Errorf("docsOverview wrongly claims Jenkins/CircleCI are template-based")
	}

	// setup-guide resource: surfaces the cigen path alongside the scaffold_ci flow.
	for _, want := range []string{"ci_plan", "generate_github_actions", "wfctl ci generate"} {
		if !strings.Contains(setupGuideContent, want) {
			t.Errorf("setupGuideContent missing %q", want)
		}
	}

	// Server Instructions (sent to clients in the initialize response): name the
	// cigen surface. Asserted via the package const because mcp-go exposes no
	// Instructions getter on the constructed server.
	for _, want := range []string{"cigen", "ci_plan", "config-derived"} {
		if !strings.Contains(serverInstructions, want) {
			t.Errorf("serverInstructions missing %q", want)
		}
	}

	srv := NewServer("")
	tools := srv.MCPServer().ListTools()

	gha, ok := tools["generate_github_actions"]
	if !ok {
		t.Fatal("generate_github_actions tool not registered")
	}
	for _, want := range []string{"cigen", "ci_plan"} {
		if !strings.Contains(gha.Tool.Description, want) {
			t.Errorf("generate_github_actions description missing %q", want)
		}
	}
	ciPlan, ok := tools["ci_plan"]
	if !ok {
		t.Fatal("ci_plan tool not registered")
	}
	if !strings.Contains(ciPlan.Tool.Description, "cigen") {
		t.Errorf("ci_plan description missing %q", "cigen")
	}
}
