//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func pipelineRecordHostTempDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func buildPipelineRecordHostBinary(t *testing.T, output, target string) {
	t.Helper()
	buildPipelineRecordHostGo(t, output, target, true)
}

func buildPipelineRecordHostGo(t *testing.T, output, target string, race bool) {
	t.Helper()
	if binary := os.Getenv("WFCTL_RECORD_HOST_BINARY"); target == "." && binary != "" {
		if !filepath.IsAbs(binary) {
			t.Fatal("prebuilt host binary must be an absolute path")
		}
		info, err := os.Lstat(binary)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("prebuilt host binary must be an existing regular file: %v", err)
		}
		copyPipelineRecordHostBinary(t, binary, output)
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	args := []string{"build", "-o", output}
	if race {
		args = append(args, "-race")
	}
	args = append(args, target)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var data []byte
	var err error
	switch target {
	case ".", "./testdata/pipeline-record/plugin", "./testdata/pipeline-record/docker":
		data, err = buildFixtureArtifact(t, ctx, cmd, "../..", output)
	default:
		data, err = cmd.CombinedOutput()
	}
	if err != nil {
		t.Fatalf("build actual host/SDK fixture %s: %v\n%s", target, err, data)
	}
}

func TestPipelineRecordHostPrebuiltBinary(t *testing.T) {
	if output := os.Getenv("WFCTL_RECORD_HOST_COPY_PROBE_OUTPUT"); output != "" {
		buildPipelineRecordHostGo(t, output, ".", true)
		return
	}
	root := pipelineRecordHostTempDir(t)
	source := filepath.Join(root, "preverified-download")
	want := []byte("helper-unit-sentinel: not a runtime executable\n")
	if err := os.WriteFile(source, want, 0600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(root, "download-link")
	if err := os.Symlink(source, symlink); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		source string
		valid  bool
	}{
		{name: "selected-bytes", source: source, valid: true},
		{name: "missing-no-source-fallback", source: filepath.Join(root, "missing")},
		{name: "relative-no-source-fallback", source: "relative-download"},
		{name: "directory-no-source-fallback", source: root},
		{name: "symlink-no-source-fallback", source: symlink},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := filepath.Join(pipelineRecordHostTempDir(t), "wfctl")
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, self, "-test.run=^TestPipelineRecordHostPrebuiltBinary$", "-test.count=1")
			cmd.Env = append(os.Environ(), "WFCTL_RECORD_HOST_COPY_PROBE_OUTPUT="+output,
				"WFCTL_RECORD_HOST_BINARY="+tc.source, "PATH="+filepath.Join(root, "no-build-tools"))
			data, runErr := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("prebuilt selection probe exceeded deadline: %v\n%s", ctx.Err(), data)
			}
			if !tc.valid {
				if runErr == nil || !bytes.Contains(data, []byte("prebuilt host binary")) {
					t.Fatalf("invalid explicit binary did not fail closed before source build: error=%v\n%s", runErr, data)
				}
				if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("invalid explicit binary created a host output: %v", err)
				}
				return
			}
			if runErr != nil {
				t.Fatalf("explicit binary was not selected without build tools: %v\n%s", runErr, data)
			}
			got, err := os.ReadFile(output)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("selected host bytes differ from the explicit source: %q, %v", got, err)
			}
			info, err := os.Stat(output)
			if err != nil || info.Mode().Perm() != 0700 {
				t.Fatalf("copied host must have mode 0700: %v, %v", info, err)
			}
		})
	}
}

