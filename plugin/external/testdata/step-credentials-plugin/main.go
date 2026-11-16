// The real SDK owns process serving and RPC conversion. Only HTTP is loopback.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

type provider struct{ name string }

func (p *provider) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{Name: p.name, Version: "0.1.0", Author: "workflow-tests", Description: "Native scoped step credential fixture"}
}

func (p *provider) StepTypes() []string {
	if p.name == "step-credentials-unrelated" {
		return []string{"step.scoped_foreign"}
	}
	return []string{"step.scoped_submit", "step.scoped_observe"}
}

type stepConfig struct {
	ServerURL    string `json:"server_url"`
	AuthTokenRef string `json:"auth_token_ref"`
	URL          string `json:"url"`
	ConfigDir    string `json:"_config_dir,omitempty"`
}

type observation struct {
	PID             int  `json:"pid"`
	CarrierPresence bool `json:"carrier_presence"`
}

type urlPayload struct {
	URL string `json:"url"`
}

type step struct {
	submit bool
	config stepConfig
}

func (p *provider) CreateStep(typeName, _ string, raw map[string]any) (sdk.StepInstance, error) {
	allowed := false
	for _, name := range p.StepTypes() {
		allowed = allowed || name == typeName
	}
	if !allowed {
		return nil, errors.New("unknown fixture step type")
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, errors.New("fixture creation config encoding failed")
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	var cfg stepConfig
	if err := decoder.Decode(&cfg); err != nil {
		return nil, errors.New("fixture requires strict ref-only creation config")
	}
	submit := typeName == "step.scoped_submit"
	if submit && (cfg.ServerURL == "" || cfg.URL == "" || !strings.HasPrefix(cfg.AuthTokenRef, "config:") || strings.TrimPrefix(cfg.AuthTokenRef, "config:") == "" || strings.ContainsAny(cfg.AuthTokenRef, "{} \t\r\n")) {
		return nil, errors.New("fixture requires endpoint, URL and literal config ref")
	}
	return &step{submit: submit, config: cfg}, nil
}

func (s *step) Execute(ctx context.Context, trigger map[string]any, outputs map[string]map[string]any, current, metadata, runtimeConfig map[string]any) (*sdk.StepResult, error) {
	_, present := runtimeConfig["config"]
	observed := observation{PID: os.Getpid(), CarrierPresence: present}
	if s.submit {
		values, ok := runtimeConfig["config"].(map[string]any)
		if !ok {
			return nil, errors.New("credential carrier missing")
		}
		key := strings.TrimPrefix(s.config.AuthTokenRef, "config:")
		token, ok := values[key].(string)
		if !ok || token == "" || runtimeConfig["auth_token_ref"] != s.config.AuthTokenRef {
			return nil, errors.New("credential carrier missing or ref changed")
		}
		// Inspect the actual received value, never a compiled expected credential.
		for _, data := range []any{trigger, outputs, current, metadata, os.Environ(), s.config} {
			b, err := json.Marshal(data)
			if err != nil || bytes.Contains(b, []byte(token)) {
				return nil, errors.New("private value appeared outside execution carrier")
			}
		}
		productURL, ok := runtimeConfig["url"].(string)
		if !ok || len(current) != 1 || current["url"] != productURL || len(trigger) != 1 || trigger["url"] != productURL || len(outputs) != 0 {
			return nil, errors.New("capture child input was not URL-only")
		}
		endpoint, ok := runtimeConfig["server_url"].(string)
		parsed, parseErr := url.Parse(endpoint)
		if !ok || parseErr != nil || parsed.Scheme != "http" || !net.ParseIP(parsed.Hostname()).IsLoopback() {
			return nil, errors.New("fixture endpoint must be loopback HTTP")
		}
		body, err := json.Marshal(urlPayload{URL: productURL})
		if err != nil {
			return nil, errors.New("fixture URL payload encoding failed")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/submit", bytes.NewReader(body))
		if err != nil {
			return nil, errors.New("fixture request construction failed")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		transport := &http.Transport{Proxy: nil}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(req)
		if err != nil {
			return nil, errors.New("fixture loopback POST failed")
		}
		_, drainErr := io.Copy(io.Discard, resp.Body)
		closeErr := resp.Body.Close()
		if drainErr != nil || closeErr != nil || resp.StatusCode != http.StatusNoContent {
			return nil, errors.New("fixture loopback POST rejected")
		}
	}
	return &sdk.StepResult{Output: map[string]any{"pid": observed.PID, "carrier_presence": observed.CarrierPresence}}, nil
}

func main() {
	sdk.Serve(&provider{name: strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")})
}
