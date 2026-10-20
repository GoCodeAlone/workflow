package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	workflowmcp "github.com/GoCodeAlone/workflow/mcp"
)

const stdioDeadline = 5 * time.Second
const stdioPublicYAML = "modules:\n  - name: public_web\n    type: http.server\n    config:\n      address: ':8080'\n"
const stdioCanary = "SYNTHETIC_STDIO_PRIVATE_CANARY"
const handlerEntered = "WFCTL_MCP_TEST_HANDLER_ENTERED"
const handlerCancelled = "WFCTL_MCP_TEST_HANDLER_CANCELLED"

// This dependency double exercises Workflow's existing optional engine tool
// through the real SDK transport; it cannot execute application workflows.
type stdioBlockingEngine struct{}

func (*stdioBlockingEngine) BuildFromConfig(*config.WorkflowConfig) error { return nil }
func (*stdioBlockingEngine) Start(context.Context) error                  { return nil }
func (*stdioBlockingEngine) Stop(context.Context) error                   { return nil }
func (*stdioBlockingEngine) TriggerWorkflow(ctx context.Context, _ string, action string, _ map[string]any) error {
	if action == "block_until_cancelled" {
		fmt.Fprintln(os.Stderr, handlerEntered)
		<-ctx.Done()
		fmt.Fprintln(os.Stderr, handlerCancelled)
		return ctx.Err()
	}
	return nil
}

// A separate process prevents test-framework output from entering MCP stdout.
// Production mode calls runMCP. Transport mode is always this test executable,
// even when WFCTL_MCP_TEST_BINARY selects a real CLI for production-mode tests.
func TestMCPStdioLegacyHelperProcess(t *testing.T) {
	mode := os.Getenv("WFCTL_MCP_TEST_HELPER")
	if mode == "" {
		return
	}
	var err error
	switch mode {
	case "transport":
		err = workflowmcp.NewServer("plugins", workflowmcp.WithEngine(&stdioBlockingEngine{})).ServeStdio()
	case "production":
		err = runMCP([]string{"-plugin-dir", "plugins", "-documentation-file", "public-reference.md"})
	default:
		os.Exit(2)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "WFCTL_MCP_TEST_CONTEXT_CANCELLED_EXIT")
		} else {
			fmt.Fprintln(os.Stderr, "WFCTL_MCP_TEST_SERVE_FAILED")
		}
		os.Exit(1)
	}
	os.Exit(0)
}

type stdioReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type stdioFrame struct {
	reply stdioReply
	err   error
}

type stdioContractClient struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	frames      chan stdioFrame
	barriers    chan string
	done        chan struct{}
	readersDone chan struct{}
	stopReaders chan struct{}
	writers     sync.WaitGroup
	waitErr     error // Published by closing done.
	stderrMu    sync.Mutex
	stderr      strings.Builder
	stderrErr   bool
	next        int
}

