//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/sandbox"
	"golang.org/x/sys/unix"
)

// Parse only the closed status schema. Unrelated stderr is never reflected.
func safePipelineRecordDiagnostic(stderr []byte) (pipelineRecordDiagnosticReport, string) {
	var report pipelineRecordDiagnosticReport
	if len(stderr) > maxPipelineRecordBytes {
		return report, "oversize"
	}
	var payload []byte
	for _, line := range bytes.Split(stderr, []byte{'\n'}) {
		if !bytes.HasPrefix(line, []byte(pipelineRecordDiagnosticPrefix)) {
			continue
		}
		if payload != nil {
			return report, "invalid"
		}
		if len(line)+1 > maxPipelineRecordDiagnosticBytes {
			return report, "oversize"
		}
		payload = line[len(pipelineRecordDiagnosticPrefix):]
	}
	if payload == nil {
		return report, "missing"
	}
	if validatePipelinePrivateJSON(payload) != nil {
		return report, "invalid"
	}
	var properties map[string]json.RawMessage
	required := []string{"phase", "event", "child_observed", "child_exit", "child_signal", "docker_operation", "docker_cli_exit", "container_exit", "cleanup", "cancelled", "deadline"}
	if json.Unmarshal(payload, &properties) != nil || len(properties) != len(required) {
		return report, "invalid"
	}
	// encoding/json accepts case aliases and null scalars. Reject both before
	// typed decoding can turn an absent/unknown observed status into zero.
	for _, name := range required {
		value, present := properties[name]
		if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return report, "invalid"
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&report) != nil || decoder.Decode(new(any)) != io.EOF {
		return pipelineRecordDiagnosticReport{}, "invalid"
	}
	if !pipelineRecordDiagnosticPhases[report.Phase] ||
		pipelineRecordDiagnosticExit(report.ChildExit) != report.ChildExit ||
		pipelineRecordDiagnosticExit(report.DockerExit) != report.DockerExit ||
		pipelineRecordDiagnosticExit(report.ContainerExit) != report.ContainerExit ||
		(report.Operation == "unknown" && (report.DockerExit != -1 || report.ContainerExit != -1)) ||
		(report.ContainerExit != -1 && report.Operation != "wait") ||
		report.ChildSignal < 0 || report.ChildSignal > 64 ||
		(!report.ChildObserved && (report.ChildExit != -1 || report.ChildSignal != 0)) ||
		(report.ChildSignal != 0 && report.ChildExit != -1) || (report.Cancelled && report.Deadline) {
		return pipelineRecordDiagnosticReport{}, "invalid"
	}
	switch report.Event {
	case "unknown", "result", "broker", "context", "quarantine":
	default:
		return pipelineRecordDiagnosticReport{}, "invalid"
	}
	switch report.Cleanup {
	case "unknown", "not_started", "complete", "failed", "incomplete":
	default:
		return pipelineRecordDiagnosticReport{}, "invalid"
	}
	switch report.Operation {
	case "unknown", "create", "start", "wait", "resolve_context", "resolve_identity":
	default:
		return pipelineRecordDiagnosticReport{}, "invalid"
	}
	var resolutionOperation string
	switch report.Phase {
	case "docker_resolve_context_cancelled", "docker_resolve_context_deadline", "docker_resolve_context_capture", "docker_resolve_context_command":
		resolutionOperation = "resolve_context"
	case "docker_resolve_identity_cancelled", "docker_resolve_identity_deadline", "docker_resolve_identity_capture", "docker_resolve_identity_command":
		resolutionOperation = "resolve_identity"
	}
	if (resolutionOperation != "" && report.Operation != resolutionOperation) ||
		((report.Operation == "resolve_context" || report.Operation == "resolve_identity") && report.Operation != resolutionOperation) {
		return pipelineRecordDiagnosticReport{}, "invalid"
	}
	return report, "present"
}

func logSafePipelineRecordDiagnostic(t *testing.T, stderr []byte) {
	t.Helper()
	report, status := safePipelineRecordDiagnostic(stderr)
	if status != "present" {
		t.Logf("record diagnostic: status=%s", status)
		return
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Log("record diagnostic: status=invalid")
		return
	}
	t.Logf("record diagnostic: %s", data)
}

func pipelineRecordHostExit(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ProcessState != nil {
		return pipelineRecordDiagnosticExit(exit.ExitCode())
	}
	return -1
}

func pipelineRecordLiveBinaryDigest(path string) string {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "unavailable"
	}
	file, err := os.Open(path)
	if err != nil {
		return "unavailable"
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return "unavailable"
	}
	digest := sha256.New()
	const limit = 512 << 20
	n, err := io.Copy(digest, io.LimitReader(file, limit+1))
	if err != nil || n > limit {
		return "unavailable"
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func pipelineRecordLiveCleanupSummary(root string) (string, int, int) {
	file, err := os.Open(filepath.Join(root, "state", "wfctl", "pipeline-cleanup"))
	if errors.Is(err, os.ErrNotExist) {
		return "missing", 0, 0
	}
	if err != nil {
		return "read_error", 0, 0
	}
	defer file.Close()
	entries, err := file.ReadDir(65)
	if err != nil && !errors.Is(err, io.EOF) {
		return "read_error", 0, 0
	}
	status := "observed"
	if len(entries) > 64 {
		entries, status = entries[:64], "capped"
	}
	journals := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			journals++
		}
	}
	return status, len(entries), journals
}

