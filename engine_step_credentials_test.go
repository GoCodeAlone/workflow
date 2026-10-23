package workflow_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	workflow "github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
	pluginpkg "github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/plugin/external"
	"github.com/GoCodeAlone/workflow/plugins/configprovider"
	"github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
	"google.golang.org/grpc/connectivity"
)

const (
	scopedFixtureName = "step-credentials-plugin"
	scopedForeignName = "step-credentials-unrelated"
	scopedCaptureName = "workflow-plugin-product-capture"
	scopedCaptureSHA  = "fbce8a8169aa740002e165fa3263e7322155ffa3d2da11e6bd44a507ddace775"
	scopedKey         = "product_capture.compute_token"
	scopedProductURL  = "https://example.invalid/products/credential-proof"
)

type scopedObservation struct {
	PID             int  `json:"pid"`
	CarrierPresence bool `json:"carrier_presence"`
}

type scopedChildOutput struct {
	URL             string `json:"url"`
	PID             int    `json:"pid"`
	CarrierPresence bool   `json:"carrier_presence"`
	Error           string `json:"error"`
	Continued       bool   `json:"continued"`
}

type scopedCallOutput struct {
	Result struct {
		URL    string            `json:"url"`
		Result scopedChildOutput `json:"result"`
	} `json:"result"`
}

type scopedURLPayload struct {
	URL string `json:"url"`
}

// Only the Compute HTTP dependency is substituted; both SDK/host sides are real.
type scopedTaskPayload struct {
	Workload struct {
		Kind     string `json:"kind"`
		Provider struct {
			Operation string           `json:"operation"`
			Input     scopedURLPayload `json:"input"`
		} `json:"provider"`
	} `json:"workload"`
}

type scopedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *scopedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *scopedLog) snapshot() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func scopedNoPrivateValue(t *testing.T, values []string, surface string, data []byte) {
	t.Helper()
	for _, value := range values {
		if bytes.Contains(data, []byte(value)) {
			t.Fatalf("private canary appeared in %s (value withheld)", surface)
		}
	}
}

func scopedDecode(t *testing.T, data any, dst any) {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal("could not encode proof output")
	}
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatal("could not decode proof output")
	}
}

type scopedEndpoint struct {
	label  string
	server *httptest.Server
	mu     sync.Mutex
	count  int
	valid  int
}

func newScopedEndpoint(t *testing.T, label, token string, canaries []string, released bool) *scopedEndpoint {
	t.Helper()
	e := &scopedEndpoint{label: label}
	e.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := r.Body.Close(); err != nil {
				t.Error("loopback request body Close failed")
			}
		}()
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		path := "/submit"
		if released {
			path = "/v1/tasks"
		}
		valid := err == nil && r.Method == http.MethodPost && r.URL.RequestURI() == path &&
			"http://"+r.Host+path == e.server.URL+path && r.Header.Get("Authorization") == "Bearer "+token
		for _, canary := range canaries {
			valid = valid && !bytes.Contains(body, []byte(canary))
		}
		if released {
			var task scopedTaskPayload
			valid = valid && json.Unmarshal(body, &task) == nil && task.Workload.Kind == "provider" &&
				task.Workload.Provider.Operation == "capture_product" && task.Workload.Provider.Input.URL == scopedProductURL
		} else {
			var payload scopedURLPayload
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.DisallowUnknownFields()
			valid = valid && decoder.Decode(&payload) == nil && payload.URL == scopedProductURL && decoder.Decode(&struct{}{}) == io.EOF
		}
		e.mu.Lock()
		e.count++
		if valid {
			e.valid++
		}
		e.mu.Unlock()
		if !valid {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if released {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(func() {
		e.server.CloseClientConnections()
		e.server.Close()
		conn, err := net.DialTimeout("tcp", e.server.Listener.Addr().String(), 100*time.Millisecond)
		if err == nil {
			if err := conn.Close(); err != nil {
				t.Error("listener probe connection Close failed")
			}
			t.Error("owned loopback listener survived Close")
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		t.Logf("loopback app=%s final POST count=%d exact_endpoint_header_payload=%d listener_gone=%t", e.label, e.count, e.valid, err != nil)
	})
	return e
}

func (e *scopedEndpoint) assertCounts(t *testing.T, want int) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.count != want || e.valid != want {
		t.Fatalf("loopback app=%s POST count=%d exact_endpoint_header_payload=%d want=%d (headers/body withheld)", e.label, e.count, e.valid, want)
	}
	t.Logf("loopback app=%s POST count=%d exact_endpoint_header_payload=%d", e.label, e.count, e.valid)
}

func scopedHash(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error("proof binary Close failed")
		}
	}()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func buildScopedFixture(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), scopedFixtureName)
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	args := []string{"build"}
	race := false
	switch runtime.GOOS {
	case "linux":
		race = slices.Contains([]string{"amd64", "arm64", "loong64", "ppc64le", "riscv64", "s390x"}, runtime.GOARCH)
	case "darwin":
		race = runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
	case "freebsd", "netbsd", "windows":
		race = runtime.GOARCH == "amd64"
	}
	if race {
		args = append(args, "-race")
	}
	args = append(args, "-o", binary, "./plugin/external/testdata/step-credentials-plugin")
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build native SDK fixture: %v\n%s", err, output)
	}
	t.Logf("native fixture build race=%t sha256=%s", race, scopedHash(t, binary))
	return binary
}