func startStdioContract(t *testing.T, transport bool) *stdioContractClient {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"plugins", "tmp"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "public-reference.md"), []byte("# Public synthetic reference\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	mode := "production"
	args := []string{"-test.run=^TestMCPStdioLegacyHelperProcess$"}
	if transport {
		mode = "transport"
	} else if override := os.Getenv("WFCTL_MCP_TEST_BINARY"); override != "" {
		bin = override
		args = []string{"mcp", "-plugin-dir", "plugins", "-documentation-file", "public-reference.md"}
	}
	c := &stdioContractClient{frames: make(chan stdioFrame, 32), barriers: make(chan string, 8), done: make(chan struct{}), readersDone: make(chan struct{}), stopReaders: make(chan struct{})}
	c.cmd = exec.Command(bin, args...) //nolint:gosec // Explicit test-owned executable, never user tool input.
	c.cmd.Dir = root
	// No ambient HOME, CODEX_HOME, plugin locations, credentials or app config.
	c.cmd.Env = []string{"PATH=/usr/bin:/bin", "TMPDIR=" + filepath.Join(root, "tmp"), "WFCTL_NO_UPDATE_CHECK=1", "WFCTL_DIFFCACHE=disabled", "WFCTL_MCP_TEST_HELPER=" + mode}
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_ = c.stdin.Close()
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		_ = c.stdin.Close()
		_ = outR.Close()
		_ = outW.Close()
		t.Fatal(err)
	}
	c.cmd.Stdout, c.cmd.Stderr = outW, errW
	if err := c.cmd.Start(); err != nil {
		_ = c.stdin.Close()
		_ = outR.Close()
		_ = outW.Close()
		_ = errR.Close()
		_ = errW.Close()
		t.Fatal(err)
	}
	_ = outW.Close()
	_ = errW.Close()
	var readers sync.WaitGroup
	readers.Add(2)
	publish := func(frame stdioFrame) bool {
		select {
		case c.frames <- frame:
			return true
		case <-c.stopReaders:
			return false
		}
	}
	go func() {
		defer readers.Done()
		defer outR.Close()
		defer close(c.frames)
		scan := bufio.NewScanner(outR)
		scan.Buffer(make([]byte, 4096), 4<<20)
		for scan.Scan() {
			var reply stdioReply
			err := json.Unmarshal(scan.Bytes(), &reply)
			if err != nil || reply.JSONRPC != "2.0" || len(reply.ID) == 0 {
				if !publish(stdioFrame{err: fmt.Errorf("stdout contained non-response JSON-RPC framing")}) {
					return
				}
				continue
			}
			if !publish(stdioFrame{reply: reply}) {
				return
			}
		}
		if scan.Err() != nil {
			publish(stdioFrame{err: fmt.Errorf("stdout reader failed")})
		}
	}()
	go func() {
		defer readers.Done()
		defer errR.Close()
		scan := bufio.NewScanner(errR)
		scan.Buffer(make([]byte, 4096), 4<<20)
		for scan.Scan() {
			line := scan.Text()
			c.stderrMu.Lock()
			c.stderr.WriteString(line + "\n")
			c.stderrMu.Unlock()
			if line == handlerEntered || line == handlerCancelled {
				select {
				case c.barriers <- line:
				case <-c.stopReaders:
					return
				}
			}
		}
		if scan.Err() != nil {
			c.stderrMu.Lock()
			c.stderrErr = true
			c.stderrMu.Unlock()
		}
	}()
	go func() { readers.Wait(); close(c.readersDone) }()
	go func() { c.waitErr = c.cmd.Wait(); close(c.done) }()
	t.Cleanup(func() {
		close(c.stopReaders)
		_ = c.stdin.Close()
		select {
		case <-c.done:
		default:
			_ = c.cmd.Process.Kill()
		}
		c.writers.Wait() // Closing stdin releases any timed-out write.
		select {
		case <-c.done:
		case <-time.After(stdioDeadline):
			t.Error("child was not reaped")
		}
		select {
		case <-c.readersDone:
		case <-time.After(stdioDeadline):
			t.Error("stdio readers did not finish")
		}
		c.stderrMu.Lock()
		defer c.stderrMu.Unlock()
		if strings.Contains(c.stderr.String(), stdioCanary) {
			t.Error("raw synthetic canary leaked to stderr")
		}
		if c.stderrErr {
			t.Error("stderr reader failed; canary audit incomplete")
		}
	})
	return c
}

func (c *stdioContractClient) send(t *testing.T, message any) {
	t.Helper()
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	c.sendRaw(t, string(data))
}

func (c *stdioContractClient) sendRaw(t *testing.T, line string) {
	t.Helper()
	written := make(chan error, 1)
	c.writers.Add(1)
	go func() { defer c.writers.Done(); _, err := io.WriteString(c.stdin, line+"\n"); written <- err }()
	select {
	case err := <-written:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(stdioDeadline):
		_ = c.cmd.Process.Kill()
		t.Fatal("stdio write timed out")
	}
}

func (c *stdioContractClient) receive(t *testing.T, id string) stdioReply {
	t.Helper()
	select {
	case frame, ok := <-c.frames:
		if !ok {
			t.Fatal("stdout closed before response")
		}
		if frame.err != nil {
			t.Fatal(frame.err)
		}
		if string(frame.reply.ID) != id {
			t.Fatalf("unexpected response ID: got %s, want %s", frame.reply.ID, id)
		}
		return frame.reply
	case <-time.After(stdioDeadline):
		t.Fatal("stdio response timed out")
	}
	return stdioReply{}
}

func (c *stdioContractClient) request(t *testing.T, method string, params any) stdioReply {
	t.Helper()
	c.next++
	c.send(t, map[string]any{"jsonrpc": "2.0", "id": c.next, "method": method, "params": params})
	return c.receive(t, fmt.Sprint(c.next))
}