func logPipelineRecordLiveFailure(t *testing.T, root, binary, imageDigest string) {
	t.Helper()
	if !t.Failed() {
		return
	}
	// Digest/count observations run before TempDir cleanup and only on failure.
	// No journal contents, names, paths, image name or workload output is read out.
	status, entries, journals := pipelineRecordLiveCleanupSummary(root)
	t.Logf("live fixture identity: executable_sha256=%s image_sha256=%s", pipelineRecordLiveBinaryDigest(binary), imageDigest)
	t.Logf("live fixture cleanup: status=%s entries=%d journals=%d", status, entries, journals)
}

func diagnosticTestReport(t *testing.T, diagnostic *pipelineRecordDiagnostic) (pipelineRecordDiagnosticReport, []byte) {
	t.Helper()
	var output bytes.Buffer
	diagnostic.emit(context.Background(), &output)
	report, status := safePipelineRecordDiagnostic(output.Bytes())
	if status != "present" {
		t.Fatalf("diagnostic did not satisfy its bounded closed schema: status=%s", status)
	}
	return report, output.Bytes()
}

func TestPipelineRecordDiagnosticsBrokerInjectedFailures(t *testing.T) {
	for _, tc := range []struct {
		name, fail, wait, phase, operation string
		cli, container                     int
		wantError                          bool
	}{
		{name: "create", fail: "create", phase: "container_create", operation: "create", cli: 7, container: -1, wantError: true},
		{name: "attach", fail: "start", phase: "container_attach", operation: "start", cli: 7, container: -1, wantError: true},
		{name: "wait", fail: "wait", phase: "container_wait", operation: "wait", cli: 7, container: -1, wantError: true},
		{name: "attach-before-command", fail: "after-create-server-change", phase: "container_attach", operation: "start", cli: -1, container: -1, wantError: true},
		{name: "wait-before-command", fail: "after-start-server-change", phase: "container_wait", operation: "wait", cli: -1, container: -1, wantError: true},
		{name: "invalid-status", wait: "secret-invalid-status", phase: "container_status", operation: "wait", cli: 0, container: -1, wantError: true},
		{name: "nonzero-container", wait: "23", phase: "container_command", operation: "wait", cli: 0, container: 23},
		{name: "success", wait: "0", phase: "unknown", operation: "wait", cli: 0, container: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, fixture := newPipelineDockerFixture(t)
			client := resolvePipelineDockerFixture(t)
			fixture.Fail, fixture.WaitStatus = tc.fail, tc.wait
			writePipelineDockerFixture(t, dir, fixture)
			state, err := openPipelineCleanupState(filepath.Join(pipelineRecordHostTempDir(t), "state"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = state.Close() })
			parent, err := pipelineProcessStartToken(os.Getpid())
			if err != nil {
				t.Fatal(err)
			}
			entry := pipelineCleanupEntry{Version: 1, Label: pipelineDockerTestLabel,
				Parent: pipelineProcessIdentity{PID: os.Getpid(), Start: parent},
				Child:  newPipelineDockerProcessGroup(t), Docker: client.Identity,
				Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
			diagnostic := newPipelineRecordDiagnostic()
			ctx := context.WithValue(context.Background(), pipelineRecordDiagnosticKey{}, diagnostic)
			result, err := executePipelineBrokerSandbox(ctx, pipelineBrokerRequest{
				Config: sandbox.DefaultSecureSandboxConfig("alpine:latest"), Command: []string{"true"},
			}, state, client, &entry)
			if (err != nil) != tc.wantError || (!tc.wantError && (result == nil || result.ExitCode != tc.container)) {
				t.Fatalf("injected operation did not produce expected outcome: error=%t", err != nil)
			}
			report, output := diagnosticTestReport(t, diagnostic)
			if report.Phase != tc.phase || report.Operation != tc.operation || report.DockerExit != tc.cli || report.ContainerExit != tc.container {
				t.Fatalf("diagnostic lost operation/status origin: %+v", report)
			}
			for _, secret := range []string{"TEST_SECRET_TOKEN", "secret-invalid-status", "raw daemon stderr", dir, parent} {
				if bytes.Contains(output, []byte(secret)) {
					t.Fatal("diagnostic reflected private operation data")
				}
			}
			if tc.name == "create" {
				// A later successful real broker execution cannot rewrite the
				// operation/status attached to the first failed execution.
				fixture.Fail = ""
				writePipelineDockerFixture(t, dir, fixture)
				if _, err := executePipelineBrokerSandbox(ctx, pipelineBrokerRequest{
					Config: sandbox.DefaultSecureSandboxConfig("alpine:latest"), Command: []string{"true"},
				}, state, client, &entry); err != nil {
					t.Fatal("subsequent synthetic broker execution failed")
				}
				report, _ = diagnosticTestReport(t, diagnostic)
				if report.Phase != "container_create" || report.Operation != "create" || report.DockerExit != 7 || report.ContainerExit != -1 {
					t.Fatal("subsequent success rewrote first-failure status")
				}
			}
			if tc.name == "nonzero-container" {
				// A real cleanup failure must not mask the earlier container failure.
				fixture.Containers[strings.Repeat("a", 64)] = pipelineDockerTestLabel
				fixture.Fail = "rm"
				writePipelineDockerFixture(t, dir, fixture)
				if finishPipelineCleanupObserved(ctx, state, client, entry, "cleanup") == nil {
					t.Fatal("injected cleanup failure was silently accepted")
				}
				report, _ = diagnosticTestReport(t, diagnostic)
				if report.Phase != "container_command" || report.Cleanup != "failed" {
					t.Fatalf("cleanup masked first failure: %+v", report)
				}
			}
		})
	}
}

