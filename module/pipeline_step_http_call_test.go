package module

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPCallStep_BasicGET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"hello":"world"}`))
	}))
	defer srv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("get-test", map[string]any{
		"url":    srv.URL + "/resource",
		"method": "GET",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	// inject test client
	step.(*HTTPCallStep).httpClient = srv.Client()

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["status_code"] != http.StatusOK {
		t.Errorf("expected status_code 200, got %v", result.Output["status_code"])
	}
	body, ok := result.Output["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected JSON body, got %T", result.Output["body"])
	}
	if body["hello"] != "world" {
		t.Errorf("expected hello=world, got %v", body["hello"])
	}
}

func TestHTTPCallStep_MissingURL(t *testing.T) {
	factory := NewHTTPCallStepFactory()
	_, err := factory("no-url", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing url")
	}
	if !strings.Contains(err.Error(), "'url' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHTTPCallStep_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`bad request`))
	}))
	defer srv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("err-test", map[string]any{"url": srv.URL}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPCallStep).httpClient = srv.Client()

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestHTTPCallStep_OAuth2_FetchesToken verifies that a bearer token is obtained and sent.
func TestHTTPCallStep_OAuth2_FetchesToken(t *testing.T) {
	var tokenRequests int32

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenRequests, 1)
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		_ = r.ParseForm()
		if r.FormValue("grant_type") != "client_credentials" {
			t.Errorf("expected grant_type=client_credentials, got %q", r.FormValue("grant_type"))
		}
		if r.FormValue("client_id") != "my-client" {
			t.Errorf("expected client_id=my-client, got %q", r.FormValue("client_id"))
		}
		if r.FormValue("client_secret") != "my-secret" {
			t.Errorf("expected client_secret=my-secret, got %q", r.FormValue("client_secret"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-access-token",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-access-token" {
			t.Errorf("expected Authorization: Bearer test-access-token, got %q", auth)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer apiSrv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("oauth-test", map[string]any{
		"url":    apiSrv.URL + "/data",
		"method": "GET",
		"auth": map[string]any{
			"type":          "oauth2_client_credentials",
			"token_url":     tokenSrv.URL + "/token",
			"client_id":     "my-client",
			"client_secret": "my-secret",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	// Use a shared transport so both servers are reachable with a single client
	step.(*HTTPCallStep).httpClient = &http.Client{}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["status_code"] != http.StatusOK {
		t.Errorf("expected 200, got %v", result.Output["status_code"])
	}
	if atomic.LoadInt32(&tokenRequests) != 1 {
		t.Errorf("expected 1 token request, got %d", atomic.LoadInt32(&tokenRequests))
	}
}

// TestHTTPCallStep_OAuth2_TokenCached verifies that a second call reuses the cached token.
func TestHTTPCallStep_OAuth2_TokenCached(t *testing.T) {
	var tokenRequests int32

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenRequests, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "cached-token",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer apiSrv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("cache-test", map[string]any{
		"url":    apiSrv.URL,
		"method": "GET",
		"auth": map[string]any{
			"type":          "oauth2_client_credentials",
			"token_url":     tokenSrv.URL + "/token",
			"client_id":     "cid",
			"client_secret": "csec",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPCallStep).httpClient = &http.Client{}

	pc := NewPipelineContext(nil, nil)

	// First call – token is fetched
	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("first execute error: %v", err)
	}
	// Second call – token is reused from cache
	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("second execute error: %v", err)
	}

	if atomic.LoadInt32(&tokenRequests) != 1 {
		t.Errorf("expected token to be fetched only once, got %d requests", atomic.LoadInt32(&tokenRequests))
	}
}

// TestHTTPCallStep_OAuth2_Retry401 verifies that a 401 triggers token invalidation and retry.
func TestHTTPCallStep_OAuth2_Retry401(t *testing.T) {
	var tokenRequests int32
	var apiRequests int32

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&tokenRequests, 1)
		token := "token-v1"
		if n > 1 {
			token = "token-v2"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": token,
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&apiRequests, 1)
		if n == 1 {
			// First call: return 401
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`unauthorized`))
			return
		}
		// Retry: verify fresh token
		if r.Header.Get("Authorization") != "Bearer token-v2" {
			t.Errorf("expected Bearer token-v2, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer apiSrv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("retry-test", map[string]any{
		"url":    apiSrv.URL + "/api",
		"method": "GET",
		"auth": map[string]any{
			"type":          "oauth2_client_credentials",
			"token_url":     tokenSrv.URL + "/token",
			"client_id":     "cid",
			"client_secret": "csec",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPCallStep).httpClient = &http.Client{}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["status_code"] != http.StatusOK {
		t.Errorf("expected 200 after retry, got %v", result.Output["status_code"])
	}
	if atomic.LoadInt32(&tokenRequests) != 2 {
		t.Errorf("expected 2 token requests, got %d", atomic.LoadInt32(&tokenRequests))
	}
	if atomic.LoadInt32(&apiRequests) != 2 {
		t.Errorf("expected 2 API requests, got %d", atomic.LoadInt32(&apiRequests))
	}
}

// TestHTTPCallStep_OAuth2_Scopes verifies that scopes are sent in the token request.
func TestHTTPCallStep_OAuth2_Scopes(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		scope := r.FormValue("scope")
		if scope != "read write" {
			t.Errorf("expected scope='read write', got %q", scope)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "scoped-token",
			"expires_in":   3600,
		})
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer apiSrv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("scope-test", map[string]any{
		"url":    apiSrv.URL,
		"method": "GET",
		"auth": map[string]any{
			"type":          "oauth2_client_credentials",
			"token_url":     tokenSrv.URL + "/token",
			"client_id":     "cid",
			"client_secret": "csec",
			"scopes":        []any{"read", "write"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPCallStep).httpClient = &http.Client{}

	pc := NewPipelineContext(nil, nil)
	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("execute error: %v", err)
	}
}

// TestHTTPCallStep_OAuth2_MissingFields verifies that missing auth fields produce errors.
func TestHTTPCallStep_OAuth2_MissingFields(t *testing.T) {
	factory := NewHTTPCallStepFactory()

	tests := []struct {
		name   string
		auth   map[string]any
		errMsg string
	}{
		{
			name: "missing token_url",
			auth: map[string]any{
				"type":          "oauth2_client_credentials",
				"client_id":     "cid",
				"client_secret": "csec",
			},
			errMsg: "auth.token_url is required",
		},
		{
			name: "missing client_id",
			auth: map[string]any{
				"type":          "oauth2_client_credentials",
				"token_url":     "http://example.com/token",
				"client_secret": "csec",
			},
			errMsg: "auth.client_id is required",
		},
		{
			name: "missing client_secret",
			auth: map[string]any{
				"type":      "oauth2_client_credentials",
				"token_url": "http://example.com/token",
				"client_id": "cid",
			},
			errMsg: "auth.client_secret is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := factory("test", map[string]any{
				"url":  "http://example.com/api",
				"auth": tc.auth,
			}, nil)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.errMsg) {
				t.Errorf("expected %q in error, got: %v", tc.errMsg, err)
			}
		})
	}
}

// TestHTTPCallStep_OAuth2_TokenExpiry verifies that an expired token is refreshed.
func TestHTTPCallStep_OAuth2_TokenExpiry(t *testing.T) {
	var tokenRequests int32

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenRequests, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "short-lived-token",
			"expires_in":   1, // 1 second TTL (minus 10s buffer => immediately invalid)
			"token_type":   "Bearer",
		})
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer apiSrv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("expiry-test", map[string]any{
		"url":    apiSrv.URL,
		"method": "GET",
		"auth": map[string]any{
			"type":          "oauth2_client_credentials",
			"token_url":     tokenSrv.URL + "/token",
			"client_id":     "cid",
			"client_secret": "csec",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPCallStep).httpClient = &http.Client{}

	pc := NewPipelineContext(nil, nil)

	// First call fetches a token with TTL=1s; after subtracting 10s buffer it expires immediately.
	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("first execute error: %v", err)
	}
	// Force expiry: sleep briefly then call again; the cache should be empty.
	time.Sleep(50 * time.Millisecond)
	// Manually set the token expiry to the past to simulate expiration.
	step.(*HTTPCallStep).tokenCache.mu.Lock()
	step.(*HTTPCallStep).tokenCache.expiry = time.Now().Add(-time.Second)
	step.(*HTTPCallStep).tokenCache.mu.Unlock()

	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("second execute error: %v", err)
	}

	if atomic.LoadInt32(&tokenRequests) != 2 {
		t.Errorf("expected token to be fetched twice (once per expired cache), got %d", atomic.LoadInt32(&tokenRequests))
	}
}