func (c *stdioContractClient) initialize(t *testing.T, version string) {
	t.Helper()
	reply := c.request(t, "initialize", map[string]any{"protocolVersion": version, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "public-contract-test", "version": "1"}})
	var result struct {
		ProtocolVersion string                         `json:"protocolVersion"`
		ServerInfo      struct{ Name, Version string } `json:"serverInfo"`
	}
	if reply.Error != nil || json.Unmarshal(reply.Result, &result) != nil || result.ProtocolVersion != version || result.ServerInfo.Name != "wfctl-mcp" || result.ServerInfo.Version == "" {
		t.Fatal("initialize failed to negotiate requested legacy version and server identity")
	}
	c.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
}

func (c *stdioContractClient) call(t *testing.T, name string, args map[string]any, wantError bool) string {
	t.Helper()
	reply := c.request(t, "tools/call", map[string]any{"name": name, "arguments": args})
	return assertStdioTool(t, reply, wantError)
}

func assertStdioTool(t *testing.T, reply stdioReply, wantError bool) string {
	t.Helper()
	var result struct {
		IsError bool                          `json:"isError"`
		Content []struct{ Type, Text string } `json:"content"`
	}
	if reply.Error != nil || json.Unmarshal(reply.Result, &result) != nil || result.IsError != wantError || len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatal("unexpected tool error/content contract")
	}
	return result.Content[0].Text
}

func (c *stdioContractClient) inventory(t *testing.T, engine bool) {
	t.Helper()
	reply := c.request(t, "tools/list", map[string]any{})
	var tools struct {
		Tools []struct{ Name string } `json:"tools"`
	}
	if reply.Error != nil || json.Unmarshal(reply.Result, &tools) != nil {
		t.Fatal("tools/list failed")
	}
	want := 33
	if engine {
		want++
	}
	if len(tools.Tools) != want {
		t.Fatalf("got %d tools, want %d", len(tools.Tools), want)
	}
	seen := map[string]bool{}
	for _, tool := range tools.Tools {
		if seen[tool.Name] {
			t.Fatal("duplicate tool")
		}
		seen[tool.Name] = true
	}
	if seen["run_workflow"] != engine {
		t.Fatal("conditional engine tool visibility mismatch")
	}
	reply = c.request(t, "resources/list", map[string]any{})
	var resources struct {
		Resources []struct {
			URI string `json:"uri"`
		} `json:"resources"`
	}
	if reply.Error != nil || json.Unmarshal(reply.Result, &resources) != nil || len(resources.Resources) != 5 {
		t.Fatal("expected five resources")
	}
	wantResources := map[string]bool{"workflow://docs/overview": true, "workflow://docs/yaml-syntax": true, "workflow://docs/module-reference": true, "workflow://docs/full-reference": true, "workflow://docs/setup-guide": true}
	for _, resource := range resources.Resources {
		if !wantResources[resource.URI] {
			t.Fatal("unknown or duplicate resource URI")
		}
		delete(wantResources, resource.URI)
	}
}

func (c *stdioContractClient) finish(t *testing.T, terminate bool) {
	t.Helper()
	if terminate {
		if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
	} else if err := c.stdin.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.done:
	case <-time.After(stdioDeadline):
		t.Fatal("MCP child did not exit within deadline")
	}
	select {
	case <-c.readersDone:
	case <-time.After(stdioDeadline):
		t.Fatal("reader join timed out")
	}
	if terminate {
		// v0.54.1 ServeStdio returns context.Canceled on SIGTERM; both
		// runMCP's caller and the real CLI currently report this as exit 1.
		var exitErr *exec.ExitError
		if !errors.As(c.waitErr, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("expected bounded SIGTERM context-cancel exit 1, got %v", c.waitErr)
		}
		c.stderrMu.Lock()
		cancelled := strings.Contains(c.stderr.String(), "WFCTL_MCP_TEST_CONTEXT_CANCELLED_EXIT") || strings.Contains(c.stderr.String(), context.Canceled.Error())
		c.stderrMu.Unlock()
		if !cancelled {
			t.Fatal("SIGTERM exit lacked context-cancellation evidence")
		}
	} else if c.waitErr != nil {
		t.Fatalf("EOF child exit: %v", c.waitErr)
	}
	for frame := range c.frames {
		if frame.err != nil {
			t.Error(frame.err)
		} else {
			t.Error("unsolicited/extra stdout response (including notification reply)")
		}
	}
}