func TestPipelineRecordDiagnosticsChildStatusAndConcurrency(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		exit, signal int
	}{
		{name: "exit", script: "exit 23", exit: 23},
		{name: "signal", script: "kill -TERM $$", exit: -1, signal: 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := exec.Command("/bin/sh", "-c", tc.script)
			if command.Run() == nil {
				t.Fatal("synthetic child did not fail")
			}
			diagnostic := newPipelineRecordDiagnostic()
			diagnostic.fail("child_exit")
			diagnostic.child(command.ProcessState)
			var workers sync.WaitGroup
			for range 8 {
				workers.Go(func() {
					for range 20 {
						diagnostic.beginOperation("create")
						diagnostic.dockerExit(0)
						diagnostic.containerExit(0)
						diagnostic.fail("cleanup")
						diagnostic.cleanup("failed")
						var output bytes.Buffer
						diagnostic.emit(context.Background(), &output)
					}
				})
			}
			workers.Wait()
			report, _ := diagnosticTestReport(t, diagnostic)
			if !report.ChildObserved || report.ChildExit != tc.exit || report.ChildSignal != tc.signal || report.Phase != "child_exit" || report.Operation != "unknown" || report.DockerExit != -1 || report.ContainerExit != -1 {
				t.Fatalf("lost actual child status under concurrent observations: %+v", report)
			}
		})
	}
	for _, code := range []int{-999, -2, 256, 1 << 20} {
		diagnostic := newPipelineRecordDiagnostic()
		diagnostic.beginOperation("private-operation-canary")
		diagnostic.dockerExit(code)
		diagnostic.containerExit(code)
		diagnostic.fail("private-phase-canary")
		diagnostic.event("private-event-canary")
		diagnostic.cleanup("private-cleanup-canary")
		report, output := diagnosticTestReport(t, diagnostic)
		if report.Phase != "unknown" || report.Event != "unknown" || report.Operation != "unknown" || report.Cleanup != "unknown" || report.DockerExit != -1 || report.ContainerExit != -1 || bytes.Contains(output, []byte("canary")) {
			t.Fatal("diagnostic accepted arbitrary strings or out-of-range status")
		}
	}
}

func TestPipelineRecordDiagnosticsRejectUntrustedReports(t *testing.T) {
	diagnostic := newPipelineRecordDiagnostic()
	diagnostic.fail("container_create")
	_, valid := diagnosticTestReport(t, diagnostic)
	aliasMissing := bytes.Replace(valid, []byte(`"container_exit":-1,`), nil, 1)
	aliasMissing = bytes.Replace(aliasMissing, []byte(`"phase":`), []byte(`"PHASE":"cleanup","phase":`), 1)
	for name, data := range map[string][]byte{
		"missing":                   []byte("private stderr canary"),
		"malformed":                 []byte(pipelineRecordDiagnosticPrefix + "{private-canary}"),
		"unknown-field":             bytes.Replace(valid, []byte(`"phase":`), []byte(`"private":"canary","phase":`), 1),
		"null-status":               bytes.Replace(valid, []byte(`"container_exit":-1`), []byte(`"container_exit":null`), 1),
		"missing-field":             bytes.Replace(valid, []byte(`"deadline":false`), []byte(`"foreign":false`), 1),
		"duplicate-key":             bytes.Replace(valid, []byte(`"phase":`), []byte(`"phase":"cleanup","phase":`), 1),
		"unknown-phase":             bytes.Replace(valid, []byte(`"container_create"`), []byte(`"private-phase-canary"`), 1),
		"exit-range":                bytes.Replace(valid, []byte(`"docker_cli_exit":-1`), []byte(`"docker_cli_exit":999`), 1),
		"exit-without-operation":    bytes.Replace(valid, []byte(`"docker_cli_exit":-1`), []byte(`"docker_cli_exit":0`), 1),
		"signal-range":              bytes.Replace(valid, []byte(`"child_signal":0`), []byte(`"child_signal":999`), 1),
		"oversize":                  []byte(pipelineRecordDiagnosticPrefix + strings.Repeat("x", maxPipelineRecordDiagnosticBytes)),
		"duplicate-report":          append(append([]byte{}, valid...), valid...),
		"case-alias-missing-status": aliasMissing,
	} {
		t.Run(name, func(t *testing.T) {
			if _, status := safePipelineRecordDiagnostic(data); status == "present" {
				t.Fatal("accepted an untrusted diagnostic report")
			}
		})
	}
	data := append([]byte("private surrounding stderr canary\n"), valid...)
	if _, status := safePipelineRecordDiagnostic(data); status != "present" {
		t.Fatal("unrelated stderr hid the bounded diagnostic")
	}
}

