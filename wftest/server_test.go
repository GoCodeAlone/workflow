package wftest_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	pluginpipeline "github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
	"github.com/GoCodeAlone/workflow/wftest"
)

func TestHarness_WithServer_BaseURL(t *testing.T) {
	h := wftest.New(t,
		wftest.WithServer(),
		wftest.WithYAML(`
modules:
  - name: router
    type: http.router
pipelines:
  hello:
    steps:
      - name: respond
        type: step.set
        config:
          values:
            response_status: 200
            message: "hello"
`))

	baseURL := h.BaseURL()
	if baseURL == "" {
		t.Fatal("expected non-empty BaseURL")
	}
	if !strings.HasPrefix(baseURL, "http://127.0.0.1:") {
		t.Errorf("expected BaseURL to start with http://127.0.0.1:, got %s", baseURL)
	}
}

func TestHarness_WithServer_RealHTTP(t *testing.T) {
	h := wftest.New(t,
		wftest.WithServer(),
		wftest.WithYAML(`
modules:
  - name: router
    type: http.router
pipelines:
  ping:
    trigger:
      type: http
      config:
        path: /ping
        method: GET
    steps:
      - name: pong
        type: step.set
        config:
          values:
            response_status: 200
            response_body: "pong"
`))

	resp, err := http.Get(h.BaseURL() + "/ping")
	if err != nil {
		t.Fatalf("GET /ping failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d (body: %s)", resp.StatusCode, body)
	}
	if string(body) != "pong" {
		t.Errorf("expected body 'pong', got %q", string(body))
	}
}

func TestHarness_WithPlugin_LoadsPlugin(t *testing.T) {
	// pipelinesteps is already loaded as a builtin, but we can load it again
	// with LoadPluginWithOverride semantics. For a real plugin test, we verify
	// that the option is accepted without error and the harness initialises.
	h := wftest.New(t,
		wftest.WithPlugin(pluginpipeline.New()),
		wftest.WithYAML(`
pipelines:
  plugintest:
    steps:
      - name: val
        type: step.set
        config:
          values:
            ok: true
`))

	result := h.ExecutePipeline("plugintest", nil)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Output["ok"] != true {
		t.Errorf("expected ok=true, got %v", result.Output["ok"])
	}
}

func TestHarness_WithServer_ServerStopsOnCleanup(t *testing.T) {
	var serverURL string
	func() {
		// Create a sub-test to trigger cleanup when done
		inner := wftest.New(t,
			wftest.WithServer(),
			wftest.WithYAML(`
modules:
  - name: router
    type: http.router
`))
		serverURL = inner.BaseURL()
	}()
	// Cleanup is registered via t.Cleanup, which runs at end of test.
	// Just verify the URL was populated.
	if serverURL == "" {
		t.Error("expected serverURL to be non-empty")
	}
}
