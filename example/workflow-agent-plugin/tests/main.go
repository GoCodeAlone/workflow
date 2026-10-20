// Maintainer smoke consumer for the package's actual wfctl stdio entry.
// Public fixtures only; no raw subprocess output, app start or host activation.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

type failure string

func (e failure) Error() string { return string(e) }

type options struct{ root, executable, expected string }

var reasons = map[string]bool{
	"missing_executable": true, "version_unknown": true, "version_mismatch": true, "wrong_server": true,
	"missing_tool": true, "schema_changed": true, "protocol_error": true, "tool_error": true, "timeout": true,
	"check_failed": true, "unexpected_stderr": true, "internal_error": true,
}
var requiredTools = []string{"list_module_types", "list_step_types", "list_trigger_types", "list_workflow_types", "get_module_schema", "get_config_skeleton", "validate_config", "inspect_config", "compat_check", "template_validate_config", "list_plugins"}

const deprecation = "[DEPRECATED] requires.plugins[].version and requires.plugins[].source in app.yaml are deprecated. Run 'wfctl migrate plugins' to migrate to wfctl.yaml + .wfctl-lock.yaml."
const sonicFallback = "WARNING: sonic/ast only supports (go1.17~1.26 and amd64 CPU) or (go1.20~1.26 and arm64 CPU), but your environment is not suitable and will fallback to encoding/json"
const maxStderrBytes = 1024 * 1024

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var releaseVersion = regexp.MustCompile("^v[0-9]+[.][0-9]+[.][0-9]+$")

func checkVersion(info map[string]any, expected string) error {
	if info["name"] != "wfctl-mcp" {
		return failure("wrong_server")
	}
	version, ok := info["version"].(string)
	if !ok || !releaseVersion.MatchString(version) {
		return failure("version_unknown")
	}
	if version != expected {
		return failure("version_mismatch")
	}
	return nil
}
func checkArguments(tools map[string]map[string]any, name string, args map[string]any) error {
	tool, ok := tools[name]
	if !ok {
		return failure("missing_tool")
	}
	schema, ok := tool["inputSchema"].(map[string]any)
	if !ok {
		return failure("schema_changed")
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return failure("schema_changed")
	}
	if raw, exists := schema["required"]; exists {
		required, ok := raw.([]any)
		if !ok {
			return failure("schema_changed")
		}
		for _, entry := range required {
			key, ok := entry.(string)
			if !ok {
				return failure("schema_changed")
			}
			if _, ok = args[key]; !ok {
				return failure("schema_changed")
			}
		}
	}
	for key := range args {
		if _, ok := properties[key]; !ok {
			return failure("schema_changed")
		}
	}
	return nil
}
func decodeJSON(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, failure("protocol_error")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, failure("protocol_error")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, failure("protocol_error")
	}
	return value, nil
}
func object(value any) map[string]any { result, _ := value.(map[string]any); return result }
func array(value any) []any           { result, _ := value.([]any); return result }
func responseResult(value any, requestID int) (map[string]any, error) {
	message, ok := value.(map[string]any)
	if !ok || message["jsonrpc"] != "2.0" {
		return nil, failure("protocol_error")
	}
	id, ok := message["id"].(json.Number)
	if !ok {
		return nil, failure("protocol_error")
	}
	number, err := strconv.Atoi(string(id))
	if err != nil || number != requestID {
		return nil, failure("protocol_error")
	}
	if _, exists := message["error"]; exists {
		return nil, failure("protocol_error")
	}
	result, ok := message["result"].(map[string]any)
	if !ok {
		return nil, failure("protocol_error")
	}
	return result, nil
}
func textPayload(result map[string]any) (string, bool) {
	contents, ok := result["content"].([]any)
	if !ok || len(contents) != 1 {
		return "", false
	}
	item, ok := contents[0].(map[string]any)
	if !ok || item["type"] != "text" {
		return "", false
	}
	value, ok := item["text"].(string)
	return value, ok
}
func toolJSON(result map[string]any) (map[string]any, error) {
	if result["isError"] == true {
		return nil, failure("tool_error")
	}
	text, ok := textPayload(result)
	if !ok {
		return nil, failure("tool_error")
	}
	value, err := decodeJSON([]byte(text))
	if err != nil {
		return nil, failure("tool_error")
	}
	decoded, ok := value.(map[string]any)
	if !ok {
		return nil, failure("tool_error")
	}
	return decoded, nil
}

type client struct {
	cmd             *exec.Cmd
	stdin           io.WriteCloser
	stdout          io.ReadCloser
	stderr          *os.File
	messages        chan any
	stopping        chan struct{}
	readerDone      chan struct{}
	responseTimeout time.Duration
	requestID       int
	notifications   int
	tools           map[string]map[string]any
	closeOnce       sync.Once
	closedStderr    []byte
	closeErr        error
}