func TestPipelineRecordDiagnosticsPreserveEarlyResultFailure(t *testing.T) {
	diagnostic := newPipelineRecordDiagnostic()
	frame, err := encodePipelineResultFrame(map[string]any{"status": "error", "code": "pipeline_failed"})
	if err != nil {
		t.Fatal(err)
	}
	diagnostic.result(frame)
	diagnostic.fail("child_cleanup")
	diagnostic.cleanup("incomplete")
	report, _ := diagnosticTestReport(t, diagnostic)
	if report.Phase != "child_result" || report.Cleanup != "incomplete" {
		t.Fatal("teardown masked the already-drained error result")
	}
	diagnostic = newPipelineRecordDiagnostic()
	diagnostic.result([]byte("private malformed frame canary"))
	report, output := diagnosticTestReport(t, diagnostic)
	if report.Phase != "result_frame" || bytes.Contains(output, []byte("canary")) {
		t.Fatal("invalid private frame was reflected or unclassified")
	}
}

func TestPipelineRecordDiagnosticsActualHostFailureAndSilence(t *testing.T) {
	root := pipelineRecordHostTempDir(t)
	binary := filepath.Join(root, "wfctl")
	// Build before fixture HOME/opt-in changes, reusing the ordinary shared pool.
	buildPipelineRecordHostBinary(t, binary, ".")
	for _, tc := range []struct {
		name, enabled, config, phase string
		fail                         bool
	}{
		{name: "default-success", config: "builtin.yaml"},
		{name: "opted-success", enabled: "1", config: "builtin.yaml"},
		{name: "default-failure", config: "cli-print.yaml", fail: true},
		{name: "nonliteral-opt-in-failure", enabled: "true", config: "cli-print.yaml", fail: true},
		{name: "opted-failure", enabled: "1", config: "cli-print.yaml", phase: "child_quarantine", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newPipelineDockerFixture(t)
			t.Setenv("XDG_STATE_HOME", filepath.Join(pipelineRecordHostTempDir(t), "state"))
			t.Setenv(pipelineRecordDiagnosticEnv, tc.enabled)
			stdout, stderr, err := runPipelineRecordHost(t, binary, filepath.Join("testdata/pipeline-record", tc.config), "")
			if tc.fail {
				assertPipelineRecordHostError(t, stdout, stderr, err, "pipeline_failed")
			} else if err != nil || string(stdout) != "SDK_RECORD_V1 {\"ready\":true,\"source\":\"builtin\"}\n" {
				t.Fatalf("diagnostics changed actual host success: exit=%d", pipelineRecordHostExit(err))
			}
			report, status := safePipelineRecordDiagnostic(stderr)
			if tc.phase == "" {
				if status != "missing" {
					t.Fatal("diagnostics emitted on success or without exact opt-in")
				}
			} else if status != "present" || report.Phase != tc.phase || !report.ChildObserved || report.Cleanup != "complete" || report.Operation != "unknown" || report.DockerExit != -1 {
				t.Fatalf("actual host failure lost phase/child/cleanup evidence: status=%s report=%+v", status, report)
			}
			if bytes.Contains(stderr, []byte("fixture-cli-stdout-canary")) {
				t.Fatal("actual host diagnostics reflected quarantined workload output")
			}
		})
	}
}

func TestPipelineRecordDiagnosticsChildOptInNotInherited(t *testing.T) {
	for _, value := range pipelineRecordChildEnvironment([]string{pipelineRecordDiagnosticEnv + "=1", "PUBLIC_VALUE=selected"}, "/fixture/tools") {
		if strings.HasPrefix(value, pipelineRecordDiagnosticEnv+"=") {
			t.Fatal("record child inherited the parent's diagnostic opt-in")
		}
	}
}

