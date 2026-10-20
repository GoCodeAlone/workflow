package main

// Dependency doubles below exercise the real Go smoke consumer's failure paths.
// They are explicitly not proof of wfctl, desktop activation or app runtime.
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func wantFailure(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("failure = %v, want %s", err, want)
	}
}

func TestUnknownAndMismatchedVersionsFail(t *testing.T) {
	for _, value := range []any{nil, "", "dev", "v0.85.4", "v0.87.0-rc.1", "v0.87.0-snapshot", "v0.86.3-0.20261020020202-4b2c11cc2d49", "private-value"} {
		if err := checkVersion(map[string]any{"name": "wfctl-mcp", "version": value}, "v0.86.2"); err == nil {
			t.Errorf("accepted version %#v", value)
		}
	}
	if err := checkVersion(map[string]any{"name": "wfctl-mcp", "version": "v0.86.2"}, "v0.86.2"); err != nil {
		t.Fatal(err)
	}
}

func TestStderrClassificationIsExactAndBounded(t *testing.T) {
	const sonicLine = "WARNING: sonic/ast only supports (go1.17~1.26 and amd64 CPU) or (go1.20~1.26 and arm64 CPU), but your environment is not suitable and will fallback to encoding/json"
	for _, tc := range []struct {
		name, input, want string
	}{
		{"empty", "", "empty"},
		{"legacy", deprecation + "\n", "known_deprecation_only"},
		{"legacy without newline", deprecation, "known_deprecation_only"},
		{"repeated legacy", deprecation + "\n" + deprecation + "\n", "known_deprecation_only"},
		{"sonic", sonicLine + "\n", "known_sonic_fallback_only"},
		{"sonic without newline", sonicLine, "known_sonic_fallback_only"},
		{"combined", sonicLine + "\n" + deprecation + "\n", "known_deprecation_and_sonic_fallback"},
		{"combined reversed", deprecation + "\n" + sonicLine + "\n", "known_deprecation_and_sonic_fallback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classifyStderr([]byte(tc.input))
			if err != nil || got != tc.want {
				t.Fatalf("classification = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, input := range []string{
		"SYNTHETIC_STDERR_CANARY", "prefix " + sonicLine, sonicLine + " suffix",
		strings.Replace(sonicLine, "1.26", "1.27", 1), sonicLine + "\r\n",
		"\n", "\n" + sonicLine, sonicLine + "\n\n", deprecation + "\n\n",
		sonicLine + "\nSYNTHETIC_STDERR_CANARY\n", deprecation + "\nSYNTHETIC_STDERR_CANARY\n",
		sonicLine + string([]byte{0xff}), sonicLine + "\x00", " " + deprecation,
	} {
		var report bytes.Buffer
		status := mainWith(t.Context(), nil, &report, func(context.Context, options) (map[string]any, error) {
			got, err := classifyStderr([]byte(input))
			if got != "" {
				t.Fatal("rejected stderr produced a success classification")
			}
			wantFailure(t, err, "unexpected_stderr")
			return nil, err
		})
		if status != 1 || strings.Contains(report.String(), "CANARY") || strings.Contains(report.String(), sonicLine) {
			t.Fatal("stderr leaked or failure not reported")
		}
	}
}

func TestWrongServerFails(t *testing.T) {
	wantFailure(t, checkVersion(map[string]any{"name": "other", "version": "v0.86.2"}, "v0.86.2"), "wrong_server")
}

func TestMissingToolOrArgumentSchemaFails(t *testing.T) {
	wantFailure(t, checkArguments(nil, "validate_config", map[string]any{"yaml_content": "x"}), "missing_tool")
	tools := map[string]map[string]any{"validate_config": {"inputSchema": map[string]any{"properties": map[string]any{"yaml_content": map[string]any{"type": "string"}}, "required": []any{"yaml_content"}}}}
	for _, args := range []map[string]any{{}, {"config": "x"}, {"yaml_content": "x", "target_version": "v1.0.0"}} {
		wantFailure(t, checkArguments(tools, "validate_config", args), "schema_changed")
	}
	if err := checkArguments(tools, "validate_config", map[string]any{"yaml_content": "x"}); err != nil {
		t.Fatal(err)
	}
	for _, schema := range []any{nil, "wrong", map[string]any{}, map[string]any{"properties": "wrong"}} {
		wantFailure(t, checkArguments(map[string]map[string]any{"validate_config": {"inputSchema": schema}}, "validate_config", map[string]any{}), "schema_changed")
	}
}

func TestProtocolAndToolErrorsFailWithoutEcho(t *testing.T) {
	for _, raw := range []string{`{}`, `{"jsonrpc":"2.0","id":9,"error":{"message":"SYNTHETIC_CANARY"}}`, `{"jsonrpc":"2.0","id":10,"result":{}}`, `{"jsonrpc":"2.0","id":true,"result":{}}`, `{"jsonrpc":"2.0","id":9.0,"result":{}}`} {
		value, _ := decodeJSON([]byte(raw))
		_, err := responseResult(value, 9)
		wantFailure(t, err, "protocol_error")
		if strings.Contains(err.Error(), "SYNTHETIC_CANARY") {
			t.Fatal("raw error leaked")
		}
	}
	for _, result := range []map[string]any{{"isError": true, "content": []any{map[string]any{"type": "text", "text": "SYNTHETIC_CANARY"}}}, {"content": []any{map[string]any{"type": "text", "text": "bad JSON"}}}, {"content": []any{map[string]any{"type": "text", "text": "[]"}}}, {"content": []any{}}} {
		_, err := toolJSON(result)
		wantFailure(t, err, "tool_error")
	}
}

func TestProtocolJSONRejectsInvalidUTF8(t *testing.T) {
	data := append([]byte(`{"jsonrpc":"2.0","id":9,"result":{"message":"`), 0xff)
	data = append(data, []byte(`"}}`)...)
	_, err := decodeJSON(data)
	wantFailure(t, err, "protocol_error")
}

func TestMissingExecutableIsBounded(t *testing.T) {
	var out bytes.Buffer
	status := mainWith(t.Context(), []string{"-wfctl", "missing-public-fixture-executable"}, &out, runSmoke)
	if status != 1 {
		t.Fatalf("status %d", status)
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report["reason"] != "missing_executable" {
		t.Fatalf("report %s", out.String())
	}
}

func TestRawErrorsConfigAndStderrAreNeverReported(t *testing.T) {
	canary := "SYNTHETIC_CANARY_DO_NOT_REPORT"
	for _, err := range []error{errors.New(canary), &os.PathError{Op: canary, Path: canary, Err: errors.New(canary)}, failure(canary)} {
		var out bytes.Buffer
		status := mainWith(t.Context(), nil, &out, func(context.Context, options) (map[string]any, error) { return nil, err })
		if status != 1 || strings.Contains(out.String(), canary) {
			t.Fatal("raw error leaked or failure not reported")
		}
		var report map[string]any
		if json.Unmarshal(out.Bytes(), &report) != nil || report["status"] != "failed" || report["reason"] != "internal_error" {
			t.Fatal("unsafe failure report")
		}
	}
}

func TestSnapshotDigestIsExactBytes(t *testing.T) {
	if digest([]byte("x\n")) == digest([]byte("x\r\n")) {
		t.Fatal("newline bytes lost")
	}
	if digest([]byte("abc")) != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal("wrong SHA-256")
	}
}

func helperClient(t *testing.T, mode string) *client {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c, err := startClient(t.Context(), executable, []string{"-test.run=^TestHelperProcess$", "--", mode}, "", []string{"GO_WANT_HELPER_PROCESS=1"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = c.close() })
	return c
}

func TestClientShutdownSendsEOFBeforeSignals(t *testing.T) {
	c := helperClient(t, "eof")
	stderr, err := c.close()
	if err != nil || len(stderr) != 0 {
		t.Fatalf("close = %q, %v", stderr, err)
	}
	if c.cmd.ProcessState == nil || c.cmd.ProcessState.ExitCode() != 0 {
		t.Fatal("child did not exit cleanly on EOF")
	}
}

func TestKnownWarningDoesNotMaskChildExitFailure(t *testing.T) {
	c := helperClient(t, "known-warning-exit")
	stderr, err := c.close()
	wantFailure(t, err, "check_failed")
	got, classificationErr := classifyStderr(stderr)
	if classificationErr != nil || got != "known_sonic_fallback_only" {
		t.Fatal("helper did not emit the known diagnostic")
	}
}

func TestEscalatedExitZeroDoesNotPassShutdown(t *testing.T) {
	c := helperClient(t, "term-zero")
	select {
	case ready := <-c.messages:
		if object(ready)["ready"] != true {
			t.Fatal("signal helper not ready")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("signal helper readiness timeout")
	}
	_, err := c.close()
	wantFailure(t, err, "check_failed")
	if c.cmd.ProcessState == nil || !c.cmd.ProcessState.Success() {
		t.Fatal("helper did not handle SIGTERM with exit 0")
	}
}

func TestRealChildCanaryOutputIsSuppressed(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	settings := map[string]any{"mcpServers": map[string]any{"workflow-config": map[string]any{"type": "stdio", "command": "wfctl", "args": []string{"-test.run=^TestHelperProcess$", "--", "canary"}, "cwd": "${PLUGIN_ROOT}", "env": map[string]string{"GO_WANT_HELPER_PROCESS": "1"}}}}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "mcp.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(root, "fixtures"), 0700); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	status := mainWith(t.Context(), []string{"-wfctl", executable}, &out, func(ctx context.Context, opts options) (map[string]any, error) {
		opts.root = root
		return runSmoke(ctx, opts)
	})
	if status != 1 || strings.Contains(out.String(), "SYNTHETIC_PROCESS_CANARY") {
		t.Fatal("child canary leaked or not failed")
	}
	var report map[string]any
	if json.Unmarshal(out.Bytes(), &report) != nil || report["reason"] != "protocol_error" {
		t.Fatalf("report: %s", out.String())
	}
}

func TestResponseTimeoutIsBounded(t *testing.T) {
	c := helperClient(t, "eof")
	c.responseTimeout = 20 * time.Millisecond
	_, err := c.request("initialize", map[string]any{})
	wantFailure(t, err, "timeout")
}

func TestBrokenStdinDoesNotInterruptCleanup(t *testing.T) {
	c := helperClient(t, "broken-stdin")
	select {
	case msg := <-c.messages:
		if msg == nil {
			t.Fatal("child not ready")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("child readiness timeout")
	}
	if err := c.send(map[string]any{"method": "pending buffered input"}); err == nil {
		t.Fatal("closed child stdin accepted write")
	}
	stderr, err := c.close()
	wantFailure(t, err, "check_failed")
	if len(stderr) != 0 {
		t.Fatalf("close = %q, %v", stderr, err)
	}
	if c.cmd.ProcessState == nil {
		t.Fatal("child not reaped")
	}
}

func TestFullMessageQueueDoesNotLeaveReaderAlive(t *testing.T) {
	c := helperClient(t, "flood")
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for len(c.messages) != cap(c.messages) {
		select {
		case <-timer.C:
			t.Fatal("child did not fill bounded queue")
		case <-ticker.C:
		}
	}
	stderr, err := c.close()
	if err != nil || len(stderr) != 0 {
		t.Fatalf("close = %q, %v", stderr, err)
	}
	select {
	case <-c.readerDone:
	default:
		t.Fatal("reader leaked after full queue shutdown")
	}
}

func TestNotificationsShareOneResponseDeadline(t *testing.T) {
	c := helperClient(t, "notifications")
	select {
	case ready := <-c.messages:
		if object(ready)["ready"] != true {
			t.Fatal("notification helper not ready")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("notification helper readiness timeout")
	}
	c.responseTimeout = 100 * time.Millisecond
	_, err := c.request("initialize", map[string]any{})
	wantFailure(t, err, "timeout")
	if c.notifications < 2 {
		t.Fatal("deadline test did not consume notifications; timing did not exercise the regression")
	}
}

func TestContextCancellationReapsSilentChildAndReader(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c, err := startClient(ctx, executable, []string{"-test.run=^TestHelperProcess$", "--", "silent"}, "", []string{"GO_WANT_HELPER_PROCESS=1"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = c.close() })
	select {
	case ready := <-c.messages:
		if object(ready)["ready"] != true {
			t.Fatal("silent helper not ready")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("silent helper readiness timeout")
	}
	cancel()
	// Reader completion before close proves cancellation itself stopped the child.
	select {
	case <-c.readerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("context cancellation did not stop silent child reader")
	}
	stderr, err := c.close()
	wantFailure(t, err, "check_failed")
	if len(stderr) != 0 {
		t.Fatalf("close = %q, %v", stderr, err)
	}
	if c.cmd.ProcessState == nil || c.cmd.ProcessState.Success() {
		t.Fatal("cancelled child was not reaped after termination")
	}
}

func TestInvalidUTF8FixtureRejectedBeforeLaunch(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	marker := filepath.Join(root, "child-launched")
	settings := map[string]any{"mcpServers": map[string]any{"workflow-config": map[string]any{
		"type": "stdio", "command": "wfctl", "args": []string{"-test.run=^TestHelperProcess$", "--", "launch-marker"},
		"cwd": "${PLUGIN_ROOT}", "env": map[string]string{"GO_WANT_HELPER_PROCESS": "1", "SMOKE_LAUNCH_MARKER": marker},
	}}}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "mcp.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(root, "fixtures"), 0700); err != nil {
		t.Fatal(err)
	}
	fixture := append(readFile(t, filepath.Join(packageRoot(t), "fixtures", "minimal.yaml.txt")), []byte("# invalid test comment: ")...)
	fixture = append(fixture, 0xff, '\n')
	if err = os.WriteFile(filepath.Join(root, "fixtures", "minimal.yaml.txt"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	status := mainWith(t.Context(), []string{"-wfctl", executable}, &out, func(ctx context.Context, opts options) (map[string]any, error) {
		opts.root = root
		return runSmoke(ctx, opts)
	})
	var report map[string]any
	if err = json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if status != 1 || report["status"] != "failed" || report["reason"] != "check_failed" {
		t.Errorf("invalid UTF-8 did not fail fixture loading: %s", out.String())
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Error("invalid UTF-8 fixture launched a child process")
	}
	if _, exists := report["fixture_sha256"]; exists {
		t.Error("invalid UTF-8 produced success evidence")
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "eof":
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "silent":
		fmt.Fprintln(os.Stdout, `{"ready":true}`)
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "canary":
		fmt.Fprintln(os.Stderr, sonicFallback)
		fmt.Fprintln(os.Stderr, "SYNTHETIC_PROCESS_CANARY")
		fmt.Fprintln(os.Stdout, "SYNTHETIC_PROCESS_CANARY")
	case "known-warning-exit":
		fmt.Fprintln(os.Stderr, sonicFallback)
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(2)
	case "term-zero":
		terminated := make(chan os.Signal, 1)
		signal.Notify(terminated, syscall.SIGTERM)
		fmt.Fprintln(os.Stderr, sonicFallback)
		fmt.Fprintln(os.Stdout, `{"ready":true}`)
		<-terminated
	case "launch-marker":
		if err := os.WriteFile(os.Getenv("SMOKE_LAUNCH_MARKER"), []byte("launched"), 0600); err != nil {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stdout, "SYNTHETIC_INVALID_PROTOCOL")
	case "broken-stdin":
		_ = os.Stdin.Close()
		fmt.Fprintln(os.Stdout, `{"ready":true}`)
		time.Sleep(time.Hour)
	case "flood":
		for range 64 {
			fmt.Fprintln(os.Stdout, `{}`)
		}
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "notifications":
		fmt.Fprintln(os.Stdout, `{"ready":true}`)
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			for range 30 {
				fmt.Fprintln(os.Stdout, `{"jsonrpc":"2.0","method":"notification"}`)
				time.Sleep(10 * time.Millisecond)
			}
			fmt.Fprintln(os.Stdout, `{"jsonrpc":"2.0","id":1,"result":{}}`)
		}
		_, _ = io.Copy(io.Discard, os.Stdin)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
