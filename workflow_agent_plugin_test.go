package workflow_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Keep the independently distributable test module in the repository's existing
// native test job without changing its staged workflow trust policy.
func TestWorkflowAgentPluginPackage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-race", "-count=1", "-timeout=90s", "./...")
	cmd.Dir = filepath.Join("example", "workflow-agent-plugin", "tests")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("standalone Workflow assistant tests failed: %v\n%s", err, output)
	}
}
