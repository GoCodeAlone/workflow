//go:build linux || darwin

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPipelineRecordHostSandboxBroker(t *testing.T) {
	root := pipelineRecordHostTempDir(t)
	binary := filepath.Join(root, "wfctl")
	buildPipelineRecordHostBinary(t, binary, ".")
	// This substitutes container transport only. The actual Workflow sandbox
	// step, templates, WFD2 channel, journal, and parent Docker adapter execute.
	dir, _ := newPipelineDockerFixture(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("GITHUB_TOKEN", "host-private-canary")
	stdout, stderr, err := runPipelineRecordHost(t, binary, "testdata/pipeline-record/sandbox.yaml", "", "--var", "message=dynamic-message")
	if err != nil || string(stdout) != "SDK_RECORD_V1 {\"message\":\"captured output\",\"source\":\"broker\"}\n" {
		t.Fatalf("Workflow sandbox broker path failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	calls := pipelineDockerCalls(t, dir)
	var created bool
	for _, call := range calls {
		if !slices.Contains(call.Args, "create") {
			continue
		}
		created = true
		args := strings.Join(call.Args, " ")
		for _, value := range []string{"--user 65532:65532", "--read-only", "--cap-drop ALL", "--security-opt no-new-privileges", "--pids-limit 64", "--cpus 1", "PUBLIC_VALUE=dynamic-message", "printf '%s' 'dynamic-message'"} {
			if !strings.Contains(args, value) {
				t.Errorf("broker omitted strict or resolved argument %q: %q", value, args)
			}
		}
		if strings.Contains(args, "host-private-canary") || strings.Contains(args, "GITHUB_TOKEN") {
			t.Fatal("host token entered the container request")
		}
	}
	if !created {
		t.Fatal("sandbox transport never received create")
	}
	entries, err := os.ReadDir(filepath.Join(root, "state", "wfctl", "pipeline-cleanup"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "lock" {
		t.Fatalf("completed sandbox left retained journal state: %v, %v", entries, err)
	}
}
