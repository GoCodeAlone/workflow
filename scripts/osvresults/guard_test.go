//go:build unix

package osvresults

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These cases exercise the committed workflow's file-presence guards. Nonempty
// contents do not establish valid JSON, fresh results, or complete OSV analysis.
func TestOSVScanOutputGuards(t *testing.T) {
	workflow, err := os.ReadFile("../../.github/workflows/osv-scanner.yml")
	if err != nil {
		t.Fatal(err)
	}
	scheduled := workflowRunBlock(t, string(workflow), "Check scheduled scan output", "Build SARIF report")
	differential := workflowRunBlock(t, string(workflow), "Check differential scan output", "Build differential SARIF report")
	for _, tc := range []struct {
		name, guard string
		files       map[string]string
		wantPass    bool
	}{
		{"scheduled_missing", scheduled, nil, false},
		{"scheduled_empty", scheduled, map[string]string{"results.json": ""}, false},
		{"scheduled_nonempty", scheduled, map[string]string{"results.json": "nonempty"}, true},
		{"differential_missing_old", differential, map[string]string{"new-results.json": "nonempty"}, false},
		{"differential_empty_old", differential, map[string]string{"old-results.json": "", "new-results.json": "nonempty"}, false},
		{"differential_missing_new", differential, map[string]string{"old-results.json": "nonempty"}, false},
		{"differential_empty_new", differential, map[string]string{"old-results.json": "nonempty", "new-results.json": ""}, false},
		{"differential_both_nonempty", differential, map[string]string{"old-results.json": "nonempty", "new-results.json": "nonempty"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			const sentinel = "OSV_GUARD_REACHED_REPORTER"
			cmd := exec.CommandContext(ctx, "/bin/bash", "-e", "-o", "pipefail", "-c", tc.guard+"\nprintf '%s\\n' '"+sentinel+"'\n")
			cmd.Dir, cmd.Env, cmd.WaitDelay = dir, []string{"PATH=/usr/bin:/bin"}, time.Second
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("guard timed out: %v\n%s", ctx.Err(), output)
			}
			if tc.wantPass {
				if err != nil || strings.TrimSpace(string(output)) != sentinel {
					t.Fatalf("nonempty results did not reach reporter sentinel: %v\n%s", err, output)
				}
				return
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("missing or empty results should exit 1: %v\n%s", err, output)
			}
			if strings.Contains(string(output), sentinel) {
				t.Fatalf("missing or empty results reached reporter sentinel:\n%s", output)
			}
		})
	}
}

// Keep extraction deliberately limited to an unconditional run block immediately
// before its reporter; any extra attributes or layout changes require review.
func workflowRunBlock(t *testing.T, workflow, name, reporter string) string {
	t.Helper()
	lines := strings.Split(workflow, "\n")
	start, matches := -1, 0
	for i, line := range lines {
		if line == "    - name: "+name {
			start, matches = i, matches+1
		}
	}
	if matches != 1 {
		t.Fatalf("workflow must contain exactly one %q step; found %d", name, matches)
	}
	i := start + 1
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i == len(lines) || lines[i] != "      run: |" {
		t.Fatalf("workflow step %q must have run: | as its only attribute", name)
	}
	i++
	var body []string
	for i < len(lines) {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			body = append(body, "")
		} else if strings.HasPrefix(line, "        ") {
			body = append(body, strings.TrimPrefix(line, "        "))
		} else {
			break
		}
		i++
	}
	script := strings.Join(body, "\n")
	if strings.TrimSpace(script) == "" {
		t.Fatalf("workflow step %q must contain a nonempty run block with eight-space indentation", name)
	}
	if i == len(lines) || lines[i] != "    - name: "+reporter {
		t.Fatalf("workflow step %q must have no other attributes and immediately precede reporter %q", name, reporter)
	}
	return script
}
