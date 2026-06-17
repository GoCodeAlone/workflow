package sdk

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSDKImportGraphDoesNotDependOnHostRuntime(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-f", "{{.ImportPath}}", "github.com/GoCodeAlone/workflow/plugin/external/sdk")
	cmd.Env = append(os.Environ(), "GOWORK=off")

	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("go list timed out: %v\n%s", ctx.Err(), out)
	}
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, out)
	}

	forbidden := []string{
		"github.com/GoCodeAlone/workflow/module",
		"github.com/docker/docker",
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, prefix := range forbidden {
			if dep == prefix || strings.HasPrefix(dep, prefix+"/") {
				t.Fatalf("SDK import graph includes host runtime dependency %q", dep)
			}
		}
	}
}
