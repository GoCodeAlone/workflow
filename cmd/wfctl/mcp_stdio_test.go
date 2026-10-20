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
	"slices"
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
const stdioPublicDocs = "Workflow MCP synthetic public documentation fixture\nmarker: PUBLIC_MCP_DOCS_20261019\n"
const stdioDocsMarker = "PUBLIC_MCP_DOCS_20261019"

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
	documentation := os.Getenv("WFCTL_MCP_TEST_DOCUMENTATION")
	switch mode {
	case "transport":
		err = workflowmcp.NewServer("plugins", workflowmcp.WithEngine(&stdioBlockingEngine{}), workflowmcp.WithDocumentationFile(documentation)).ServeStdio()
	case "production":
		err = runMCP([]string{"-plugin-dir", "plugins", "-documentation-file", documentation})
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
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

type stdioFrame struct {
	reply stdioReply
	err   error
}

type stdioContractClient struct {
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	frames        chan stdioFrame
	barriers      chan string
	done          chan struct{}
	readersDone   chan struct{}
	stopReaders   chan struct{}
	writers       sync.WaitGroup
	waitErr       error // Published by closing done.
	stderrMu      sync.Mutex
	stderr        strings.Builder
	stderrErr     bool
	next          int
	modern        bool
	serverVersion string
}

func startStdioContract(t *testing.T, transport bool) *stdioContractClient {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"plugins", "tmp", "fixtures"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	documentation := filepath.Join(root, "fixtures", "DOCUMENTATION.md")
	if err := os.WriteFile(documentation, []byte(stdioPublicDocs), 0o600); err != nil {
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
		args = []string{"mcp", "-plugin-dir", "plugins", "-documentation-file", documentation}
	}
	c := &stdioContractClient{frames: make(chan stdioFrame, 32), barriers: make(chan string, 8), done: make(chan struct{}), readersDone: make(chan struct{}), stopReaders: make(chan struct{})}
	c.cmd = exec.Command(bin, args...) //nolint:gosec // Explicit test-owned executable, never user tool input.
	c.cmd.Dir = root
	// No ambient HOME, CODEX_HOME, plugin locations, credentials or app config.
	c.cmd.Env = []string{"TMPDIR=" + filepath.Join(root, "tmp"), "WFCTL_NO_UPDATE_CHECK=1", "WFCTL_DIFFCACHE=disabled", "WFCTL_MCP_TEST_HELPER=" + mode, "WFCTL_MCP_TEST_DOCUMENTATION=" + documentation}
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
		if strings.Contains(c.stderr.String(), stdioCanary) || strings.Contains(c.stderr.String(), stdioDocsMarker) {
			t.Error("raw synthetic canary/documentation marker leaked to stderr")
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
	if c.modern {
		copy := map[string]any{"_meta": stdioJulyMeta()}
		if params != nil {
			for key, value := range params.(map[string]any) {
				copy[key] = value
			}
		}
		params = copy
	}
	c.next++
	c.send(t, map[string]any{"jsonrpc": "2.0", "id": c.next, "method": method, "params": params})
	reply := c.receive(t, fmt.Sprint(c.next))
	if c.modern && reply.Error == nil {
		c.assertJulyMetadata(t, reply, method)
	}
	return reply
}

func stdioJulyMeta() map[string]any {
	return map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
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
	if c.serverVersion != "" && c.serverVersion != result.ServerInfo.Version {
		t.Fatal("server identity changed between modern and legacy requests")
	}
	c.serverVersion = result.ServerInfo.Version
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
			c.cancellationRobustness(t)
			c.finish(t, false)
		})
	}
}

