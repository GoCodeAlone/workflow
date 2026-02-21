package secrets

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestVaultServer creates an httptest server that mocks the Vault KV v2 API.
// It returns the server and a mutable data store for controlling responses.
func newTestVaultServer(t *testing.T) (*httptest.Server, map[string]map[string]interface{}) {
	t.Helper()
	store := make(map[string]map[string]interface{})

	mux := http.NewServeMux()

	// KV v2 read: GET /v1/{mount}/data/{path}
	// KV v2 write: POST /v1/{mount}/data/{path} (vault uses POST for PUT)
	// KV v2 delete metadata: DELETE /v1/{mount}/metadata/{path}
	// KV v2 list: LIST /v1/{mount}/metadata/{prefix}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Extract token verification
		token := r.Header.Get("X-Vault-Token")
		if token == "" {
			http.Error(w, `{"errors":["missing client token"]}`, http.StatusForbidden)
			return
		}

		path := r.URL.Path

		switch {
		case strings.Contains(path, "/data/"):
			handleData(w, r, path, store)
		case strings.Contains(path, "/metadata"):
			handleMetadata(w, r, path, store)
		default:
			http.Error(w, `{"errors":["not found"]}`, http.StatusNotFound)
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, store
}

func handleData(w http.ResponseWriter, r *http.Request, path string, store map[string]map[string]interface{}) {
	// Extract key from /v1/{mount}/data/{key}
	parts := strings.SplitN(path, "/data/", 2)
	if len(parts) < 2 {
		http.Error(w, `{"errors":["invalid path"]}`, http.StatusBadRequest)
		return
	}
	key := parts[1]

	switch r.Method {
	case http.MethodGet:
		data, ok := store[key]
		if !ok {
			http.Error(w, `{"errors":[]}`, http.StatusNotFound)
			return
		}
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"data":     data,
				"metadata": map[string]interface{}{"version": 1},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

	case http.MethodPost, http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, `{"errors":["read body failed"]}`, http.StatusBadRequest)
			return
		}
		var payload struct {
			Data map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, `{"errors":["invalid json"]}`, http.StatusBadRequest)
			return
		}
		store[key] = payload.Data
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"version": 1,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

	default:
		http.Error(w, `{"errors":["method not allowed"]}`, http.StatusMethodNotAllowed)
	}
}

func handleMetadata(w http.ResponseWriter, r *http.Request, path string, store map[string]map[string]interface{}) {
	// Extract prefix from path: /v1/{mount}/metadata/{prefix}
	// The vault client sends paths like /v1/secret/metadata or /v1/secret/metadata/app
	prefix := ""
	if idx := strings.Index(path, "/metadata"); idx >= 0 {
		rest := path[idx+len("/metadata"):]
		rest = strings.TrimPrefix(rest, "/")
		if rest != "" {
			// Add trailing slash since this represents a directory prefix
			prefix = rest + "/"
		}
	}

	// vault/api converts LIST to GET with ?list=true query param
	isList := r.Method == "LIST" ||
		(r.Method == http.MethodGet && r.URL.Query().Get("list") == "true")

	switch {
	case isList:
		var keys []interface{}
		seen := make(map[string]bool)
		for k := range store {
			if !strings.HasPrefix(k, prefix) {
				continue
			}
			remainder := strings.TrimPrefix(k, prefix)
			if remainder == "" {
				continue
			}
			// If there's a slash, return the directory prefix
			if idx := strings.Index(remainder, "/"); idx >= 0 {
				dir := remainder[:idx+1]
				if !seen[dir] {
					keys = append(keys, dir)
					seen[dir] = true
				}
			} else {
				if !seen[remainder] {
					keys = append(keys, remainder)
					seen[remainder] = true
				}
			}
		}
		if len(keys) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errors":[]}`))
			return
		}
		resp := map[string]interface{}{
			"request_id": "mock-list",
			"data": map[string]interface{}{
				"keys": keys,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

	case r.Method == http.MethodDelete:
		key := strings.TrimSuffix(prefix, "/")
		delete(store, key)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, `{"errors":["method not allowed"]}`, http.StatusMethodNotAllowed)
	}
}