func scopedPluginRoot(t *testing.T, binary, name string, manifest []byte) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The manager discovers <root>/<name>/<name>. Keep that alias on Windows
	// as well as the normal .exe file, without changing manager launch policy.
	dest := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		if err := os.Link(binary, dest+".exe"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(binary, dest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func scopedFixtureRoot(t *testing.T, binary, name string) string {
	t.Helper()
	types := []string{"step.scoped_submit", "step.scoped_observe"}
	if name == scopedForeignName {
		types = []string{"step.scoped_foreign"}
	}
	manifest, err := json.Marshal(pluginpkg.PluginManifest{
		Name: name, Version: "0.1.0", Author: "workflow-tests", Description: "Native scoped step credential fixture", StepTypes: types,
	})
	if err != nil {
		t.Fatal(err)
	}
	root := scopedPluginRoot(t, binary, name, manifest)
	if name == scopedFixtureName {
		dir := filepath.Join(root, scopedForeignName)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(binary, filepath.Join(dir, scopedForeignName)); err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS == "windows" {
			if err := os.Link(binary, filepath.Join(dir, scopedForeignName+".exe")); err != nil {
				t.Fatal(err)
			}
		}
		foreignManifest, err := json.Marshal(pluginpkg.PluginManifest{Name: scopedForeignName, Version: "0.1.0", Author: "workflow-tests", Description: "Unrelated native target", StepTypes: []string{"step.scoped_foreign"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "plugin.json"), foreignManifest, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

type scopedHost struct {
	engine     *workflow.StdEngine
	manager    *external.ExternalPluginManager
	adapter    *external.ExternalPluginAdapter
	logs       *scopedLog
	canaries   []string
	pid        int
	foreign    *external.ExternalPluginAdapter
	foreignPID int
}

func newScopedHost(t *testing.T, root, name string, canaries []string) *scopedHost {
	t.Helper()
	h := &scopedHost{logs: &scopedLog{}, canaries: canaries}
	logger := slog.New(slog.NewTextHandler(h.logs, nil))
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	h.engine = workflow.NewStdEngine(app, logger)
	h.manager = external.NewExternalPluginManager(root, log.New(h.logs, "", 0))
	h.manager.SetProcessOutput(h.logs, h.logs)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.engine.Stop(ctx); err != nil {
			scopedNoPrivateValue(t, canaries, "Stop error", []byte(err.Error()))
			t.Error("engine Stop failed")
		}
		h.manager.Shutdown()
		managerEmpty := len(h.manager.LoadedPlugins()) == 0
		if !managerEmpty {
			t.Error("manager retained an owned process")
		}
		ipcClosed := h.adapter == nil || h.adapter.Conn().GetState() == connectivity.Shutdown
		if !ipcClosed {
			t.Error("owned plugin IPC connection survived Shutdown")
		}
		if h.foreign != nil && h.foreign.Conn().GetState() != connectivity.Shutdown {
			ipcClosed = false
			t.Error("unrelated owned plugin IPC survived Shutdown")
		}
		for _, pid := range []int{h.pid, h.foreignPID} {
			if pid <= 0 {
				continue
			}
			for scopedPIDAlive(t, pid) && ctx.Err() == nil {
				time.Sleep(10 * time.Millisecond)
			}
			reaped := !scopedPIDAlive(t, pid)
			if !reaped {
				t.Error("owned plugin PID survived manager Shutdown/reap")
			}
			t.Logf("owned pid=%d reaped=%t ipc_closed=%t manager_empty=%t", pid, reaped, ipcClosed, managerEmpty)
		}
		transcript := h.logs.snapshot()
		scopedNoPrivateValue(t, canaries, "host/plugin logs", []byte(transcript))
		for _, signature := range []string{"panic:", "runtime error:", "no such host", "module not found", "import error", "version mismatch", "incompatible api version", "schema drift", "missing column", "constraint violation", "permission denied", "address already in use", "goroutine ", "data race"} {
			if strings.Contains(strings.ToLower(transcript), signature) {
				t.Errorf("host/plugin failure signature=%q (transcript withheld)", signature)
			}
		}
		t.Log("host/plugin failure-signature audit complete; raw transcript withheld")
	})
	names, err := h.manager.DiscoverPlugins()
	wantNames := []string{name}
	if name == scopedFixtureName {
		wantNames = append(wantNames, scopedForeignName)
	}
	if err != nil || !reflect.DeepEqual(names, wantNames) {
		t.Fatal("manager did not discover the exact owned plugin")
	}
	h.adapter, err = h.manager.LoadPlugin(name)
	if err != nil {
		t.Fatalf("manager LoadPlugin failed: %v", err)
	}
	if err := h.adapter.ContractRegistryError(); err != nil {
		t.Fatal("plugin contract discovery failed")
	}
	h.pid = scopedOwnedPID(t, filepath.Join(root, name, name))
	for _, p := range []pluginpkg.EnginePlugin{configprovider.New(), pipelinesteps.New(), h.adapter} {
		if err := h.engine.LoadPlugin(p); err != nil {
			t.Fatalf("engine LoadPlugin failed: %v", err)
		}
	}
	if name == scopedFixtureName {
		h.foreign, err = h.manager.LoadPlugin(scopedForeignName)
		if err != nil {
			t.Fatal("manager did not load the unrelated plugin")
		}
		h.foreignPID = scopedOwnedPID(t, filepath.Join(root, scopedForeignName, scopedForeignName))
		if err := h.engine.LoadPlugin(h.foreign); err != nil {
			t.Fatal("engine did not load the unrelated plugin")
		}
	}
	return h
}

// These read-only, bounded checks inspect only a PID/path owned by this test.
func scopedOwnedPID(t *testing.T, binary string) int {
	t.Helper()
	if runtime.GOOS == "windows" {
		// The native fixture reports its PID through a real SDK observation call.
		return 0
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,args=").Output()
	if err != nil {
		t.Fatal("read-only owned-process inspection failed")
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[1] == strconv.Itoa(os.Getpid()) && strings.Join(fields[2:], " ") == binary {
			pid, err := strconv.Atoi(fields[0])
			if err == nil && pid > 0 && pid != os.Getpid() {
				return pid
			}
		}
	}
	t.Fatal("could not identify the exact manager-owned native process")
	return 0
}

func scopedPIDAlive(t *testing.T, pid int) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if runtime.GOOS == "windows" {
		output, err := exec.CommandContext(ctx, "tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
		if err != nil {
			t.Error("read-only owned-PID check failed")
			return true
		}
		rows, _ := csv.NewReader(bytes.NewReader(output)).ReadAll()
		for _, row := range rows {
			if len(row) > 1 && row[1] == strconv.Itoa(pid) {
				return true
			}
		}
		return false
	}
	output, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "pid=").Output()
	if ctx.Err() != nil {
		t.Error("owned-PID check timed out")
		return true
	}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Error("read-only owned-PID check failed")
			return true
		}
	}
	return strings.TrimSpace(string(output)) != ""
}

func scopedYAML(name, stepType, token, endpoint, expression string, grant, native bool) string {
	grants := ""
	if grant {
		grants = fmt.Sprintf(`      step_credentials:
        - plugin: %s
          step_type: %s
          step_name: capture
          field: auth_token_ref
          ref: config:%s
          scope: application
`, name, stepType, scopedKey)
	}
	observers := ""
	after := ""
	if native {
		observers = `      - name: unrelated_before
        type: step.scoped_observe
        config: {}
`
		after = `      - name: unrelated_after
        type: step.scoped_observe
        config: {}
      - name: unrelated_plugin
        type: step.scoped_foreign
        config: {}
`
	}
	extraConfig := ""
	urlConfig := "          url: '{{ .url }}'\n"
	if !native {
		urlConfig = "          url_field: url\n"
		extraConfig = `          product_id: product-local
          org_id: org-local
          pool_id: pool-local
          policy_id: policy-local
          timeout_seconds: 5
          allowed_hosts: [example.invalid]
          provider_image_ref: "example.invalid/credential-proof@sha256:0000000000000000000000000000000000000000000000000000000000000000"
          request_timeout: 3s
          wait_timeout: 3s
`
	}
	return fmt.Sprintf(`modules:
  - name: settings
    type: config.provider
    config:
      schema:
        %s: {default: %q, sensitive: true}
        server_url: {default: %q}
      sources: [{type: defaults}]
%spipelines:
  parent:
    timeout: 10s
    steps:
%s      - name: response
        type: step.workflow_call
        config:
          workflow: response-child
          timeout: 5s
          input: {url: '{{ .url }}'}
%s  response-child:
    timeout: 5s
    steps:
      - name: capture_child
        type: step.workflow_call
        config:
          workflow: capture-child
          timeout: 5s
          input: {url: '{{ .url }}'}
  capture-child:
    timeout: 5s
    steps:
      - name: capture
        type: %s
        config:
          server_url: %q
          auth_token_ref: config:%s
%s%s      - name: continued
        type: step.set
        config: {values: {continued: true}}
`, scopedKey, token, endpoint, grants, observers, after, stepType, expression, scopedKey, urlConfig, extraConfig)
}

func (h *scopedHost) build(t *testing.T, yaml string) {
	t.Helper()
	cfg, err := config.LoadFromString(yaml)
	if err != nil {
		t.Fatal("config loader rejected proof YAML")
	}
	if err := h.engine.BuildFromConfig(cfg); err != nil {
		scopedNoPrivateValue(t, h.canaries, "Build error", []byte(err.Error()))
		t.Fatalf("BuildFromConfig: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := h.engine.Start(ctx); err != nil {
		t.Fatal("engine Start failed")
	}
}

func (h *scopedHost) execute(t *testing.T) (*interfaces.PipelineContext, scopedChildOutput) {
	t.Helper()
	input := map[string]any{"url": scopedProductURL}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	pc, err := h.engine.ExecutePipelineContext(ctx, "parent", input)
	if err != nil {
		scopedNoPrivateValue(t, h.canaries, "execution error", []byte(err.Error()))
		t.Fatalf("ExecutePipelineContext: %v", err)
	}
	b, err := json.Marshal(pc)
	if err != nil {
		t.Fatal("cannot inspect full pipeline context")
	}
	scopedNoPrivateValue(t, h.canaries, "Context/Metadata/StepOutputs/result", b)
	scopedNoPrivateValue(t, h.canaries, "host environment", []byte(strings.Join(os.Environ(), "\n")))
	if !reflect.DeepEqual(input, map[string]any{"url": scopedProductURL}) {
		t.Fatal("URL-only caller input was mutated")
	}
	var result scopedCallOutput
	scopedDecode(t, pc.StepOutputs["response"], &result)
	if result.Result.URL != scopedProductURL || result.Result.Result.URL != scopedProductURL {
		t.Fatal("nested children did not receive the original URL-only input")
	}
	return pc, result.Result.Result
}

func TestEngineScopedCredentialsNative(t *testing.T) {
	binary := buildScopedFixture(t)
	for _, syntax := range []struct{ name, expression string }{
		{"pure_expansion", `{{ config "server_url" }}`},
		{"composed_go", `{{ config "server_url" | default "" }}`},
		{"expression", `${ config("server_url") }`},
	} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse_%t", syntax.name, reverse), func(t *testing.T) {
				tokens := []string{"dummy-private-native-A-canary", "dummy-private-native-B-canary"}
				endpoints := []*scopedEndpoint{newScopedEndpoint(t, "A", tokens[0], tokens, false), newScopedEndpoint(t, "B", tokens[1], tokens, false)}
				hosts := []*scopedHost{
					newScopedHost(t, scopedFixtureRoot(t, binary, scopedFixtureName), scopedFixtureName, tokens),
					newScopedHost(t, scopedFixtureRoot(t, binary, scopedFixtureName), scopedFixtureName, tokens),
				}
				order := []int{0, 1}
				if reverse {
					slices.Reverse(order)
				}
				for _, i := range order {
					hosts[i].build(t, scopedYAML(scopedFixtureName, "step.scoped_submit", tokens[i], endpoints[i].server.URL, syntax.expression, true, true))
				}
				last := order[1]
				if got, ok := module.GetConfigRegistry().Get("server_url"); !ok || got != endpoints[last].server.URL {
					t.Fatal("second build did not replace the compatibility mirror")
				}
				var registries [2]*module.ConfigRegistry
				for i, h := range hosts {
					if err := h.engine.App().GetService("config.registry", &registries[i]); err != nil {
						t.Fatal("application lost its private registry")
					}
				}
				if registries[0] == registries[1] || registries[0] == module.GetConfigRegistry() || registries[1] == module.GetConfigRegistry() {
					t.Fatal("applications did not retain separate private registries")
				}
				counts := [2]int{}
				for _, i := range []int{order[0], order[1], order[0], order[1]} {
					pc, result := hosts[i].execute(t)
					var observer scopedObservation
					scopedDecode(t, pc.StepOutputs["unrelated_before"], &observer)
					if observer.CarrierPresence || observer.PID <= 0 || observer.PID == os.Getpid() {
						t.Fatal("unrelated native target got a carrier or did not run in a child process")
					}
					if hosts[i].pid == 0 {
						hosts[i].pid = observer.PID
					}
					if !result.CarrierPresence || result.PID != hosts[i].pid || observer.PID != result.PID || result.Error != "" || !result.Continued {
						t.Fatal("credentialed native step did not consume the execution-private carrier")
					}
					var after, foreign scopedObservation
					scopedDecode(t, pc.StepOutputs["unrelated_after"], &after)
					scopedDecode(t, pc.StepOutputs["unrelated_plugin"], &foreign)
					if hosts[i].foreignPID == 0 {
						hosts[i].foreignPID = foreign.PID
					}
					if after.CarrierPresence || after.PID != result.PID || foreign.CarrierPresence || foreign.PID <= 0 || foreign.PID != hosts[i].foreignPID || foreign.PID == result.PID || foreign.PID == os.Getpid() {
						t.Fatal("same-application unrelated target/plugin received a private carrier")
					}
					counts[i]++
					for j := range endpoints {
						endpoints[j].assertCounts(t, counts[j])
					}
				}
			})
		}
	}
}

