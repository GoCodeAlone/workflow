package module

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	step.(*HTTPCallStep).oauthEntry.mu.Lock()
	step.(*HTTPCallStep).oauthEntry.expiry = time.Now().Add(-time.Second)
	step.(*HTTPCallStep).oauthEntry.mu.Unlock()

	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("second execute error: %v", err)
	}

	if atomic.LoadInt32(&tokenRequests) != 2 {
		t.Errorf("expected token to be fetched twice (once per expired cache), got %d", atomic.LoadInt32(&tokenRequests))
	}
}

// TestHTTPCallStep_OAuth2_ConcurrentFetch verifies that concurrent executions on different step
// instances sharing the same credentials only call the token endpoint once (singleflight).
func TestHTTPCallStep_OAuth2_ConcurrentFetch(t *testing.T) {
	var tokenRequests int32

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Small delay to allow multiple goroutines to pile up before the first response.
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&tokenRequests, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "shared-token",
			"expires_in":   3600,
		})
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer apiSrv.Close()

	// Use a unique client_secret per test run to get a fresh global cache entry.
	uniqueSecret := fmt.Sprintf("concurrent-secret-%d", time.Now().UnixNano())

	factory := NewHTTPCallStepFactory()
	authCfg := map[string]any{
		"type":          "oauth2_client_credentials",
		"token_url":     tokenSrv.URL + "/token",
		"client_id":     "concurrent-cid",
		"client_secret": uniqueSecret,
	}

	const concurrency = 5
	errs := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			step, err := factory("concurrent-test", map[string]any{
				"url":    apiSrv.URL,
				"method": "GET",
				"auth":   authCfg,
			}, nil)
			if err != nil {
				errs <- err
				return
			}
			step.(*HTTPCallStep).httpClient = &http.Client{}
			pc := NewPipelineContext(nil, nil)
			_, err = step.Execute(context.Background(), pc)
			errs <- err
		}()
	}

	for i := 0; i < concurrency; i++ {
		if err := <-errs; err != nil {
			t.Errorf("goroutine error: %v", err)
		}
	}

	if n := atomic.LoadInt32(&tokenRequests); n != 1 {
		t.Errorf("expected exactly 1 token request via singleflight, got %d", n)
	}
}