func TestVaultProvider_Get_FullData(t *testing.T) {
	server, store := newTestVaultServer(t)
	store["myapp/config"] = map[string]interface{}{
		"password": "s3cret",
		"username": "admin",
	}

	p, err := NewVaultProvider(VaultConfig{
		Address:   server.URL,
		Token:     "test-token",
		MountPath: "secret",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	val, err := p.Get(context.Background(), "myapp/config")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(val, "password") || !strings.Contains(val, "s3cret") {
		t.Errorf("expected JSON with password:s3cret, got %q", val)
	}
}

func TestVaultProvider_Get_SpecificField(t *testing.T) {
	server, store := newTestVaultServer(t)
	store["myapp/config"] = map[string]interface{}{
		"password": "s3cret",
		"username": "admin",
	}

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	val, err := p.Get(context.Background(), "myapp/config#password")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "s3cret" {
		t.Errorf("expected 's3cret', got %q", val)
	}
}

func TestVaultProvider_Get_MissingField(t *testing.T) {
	server, store := newTestVaultServer(t)
	store["myapp/config"] = map[string]interface{}{
		"password": "s3cret",
	}

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	_, err = p.Get(context.Background(), "myapp/config#nonexistent")
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestVaultProvider_Get_NotFound(t *testing.T) {
	server, _ := newTestVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	_, err = p.Get(context.Background(), "nonexistent/key")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestVaultProvider_Get_WithNamespace(t *testing.T) {
	server, store := newTestVaultServer(t)
	store["myapp/secret"] = map[string]interface{}{
		"key": "value",
	}

	p, err := NewVaultProvider(VaultConfig{
		Address:   server.URL,
		Token:     "test-token",
		Namespace: "admin",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	val, err := p.Get(context.Background(), "myapp/secret#key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "value" {
		t.Errorf("expected 'value', got %q", val)
	}
}

func TestVaultProvider_Get_EmptyKey(t *testing.T) {
	server, _ := newTestVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	_, err = p.Get(context.Background(), "")
	if err != ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey, got %v", err)
	}
}

func TestVaultProvider_Get_CustomMountPath(t *testing.T) {
	// Use custom mount path "kv" — the mock server handles any mount
	server, store := newTestVaultServer(t)
	store["path"] = map[string]interface{}{
		"val": "ok",
	}

	p, err := NewVaultProvider(VaultConfig{
		Address:   server.URL,
		Token:     "test-token",
		MountPath: "kv",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	val, err := p.Get(context.Background(), "path#val")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "ok" {
		t.Errorf("expected 'ok', got %q", val)
	}
}

func TestVaultProvider_Set(t *testing.T) {
	server, store := newTestVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	ctx := context.Background()

	// Set a value
	if err := p.Set(ctx, "myapp/db-password", "super-secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Verify it was stored correctly
	data, ok := store["myapp/db-password"]
	if !ok {
		t.Fatal("expected key to exist in store")
	}
	if data["value"] != "super-secret" {
		t.Errorf("expected value 'super-secret', got %v", data["value"])
	}

	// Now read it back via Get
	val, err := p.Get(ctx, "myapp/db-password#value")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if val != "super-secret" {
		t.Errorf("expected 'super-secret', got %q", val)
	}
}

func TestVaultProvider_Set_EmptyKey(t *testing.T) {
	server, _ := newTestVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	if err := p.Set(context.Background(), "", "val"); err != ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey, got %v", err)
	}
}

func TestVaultProvider_Delete(t *testing.T) {
	server, store := newTestVaultServer(t)
	store["myapp/to-delete"] = map[string]interface{}{
		"value": "delete-me",
	}

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	ctx := context.Background()

	// Verify it exists first
	_, err = p.Get(ctx, "myapp/to-delete#value")
	if err != nil {
		t.Fatalf("Get before delete: %v", err)
	}

	// Delete it
	if err := p.Delete(ctx, "myapp/to-delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify it's gone
	_, err = p.Get(ctx, "myapp/to-delete")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestVaultProvider_Delete_EmptyKey(t *testing.T) {
	server, _ := newTestVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	if err := p.Delete(context.Background(), ""); err != ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey, got %v", err)
	}
}

func TestVaultProvider_List(t *testing.T) {
	server, store := newTestVaultServer(t)
	store["app/key1"] = map[string]interface{}{"value": "v1"}
	store["app/key2"] = map[string]interface{}{"value": "v2"}
	store["other/key3"] = map[string]interface{}{"value": "v3"}

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	keys, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(keys) < 3 {
		t.Errorf("expected at least 3 keys, got %d: %v", len(keys), keys)
	}
}

func TestVaultProvider_SetGetDeleteRoundTrip(t *testing.T) {
	server, _ := newTestVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	ctx := context.Background()

	// Set
	if err := p.Set(ctx, "roundtrip/key", "hello-world"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Get
	val, err := p.Get(ctx, "roundtrip/key#value")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "hello-world" {
		t.Errorf("expected 'hello-world', got %q", val)
	}

	// Delete
	if err := p.Delete(ctx, "roundtrip/key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify gone
	_, err = p.Get(ctx, "roundtrip/key")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestNewVaultProvider_DefaultMountPath(t *testing.T) {
	server, _ := newTestVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}
	if p.config.MountPath != "secret" {
		t.Errorf("expected default mount path 'secret', got %q", p.config.MountPath)
	}
}

func TestNewVaultProviderHTTP_BackwardCompat(t *testing.T) {
	server, _ := newTestVaultServer(t)

	p, err := NewVaultProviderHTTP(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProviderHTTP: %v", err)
	}
	if p.Name() != "vault" {
		t.Errorf("expected 'vault', got %q", p.Name())
	}
}

func TestParseVaultKey(t *testing.T) {
	tests := []struct {
		input string
		path  string
		field string
	}{
		{"secret/path#field", "secret/path", "field"},
		{"just/a/path", "just/a/path", ""},
		{"path#with#multiple#hashes", "path#with#multiple", "hashes"},
		{"#leading", "", "leading"},
	}

	for _, tt := range tests {
		path, field := parseVaultKey(tt.input)
		if path != tt.path || field != tt.field {
			t.Errorf("parseVaultKey(%q) = (%q, %q), want (%q, %q)",
				tt.input, path, field, tt.path, tt.field)
		}
	}
}

func TestVaultProvider_Name(t *testing.T) {
	server, _ := newTestVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}
	if p.Name() != "vault" {
		t.Errorf("expected 'vault', got %q", p.Name())
	}
}

func TestVaultProvider_Config(t *testing.T) {
	server, _ := newTestVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address:   server.URL,
		Token:     "test-token",
		MountPath: "kv",
		Namespace: "ns1",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}
	cfg := p.Config()
	if cfg.MountPath != "kv" {
		t.Errorf("expected mount path 'kv', got %q", cfg.MountPath)
	}
	if cfg.Namespace != "ns1" {
		t.Errorf("expected namespace 'ns1', got %q", cfg.Namespace)
	}
}

func TestVaultProvider_Client(t *testing.T) {
	server, _ := newTestVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}
	if p.Client() == nil {
		t.Error("expected non-nil client")
	}
}