func runPipelineRecordHost(t *testing.T, binary, config, pluginDir string, extra ...string) ([]byte, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, pipelineRecordHostArgs(config, pluginDir, extra...)...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 15 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("built wfctl did not finish within its deadline: %v\nstdout=%s\nstderr=%s", ctx.Err(), stdout.Bytes(), stderr.Bytes())
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func pipelineRecordHostArgs(config, pluginDir string, extra ...string) []string {
	args := []string{"pipeline", "run", "-c", config, "-p", "selected", "--plugin-dir", pluginDir,
		"--output", "record", "--result-step", "result", "--record-prefix", "SDK_RECORD_V1", "--error-prefix", "SDK_RECORD_ERROR_V1"}
	return append(args, extra...)
}

func configurePipelineRecordHostDocker(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "docker-bin")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	buildPipelineRecordHostGo(t, filepath.Join(dir, "docker"), "./testdata/pipeline-record/docker", false)
	for _, key := range []string{"DOCKER_CONTEXT", "DOCKER_HOST", "DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "DOCKER_CONFIG"} {
		t.Setenv(key, "")
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", root)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
}

func TestPipelineRecordHostBuiltin(t *testing.T) {
	root := pipelineRecordHostTempDir(t)
	binary := filepath.Join(root, "wfctl")
	buildPipelineRecordHostBinary(t, binary, ".")
	configurePipelineRecordHostDocker(t, root)
	stdout, stderr, err := runPipelineRecordHost(t, binary, "testdata/pipeline-record/builtin.yaml", "")
	if err != nil || string(stdout) != "SDK_RECORD_V1 {\"ready\":true,\"source\":\"builtin\"}\n" {
		t.Fatalf("built wfctl did not emit one canonical builtin success record: error=%v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	assertPipelineRecordHostJournalEmpty(t, root)
	t.Run("cli-print-quarantine", func(t *testing.T) {
		caseRoot := pipelineRecordHostTempDir(t)
		t.Setenv("XDG_STATE_HOME", filepath.Join(caseRoot, "state"))
		stdout, stderr, runErr := runPipelineRecordHost(t, binary, "testdata/pipeline-record/cli-print.yaml", "")
		assertPipelineRecordHostError(t, stdout, stderr, runErr, "quarantine")
		assertPipelineRecordHostJournalEmpty(t, caseRoot)
	})
}

func TestPipelineRecordHostSDK(t *testing.T) {
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
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "plugin.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	configurePipelineRecordHostDocker(t, root)
	for _, tc := range []struct {
		name      string
		mode      string
		config    string
		code      string
		branch    bool
		late      bool
		composite bool
	}{
		{name: "success", mode: "success"},
		{name: "descriptors", mode: "descriptors"},
		{name: "error", mode: "error", code: "pipeline_failed"},
		{name: "stopped-error", mode: "stopped-error", code: "pipeline_failed"},
		{name: "number", mode: "number", code: "result_invalid"},
		{name: "result-overflow", mode: "result-overflow", code: "result_invalid"},
		{name: "invalid-utf8", mode: "invalid-utf8", code: "pipeline_failed"},
		{name: "stdout", mode: "stdout", code: "quarantine"},
		{name: "stdout-overflow", mode: "stdout-overflow", code: "quarantine"},
		{name: "stderr", mode: "stderr"},
		{name: "stderr-overflow", mode: "stderr-overflow", code: "quarantine"},
		{name: "late-stdout", mode: "late-stdout", config: "late-stdout.yaml", code: "quarantine", late: true},
		{name: "late-stdout-overflow", mode: "late-stdout-overflow", config: "late-stdout.yaml", code: "quarantine", late: true},
		{name: "branch-success", mode: "success", config: "branch.yaml", branch: true},
		{name: "branch-stopped-error", mode: "stopped-error", config: "branch.yaml", code: "pipeline_failed"},
		{name: "workflow-call-success", mode: "success", config: "workflow-call.yaml"},
		{name: "workflow-call-stopped-error", mode: "stopped-error", config: "workflow-call.yaml", code: "pipeline_failed"},
		{name: "missing-result", mode: "stop", config: "missing-result.yaml", code: "result_missing"},
		{name: "foreach", config: "foreach.yaml", composite: true},
		{name: "parallel", config: "parallel.yaml", composite: true},
		{name: "while", config: "while.yaml", composite: true},
		{name: "retry-with-backoff", config: "retry-with-backoff.yaml", composite: true},
		{name: "resilient-circuit-breaker", config: "resilient-circuit-breaker.yaml", composite: true},
		{name: "compensation", config: "compensation.yaml", code: "pipeline_failed", composite: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.composite {
				runPipelineRecordHostComposite(t, binary, pluginDir, tc.name, tc.config, tc.code)
				return
			}
			caseRoot := pipelineRecordHostTempDir(t)
			t.Setenv("XDG_STATE_HOME", filepath.Join(caseRoot, "state"))
			auditPath := filepath.Join(caseRoot, "sdk-execution.jsonl")
			config := tc.config
			if config == "" {
				config = "external.yaml"
			}
			stdout, stderr, runErr := runPipelineRecordHost(t, binary, filepath.Join("testdata/pipeline-record", config), pluginDir,
				"--var", "mode="+tc.mode, "--var", "audit_path="+auditPath, "--input", `{"marker":"fixture-input-canary"}`)
			audit := readPipelineRecordHostAudit(t, auditPath, tc.mode, stdout, stderr)
			if tc.late {
				armed := readPipelineRecordHostAudit(t, auditPath+".armed", "arm-late-stdout", stdout, stderr)
				response := readPipelineRecordHostAudit(t, auditPath+".response", tc.mode+"-response-confirmed", stdout, stderr)
				emitted := readPipelineRecordHostAudit(t, auditPath+".emitted", tc.mode+"-emitted", stdout, stderr)
				if armed.PID != audit.PID || response.PID != audit.PID || emitted.PID != audit.PID {
					t.Fatal("late-output barrier/emission did not come from the same SDK process")
				}
				wantEmission := map[string]string{"bytes": "64", "requested_bytes": strconv.Itoa(maxPipelineRecordBytes + 1), "stage": "prefix"}
				if tc.mode == "late-stdout" {
					count := strconv.Itoa(len("SDK_RECORD_V1 {\"forged_late\":true}\nfixture-late-stdout-canary\n"))
					wantEmission = map[string]string{"bytes": count, "requested_bytes": count, "stage": "complete"}
				}
				if !reflect.DeepEqual(emitted.Data, wantEmission) {
					t.Fatalf("late output did not audit actual completed bytes after the prior RPC response: got %+v, want %+v", emitted, wantEmission)
				}
			}
			if tc.code == "" {
				want := map[string]any{
					"source": "real-sdk", "echo": "fixture-input-canary", "prior": "host-prepared", "config": "host-prepared",
					"process_id": audit.PID, "private_descriptors_closed": true, "detail": map[string]any{"z": "last", "a": "first"},
				}
				if tc.branch {
					want = map[string]any{"branch": "success", "matched_value": "success"}
				}
				payload, err := json.Marshal(want)
				if err != nil {
					t.Fatal(err)
				}
				if runErr != nil || string(stdout) != "SDK_RECORD_V1 "+string(payload)+"\n" {
					t.Fatalf("SDK result was not selected/canonical/isolated: error=%v\nstdout=%s\nstderr=%s", runErr, stdout, stderr)
				}
			} else {
				assertPipelineRecordHostError(t, stdout, stderr, runErr, tc.code)
			}
			assertPipelineRecordHostJournalEmpty(t, caseRoot)
		})
	}
	t.Run("runtime-ownership", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			installed string
			stepTypes []string
		}{
			{"extra-factory", "record-fixture", []string{"step.record_fixture"}},
			{"missing-factory", "record-fixture", []string{"step.record_fixture", "step.record_fixture_aux", "step.record_fixture_absent"}},
			{"different-factory-same-count", "record-fixture", []string{"step.record_fixture", "step.record_fixture_absent"}},
			{"runtime-name", "record-fixture-renamed", []string{"step.record_fixture", "step.record_fixture_aux"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				caseRoot := pipelineRecordHostTempDir(t)
				t.Setenv("XDG_STATE_HOME", filepath.Join(caseRoot, "state"))
				casePlugins := filepath.Join(caseRoot, "plugins")
				dir := filepath.Join(casePlugins, tc.installed)
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				copyPipelineRecordHostBinary(t, filepath.Join(installed, "record-fixture"), filepath.Join(dir, tc.installed))
				var disk map[string]any
				if err := json.Unmarshal(manifest, &disk); err != nil {
					t.Fatal(err)
				}
				disk["name"], disk["stepTypes"] = tc.installed, tc.stepTypes
				data, err := json.Marshal(disk)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
				startupPath := filepath.Join(caseRoot, "sdk-startup.jsonl")
				executePath := filepath.Join(caseRoot, "sdk-execution.jsonl")
				t.Setenv("WFCTL_RECORD_FIXTURE_STARTUP", startupPath)
				stdout, stderr, runErr := runPipelineRecordHost(t, binary, "testdata/pipeline-record/external.yaml", casePlugins,
					"--var", "mode=success", "--var", "audit_path="+executePath)
				readPipelineRecordHostAudit(t, startupPath, "startup", stdout, stderr, tc.installed)
				if _, err := os.Stat(executePath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("runtime identity/catalog denial allowed step execution: %v", err)
				}
				assertPipelineRecordHostError(t, stdout, stderr, runErr, "pipeline_failed")
				assertPipelineRecordHostJournalEmpty(t, caseRoot)
			})
		}
	})
}