func TestPipelineRecordDiagnosticsActualParentChildSignal(t *testing.T) {
	root := pipelineRecordHostTempDir(t)
	binary := filepath.Join(root, "wfctl")
	buildPipelineRecordHostBinary(t, binary, ".")
	pluginDir := filepath.Join(root, "plugins")
	installed := filepath.Join(pluginDir, "record-fixture")
	if err := os.MkdirAll(installed, 0700); err != nil {
		t.Fatal(err)
	}
	buildPipelineRecordHostBinary(t, filepath.Join(installed, "record-fixture"), "./testdata/pipeline-record/plugin")
	manifest, err := os.ReadFile("testdata/pipeline-record/plugin/plugin.json")
	if err != nil || os.WriteFile(filepath.Join(installed, "plugin.json"), manifest, 0600) != nil {
		t.Fatal("could not install synthetic SDK manifest")
	}
	newPipelineDockerFixture(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv(pipelineRecordDiagnosticEnv, "1")
	audit := filepath.Join(root, "synthetic-sdk-ready")
	process := startPipelineRecordHost(t, root, binary, "testdata/pipeline-record/external.yaml", pluginDir,
		"--var", "mode=wait", "--var", "audit_path="+audit)
	entry := readPipelineRecordHostEntry(t, root)
	if entry.Parent.PID != process.command.Process.Pid {
		t.Fatal("synthetic journal belongs to a different record parent")
	}
	waitPipelineRecordHostCondition(t, "synthetic SDK wait boundary", func() bool {
		_, err := os.Stat(audit)
		return err == nil
	})
	token, tokenErr := pipelineProcessStartToken(entry.Child.PID)
	group, groupErr := unix.Getpgid(entry.Child.PID)
	if tokenErr != nil || groupErr != nil || token != entry.Child.Start || group != entry.Child.PGID || entry.Child.PGID != entry.Child.PID {
		t.Fatal("synthetic child identity was not proven before fault injection")
	}
	// Kill only this test's recorded leader. The actual record parent retains
	// group custody, reaps this status and performs its unchanged cleanup.
	if unix.Kill(entry.Child.PID, unix.SIGKILL) != nil {
		t.Fatal("could not inject synthetic record child failure")
	}
	stdout, stderr, runErr := process.finish(t)
	assertPipelineRecordHostError(t, stdout, stderr, runErr, "pipeline_failed")
	report, status := safePipelineRecordDiagnostic(stderr)
	if status != "present" || !report.ChildObserved || report.ChildExit != -1 || report.ChildSignal != int(unix.SIGKILL) || report.Cleanup != "complete" ||
		(report.Phase != "result_frame" && report.Phase != "broker_closed") {
		t.Fatalf("actual parent lost synthetic child wait/cleanup evidence: status=%s report=%+v", status, report)
	}
	assertPipelineRecordHostJournalEmpty(t, root)
}

func TestPipelineRecordDiagnosticsCleanupAssertionHelper(t *testing.T) {
	if root := os.Getenv("WFCTL_RECORD_DIAGNOSTIC_ASSERT_ROOT"); root != "" {
		assertPipelineRecordHostJournalEmpty(t, root)
	}
}

func TestPipelineRecordDiagnosticsCleanupAssertionRedaction(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, retained := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "retained"}[retained], func(t *testing.T) {
			root := filepath.Join(pipelineRecordHostTempDir(t), "private-path-canary")
			if retained {
				directory := filepath.Join(root, "state", "wfctl", "pipeline-cleanup")
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "private-journal-canary.json"), []byte("private-content-canary"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestPipelineRecordDiagnosticsCleanupAssertionHelper$")
			command.Env = append(os.Environ(), pipelineRecordDiagnosticEnv+"=1", "WFCTL_RECORD_DIAGNOSTIC_ASSERT_ROOT="+root)
			output, runErr := command.CombinedOutput()
			if ctx.Err() != nil || pipelineRecordHostExit(runErr) != 1 || !bytes.Contains(output, []byte("host ")) {
				t.Fatal("synthetic cleanup assertion did not fail through its bounded safe path")
			}
			for _, secret := range []string{root, "private-path-canary", "private-journal-canary", "private-content-canary"} {
				if bytes.Contains(output, []byte(secret)) {
					t.Fatal("opted-in cleanup assertion reflected a private path/name/content")
				}
			}
		})
	}
}

func TestPipelineRecordDiagnosticsImageSetupHelper(t *testing.T) {
	if os.Getenv("WFCTL_RECORD_DIAGNOSTIC_IMAGE_SETUP") == "1" {
		pullPipelineRecordLiveImage(t)
	}
}