// TestHTTPCallStep_BodyFrom_String verifies that body_from with a string value sends raw bytes
// without JSON-encoding and without auto-setting Content-Type: application/json.
func TestHTTPCallStep_BodyFrom_String(t *testing.T) {
	type captured struct {
		body        []byte
		contentType string
	}
	ch := make(chan captured, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		ch <- captured{body: b, contentType: r.Header.Get("Content-Type")}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("body-from-string", map[string]any{
		"url":       srv.URL,
		"method":    "POST",
		"body_from": "raw_payload",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPCallStep).httpClient = srv.Client()

	pc := NewPipelineContext(nil, nil)
	pc.Current["raw_payload"] = `{"hello":"world"}`

	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("execute error: %v", err)
	}

	got := <-ch
	if string(got.body) != `{"hello":"world"}` {
		t.Errorf("expected raw body %q, got %q", `{"hello":"world"}`, string(got.body))
	}
	// Content-Type should NOT be auto-set to application/json for raw bodies
	if got.contentType == "application/json" {
		t.Errorf("expected Content-Type not to be application/json for body_from, got %q", got.contentType)
	}
}

// TestHTTPCallStep_BodyFrom_Bytes verifies that body_from with a []byte value sends raw bytes.
func TestHTTPCallStep_BodyFrom_Bytes(t *testing.T) {
	ch := make(chan []byte, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		ch <- b
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("body-from-bytes", map[string]any{
		"url":       srv.URL,
		"method":    "POST",
		"body_from": "raw_data",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPCallStep).httpClient = srv.Client()

	pc := NewPipelineContext(nil, nil)
	pc.Current["raw_data"] = []byte("binary\x00data")

	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("execute error: %v", err)
	}

	gotBody := <-ch
	if !bytes.Equal(gotBody, []byte("binary\x00data")) {
		t.Errorf("expected raw bytes, got %q", string(gotBody))
	}
}

// TestHTTPCallStep_BodyFrom_StepOutput verifies that body_from can resolve from step outputs.
func TestHTTPCallStep_BodyFrom_StepOutput(t *testing.T) {
	ch := make(chan []byte, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		ch <- b
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("body-from-step", map[string]any{
		"url":       srv.URL,
		"method":    "POST",
		"body_from": "steps.parse.raw_body",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPCallStep).httpClient = srv.Client()

	pc := NewPipelineContext(nil, nil)
	pc.StepOutputs["parse"] = map[string]any{
		"raw_body": `{"event":"push"}`,
	}

	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("execute error: %v", err)
	}

	gotBody := <-ch
	if string(gotBody) != `{"event":"push"}` {
		t.Errorf("expected raw body from step output, got %q", string(gotBody))
	}
}

// TestHTTPCallStep_BodyFrom_ContentTypeOverride verifies that Content-Type set in headers
// takes effect even with body_from.
func TestHTTPCallStep_BodyFrom_ContentTypeOverride(t *testing.T) {
	ch := make(chan string, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ch <- r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("body-from-ct", map[string]any{
		"url":       srv.URL,
		"method":    "POST",
		"body_from": "payload",
		"headers": map[string]any{
			"Content-Type": "application/xml",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPCallStep).httpClient = srv.Client()

	pc := NewPipelineContext(nil, nil)
	pc.Current["payload"] = `<root><item>1</item></root>`

	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("execute error: %v", err)
	}

	gotCT := <-ch
	if gotCT != "application/xml" {
		t.Errorf("expected Content-Type application/xml, got %q", gotCT)
	}
}

// TestHTTPCallStep_OAuth2Key_FetchesToken verifies that the top-level "oauth2" config key works
// as an alternative to "auth" with type=oauth2_client_credentials.
func TestHTTPCallStep_OAuth2Key_FetchesToken(t *testing.T) {
	var tokenRequests int32

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenRequests, 1)
		_ = r.ParseForm()
		if r.FormValue("grant_type") != "client_credentials" {
			t.Errorf("expected grant_type=client_credentials, got %q", r.FormValue("grant_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "oauth2-key-token",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer oauth2-key-token" {
			t.Errorf("expected Bearer oauth2-key-token, got %q", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer apiSrv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("oauth2-key-test", map[string]any{
		"url":    apiSrv.URL + "/data",
		"method": "GET",
		"oauth2": map[string]any{
			"grant_type":    "client_credentials",
			"token_url":     tokenSrv.URL + "/token",
			"client_id":     "sf-client",
			"client_secret": "sf-secret",
			"scopes":        []any{"api"},
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
		t.Errorf("expected 200, got %v", result.Output["status_code"])
	}
	if atomic.LoadInt32(&tokenRequests) != 1 {
		t.Errorf("expected 1 token request, got %d", atomic.LoadInt32(&tokenRequests))
	}
}

// TestHTTPCallStep_OAuth2Key_DefaultGrantType verifies that grant_type defaults to client_credentials.
func TestHTTPCallStep_OAuth2Key_DefaultGrantType(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("grant_type") != "client_credentials" {
			t.Errorf("expected grant_type=client_credentials, got %q", r.FormValue("grant_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer apiSrv.Close()

	factory := NewHTTPCallStepFactory()
	// No grant_type specified – should default to client_credentials
	step, err := factory("oauth2-default-grant", map[string]any{
		"url": apiSrv.URL,
		"oauth2": map[string]any{
			"token_url":     tokenSrv.URL + "/token",
			"client_id":     "cid",
			"client_secret": "csec",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPCallStep).httpClient = &http.Client{}

	if _, err := step.Execute(context.Background(), NewPipelineContext(nil, nil)); err != nil {
		t.Fatalf("execute error: %v", err)
	}
}

// TestHTTPCallStep_OAuth2Key_InvalidGrantType verifies that an unsupported grant_type is rejected.
func TestHTTPCallStep_OAuth2Key_InvalidGrantType(t *testing.T) {
	factory := NewHTTPCallStepFactory()
	_, err := factory("bad-grant", map[string]any{
		"url": "http://example.com/api",
		"oauth2": map[string]any{
			"grant_type":    "authorization_code",
			"token_url":     "http://example.com/token",
			"client_id":     "cid",
			"client_secret": "csec",
		},
	}, nil)
	if err == nil {
		t.Fatal("expected error for unsupported grant_type")
	}
	if !strings.Contains(err.Error(), "grant_type must be 'client_credentials'") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestHTTPCallStep_OAuth2Key_MissingFields verifies that missing oauth2 fields produce errors.
func TestHTTPCallStep_OAuth2Key_MissingFields(t *testing.T) {
	factory := NewHTTPCallStepFactory()

	tests := []struct {
		name   string
		oauth2 map[string]any
		errMsg string
	}{
		{
			name: "missing token_url",
			oauth2: map[string]any{
				"client_id":     "cid",
				"client_secret": "csec",
			},
			errMsg: "oauth2.token_url is required",
		},
		{
			name: "missing client_id",
			oauth2: map[string]any{
				"token_url":     "http://example.com/token",
				"client_secret": "csec",
			},
			errMsg: "oauth2.client_id is required",
		},
		{
			name: "missing client_secret",
			oauth2: map[string]any{
				"token_url": "http://example.com/token",
				"client_id": "cid",
			},
			errMsg: "oauth2.client_secret is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := factory("test", map[string]any{
				"url":    "http://example.com/api",
				"oauth2": tc.oauth2,
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

// TestHTTPCallStep_OAuth2_InstanceURL verifies that instance_url from the token response is
// parsed, injected into the pipeline context for URL template resolution, and included in step output.
func TestHTTPCallStep_OAuth2_InstanceURL(t *testing.T) {
	var tokenRequests int32

	// apiSrv is started first so its URL can be returned as instance_url from the token server.
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/services/data/v62.0/sobjects" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"created": true})
	}))
	defer apiSrv.Close()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenRequests, 1)
		w.Header().Set("Content-Type", "application/json")
		// Return apiSrv.URL as instance_url so {{.instance_url}} resolves to the test server.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "sf-token",
			"expires_in":   3600,
			"token_type":   "Bearer",
			"instance_url": apiSrv.URL,
		})
	}))
	defer tokenSrv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("sf-test", map[string]any{
		"url":    "{{.instance_url}}/services/data/v62.0/sobjects",
		"method": "GET",
		"oauth2": map[string]any{
			"token_url":     tokenSrv.URL + "/token",
			"client_id":     "sf-cid",
			"client_secret": "sf-csec",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	hs := step.(*HTTPCallStep)
	hs.httpClient = &http.Client{}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// Token endpoint should have been called exactly once.
	if atomic.LoadInt32(&tokenRequests) != 1 {
		t.Errorf("expected 1 token request, got %d", atomic.LoadInt32(&tokenRequests))
	}
	// instance_url should be present in the step output.
	if result.Output["instance_url"] != apiSrv.URL {
		t.Errorf("expected instance_url=%q in output, got %v", apiSrv.URL, result.Output["instance_url"])
	}
	// instance_url should also have been injected into pc.Current.
	if pc.Current["instance_url"] != apiSrv.URL {
		t.Errorf("expected instance_url in pc.Current, got %v", pc.Current["instance_url"])
	}
}

// TestHTTPCallStep_OAuth2_InstanceURL_Cached verifies that instance_url persists across calls
// and is refreshed when the token is invalidated and re-fetched.
func TestHTTPCallStep_OAuth2_InstanceURL_Cached(t *testing.T) {
	var tokenRequests int32
	const instanceURL1 = "https://instance1.salesforce.com"
	const instanceURL2 = "https://instance2.salesforce.com"

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&tokenRequests, 1)
		iurl := instanceURL1
		if n > 1 {
			iurl = instanceURL2
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok",
			"expires_in":   3600,
			"instance_url": iurl,
		})
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer apiSrv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("iurl-cached", map[string]any{
		"url":    apiSrv.URL,
		"method": "GET",
		"oauth2": map[string]any{
			"token_url":     tokenSrv.URL + "/token",
			"client_id":     "cid",
			"client_secret": "csec",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	hs := step.(*HTTPCallStep)
	hs.httpClient = &http.Client{}

	pc := NewPipelineContext(nil, nil)

	// First call – token is fetched, instance_url = instanceURL1.
	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("first execute error: %v", err)
	}
	if hs.oauthEntry.getInstanceURL() != instanceURL1 {
		t.Errorf("expected instance_url=%q after first fetch, got %q", instanceURL1, hs.oauthEntry.getInstanceURL())
	}

	// Invalidate and fetch again – instance_url should update to instanceURL2.
	hs.oauthEntry.invalidate()
	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("second execute error: %v", err)
	}
	if hs.oauthEntry.getInstanceURL() != instanceURL2 {
		t.Errorf("expected instance_url=%q after second fetch, got %q", instanceURL2, hs.oauthEntry.getInstanceURL())
	}
	if atomic.LoadInt32(&tokenRequests) != 2 {
		t.Errorf("expected 2 token requests, got %d", atomic.LoadInt32(&tokenRequests))
	}
}

// TestHTTPCallStep_OAuth2_Retry401_RefreshesInstanceURL verifies that on a 401, the retry uses
// an updated instance_url if the refreshed token response returns a new one.
func TestHTTPCallStep_OAuth2_Retry401_RefreshesInstanceURL(t *testing.T) {
	var tokenRequests int32

	// Two API servers represent two possible instance URLs.
	// First call → server1 returns 401; second call (retry) → server2 returns 200.
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server2.Close()

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return 401; the real API is on server2 after token refresh.
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`unauthorized`))
	}))
	defer server1.Close()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&tokenRequests, 1)
		instanceURL := server1.URL // first token → server1
		if n > 1 {
			instanceURL = server2.URL // refreshed token → server2
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("token-v%d", n),
			"expires_in":   3600,
			"instance_url": instanceURL,
		})
	}))
	defer tokenSrv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("retry-iurl-test", map[string]any{
		"url":    "{{.instance_url}}/api/resource",
		"method": "GET",
		"oauth2": map[string]any{
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
		t.Errorf("expected 2 token requests (initial + refresh), got %d", atomic.LoadInt32(&tokenRequests))
	}
	// After the retry, pc.Current["instance_url"] should reflect the refreshed server2 URL.
	if pc.Current["instance_url"] != server2.URL {
		t.Errorf("expected pc.Current[instance_url]=%q after retry, got %v", server2.URL, pc.Current["instance_url"])
	}
}

// TestHTTPCallStep_BodyFrom_NilValue verifies that body_from with a missing path sends no body.
func TestHTTPCallStep_BodyFrom_NilValue(t *testing.T) {
	ch := make(chan []byte, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		ch <- b
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	factory := NewHTTPCallStepFactory()
	step, err := factory("body-from-nil", map[string]any{
		"url":       srv.URL,
		"method":    "POST",
		"body_from": "nonexistent.path",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*HTTPCallStep).httpClient = srv.Client()

	pc := NewPipelineContext(nil, nil)

	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("execute error: %v", err)
	}

	gotBody := <-ch
	if len(gotBody) != 0 {
		t.Errorf("expected empty body for nil body_from, got %q", string(gotBody))
	}
}