func runPipelineRecordHostComposite(t *testing.T, binary, pluginDir, name, config, code string) {
	t.Helper()
	for _, size := range []int{2, 3} {
		t.Run("pool-"+strconv.Itoa(size), func(t *testing.T) {
			root := pipelineRecordHostTempDir(t)
			t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
			auditPath := filepath.Join(root, "sdk-execution.jsonl")
			poolName := name + "-pool-" + strconv.Itoa(size)
			pool := make([]string, size)
			for i := range pool {
				pool[i] = poolName + "-participant-" + strconv.Itoa(i)
			}
			marker := "fixture-input-canary:" + poolName
			input, err := json.Marshal(map[string]any{
				"marker": marker, "pool_name": poolName, "pool": pool, "last_ordinal": strconv.Itoa(size - 1),
			})
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, runErr := runPipelineRecordHost(t, binary, filepath.Join("testdata/pipeline-record", config), pluginDir,
				"--var", "audit_path="+auditPath, "--input", string(input))
			audits := readPipelineRecordHostAudits(t, auditPath, stdout, stderr)
			wantAudits, wantOutput := pipelineRecordHostCompositeExpectations(t, name, poolName, marker, pool)
			if name == "foreach" || name == "parallel" {
				slices.SortFunc(audits, func(a, b pipelineRecordHostAudit) int {
					return strings.Compare(a.Data["ordinal"], b.Data["ordinal"])
				})
			}
			if len(audits) != len(wantAudits) {
				t.Fatalf("composite did not execute exactly its SDK calls: got %+v, want %+v\nstdout=%s\nstderr=%s", audits, wantAudits, stdout, stderr)
			}
			for i, audit := range audits {
				if audit.Mode != wantAudits[i].Mode || !reflect.DeepEqual(audit.Data, wantAudits[i].Data) {
					t.Fatalf("SDK call %d did not receive parent input, resolved config, or correct composite order: got %+v, want %+v", i, audit, wantAudits[i])
				}
			}
			if code != "" {
				assertPipelineRecordHostError(t, stdout, stderr, runErr, code)
			} else {
				payload, err := json.Marshal(wantOutput)
				if err != nil {
					t.Fatal(err)
				}
				if runErr != nil || string(stdout) != "SDK_RECORD_V1 "+string(payload)+"\n" {
					t.Fatalf("top-level result did not select nested SDK output and composite properties: error=%v\nstdout=%s\nstderr=%s\nwant=%s", runErr, stdout, stderr, payload)
				}
			}
			assertPipelineRecordHostJournalEmpty(t, root)
		})
	}
}