func TestPipelineRecordDiagnosticsImageSetupRedaction(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ command, phase string }{{"pull", "pull"}, {"image", "image_inspect"}} {
		t.Run(tc.command, func(t *testing.T) {
			const ambientCanary = "synthetic-image-setup-credential-canary"
			t.Setenv("GITHUB_TOKEN", ambientCanary)
			t.Setenv("NODE_AUTH_TOKEN", ambientCanary)
			directory, fixture := newPipelineDockerFixture(t)
			fixture.Fail = tc.command
			writePipelineDockerFixture(t, directory, fixture)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestPipelineRecordDiagnosticsImageSetupHelper$")
			command.Dir = directory
			// This setup path invokes Docker directly, whose test helper records
			// its environment. It needs only the fixed Docker wrapper, whose
			// interpreter and test binary are absolute, and no Go toolchain.
			command.Env = []string{
				"PATH=" + directory,
				"HOME=" + directory,
				"TMPDIR=" + directory,
				"GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local",
				"GOPROXY=off", "GOSUMDB=off", "GOVCS=*:off",
				"GORACE=" + os.Getenv("GORACE"),
				"WFCTL_RECORD_DIAGNOSTIC_IMAGE_SETUP=1",
				pipelineRecordDiagnosticEnv + "=",
			}
			for _, key := range []string{"GOCOVERDIR", "GOMAXPROCS", "GOMEMLIMIT"} {
				if value, present := os.LookupEnv(key); present {
					command.Env = append(command.Env, key+"="+value)
				}
			}
			output, runErr := command.CombinedOutput()
			want := "phase=" + tc.phase + " host_exit=7 cancelled=false deadline=false"
			if ctx.Err() != nil || pipelineRecordHostExit(runErr) != 1 || !bytes.Contains(output, []byte(want)) {
				t.Fatal("synthetic image setup did not expose only its fixed failure phase/status")
			}
			for _, secret := range []string{"TEST_SECRET_TOKEN", "raw daemon stderr", directory, ambientCanary, "GITHUB_TOKEN", "NODE_AUTH_TOKEN"} {
				if bytes.Contains(output, []byte(secret)) {
					t.Fatal("image setup reflected private Docker output or paths")
				}
			}
			calls, err := os.ReadFile(filepath.Join(directory, "calls.jsonl"))
			if err != nil || len(calls) == 0 {
				t.Fatal("synthetic image setup did not retain its private invocation capture")
			}
			for _, secret := range []string{ambientCanary, "GITHUB_TOKEN", "NODE_AUTH_TOKEN"} {
				if bytes.Contains(calls, []byte(secret)) {
					t.Fatal("image setup copied an ambient credential into its private capture")
				}
			}
			for _, relative := range []string{"go/pkg/mod", ".cache/go-build", "Library/Caches/go-build"} {
				if _, err := os.Lstat(filepath.Join(directory, filepath.FromSlash(relative))); !os.IsNotExist(err) {
					t.Fatal("synthetic image setup did not exclude private Go cache creation")
				}
			}
		})
	}
}

