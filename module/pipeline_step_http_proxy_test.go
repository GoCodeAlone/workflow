package module

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPProxyStep_BasicProxy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Backend", "test")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"proxied":true}`))
	}))
	defer backend.Close()

	factory := NewHTTPProxyStepFactory()
	step, err := factory("proxy-test", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPProxyStep).httpClient = backend.Client()

	origReq := httptest.NewRequest("GET", "/original", nil)
	recorder := httptest.NewRecorder()

	pc := NewPipelineContext(map[string]any{
		"backend_url": backend.URL,
	}, map[string]any{
		"_http_request":         origReq,
		"_http_response_writer": recorder,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if !result.Stop {
		t.Error("expected Stop=true")
	}

	if result.Output["status_code"] != http.StatusOK {
		t.Errorf("expected status_code 200, got %v", result.Output["status_code"])
	}

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected response status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"proxied":true}` {
		t.Errorf("expected proxied body, got %q", string(body))
	}

	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", resp.Header.Get("Content-Type"))
	}

	if resp.Header.Get("X-Backend") != "test" {
		t.Errorf("expected X-Backend header, got %q", resp.Header.Get("X-Backend"))
	}

	if pc.Metadata["_response_handled"] != true {
		t.Error("expected _response_handled=true")
	}
}

func TestHTTPProxyStep_WithResourcePath(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	defer backend.Close()

	factory := NewHTTPProxyStepFactory()
	step, err := factory("proxy-resource", map[string]any{
		"resource_key": "path_params.resource",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPProxyStep).httpClient = backend.Client()

	origReq := httptest.NewRequest("GET", "/original", nil)
	recorder := httptest.NewRecorder()

	pc := NewPipelineContext(map[string]any{
		"backend_url": backend.URL,
		"path_params": map[string]any{
			"resource": "api/v1/users",
		},
	}, map[string]any{
		"_http_request":         origReq,
		"_http_response_writer": recorder,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if !result.Stop {
		t.Error("expected Stop=true")
	}

	body, _ := io.ReadAll(recorder.Result().Body)
	if string(body) != "/api/v1/users" {
		t.Errorf("expected path /api/v1/users, got %q", string(body))
	}
}

func TestHTTPProxyStep_ForwardHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.Header.Get("Authorization") + "|" + r.Header.Get("X-Request-Id")))
	}))
	defer backend.Close()

	factory := NewHTTPProxyStepFactory()
	step, err := factory("proxy-headers", map[string]any{
		"forward_headers": []any{"Authorization", "X-Request-Id"},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPProxyStep).httpClient = backend.Client()

	origReq := httptest.NewRequest("GET", "/original", nil)
	origReq.Header.Set("Authorization", "Bearer token123")
	origReq.Header.Set("X-Request-Id", "req-456")
	origReq.Header.Set("X-Not-Forwarded", "should-not-appear")
	recorder := httptest.NewRecorder()

	pc := NewPipelineContext(map[string]any{
		"backend_url": backend.URL,
	}, map[string]any{
		"_http_request":         origReq,
		"_http_response_writer": recorder,
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	body, _ := io.ReadAll(recorder.Result().Body)
	if string(body) != "Bearer token123|req-456" {
		t.Errorf("expected forwarded headers, got %q", string(body))
	}
}

func TestHTTPProxyStep_ForwardQueryString(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.URL.RawQuery))
	}))
	defer backend.Close()

	factory := NewHTTPProxyStepFactory()
	step, err := factory("proxy-query", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPProxyStep).httpClient = backend.Client()

	origReq := httptest.NewRequest("GET", "/original?foo=bar&baz=qux", nil)
	recorder := httptest.NewRecorder()

	pc := NewPipelineContext(map[string]any{
		"backend_url": backend.URL,
	}, map[string]any{
		"_http_request":         origReq,
		"_http_response_writer": recorder,
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	body, _ := io.ReadAll(recorder.Result().Body)
	if string(body) != "foo=bar&baz=qux" {
		t.Errorf("expected query string forwarded, got %q", string(body))
	}
}

func TestHTTPProxyStep_POSTWithBody(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
	}))
	defer backend.Close()

	factory := NewHTTPProxyStepFactory()
	step, err := factory("proxy-post", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPProxyStep).httpClient = backend.Client()

	reqBody := `{"name":"test","data":"binary-ish"}`
	origReq := httptest.NewRequest("POST", "/original", strings.NewReader(reqBody))
	origReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	pc := NewPipelineContext(map[string]any{
		"backend_url": backend.URL,
	}, map[string]any{
		"_http_request":         origReq,
		"_http_response_writer": recorder,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["status_code"] != http.StatusCreated {
		t.Errorf("expected status_code 201, got %v", result.Output["status_code"])
	}

	body, _ := io.ReadAll(recorder.Result().Body)
	if string(body) != reqBody {
		t.Errorf("expected body passthrough, got %q", string(body))
	}
}

func TestHTTPProxyStep_MissingBackendURL(t *testing.T) {
	factory := NewHTTPProxyStepFactory()
	step, err := factory("proxy-missing", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{}, map[string]any{})
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for missing backend URL")
	}
	if !strings.Contains(err.Error(), "backend URL not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHTTPProxyStep_NoResponseWriter(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Custom", "value")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello from backend"))
	}))
	defer backend.Close()

	factory := NewHTTPProxyStepFactory()
	step, err := factory("proxy-no-writer", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPProxyStep).httpClient = backend.Client()

	origReq := httptest.NewRequest("GET", "/test", nil)
	pc := NewPipelineContext(map[string]any{
		"backend_url": backend.URL,
	}, map[string]any{
		"_http_request": origReq,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if !result.Stop {
		t.Error("expected Stop=true")
	}
	if result.Output["status_code"] != http.StatusOK {
		t.Errorf("expected status_code 200, got %v", result.Output["status_code"])
	}
	if result.Output["body"] != "hello from backend" {
		t.Errorf("expected body 'hello from backend', got %v", result.Output["body"])
	}
	headers, ok := result.Output["headers"].(map[string]any)
	if !ok {
		t.Fatal("expected headers in output")
	}
	if headers["X-Custom"] != "value" {
		t.Errorf("expected X-Custom header, got %v", headers["X-Custom"])
	}
}

func TestHTTPProxyStep_CustomBackendURLKey(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	factory := NewHTTPProxyStepFactory()
	step, err := factory("proxy-custom-key", map[string]any{
		"backend_url_key": "target.url",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPProxyStep).httpClient = backend.Client()

	origReq := httptest.NewRequest("GET", "/test", nil)
	recorder := httptest.NewRecorder()

	pc := NewPipelineContext(map[string]any{
		"target": map[string]any{
			"url": backend.URL,
		},
	}, map[string]any{
		"_http_request":         origReq,
		"_http_response_writer": recorder,
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	body, _ := io.ReadAll(recorder.Result().Body)
	if string(body) != "ok" {
		t.Errorf("expected 'ok', got %q", string(body))
	}
}

func TestHTTPProxyStep_CustomTimeout(t *testing.T) {
	factory := NewHTTPProxyStepFactory()
	step, err := factory("proxy-timeout", map[string]any{
		"timeout": "5s",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	proxyStep := step.(*HTTPProxyStep)
	if proxyStep.timeout != 5*1e9 {
		t.Errorf("expected 5s timeout, got %v", proxyStep.timeout)
	}
}

func TestHTTPProxyStep_NoOriginalRequest(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET (default), got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	factory := NewHTTPProxyStepFactory()
	step, err := factory("proxy-no-req", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPProxyStep).httpClient = backend.Client()

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(map[string]any{
		"backend_url": backend.URL,
	}, map[string]any{
		"_http_response_writer": recorder,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if !result.Stop {
		t.Error("expected Stop=true")
	}

	body, _ := io.ReadAll(recorder.Result().Body)
	if string(body) != "ok" {
		t.Errorf("expected 'ok', got %q", string(body))
	}
}

func TestHTTPProxyStep_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream error"))
	}))
	defer backend.Close()

	factory := NewHTTPProxyStepFactory()
	step, err := factory("proxy-error", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPProxyStep).httpClient = backend.Client()

	origReq := httptest.NewRequest("GET", "/test", nil)
	recorder := httptest.NewRecorder()

	pc := NewPipelineContext(map[string]any{
		"backend_url": backend.URL,
	}, map[string]any{
		"_http_request":         origReq,
		"_http_response_writer": recorder,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// Proxy should forward error responses as-is, not return an error
	if result.Output["status_code"] != http.StatusBadGateway {
		t.Errorf("expected status_code 502, got %v", result.Output["status_code"])
	}

	resp := recorder.Result()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected response status 502, got %d", resp.StatusCode)
	}
}

func TestHTTPProxyStep_ContentLengthForwarded(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength != 11 {
			t.Errorf("expected Content-Length 11, got %d", r.ContentLength)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	factory := NewHTTPProxyStepFactory()
	step, err := factory("proxy-cl", map[string]any{
		"forward_headers": []any{"Content-Type"},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPProxyStep).httpClient = backend.Client()

	body := "hello world"
	origReq := httptest.NewRequest("POST", "/test", bytes.NewReader([]byte(body)))
	origReq.Header.Set("Content-Type", "text/plain")
	recorder := httptest.NewRecorder()

	pc := NewPipelineContext(map[string]any{
		"backend_url": backend.URL,
	}, map[string]any{
		"_http_request":         origReq,
		"_http_response_writer": recorder,
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
}

func TestHTTPProxyStep_DefaultConfigValues(t *testing.T) {
	factory := NewHTTPProxyStepFactory()
	step, err := factory("proxy-defaults", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	proxyStep := step.(*HTTPProxyStep)
	if proxyStep.backendURLKey != "backend_url" {
		t.Errorf("expected default backend_url_key='backend_url', got %q", proxyStep.backendURLKey)
	}
	if proxyStep.resourceKey != "path_params.resource" {
		t.Errorf("expected default resource_key='path_params.resource', got %q", proxyStep.resourceKey)
	}
	if proxyStep.timeout != 30*1e9 {
		t.Errorf("expected default timeout=30s, got %v", proxyStep.timeout)
	}
}