func pipelineRecordHostCompositeExpectations(t *testing.T, name, poolName, marker string, pool []string) ([]pipelineRecordHostAudit, map[string]any) {
	t.Helper()
	var audits []pipelineRecordHostAudit
	call := func(mode, action string, ordinal int, attempt string) {
		audits = append(audits, pipelineRecordHostAudit{Mode: mode, Data: map[string]string{
			"participant": pool[ordinal], "pool": poolName, "ordinal": strconv.Itoa(ordinal), "action": action,
			"attempt": attempt, "marker": marker, "prior_greeting": poolName, "pipeline": "selected",
		}})
	}
	output := map[string]any{"source": name, "pool": poolName, "echo": marker, "ready": "true"}
	switch name {
	case "foreach":
		for i := range pool {
			call("composite", "visit", i, "1")
		}
		output["count"], output["first"], output["last"] = strconv.Itoa(len(pool)), pool[0], pool[len(pool)-1]
	case "parallel":
		call("composite", "left", 0, "1")
		call("composite", "right", len(pool)-1, "1")
		output["completed"], output["failed"], output["left"], output["right"] = "2", "0", pool[0], pool[len(pool)-1]
	case "while":
		for i := range pool {
			call("composite-while", "page", i, "1")
		}
		output["iterations"], output["last"], output["continuing"] = strconv.Itoa(len(pool)), pool[len(pool)-1], "false"
	case "retry-with-backoff":
		call("composite-flaky", "retry", 0, "1")
		call("composite-flaky", "retry", 0, "2")
		output["retries"], output["attempt"], output["participant"] = "1", "2", pool[0]
	case "resilient-circuit-breaker":
		call("composite-error", "primary", 1, "1")
		call("composite", "fallback", 1, "1")
		output["retries"], output["open"], output["action"], output["participant"] = "1", "true", "fallback", pool[1]
	case "compensation":
		call("composite", "forward", 0, "1")
		call("composite-error", "fail", len(pool)-1, "1")
		call("composite-compensate", "undo-last", len(pool)-1, "1")
		call("composite-compensate", "undo-first", 0, "1")
		output = nil
	default:
		t.Fatalf("unknown composite fixture %q", name)
	}
	return audits, output
}

func copyPipelineRecordHostBinary(t *testing.T, source, destination string) {
	t.Helper()
	if err := copyFixtureBuildBinary(t.Context(), source, destination); err != nil {
		t.Fatal(err)
	}
}

type pipelineRecordHostAudit struct {
	PID  string            `json:"pid"`
	Mode string            `json:"mode"`
	Argv []string          `json:"argv"`
	Data map[string]string `json:"data,omitzero"`
}

func readPipelineRecordHostAudit(t *testing.T, path, mode string, stdout, stderr []byte, binaryNames ...string) pipelineRecordHostAudit {
	t.Helper()
	audits := readPipelineRecordHostAudits(t, path, stdout, stderr, binaryNames...)
	if len(audits) != 1 || audits[0].Mode != mode {
		t.Fatalf("expected exactly one matching SDK execution audit: %+v", audits)
	}
	return audits[0]
}