func TestEngineScopedCredentialsReleasedCapture065(t *testing.T) {
	dir := os.Getenv("WORKFLOW_CAPTURE_V0165_PLUGIN_DIR")
	if dir == "" {
		t.Skip("optional released065 asset absent; set WORKFLOW_CAPTURE_V0165_PLUGIN_DIR for mandatory local acceptance")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Fatal("opted-in released065 proof requires the pinned Darwin/arm64 asset")
	}
	binary := filepath.Join(dir, scopedCaptureName)
	if got := scopedHash(t, binary); got != scopedCaptureSHA {
		t.Fatal("released065 binary SHA256 mismatch; refusing to spawn")
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk pluginpkg.PluginManifest
	if err := json.Unmarshal(manifest, &disk); err != nil || disk.Name != scopedCaptureName || disk.Version != "0.1.65" || !slices.Contains(disk.StepTypes, "step.product_capture") {
		t.Fatal("released065 disk manifest identity/version/step mismatch")
	}
	t.Logf("released065 verified before spawn sha256=%s manifest_version=%s", scopedCaptureSHA, disk.Version)
	for _, grant := range []bool{true, false} {
		t.Run(fmt.Sprintf("grant_%t", grant), func(t *testing.T) {
			token := "dummy-private-released065-canary"
			endpoint := newScopedEndpoint(t, "released065", token, []string{token}, true)
			h := newScopedHost(t, scopedPluginRoot(t, binary, scopedCaptureName, manifest), scopedCaptureName, []string{token})
			if h.adapter.Name() != scopedCaptureName || h.adapter.Version() != "0.1.65" || !slices.Contains(h.engine.RegisteredStepTypes(), "step.product_capture") {
				t.Fatal("released065 runtime identity/version/step mismatch")
			}
			h.build(t, scopedYAML(scopedCaptureName, "step.product_capture", token, endpoint.server.URL, `{{ config "server_url" }}`, grant, false))
			_, result := h.execute(t)
			wantError, wantRequests := `config ref "product_capture.compute_token" not resolved`, 0
			if grant {
				wantError, wantRequests = "POST /v1/tasks: got status 403", 1
			}
			if result.Error != wantError || result.Continued {
				t.Fatal("released065 did not return the exact expected Stop/error (output withheld)")
			}
			endpoint.assertCounts(t, wantRequests)
			t.Logf("released065 runtime_version=%s grant=%t status=%q Stop=true", h.adapter.Version(), grant, result.Error)
		})
	}
}
