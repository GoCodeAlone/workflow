//go:build linux || darwin

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestPipelineRecordHostLiveDockerSandbox(t *testing.T) {
	githubLinuxCI := os.Getenv("GITHUB_ACTIONS") == "true" && os.Getenv("RUNNER_OS") == "Linux"
	if os.Getenv("WFCTL_RECORD_HOST_LIVE_DOCKER") != "1" && !githubLinuxCI {
		t.Skip("requires explicit live Docker opt-in; transport fixtures do not execute containers")
	}
	image := os.Getenv("WFCTL_RECORD_HOST_LIVE_IMAGE")
	if image == "" && githubLinuxCI {
		image = pullPipelineRecordLiveImage(t)
	}
	name, digest, ok := strings.Cut(image, "@sha256:")
	if !ok || name == "" || len(digest) != 64 {
		t.Fatal("live sandbox proof requires a pre-pulled digest-pinned image")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		t.Fatal("live sandbox proof image digest is invalid")
	}
	root := pipelineRecordHostTempDir(t)
	binary := filepath.Join(root, "wfctl")
	buildPipelineRecordHostBinary(t, binary, ".")
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("GITHUB_TOKEN", "host-private-live-canary")
	t.Setenv("AWS_SESSION_TOKEN", "host-private-live-canary")
	command := `test "$(id -u)" = 65532
test "$(id -g)" = 65532
if env | grep -Eq '^(GITHUB_TOKEN|AWS_SESSION_TOKEN)='; then exit 1; fi
grep -Eq '^CapEff:[[:space:]]+0000000000000000$' /proc/self/status
grep -Eq '^NoNewPrivs:[[:space:]]+1$' /proc/self/status
awk '$2 == "/" {if ($4 !~ /(^|,)ro(,|$)/) exit 1; found=1} END {if (!found) exit 1}' /proc/mounts
test "$(stat -c %a /tmp)" = 1777
test "$(stat -c %a /work)" = 700
test "$(stat -c %u /work)" = 65532
test "$PWD" = /work
printf '#!/bin/sh\nexit 0\n' > /tmp/deny-execution
chmod 700 /tmp/deny-execution
if /tmp/deny-execution 2>/dev/null; then exit 1; fi
printf '%s' "$PUBLIC_VALUE" > /work/message
cat /work/message`
	cfg := map[string]any{
		"modules": []any{},
		"pipelines": map[string]any{"selected": map[string]any{"steps": []any{
			map[string]any{"name": "command", "type": "step.sandbox_exec", "config": map[string]any{
				"image": image, "command": []string{"sh", "-euc", command}, "security_profile": "strict",
				"memory_limit": "128m", "cpu_limit": 1, "timeout": "20s", "network": "none", "work_dir": "/work",
				"tmpfs": map[string]string{"/work": "size=16m,mode=0700,uid=65532,gid=65532,noexec,nosuid,nodev"},
				"env":   map[string]string{"PUBLIC_VALUE": "{{ .message }}"},
			}},
			map[string]any{"name": "result", "type": "step.set", "config": map[string]any{"values": map[string]any{
				"source": "live-container", "message": "{{ .steps.command.stdout }}",
			}}},
		}}},
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "live-sandbox.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runPipelineRecordHost(t, binary, path, "", "--var", "message=live-dynamic-message")
	if err != nil || string(stdout) != "SDK_RECORD_V1 {\"message\":\"live-dynamic-message\",\"source\":\"live-container\"}\n" {
		t.Fatalf("built wfctl failed live sandbox lifecycle: error=%v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	assertPipelineRecordHostJournalEmpty(t, root)
}

func pullPipelineRecordLiveImage(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	const image = "busybox:1.37.0"
	if output, err := exec.CommandContext(ctx, "docker", "pull", image).CombinedOutput(); err != nil {
		t.Fatalf("prepare real CI sandbox image: %v\n%s", err, output)
	}
	output, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{json .RepoDigests}}", image).Output()
	if err != nil {
		t.Fatalf("resolve real CI sandbox image digest: %v", err)
	}
	var digests []string
	if json.Unmarshal(output, &digests) != nil || len(digests) == 0 {
		t.Fatal("real CI sandbox image has no immutable repository digest")
	}
	return digests[0]
}