func readPipelineRecordHostAudits(t *testing.T, path string, stdout, stderr []byte, binaryNames ...string) []pipelineRecordHostAudit {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("SDK step was never invoked (preflight failures must not count as fixture denials): %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var audits []pipelineRecordHostAudit
	for {
		var audit pipelineRecordHostAudit
		if err := decoder.Decode(&audit); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("invalid SDK execution audit: %v", err)
		}
		audits = append(audits, audit)
	}
	if len(audits) == 0 {
		t.Fatal("empty SDK execution audit cannot prove runtime execution")
	}
	binaryName := "record-fixture"
	if len(binaryNames) != 0 {
		binaryName = binaryNames[0]
	}
	for _, audit := range audits {
		pid, err := strconv.Atoi(audit.PID)
		if err != nil || pid <= 0 || pid == os.Getpid() || len(audit.Argv) != 1 || filepath.Base(audit.Argv[0]) != binaryName || audit.PID != audits[0].PID {
			t.Fatalf("SDK calls did not execute in the same real installed subprocess: %+v", audit)
		}
		if strings.Contains(strings.Join(audit.Argv, " "), "fixture-input-canary") {
			t.Fatal("private input reached plugin argv")
		}
	}
	pid, _ := strconv.Atoi(audits[0].PID)
	// Register cleanup before asserting reaping, and only signal the same
	// process identity if the host's cleanup failed.
	start, startErr := pipelineProcessStartToken(pid)
	if startErr == nil {
		t.Cleanup(func() {
			current, err := pipelineProcessStartToken(pid)
			if err == nil && current == start {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		})
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return audits
		}
		select {
		case <-ctx.Done():
			t.Fatalf("SDK process %d survived built wfctl exit", pid)
		case <-ticker.C:
		}
	}
}

func assertPipelineRecordHostError(t *testing.T, stdout, stderr []byte, runErr error, code string) {
	t.Helper()
	if runErr == nil {
		t.Fatalf("fixture denial was reported as success: stdout=%s stderr=%s", stdout, stderr)
	}
	if !bytes.HasPrefix(stdout, []byte("SDK_RECORD_ERROR_V1 ")) || bytes.Count(stdout, []byte{'\n'}) != 1 {
		t.Fatalf("expected exactly one public error record: stdout=%s stderr=%s", stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSuffix(bytes.TrimPrefix(stdout, []byte("SDK_RECORD_ERROR_V1 ")), []byte{'\n'}), &payload); err != nil {
		t.Fatal(err)
	}
	gotCode, _ := payload["code"].(string)
	messages := map[string]string{"pipeline_failed": "Pipeline execution failed.", "result_missing": "The selected result is missing.", "result_invalid": "The pipeline result is invalid."}
	if messages[gotCode] == "" || (code != "quarantine" && gotCode != code) {
		t.Fatalf("wrong stable public error code: %#v, want %s", payload, code)
	}
	want := map[string]any{"code": gotCode, "message": messages[gotCode], "retryable": false}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("error was not fixed/redacted: %#v", payload)
	}
	canonical, err := json.Marshal(want)
	if err != nil || string(stdout) != "SDK_RECORD_ERROR_V1 "+string(canonical)+"\n" {
		t.Fatalf("public error was not canonical: error=%v stdout=%s", err, stdout)
	}
}

func assertPipelineRecordHostJournalEmpty(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "state", "wfctl", "pipeline-cleanup"))
	if err != nil {
		t.Fatalf("host never established cleanup state: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			t.Fatalf("host retained a journal after its empty fake-daemon cleanup: %s", entry.Name())
		}
	}
}

type pipelineRecordHostTransport struct {
	Root        string `json:"root"`
	BlockCreate bool   `json:"block_create"`
	LateCreate  bool   `json:"late_create"`
	Unavailable bool   `json:"unavailable"`
}

type pipelineRecordHostTransportEvent struct {
	PID   int      `json:"pid"`
	PGID  int      `json:"pgid"`
	Label string   `json:"label"`
	Args  []string `json:"args"`
}

type pipelineRecordHostProcess struct {
	command *exec.Cmd
	done    chan error
	stdout  *os.File
	stderr  *os.File
	waited  bool
}