func TestMCPStdioLegacy(t *testing.T) {
	for _, version := range []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			c := startStdioContract(t, false)
			c.initialize(t, version)
			c.inventory(t, false)
			var report struct {
				Valid bool `json:"valid"`
			}
			text := c.call(t, "validate_config", map[string]any{"yaml_content": stdioPublicYAML}, false)
			if json.Unmarshal([]byte(text), &report) != nil || !report.Valid {
				t.Fatal("valid public config rejected")
			}
			c.call(t, "validate_config", map[string]any{}, true)
			c.call(t, "validate_config", map[string]any{"yaml_content": stdioCanary, "strict": "false"}, true)
			c.call(t, "get_config_skeleton", map[string]any{"module_types": []any{"http.server", 73}}, true)
			text = c.call(t, "get_config_skeleton", map[string]any{"module_types": []string{"http.server"}}, false)
			if !strings.Contains(text, "http.server") {
				t.Fatal("typed skeleton missing public type")
			}
			text = c.call(t, "validate_config", map[string]any{"yaml_content": "modules:\n  - name: absent\n    type: public.uninstalled\n"}, false)
			if json.Unmarshal([]byte(text), &report) != nil || report.Valid {
				t.Fatal("analytical negative must remain successful valid:false report")
			}
			c.sendRaw(t, "{")
			if reply := c.receive(t, "null"); reply.Error == nil || reply.Error.Code != -32700 {
				t.Fatal("malformed JSON must return parse error")
			}
			// This proves notification robustness only: production tools may finish
			// before cancellation arrives. In-flight proof uses the helper below.
			c.next++
			id := c.next
			c.send(t, map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": "list_module_types", "arguments": map[string]any{}}})
			c.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": id}})
			reply := c.receive(t, fmt.Sprint(id))
			if reply.Error != nil {
				if reply.Error.Code != -32800 || !strings.Contains(strings.ToLower(reply.Error.Message), "cancel") {
					t.Fatal("unexpected cancellation protocol error")
				}
			} else {
				var result struct {
					IsError bool `json:"isError"`
				}
				if json.Unmarshal(reply.Result, &result) != nil {
					t.Fatal("invalid cancellation result")
				}
				text := assertStdioTool(t, reply, result.IsError)
				if result.IsError && !strings.Contains(strings.ToLower(text), "cancel") {
					t.Fatal("unexpected cancellation tool error")
				}
			}
			c.call(t, "list_module_types", map[string]any{}, false)
			c.finish(t, false)
		})
	}
}

func TestMCPStdioLegacyCooperativeCancellation(t *testing.T) {
	c := startStdioContract(t, true)
	c.initialize(t, "2025-11-25")
	c.inventory(t, true)
	c.next++
	id := c.next
	c.send(t, map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": "run_workflow", "arguments": map[string]any{"workflow_type": "public", "action": "block_until_cancelled", "data": map[string]any{"synthetic": stdioCanary}}}})
	for _, barrier := range []string{handlerEntered, handlerCancelled} {
		select {
		case actual := <-c.barriers:
			if actual != barrier {
				t.Fatal("unexpected handler barrier")
			}
		case <-time.After(stdioDeadline):
			t.Fatalf("timed out waiting for %s", barrier)
		}
		if barrier == handlerEntered {
			c.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": id}})
		}
	}
	text := assertStdioTool(t, c.receive(t, fmt.Sprint(id)), true)
	var result struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if json.Unmarshal([]byte(text), &result) != nil || result.Success == nil || *result.Success || result.Error != context.Canceled.Error() {
		t.Fatal("cancelled handler must not report success")
	}
	text = c.call(t, "run_workflow", map[string]any{"workflow_type": "public", "action": "public_success"}, false)
	if json.Unmarshal([]byte(text), &result) != nil || result.Success == nil || !*result.Success {
		t.Fatal("subsequent engine request must succeed")
	}
	c.finish(t, false)
}

func TestMCPStdioLegacySIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Process.Signal(SIGTERM) is unsupported on Windows; framing and EOF are tested separately")
	}
	c := startStdioContract(t, false)
	c.initialize(t, "2025-11-25")
	c.call(t, "list_module_types", map[string]any{}, false)
	c.finish(t, true)
}