func TestPipelineRecordDiagnosticsLiveSummaryBoundsAndIdentity(t *testing.T) {
	root := pipelineRecordHostTempDir(t)
	if status, _, _ := pipelineRecordLiveCleanupSummary(root); status != "missing" {
		t.Fatal("missing cleanup state was reported as observed")
	}
	directory := filepath.Join(root, "state", "wfctl", "pipeline-cleanup")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "private-name-canary.json"), []byte("private journal contents canary"), 0600); err != nil {
		t.Fatal(err)
	}
	if status, entries, journals := pipelineRecordLiveCleanupSummary(root); status != "observed" || entries != 1 || journals != 1 {
		t.Fatal("cleanup count did not observe retained state")
	}
	for i := range 65 {
		name := filepath.Join(directory, strings.Repeat("x", i+1))
		if err := os.WriteFile(name, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if status, entries, journals := pipelineRecordLiveCleanupSummary(root); status != "capped" || entries != 64 || journals > 1 {
		t.Fatal("cleanup summary exceeded its directory observation bound")
	}
	binary := filepath.Join(root, "synthetic-binary")
	const content = "synthetic executable bytes"
	if err := os.WriteFile(binary, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(content))
	if pipelineRecordLiveBinaryDigest(binary) != hex.EncodeToString(want[:]) {
		t.Fatal("fixture digest did not identify the consumed bytes")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(binary, link); err != nil {
		t.Fatal(err)
	}
	if pipelineRecordLiveBinaryDigest(link) != "unavailable" {
		t.Fatal("fixture identity followed a substituted symlink")
	}
}

func TestPipelineRecordDiagnosticsContextClassification(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	deadline, stop := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer stop()
	for _, ctx := range []context.Context{cancelled, deadline} {
		diagnostic := newPipelineRecordDiagnostic()
		diagnostic.fail("cancelled")
		var output bytes.Buffer
		diagnostic.emit(ctx, &output)
		report, status := safePipelineRecordDiagnostic(output.Bytes())
		if status != "present" || report.Cancelled != (ctx.Err() == context.Canceled) || report.Deadline != (ctx.Err() == context.DeadlineExceeded) {
			t.Fatal("context classification lost cancellation/deadline distinction")
		}
	}
}

func TestPipelineRecordDiagnosticsResolverFailures(t *testing.T) {
	for _, tc := range []struct {
		name, fail, flood, metadata, server, phase, operation string
		exit, calls                                           int
	}{
		{name: "context-command", fail: "context", phase: "docker_resolve_context_command", operation: "resolve_context", exit: 7, calls: 1},
		{name: "identity-command", fail: "info", phase: "docker_resolve_identity_command", operation: "resolve_identity", exit: 7, calls: 2},
		{name: "context-capture", flood: "context", phase: "docker_resolve_context_capture", operation: "resolve_context", exit: 0, calls: 1},
		{name: "identity-capture", flood: "info", phase: "docker_resolve_identity_capture", operation: "resolve_identity", exit: 0, calls: 2},
		{name: "context-metadata", metadata: "private-context-metadata-canary", phase: "docker_resolve_context_metadata", operation: "unknown", exit: -1, calls: 1},
		{name: "endpoint", metadata: `[{"Name":"default","Endpoints":{"docker":{"Host":"private-endpoint-canary"}}}]`, phase: "docker_resolve_endpoint", operation: "unknown", exit: -1, calls: 1},
		{name: "tls-metadata", metadata: `[{"Name":"private-context-canary","Endpoints":{"docker":{"Host":"tcp://localhost:2376"}},"TLSMaterial":{"docker":["private-tls-canary"]}}]`, phase: "docker_resolve_tls", operation: "unknown", exit: -1, calls: 1},
		{name: "identity-metadata", server: "private-server-canary / invalid", phase: "docker_resolve_identity_metadata", operation: "unknown", exit: -1, calls: 2},
		{name: "private-config", phase: "docker_resolve_private_config", operation: "unknown", exit: -1, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory, fixture := newPipelineDockerFixture(t)
			fixture.Fail, fixture.Flood = tc.fail, tc.flood
			if tc.metadata != "" {
				fixture.Context = tc.metadata
			}
			if tc.server != "" {
				fixture.ServerID = tc.server
			}
			if tc.name == "private-config" {
				t.Setenv("TMPDIR", filepath.Join(directory, "nonexistent"))
			}
			writePipelineDockerFixture(t, directory, fixture)
			diagnostic := newPipelineRecordDiagnostic()
			ctx := context.WithValue(t.Context(), pipelineRecordDiagnosticKey{}, diagnostic)
			client, err := resolvePipelineDockerClient(ctx)
			if client != nil {
				_ = client.Close()
			}
			if err == nil || client != nil {
				t.Fatal("injected resolver failure was accepted")
			}
			report, output := diagnosticTestReport(t, diagnostic)
			if report.Phase != tc.phase || report.Operation != tc.operation || report.DockerExit != tc.exit || report.ContainerExit != -1 || report.ChildObserved || report.Cleanup != "not_started" {
				t.Fatal("resolver report lost actual failure stage/status origin")
			}
			if len(pipelineDockerCalls(t, directory)) != tc.calls {
				t.Fatal("resolver diagnostics added or omitted an existing CLI call")
			}
			for _, secret := range []string{"TEST_SECRET_TOKEN", "raw daemon stderr", "canary", directory, "daemon-A", "localhost"} {
				if bytes.Contains(output, []byte(secret)) {
					t.Fatal("resolver report reflected private input or output")
				}
			}
			if tc.name == "context-command" {
				for _, invalid := range [][]byte{
					bytes.Replace(output, []byte(`"resolve_context"`), []byte(`"private-operation-canary"`), 1),
					bytes.Replace(output, []byte(`"resolve_context"`), []byte(`"resolve_identity"`), 1),
					bytes.Replace(output, []byte(`"resolve_context"`), []byte(`"unknown"`), 1),
					bytes.Replace(output, []byte(`"docker_resolve_context_command"`), []byte(`"child_exit"`), 1),
					bytes.Replace(output, []byte(`"container_exit":-1`), []byte(`"container_exit":0`), 1),
				} {
					if _, status := safePipelineRecordDiagnostic(invalid); status == "present" {
						t.Fatal("safe consumer accepted an unknown or incoherent resolver status")
					}
				}
			}
		})
	}
}

func TestPipelineRecordDiagnosticsResolverCancellationAndDeadline(t *testing.T) {
	for _, stage := range []struct{ command, phase string }{{"context", "context"}, {"info", "identity"}} {
		for _, outcome := range []string{"cancelled", "deadline"} {
			t.Run(stage.phase+"-"+outcome, func(t *testing.T) {
				directory, fixture := newPipelineDockerFixture(t)
				fixture.Sleep = stage.command
				writePipelineDockerFixture(t, directory, fixture)
				diagnostic := newPipelineRecordDiagnostic()
				outer, cancel := context.WithCancel(t.Context())
				defer cancel()
				ctx := context.WithValue(outer, pipelineRecordDiagnosticKey{}, diagnostic)
				stop := make(chan struct{})
				observed := make(chan bool, 1)
				if outcome == "cancelled" {
					// Cancel only after the real selected helper has logged its
					// invocation, so identity cancellation cannot hit context discovery.
					go func() {
						deadline := time.NewTimer(10 * time.Second)
						defer deadline.Stop()
						tick := time.NewTicker(10 * time.Millisecond)
						defer tick.Stop()
						for {
							select {
							case <-stop:
								observed <- false
								return
							case <-deadline.C:
								cancel()
								observed <- false
								return
							case <-tick.C:
								data, _ := os.ReadFile(filepath.Join(directory, "calls.jsonl"))
								for _, line := range bytes.Split(data, []byte{'\n'}) {
									var call pipelineDockerInvocation
									if json.Unmarshal(line, &call) == nil {
										for _, arg := range call.Args {
											if arg == stage.command || (stage.command == "info" && arg == "dial-stdio") {
												cancel()
												observed <- true
												return
											}
										}
									}
								}
							}
						}
					}()
				}
				client, err := resolvePipelineDockerClient(ctx)
				close(stop)
				if outcome == "cancelled" && !<-observed {
					t.Fatal("cancellation control did not observe the selected real resolver call")
				}
				if client != nil {
					_ = client.Close()
				}
				if err == nil || client != nil {
					t.Fatal("resolver accepted the interrupted command")
				}
				var output bytes.Buffer
				diagnostic.emit(outer, &output)
				report, status := safePipelineRecordDiagnostic(output.Bytes())
				if status != "present" || report.Phase != "docker_resolve_"+stage.phase+"_"+outcome || report.Operation != "resolve_"+stage.phase || report.DockerExit != -1 || report.ContainerExit != -1 {
					t.Fatal("interrupted resolver command lost its actual closed outcome")
				}
				if report.Cancelled != (outcome == "cancelled") || report.Deadline || (outcome == "deadline" && outer.Err() != nil) {
					t.Fatal("inner command deadline was confused with outer context state")
				}
			})
		}
	}
}

func TestPipelineRecordDiagnosticsActualHostResolverFailure(t *testing.T) {
	root := pipelineRecordHostTempDir(t)
	binary := filepath.Join(root, "wfctl")
	buildPipelineRecordHostBinary(t, binary, ".")
	for _, enabled := range []string{"", "true", "1"} {
		t.Run("opt-in-"+enabled, func(t *testing.T) {
			directory, fixture := newPipelineDockerFixture(t)
			fixture.Fail = "context"
			writePipelineDockerFixture(t, directory, fixture)
			t.Setenv("XDG_STATE_HOME", filepath.Join(pipelineRecordHostTempDir(t), "state"))
			t.Setenv(pipelineRecordDiagnosticEnv, enabled)
			stdout, stderr, err := runPipelineRecordHost(t, binary, "testdata/pipeline-record/builtin.yaml", "")
			assertPipelineRecordHostError(t, stdout, stderr, err, "pipeline_failed")
			report, status := safePipelineRecordDiagnostic(stderr)
			if enabled == "1" {
				if status != "present" || report.Phase != "docker_resolve_context_command" || report.Operation != "resolve_context" || report.DockerExit != 7 || report.ChildObserved || report.Cleanup != "not_started" {
					t.Fatal("actual record parent did not consume the resolver observation")
				}
			} else if status != "missing" {
				t.Fatal("resolver emitted diagnostics without literal opt-in")
			}
			for _, secret := range []string{"TEST_SECRET_TOKEN", "raw daemon stderr", directory} {
				if bytes.Contains(stderr, []byte(secret)) {
					t.Fatal("actual record parent exposed private resolver stderr")
				}
			}
		})
	}
}

func TestPipelineRecordDiagnosticsResolverFirstFailureAndConcurrency(t *testing.T) {
	for _, first := range []bool{false, true} {
		t.Run(map[bool]string{false: "concurrent-first", true: "preserve-first"}[first], func(t *testing.T) {
			directory, fixture := newPipelineDockerFixture(t)
			fixture.Fail, fixture.Flood = "context", "info"
			writePipelineDockerFixture(t, directory, fixture)
			diagnostic := newPipelineRecordDiagnostic()
			ctx := context.WithValue(t.Context(), pipelineRecordDiagnosticKey{}, diagnostic)
			if first {
				if client, err := resolvePipelineDockerClient(ctx); err == nil || client != nil {
					t.Fatal("real first resolver failure was not injected")
				}
			}
			var workers sync.WaitGroup
			outcomes := make(chan bool, 8)
			for i := 0; i < 8; i++ {
				workers.Go(func() {
					operation, args, exit := "resolve_context", []string{"context", "inspect"}, 7
					var input []byte
					if i%2 == 1 {
						operation, args, exit = "resolve_identity", []string{"system", "dial-stdio"}, 0
						var err error
						input, err = pipelineDockerIdentityRequest("unix:///daemon-a.sock")
						if err != nil {
							outcomes <- false
							return
						}
					}
					result, err := runPipelineDockerCLIWithInput(pipelineDockerResolutionContext(ctx, operation), filepath.Join(directory, "docker"), []string{"PATH=" + directory, "HOME=" + directory, "LANG=C", "LC_ALL=C"}, pipelineDockerCallTimeout, input, args...)
					outcomes <- err != nil && result != nil && result.ExitCode == exit
				})
			}
			workers.Wait()
			close(outcomes)
			for ok := range outcomes {
				if !ok {
					t.Fatal("concurrent control missed a real command/capture failure")
				}
			}
			report, _ := diagnosticTestReport(t, diagnostic)
			contextFailure := report.Phase == "docker_resolve_context_command" && report.Operation == "resolve_context" && report.DockerExit == 7
			identityFailure := report.Phase == "docker_resolve_identity_capture" && report.Operation == "resolve_identity" && report.DockerExit == 0
			if (!contextFailure && !identityFailure) || (first && !contextFailure) || report.ContainerExit != -1 {
				t.Fatal("concurrent observations mixed or rewrote the first failure tuple")
			}
		})
	}
}