func startPipelineRecordHost(t *testing.T, root, binary, config, pluginDir string, extra ...string) *pipelineRecordHostProcess {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	process := &pipelineRecordHostProcess{done: make(chan error, 1)}
	var err error
	process.stdout, err = os.OpenFile(filepath.Join(root, "parent.stdout"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	process.stderr, err = os.OpenFile(filepath.Join(root, "parent.stderr"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		cancel()
		_ = process.stdout.Close()
		t.Fatal(err)
	}
	process.command = exec.CommandContext(ctx, binary, pipelineRecordHostArgs(config, pluginDir, extra...)...)
	process.command.Stdout, process.command.Stderr = process.stdout, process.stderr
	if err := process.command.Start(); err != nil {
		cancel()
		_ = process.stdout.Close()
		_ = process.stderr.Close()
		t.Fatal(err)
	}
	go func() { process.done <- process.command.Wait() }()
	t.Cleanup(func() {
		cancel()
		if !process.waited {
			_ = process.command.Process.Kill()
			<-process.done
		}
		_ = process.stdout.Close()
		_ = process.stderr.Close()
	})
	return process
}

func (p *pipelineRecordHostProcess) finish(t *testing.T) ([]byte, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	var runErr error
	select {
	case runErr = <-p.done:
		p.waited = true
	case <-ctx.Done():
		t.Fatal("actual record parent did not exit within the cleanup deadline")
	}
	if err := errors.Join(p.stdout.Close(), p.stderr.Close()); err != nil {
		t.Fatal(err)
	}
	stdout, err := os.ReadFile(p.stdout.Name())
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.ReadFile(p.stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	return stdout, stderr, runErr
}

func waitPipelineRecordHostCondition(t *testing.T, description string, condition func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !condition() {
		select {
		case <-ctx.Done():
			t.Fatalf("actual host never reached %s (no preflight-only denial evidence)", description)
		case <-ticker.C:
		}
	}
}

func waitPipelineRecordHostJSON(t *testing.T, path string, target any) {
	t.Helper()
	waitPipelineRecordHostCondition(t, filepath.Base(path), func() bool {
		data, err := os.ReadFile(path)
		return err == nil && bytes.HasSuffix(data, []byte{'\n'}) && json.Unmarshal(data, target) == nil
	})
}

func trackPipelineRecordHostProcess(t *testing.T, pid, pgid int) pipelineProcessIdentity {
	t.Helper()
	start, err := pipelineProcessStartToken(pid)
	if err != nil {
		t.Fatalf("fixture readiness process was not alive: %d: %v", pid, err)
	}
	group, err := syscall.Getpgid(pid)
	t.Cleanup(func() {
		if current, err := pipelineProcessStartToken(pid); err == nil && current == start {
			if group == pid {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			} else {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	if err != nil || group != pgid {
		t.Fatalf("fixture process %d escaped journaled group %d: actual=%d error=%v", pid, pgid, group, err)
	}
	return pipelineProcessIdentity{PID: pid, PGID: pgid, Start: start}
}

func readPipelineRecordHostEntry(t *testing.T, root string) pipelineCleanupEntry {
	t.Helper()
	var entry pipelineCleanupEntry
	waitPipelineRecordHostCondition(t, "durable child identity journal", func() bool {
		paths, err := filepath.Glob(filepath.Join(root, "state", "wfctl", "pipeline-cleanup", "*.json"))
		if err != nil || len(paths) != 1 {
			return false
		}
		data, err := os.ReadFile(paths[0])
		return err == nil && json.Unmarshal(data, &entry) == nil && entry.Child.PID > 0
	})
	if entry.Child.PGID != entry.Child.PID || entry.Child.Start == "" || entry.Parent.Start == "" {
		t.Fatalf("journal did not contain full parent/child identities: %+v", entry)
	}
	return entry
}

func readPipelineRecordHostTransportCalls(t *testing.T, root string) []pipelineRecordHostTransportEvent {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var calls []pipelineRecordHostTransportEvent
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var call pipelineRecordHostTransportEvent
		if err := decoder.Decode(&call); errors.Is(err, io.EOF) {
			return calls
		} else if err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
}

func TestPipelineRecordHostCleanupMatrix(t *testing.T) {
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
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "plugin.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	configurePipelineRecordHostDocker(t, root)
	transportPath := filepath.Join(root, "docker-bin", "transport.json")
	for _, tc := range []struct {
		name        string
		phase       string
		signal      syscall.Signal
		resistant   bool
		late        bool
		unavailable bool
	}{
		{name: "normal-lifecycle", phase: "normal"},
		{name: "SIGKILL-before-create", phase: "before", signal: syscall.SIGKILL},
		{name: "SIGKILL-during-create-late-helper", phase: "create", signal: syscall.SIGKILL, late: true},
		{name: "SIGKILL-after-create", phase: "after", signal: syscall.SIGKILL},
		{name: "SIGTERM-resistant-SDK", phase: "before", signal: syscall.SIGTERM, resistant: true},
		{name: "SIGKILL-resistant-SDK-unavailable-retry", phase: "before", signal: syscall.SIGKILL, resistant: true, unavailable: true},
		{name: "SIGTERM-during-create-denies-late-helper", phase: "create", signal: syscall.SIGTERM, late: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caseRoot := pipelineRecordHostTempDir(t)
			t.Setenv("XDG_STATE_HOME", filepath.Join(caseRoot, "state"))
			transport := pipelineRecordHostTransport{Root: caseRoot, BlockCreate: tc.phase == "create", LateCreate: tc.late}
			writeTransport := func() {
				data, err := json.Marshal(transport)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(transportPath, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			writeTransport()
			beforeMode, afterMode := "success", "wait"
			if tc.phase == "normal" {
				afterMode = "success"
			}
			if tc.phase == "before" {
				beforeMode = "wait"
			}
			if tc.resistant {
				beforeMode = "term-resistant"
			}
			beforeAudit, afterAudit := filepath.Join(caseRoot, "before.jsonl"), filepath.Join(caseRoot, "after.jsonl")
			parent := startPipelineRecordHost(t, caseRoot, binary, "testdata/pipeline-record/cleanup.yaml", pluginDir,
				"--var", "before_mode="+beforeMode, "--var", "after_mode="+afterMode,
				"--var", "before_audit="+beforeAudit, "--var", "after_audit="+afterAudit,
				"--input", `{"marker":"fixture-input-canary"}`)
			if tc.phase == "normal" {
				stdout, stderr, err := parent.finish(t)
				if err != nil || string(stdout) != "SDK_RECORD_V1 {\"ready\":true,\"source\":\"cleanup-fixture\"}\n" {
					t.Fatalf("actual SDK/sandbox/broker lifecycle control failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
				}
				readPipelineRecordHostAudit(t, beforeAudit, "success", stdout, stderr)
				readPipelineRecordHostAudit(t, afterAudit, "success", stdout, stderr)
				assertPipelineRecordHostJournalEmpty(t, caseRoot)
				return
			}
			var sdkAudit pipelineRecordHostAudit
			waitPipelineRecordHostJSON(t, beforeAudit, &sdkAudit)
			entry := readPipelineRecordHostEntry(t, caseRoot)
			if entry.Parent.PID != parent.command.Process.Pid {
				t.Fatal("journal belongs to a different CLI parent")
			}
			trackPipelineRecordHostProcess(t, entry.Child.PID, entry.Child.PGID)
			sdkPID, err := strconv.Atoi(sdkAudit.PID)
			if err != nil || sdkAudit.Mode != beforeMode {
				t.Fatalf("wrong real SDK readiness marker: %+v", sdkAudit)
			}
			trackPipelineRecordHostProcess(t, sdkPID, entry.Child.PGID)
			var lateProcess pipelineProcessIdentity
			if tc.resistant {
				var descendant pipelineRecordHostAudit
				waitPipelineRecordHostJSON(t, beforeAudit+".descendant", &descendant)
				pid, err := strconv.Atoi(descendant.PID)
				if err != nil || descendant.Mode != "term-resistant-descendant" {
					t.Fatalf("resistant SDK descendant was not ready: %+v", descendant)
				}
				trackPipelineRecordHostProcess(t, pid, entry.Child.PGID)
			}
			switch tc.phase {
			case "create":
				var creating pipelineRecordHostTransportEvent
				waitPipelineRecordHostJSON(t, filepath.Join(caseRoot, "create.ready"), &creating)
				if creating.Label != entry.Label {
					t.Fatal("broker create used a different cleanup label")
				}
				trackPipelineRecordHostProcess(t, creating.PID, entry.Child.PGID)
				if tc.late {
					var late pipelineRecordHostTransportEvent
					waitPipelineRecordHostJSON(t, filepath.Join(caseRoot, "late.ready"), &late)
					lateProcess = trackPipelineRecordHostProcess(t, late.PID, entry.Child.PGID)
				}
				entry = readPipelineRecordHostEntry(t, caseRoot)
				if len(entry.CIDFiles) != 1 || len(entry.IDs) != 0 {
					t.Fatalf("create was not journaled before receiving its ID: %+v", entry)
				}
			case "after":
				var after pipelineRecordHostAudit
				waitPipelineRecordHostJSON(t, afterAudit, &after)
				entry = readPipelineRecordHostEntry(t, caseRoot)
				if after.Mode != "wait" || after.PID != sdkAudit.PID || len(entry.IDs) != 1 || entry.IDs[0] != strings.Repeat("a", 64) {
					t.Fatalf("post-create gate did not follow successful real broker work: %+v, %+v", after, entry)
				}
			}
			calls := readPipelineRecordHostTransportCalls(t, caseRoot)
			var operations []string
			for _, call := range calls {
				if len(call.Args) != 0 && slices.Contains([]string{"create", "start", "wait"}, call.Args[0]) {
					operations = append(operations, call.Args[0])
					if call.PGID != entry.Child.PGID {
						t.Fatalf("broker CLI escaped child custody: %+v, %+v", call, entry.Child)
					}
				}
			}
			wantOperations := []string{}
			switch tc.phase {
			case "create":
				wantOperations = []string{"create"}
			case "after":
				wantOperations = []string{"create", "start", "wait"}
			}
			if !slices.Equal(operations, wantOperations) {
				t.Fatalf("wrong interruption boundary: %v, want %v", operations, wantOperations)
			}
			if err := parent.command.Process.Signal(tc.signal); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, runErr := parent.finish(t)
			if tc.signal == syscall.SIGTERM {
				assertPipelineRecordHostError(t, stdout, stderr, runErr, "pipeline_failed")
			} else {
				if runErr == nil || len(stdout) != 0 {
					t.Fatalf("SIGKILL emitted a false public result: %v, stdout=%s stderr=%s", runErr, stdout, stderr)
				}
				retained := readPipelineRecordHostEntry(t, caseRoot)
				if retained.Label != entry.Label || retained.Child != entry.Child || retained.Docker != entry.Docker {
					t.Fatalf("SIGKILL lost durable cleanup identity: %+v", retained)
				}
				transport.BlockCreate, transport.LateCreate, transport.Unavailable = false, false, tc.unavailable
				writeTransport()
				stdout, stderr, runErr = runPipelineRecordHost(t, binary, "testdata/pipeline-record/builtin.yaml", "")
				if tc.unavailable {
					assertPipelineRecordHostError(t, stdout, stderr, runErr, "pipeline_failed")
					retained := readPipelineRecordHostEntry(t, caseRoot)
					if retained.Label != entry.Label || retained.Docker != entry.Docker {
						t.Fatal("unavailable daemon lost the pinned retry journal")
					}
					if active, err := pipelineProcessGroupActive(entry.Child.PGID); err != nil || active {
						t.Fatalf("daemon outage left the old SDK group active: %v, %v", active, err)
					}
					transport.Unavailable = false
					writeTransport()
					stdout, stderr, runErr = runPipelineRecordHost(t, binary, "testdata/pipeline-record/builtin.yaml", "")
				}
				if runErr != nil || string(stdout) != "SDK_RECORD_V1 {\"ready\":true,\"source\":\"builtin\"}\n" {
					parentToken, parentErr := pipelineProcessStartToken(entry.Parent.PID)
					childToken, childErr := pipelineProcessStartToken(entry.Child.PID)
					active, groupErr := pipelineProcessGroupActive(entry.Child.PGID)
					t.Logf("retained identities: parent=%+v current=%q error=%v; child=%+v current=%q error=%v; group_active=%v error=%v",
						entry.Parent, parentToken, parentErr, entry.Child, childToken, childErr, active, groupErr)
					t.Fatalf("restart did not reconcile before pipeline execution: %v\nstdout=%s\nstderr=%s", runErr, stdout, stderr)
				}
			}
			if active, err := pipelineProcessGroupActive(entry.Child.PGID); err != nil || active {
				t.Fatalf("record parent left its journaled process group alive: %v, %v", active, err)
			}
			if tc.late {
				if err := os.WriteFile(filepath.Join(caseRoot, "late.release"), nil, 0600); err != nil {
					t.Fatal(err)
				}
				waitPipelineRecordHostCondition(t, "late-helper gone or observed late create", func() bool {
					_, lateErr := os.Stat(filepath.Join(caseRoot, "late.created"))
					current, err := pipelineProcessStartToken(lateProcess.PID)
					active, groupErr := pipelineProcessGroupActive(lateProcess.PGID)
					return lateErr == nil || pipelineProcessAbsent(err) || (err == nil && current != lateProcess.Start) || (groupErr == nil && !active)
				})
				if _, err := os.Stat(filepath.Join(caseRoot, "late.created")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("custodial CLI helper created after shutdown/empty cleanup: %v", err)
				}
			}
			assertPipelineRecordHostJournalEmpty(t, caseRoot)
			var containers map[string]string
			state, err := os.ReadFile(filepath.Join(caseRoot, "containers.json"))
			if err == nil {
				if err := json.Unmarshal(state, &containers); err != nil || len(containers) != 0 {
					t.Fatalf("cleanup left transport-owned container state: %s, %v", state, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Join(caseRoot, "state", "wfctl", "pipeline-cleanup"))
			if err != nil || len(entries) != 1 || entries[0].Name() != "lock" {
				t.Fatalf("cleanup retained a CID file or journal: %v, %v", entries, err)
			}
		})
	}
}

func TestPipelineRecordHostLiveDockerBuiltin(t *testing.T) {
	if os.Getenv("WFCTL_RECORD_HOST_LIVE_DOCKER") != "1" {
		t.Skip("requires explicit live Docker opt-in; fake CLI tests are not daemon-health evidence")
	}
	root := pipelineRecordHostTempDir(t)
	binary := filepath.Join(root, "wfctl")
	buildPipelineRecordHostBinary(t, binary, ".")
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	stdout, stderr, err := runPipelineRecordHost(t, binary, "testdata/pipeline-record/builtin.yaml", "")
	if err != nil || string(stdout) != "SDK_RECORD_V1 {\"ready\":true,\"source\":\"builtin\"}\n" {
		t.Fatalf("built wfctl failed live Docker builtin lifecycle: error=%v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	assertPipelineRecordHostJournalEmpty(t, root)
}
