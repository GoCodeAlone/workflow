package module_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GoCodeAlone/modular"
	workflow "github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	httpplugin "github.com/GoCodeAlone/workflow/plugins/http"
	"github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
)

type webhookHostCounter struct {
	name  string
	calls int
	raw   []byte
	body  map[string]any
}

func (s *webhookHostCounter) Name() string { return s.name }

func (s *webhookHostCounter) Execute(_ context.Context, pc *module.PipelineContext) (*module.StepResult, error) {
	s.calls++
	s.raw, _ = pc.Metadata["_raw_body"].([]byte)
	if parsed := pc.StepOutputs["parse"]; parsed != nil {
		s.body, _ = parsed["body"].(map[string]any)
	}
	return &module.StepResult{Output: map[string]any{"invoked": true}}, nil
}

func TestHTTPTriggerBodyLimitEngineHost(t *testing.T) {
	const capBytes = 1 << 20
	validBody := " {\n \"action\": \"opened\"\n}\n"
	exactBody := validBody + strings.Repeat(" ", capBytes-len(validBody))
	for _, tc := range []struct {
		name         string
		routeLimit   int
		body         string
		badSignature bool
		wantStatus   int
		wantStarted  int
		wantInvoked  int
		inline       bool
	}{
		{"exact GitHub cap", capBytes, exactBody, false, http.StatusAccepted, 1, 1, false},
		{"route rejects before pipeline", capBytes, exactBody + " ", false, http.StatusRequestEntityTooLarge, 0, 0, false},
		{"verifier defends legacy uncapped route", 0, exactBody + " ", false, http.StatusRequestEntityTooLarge, 1, 0, false},
		{"invalid signature stops execution", capBytes, validBody, true, http.StatusUnauthorized, 1, 0, false},
		{"flat route rejects before pipeline", capBytes, exactBody + " ", false, http.StatusRequestEntityTooLarge, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
			engine := workflow.NewStdEngine(app, logger)
			if err := engine.LoadPlugin(httpplugin.New()); err != nil {
				t.Fatal(err)
			}
			if err := engine.LoadPlugin(pipelinesteps.New()); err != nil {
				t.Fatal(err)
			}
			started := &webhookHostCounter{name: "started"}
			invoked := &webhookHostCounter{name: "invoked"}
			engine.GetStepRegistry().(*module.StepRegistry).Register("step.test_webhook_counter", func(name string, _ map[string]any, _ modular.Application) (module.PipelineStep, error) {
				if name == started.name {
					return started, nil
				}
				return invoked, nil
			})
			route := map[string]any{"path": "/github/webhook", "method": "POST", "workflow": "pipeline:webhook", "action": "execute"}
			if tc.routeLimit > 0 {
				route["max_body_bytes"] = tc.routeLimit
			}
			cfg := &config.WorkflowConfig{
				Modules:  []config.ModuleConfig{{Name: "router", Type: "http.router"}},
				Triggers: map[string]any{"http": map[string]any{"routes": []any{route}}},
				Pipelines: map[string]any{"webhook": map[string]any{"steps": []any{
					map[string]any{"name": "started", "type": "step.test_webhook_counter"},
					map[string]any{"name": "verify", "type": "step.webhook_verify", "config": map[string]any{
						"provider": "github", "secret": "host-test-secret", "max_body_bytes": capBytes,
					}},
					map[string]any{"name": "parse", "type": "step.request_parse", "config": map[string]any{"format": "json"}},
					map[string]any{"name": "invoked", "type": "step.test_webhook_counter"},
				}}},
			}
			if tc.inline {
				cfg.Triggers = nil
				cfg.Pipelines["webhook"].(map[string]any)["trigger"] = map[string]any{
					"type": "http", "config": map[string]any{"path": "/github/webhook", "method": "POST", "max_body_bytes": tc.routeLimit},
				}
			}
			if err := engine.BuildFromConfig(cfg); err != nil {
				t.Fatal(err)
			}
			if err := engine.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := engine.Stop(context.Background()); err != nil {
					t.Error(err)
				}
			})
			var router *module.StandardHTTPRouter
			if err := app.GetService("router", &router); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/github/webhook", strings.NewReader(tc.body))
			req.ContentLength = -1
			req.Header.Set("Content-Type", "application/json")
			mac := hmac.New(sha256.New, []byte("host-test-secret"))
			_, _ = mac.Write([]byte(tc.body))
			sig := hex.EncodeToString(mac.Sum(nil))
			if tc.badSignature {
				sig = strings.Repeat("0", 64)
			}
			req.Header.Set("X-Hub-Signature-256", "sha256="+sig)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.wantStatus || started.calls != tc.wantStarted || invoked.calls != tc.wantInvoked {
				t.Fatalf("status=%d pipeline starts=%d invocations=%d; want %d/%d/%d; response=%s", w.Code, started.calls, invoked.calls, tc.wantStatus, tc.wantStarted, tc.wantInvoked, w.Body.String())
			}
			if tc.wantInvoked > 0 && (!bytes.Equal(invoked.raw, []byte(tc.body)) || invoked.body["action"] != "opened") {
				t.Fatal("verified exact raw bytes or prior request_parse output changed")
			}
		})
	}
}