func (c *stdioContractClient) cancellationRobustness(t *testing.T) {
	t.Helper()
	// Production tools may finish before cancellation arrives. This is only
	// notification robustness; the entered-handler helper proves cancellation.
	c.next++
	id := c.next
	params := map[string]any{"name": "list_module_types", "arguments": map[string]any{}}
	if c.modern {
		params["_meta"] = stdioJulyMeta()
	}
	c.send(t, map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": params})
	c.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": id}})
	reply := c.receive(t, fmt.Sprint(id))
	if reply.Error != nil {
		if reply.Error.Code != -32800 || !strings.Contains(strings.ToLower(reply.Error.Message), "cancel") {
			t.Fatal("unexpected cancellation protocol error")
		}
	} else {
		if c.modern {
			c.assertJulyMetadata(t, reply, "tools/call")
		}
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
}

func TestMCPStdioLegacyCooperativeCancellation(t *testing.T) {
	stdioCooperativeCancellation(t, false)
}

func TestMCPStdioJulyCooperativeCancellation(t *testing.T) {
	stdioCooperativeCancellation(t, true)
}

func stdioCooperativeCancellation(t *testing.T, modern bool) {
	t.Helper()
	c := startStdioContract(t, true)
	c.modern = modern
	if modern {
		c.julyDiscovery(t)
		c.julyDocumentation(t)
	} else {
		c.initialize(t, "2025-11-25")
	}
	c.inventory(t, true)
	c.next++
	id := c.next
	params := map[string]any{"name": "run_workflow", "arguments": map[string]any{"workflow_type": "public", "action": "block_until_cancelled", "data": map[string]any{"synthetic": stdioCanary}}}
	if modern {
		params["_meta"] = stdioJulyMeta()
	}
	c.send(t, map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": params})
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
	reply := c.receive(t, fmt.Sprint(id))
	if modern {
		c.assertJulyMetadata(t, reply, "tools/call")
	}
	text := assertStdioTool(t, reply, true)
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

func (c *stdioContractClient) assertJulyMetadata(t *testing.T, reply stdioReply, method string) {
	t.Helper()
	var result struct {
		ResultType string                     `json:"resultType"`
		TTLMs      *int64                     `json:"ttlMs"`
		CacheScope string                     `json:"cacheScope"`
		Meta       map[string]json.RawMessage `json:"_meta"`
	}
	if json.Unmarshal(reply.Result, &result) != nil || result.ResultType != "complete" {
		t.Fatalf("%s missing complete resultType: %s", method, reply.Result)
	}
	var identity struct{ Name, Version string }
	if json.Unmarshal(result.Meta["io.modelcontextprotocol/serverInfo"], &identity) != nil || identity.Name != "wfctl-mcp" || identity.Version == "" {
		t.Fatal("July result lacks Workflow server identity metadata")
	}
	if c.serverVersion != "" && c.serverVersion != identity.Version {
		t.Fatal("server identity changed between requests")
	}
	c.serverVersion = identity.Version
	switch method {
	case "server/discover", "tools/list", "resources/list", "resources/templates/list", "resources/read":
		if result.TTLMs == nil || *result.TTLMs != 0 || result.CacheScope != "private" {
			t.Fatalf("%s lacks explicit ttlMs:0/cacheScope:private: %s", method, reply.Result)
		}
	}
	if method != "resources/read" && strings.Contains(string(reply.Result), stdioDocsMarker) {
		t.Fatal("documentation marker escaped the explicitly requested resource result")
	}
}

func assertStdioVersions(t *testing.T, versions []string) {
	t.Helper()
	want := []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25", "2026-07-28"}
	slices.Sort(versions)
	if !slices.Equal(versions, want) {
		t.Fatalf("unexpected discovered protocol versions: %v", versions)
	}
}

func (c *stdioContractClient) julyDiscovery(t *testing.T) {
	t.Helper()
	reply := c.request(t, "server/discover", nil)
	var result struct {
		SupportedVersions []string                   `json:"supportedVersions"`
		Capabilities      map[string]json.RawMessage `json:"capabilities"`
	}
	if reply.Error != nil || json.Unmarshal(reply.Result, &result) != nil {
		t.Fatal("stateless discovery failed")
	}
	assertStdioVersions(t, result.SupportedVersions)
	if len(result.Capabilities) != 2 {
		t.Fatal("unexpected optional capabilities/extensions advertised")
	}
	for _, name := range []string{"tools", "resources"} {
		var capability map[string]bool
		if json.Unmarshal(result.Capabilities[name], &capability) != nil || len(capability) != 1 || !capability["listChanged"] {
			t.Fatalf("unexpected %s capability", name)
		}
	}
}

func (c *stdioContractClient) julyDocumentation(t *testing.T) {
	t.Helper()
	reply := c.request(t, "resources/read", map[string]any{"uri": "workflow://docs/full-reference"})
	var result struct {
		Contents []struct{ URI, MimeType, Text string } `json:"contents"`
	}
	if reply.Error != nil || json.Unmarshal(reply.Result, &result) != nil || len(result.Contents) != 1 {
		t.Fatal("full-reference resource failed")
	}
	content := result.Contents[0]
	if content.URI != "workflow://docs/full-reference" || content.MimeType != "text/markdown" || content.Text != stdioPublicDocs || !strings.Contains(content.Text, stdioDocsMarker) {
		t.Fatal("full-reference must serve the exact explicitly supplied synthetic DOCUMENTATION.md bytes")
	}
}

func TestMCPStdioJuly(t *testing.T) {
	for _, transport := range []bool{false, true} {
		name := "production"
		if transport {
			name = "controlled-engine-transport"
		}
		t.Run(name, func(t *testing.T) {
			c := startStdioContract(t, transport)
			c.modern = true // No initialize or initialized notification in this path.
			c.julyDiscovery(t)
			t.Logf("observed wfctl-mcp version %s", c.serverVersion)
			c.inventory(t, transport)
			var previous []string
			for range 3 {
				reply := c.request(t, "tools/list", nil)
				var result struct {
					Tools []struct{ Name string } `json:"tools"`
				}
				if reply.Error != nil || json.Unmarshal(reply.Result, &result) != nil {
					t.Fatal("tools/list failed")
				}
				var names []string
				for _, tool := range result.Tools {
					names = append(names, tool.Name)
				}
				if !slices.IsSorted(names) || (previous != nil && !slices.Equal(previous, names)) {
					t.Fatal("wire tool order must be sorted and repeatable")
				}
				previous = names
			}
			reply := c.request(t, "resources/templates/list", nil)
			var templates struct {
				Templates []json.RawMessage `json:"resourceTemplates"`
			}
			if reply.Error != nil || json.Unmarshal(reply.Result, &templates) != nil || len(templates.Templates) != 0 {
				t.Fatal("unexpected resource templates")
			}
			c.julyDocumentation(t)
			var report struct {
				Valid *bool `json:"valid"`
			}
			text := c.call(t, "validate_config", map[string]any{"yaml_content": stdioPublicYAML}, false)
			if json.Unmarshal([]byte(text), &report) != nil || report.Valid == nil || !*report.Valid {
				t.Fatal("valid July config rejected")
			}
			c.call(t, "validate_config", map[string]any{"yaml_content": stdioCanary, "strict": "false"}, true)
			c.call(t, "get_config_skeleton", map[string]any{"module_types": []any{"http.server", 73}}, true)
			text = c.call(t, "validate_config", map[string]any{"yaml_content": "modules:\n  - name: absent\n    type: public.uninstalled\n"}, false)
			if json.Unmarshal([]byte(text), &report) != nil || report.Valid == nil || *report.Valid {
				t.Fatal("July analytical negative must remain successful valid:false")
			}
			c.sendRaw(t, "{")
			if reply := c.receive(t, "null"); reply.Error == nil || reply.Error.Code != -32700 {
				t.Fatal("malformed JSON must return parse error")
			}
			c.cancellationRobustness(t)
			c.finish(t, false) // Drains all frames: unsolicited logging/notification replies fail.
		})
	}
}

func TestMCPStdioJulySIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Process.Signal(SIGTERM) is unsupported on Windows; framing and EOF are tested separately")
	}
	c := startStdioContract(t, false)
	c.modern = true
	c.julyDiscovery(t)
	c.call(t, "list_module_types", nil, false)
	c.finish(t, true)
}

func TestMCPStdioJulyProtocolErrorsAndIsolation(t *testing.T) {
	c := startStdioContract(t, false)
	c.modern = true
	c.julyDiscovery(t)
	for _, caps := range []any{nil, "wrong-shaped", []any{}, false} {
		meta := map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28"}
		if caps != nil {
			meta["io.modelcontextprotocol/clientCapabilities"] = caps
		}
		reply := c.request(t, "tools/list", map[string]any{"_meta": meta})
		if reply.Error == nil || reply.Error.Code != -32602 {
			t.Fatalf("missing/wrong-shaped capabilities must return -32602: %+v", reply.Error)
		}
	}
	reply := c.request(t, "tools/list", map[string]any{"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientCapabilities": nil}})
	if reply.Error == nil || reply.Error.Code != -32602 {
		t.Fatal("explicit null capabilities must return -32602")
	}
	meta := stdioJulyMeta()
	meta["io.modelcontextprotocol/protocolVersion"] = "2099-01-01"
	reply = c.request(t, "server/discover", map[string]any{"_meta": meta})
	var unsupported struct {
		Supported []string `json:"supported"`
		Requested string   `json:"requested"`
	}
	if reply.Error == nil || reply.Error.Code != -32022 || json.Unmarshal(reply.Error.Data, &unsupported) != nil || unsupported.Requested != "2099-01-01" {
		t.Fatal("unsupported modern version must return -32022 with supported/requested data")
	}
	assertStdioVersions(t, unsupported.Supported)
	for _, method := range []string{"initialize", "ping"} {
		if reply := c.request(t, method, nil); reply.Error == nil || reply.Error.Code != -32601 {
			t.Fatalf("modern %s must return -32601", method)
		}
	}
	// Real persistent transport: modern -> legacy handshake -> modern -> legacy.
	// A fresh empty capabilities object on every modern call is mandatory;
	// requests missing it above must fail even after a successful discovery.
	c.modern = false
	c.initialize(t, "2025-11-25")
	for range 2 {
		reply := c.request(t, "tools/list", nil)
		var legacy map[string]json.RawMessage
		if reply.Error != nil || json.Unmarshal(reply.Result, &legacy) != nil {
			t.Fatal("legacy request after July failed")
		}
		for _, field := range []string{"resultType", "ttlMs", "cacheScope", "_meta"} {
			if _, present := legacy[field]; present {
				t.Fatalf("July %s leaked into legacy result", field)
			}
		}
		c.modern = true
		c.julyDiscovery(t)
		c.modern = false
	}
	c.call(t, "list_module_types", nil, false)
	c.finish(t, false)
}