func startClient(ctx context.Context, binary string, args []string, cwd string, env []string, dataDir string) (*client, error) {
	stderr, err := os.CreateTemp(dataDir, "smoke-stderr-")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		stderr.Close()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		stderr.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, err
	}
	c := &client{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr, messages: make(chan any, 32), stopping: make(chan struct{}), readerDone: make(chan struct{}), responseTimeout: 10 * time.Second}
	go c.read()
	return c, nil
}
func newClient(ctx context.Context, binary, root, dataDir string) (*client, error) {
	data, err := os.ReadFile(filepath.Join(root, "mcp.json"))
	if err != nil {
		return nil, err
	}
	value, err := decodeJSON(data)
	if err != nil {
		return nil, err
	}
	settings := object(object(object(value)["mcpServers"])["workflow-config"])
	if settings["command"] != "wfctl" || settings["type"] != "stdio" {
		return nil, failure("schema_changed")
	}
	expand := strings.NewReplacer("${PLUGIN_ROOT}", root, "${PLUGIN_DATA}", dataDir)
	var args []string
	for _, arg := range array(settings["args"]) {
		text, ok := arg.(string)
		if !ok {
			return nil, failure("schema_changed")
		}
		args = append(args, expand.Replace(text))
	}
	cwd, ok := settings["cwd"].(string)
	if !ok {
		return nil, failure("schema_changed")
	}
	env := []string{"TMPDIR=" + dataDir}
	for key, value := range object(settings["env"]) {
		text, ok := value.(string)
		if !ok {
			return nil, failure("schema_changed")
		}
		env = append(env, key+"="+text)
	}
	return startClient(ctx, binary, args, expand.Replace(cwd), env, dataDir)
}
func (c *client) enqueue(value any) {
	select {
	case <-c.stopping:
	case c.messages <- value:
	}
}
func (c *client) read() {
	defer close(c.readerDone)
	defer c.enqueue(nil)
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	// Reject unterminated final messages as well as oversized or invalid JSON.
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF && len(data) > 0 {
			return 0, nil, failure("protocol_error")
		}
		return 0, nil, nil
	})
	for scanner.Scan() {
		select {
		case <-c.stopping:
			return
		default:
		}
		value, err := decodeJSON(scanner.Bytes())
		if err != nil {
			return
		}
		c.enqueue(value)
	}
}
func (c *client) send(message map[string]any) error {
	message["jsonrpc"] = "2.0"
	data, err := json.Marshal(message)
	if err != nil {
		return failure("protocol_error")
	}
	if _, err = c.stdin.Write(append(data, '\n')); err != nil {
		return failure("protocol_error")
	}
	return nil
}
func (c *client) request(method string, params map[string]any) (map[string]any, error) {
	// A single deadline spans every notification and the final response.
	timer := time.NewTimer(c.responseTimeout)
	defer timer.Stop()
	c.requestID++
	if err := c.send(map[string]any{"id": c.requestID, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for range 100 {
		select {
		case <-timer.C:
			return nil, failure("timeout")
		default:
		}
		select {
		case <-timer.C:
			return nil, failure("timeout")
		case message := <-c.messages:
			if obj := object(message); obj != nil {
				if _, method := obj["method"]; method {
					if _, id := obj["id"]; !id {
						c.notifications++
						continue
					}
				}
			}
			return responseResult(message, c.requestID)
		}
	}
	return nil, failure("protocol_error")
}
func (c *client) call(name string, args map[string]any) (map[string]any, error) {
	if err := checkArguments(c.tools, name, args); err != nil {
		return nil, err
	}
	return c.request("tools/call", map[string]any{"name": name, "arguments": args})
}
func (c *client) close() ([]byte, error) {
	c.closeOnce.Do(func() {
		close(c.stopping)
		_ = c.stdin.Close() // EOF first; broken stdin must not prevent reaping.
		waited := make(chan struct{})
		var waitErr error
		go func() { waitErr = c.cmd.Wait(); close(waited) }()
		wait := func() bool {
			timer := time.NewTimer(3 * time.Second)
			defer timer.Stop()
			select {
			case <-waited:
				return true
			case <-timer.C:
				return false
			}
		}
		if !wait() {
			c.closeErr = failure("check_failed")
			_ = c.cmd.Process.Signal(syscall.SIGTERM)
			if !wait() {
				_ = c.cmd.Process.Kill()
				if !wait() {
					c.closeErr = failure("timeout")
				}
			}
		}
		if c.closeErr == nil && waitErr != nil {
			c.closeErr = failure("check_failed")
		}
		_ = c.stdout.Close()
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-c.readerDone:
		case <-timer.C:
			c.closeErr = failure("timeout")
		}
		if _, err := c.stderr.Seek(0, io.SeekStart); err == nil {
			c.closedStderr, err = io.ReadAll(io.LimitReader(c.stderr, maxStderrBytes+1))
			if err != nil {
				c.closeErr = failure("internal_error")
			} else if len(c.closedStderr) > maxStderrBytes && c.closeErr == nil {
				c.closeErr = failure("unexpected_stderr")
			}
		} else {
			c.closeErr = failure("internal_error")
		}
		_ = c.stderr.Close()
		_ = os.Remove(c.stderr.Name())
	})
	return c.closedStderr, c.closeErr
}

// Retain the first failure and stop subsequent requests without exposing output.
type session struct {
	client *client
	err    error
}

func (s *session) require(ok bool) {
	if s.err == nil && !ok {
		s.err = failure("check_failed")
	}
}
func (s *session) request(name string, args map[string]any) map[string]any {
	if s.err != nil {
		return nil
	}
	value, err := s.client.request(name, args)
	s.err = err
	return value
}
func (s *session) call(name string, args map[string]any) map[string]any {
	if s.err != nil {
		return nil
	}
	value, err := s.client.call(name, args)
	s.err = err
	return value
}
func (s *session) json(name string, args map[string]any) map[string]any {
	result := s.call(name, args)
	if s.err != nil {
		return nil
	}
	value, err := toolJSON(result)
	s.err = err
	return value
}
func contains(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func runSmoke(ctx context.Context, opts options) (map[string]any, error) {
	binary, err := exec.LookPath(opts.executable)
	if err != nil {
		return nil, failure("missing_executable")
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return nil, err
	}
	fixtures := map[string][]byte{}
	paths, err := filepath.Glob(filepath.Join(opts.root, "fixtures", "*.yaml.txt"))
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		// JSON replaces invalid UTF-8. Reject it before transport so each hash
		// identifies the exact bytes represented by the configuration tested.
		if !utf8.Valid(data) {
			return nil, failure("check_failed")
		}
		fixtures[strings.TrimSuffix(filepath.Base(path), ".yaml.txt")] = data
	}
	dataDir, err := os.MkdirTemp("", "workflow-mcp-test-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dataDir)
	c, err := newClient(ctx, binary, opts.root, dataDir)
	if err != nil {
		return nil, err
	}
	defer c.close()
	s := session{client: c}
	initialized := s.request("initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "workflow-package-smoke", "version": "0.1.1"}})
	info := object(initialized["serverInfo"])
	if s.err == nil {
		s.err = checkVersion(info, opts.expected)
	}
	s.require(initialized["protocolVersion"] == "2024-11-05")
	if s.err == nil {
		s.err = c.send(map[string]any{"method": "notifications/initialized"})
	}
	discovery := s.request("tools/list", map[string]any{})
	s.require(discovery["nextCursor"] == nil || discovery["nextCursor"] == "")
	tools := array(discovery["tools"])
	c.tools = map[string]map[string]any{}
	for _, value := range tools {
		tool := object(value)
		name, ok := tool["name"].(string)
		s.require(ok)
		c.tools[name] = tool
	}
	s.require(len(tools) == len(c.tools))
	for _, name := range requiredTools {
		if _, ok := c.tools[name]; !ok && s.err == nil {
			s.err = failure("missing_tool")
		}
	}
	modules := array(s.json("list_module_types", map[string]any{})["module_types"])
	steps := array(s.json("list_step_types", map[string]any{})["step_types"])
	triggers := array(s.json("list_trigger_types", map[string]any{})["trigger_types"])
	workflows := array(s.json("list_workflow_types", map[string]any{})["workflow_types"])
	s.require(contains(modules, "http.server") && !contains(modules, "example.uninstalled-module"))
	for _, step := range steps {
		s.require(object(step)["type"] != "step.example_uninstalled")
	}
	s.require(!contains(triggers, "example-unknown-trigger") && !contains(workflows, "example-unknown-workflow"))
	schema := s.json("get_module_schema", map[string]any{"module_type": "http.server"})
	s.require(schema["type"] == "http.server")
	skeleton := s.call("get_config_skeleton", map[string]any{"module_types": []string{"http.server"}})
	skeletonText, skeletonOK := textPayload(skeleton)
	s.require(skeleton["isError"] != true && skeletonOK && strings.Contains(skeletonText, "http.server"))
	basic := func(name string) map[string]any {
		return s.json("validate_config", map[string]any{"yaml_content": string(fixtures[name]), "strict": true, "skip_unknown_types": false})
	}
	deep := func(name string) map[string]any {
		return s.json("template_validate_config", map[string]any{"yaml_content": string(fixtures[name]), "strict": true})
	}
	s.require(basic("minimal")["valid"] == true)
	s.require(deep("minimal")["valid"] == true)
	inspection := s.json("inspect_config", map[string]any{"yaml_content": string(fixtures["minimal"])})
	s.require(inspection["module_count"] == json.Number("1"))
	compatible := s.json("compat_check", map[string]any{"yaml_content": string(fixtures["minimal"])})
	s.require(compatible["compatible"] == true && compatible["engineVersion"] == opts.expected)
	s.require(s.call("validate_config", map[string]any{"yaml_content": string(fixtures["invalid"])})["isError"] == true)
	s.require(basic("missing-plugin")["valid"] == false)
	s.require(s.json("compat_check", map[string]any{"yaml_content": string(fixtures["missing-plugin"])})["compatible"] == false)
	s.require(s.json("list_plugins", map[string]any{})["count"] == json.Number("0"))
	s.require(basic("unknown-types")["valid"] == true)
	s.require(deep("unknown-types")["valid"] == false)
	s.require(basic("unresolved-import")["valid"] == true)
	s.require(basic("missing-prerequisite")["valid"] == true)
	s.require(s.json("compat_check", map[string]any{"yaml_content": string(fixtures["missing-prerequisite"])})["compatible"] == true)
	stderr, closeErr := c.close()
	if s.err != nil {
		return nil, s.err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	stderrStatus, err := classifyStderr(stderr)
	if err != nil {
		return nil, err
	}
	binaryHash, err := fileDigest(binary)
	if err != nil {
		return nil, err
	}
	inventory, err := json.Marshal(tools)
	if err != nil {
		return nil, err
	}
	fixtureHashes := map[string]string{}
	for name, data := range fixtures {
		fixtureHashes[name] = digest(data)
	}
	return map[string]any{
		"status": "passed", "scope": "bundled_public_fixtures_only", "captured_at": time.Now().UTC().Format(time.RFC3339Nano), "server": map[string]any{"name": info["name"], "version": info["version"]},
		"release_source": "https://github.com/GoCodeAlone/workflow/tree/" + opts.expected, "binary_sha256": binaryHash, "tool_count": len(tools), "tool_inventory_sha256": digest(inventory), "fixture_sha256": fixtureHashes,
		"checks": map[string]string{"basic_and_deep_minimal": "accepted", "inspection": "one_module", "current_registry_compatibility": "compatible", "malformed_yaml": "tool_error", "missing_module": "rejected", "default_plugin_inventory": "empty", "unknown_type_coverage": "basic_accepted_deep_rejected_inventory_absent", "unresolved_import": "basic_accepted_coverage_incomplete", "missing_declared_plugin": "basic_and_compat_accepted_prerequisite_unmet"},
		"stderr": stderrStatus, "fixture_source_commit": "not_recorded_by_smoke", "redacted": false,
		"runtime_acceptance": "not_checked", "desktop_activation": "not_checked", "plugin_binary_health": "not_checked", "original_input_equivalence": "not_checked",
	}, nil
}
func classifyStderr(stderr []byte) (string, error) {
	if len(stderr) == 0 {
		return "empty", nil
	}
	var hasDeprecation, hasSonic bool
	for _, line := range strings.Split(strings.TrimSuffix(string(stderr), "\n"), "\n") {
		switch line {
		case deprecation:
			hasDeprecation = true
		case sonicFallback:
			hasSonic = true
		default:
			return "", failure("unexpected_stderr")
		}
	}
	if hasSonic {
		if hasDeprecation {
			return "known_deprecation_and_sonic_fallback", nil
		}
		return "known_sonic_fallback_only", nil
	}
	return "known_deprecation_only", nil
}
func mainWith(ctx context.Context, args []string, out io.Writer, run func(context.Context, options) (map[string]any, error)) int {
	flags := flag.NewFlagSet("workflow-package-smoke", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	opts := options{}
	flags.StringVar(&opts.executable, "wfctl", "wfctl", "reviewed executable; never downloaded")
	flags.StringVar(&opts.expected, "expect-version", "v0.86.2", "required released server version")
	err := flags.Parse(args)
	if err == nil && len(flags.Args()) != 0 {
		err = failure("internal_error")
	}
	if err == nil {
		opts.root, err = filepath.Abs("..")
	}
	var report map[string]any
	if err == nil {
		report, err = run(ctx, opts)
	}
	status := 0
	if err != nil {
		reason := "internal_error"
		var category failure
		if errors.As(err, &category) && reasons[string(category)] {
			reason = string(category)
		}
		report = map[string]any{"status": "failed", "reason": reason, "runtime_acceptance": "not_checked", "desktop_activation": "not_checked"}
		status = 1
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if encoder.Encode(report) != nil {
		return 1
	}
	return status
}
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	os.Exit(mainWith(ctx, os.Args[1:], os.Stdout, runSmoke))
}
