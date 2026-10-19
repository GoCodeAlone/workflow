package workflow_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Keep the independently distributable test module in the repository's existing
// native test job without changing its staged workflow trust policy.
func TestWorkflowAgentPluginPackage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	goBinary := "go"
	if runtime.GOOS == "windows" {
		goBinary += ".exe"
	}
	cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", goBinary), "test", "-race", "-count=1", "-timeout=90s", "./...")
	cmd.Dir = filepath.Join("example", "workflow-agent-plugin", "tests")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("standalone Workflow assistant tests failed: %v\n%s", err, output)
	}
}
