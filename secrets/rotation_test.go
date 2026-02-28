package secrets

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// Compile-time check: VaultProvider must implement RotationProvider.
var _ RotationProvider = (*VaultProvider)(nil)

// versionedEntry stores all versions of a secret so GetPrevious can be tested.
type versionedEntry struct {
	versions []map[string]interface{} // index 0 = version 1, index 1 = version 2, ...
}

func (e *versionedEntry) current() map[string]interface{} {
	if len(e.versions) == 0 {
		return nil
	}
	return e.versions[len(e.versions)-1]
}

func (e *versionedEntry) currentVersion() int {
	return len(e.versions)
}

func (e *versionedEntry) getVersion(v int) map[string]interface{} {
	if v < 1 || v > len(e.versions) {
		return nil
	}
	return e.versions[v-1]
}

func (e *versionedEntry) put(data map[string]interface{}) {
	e.versions = append(e.versions, data)
}

// newVersionedVaultServer creates an httptest server that tracks all KV v2 versions.
// It supports ?version=N query params on GET for GetVersion requests.
func newVersionedVaultServer(t *testing.T) (*httptest.Server, map[string]*versionedEntry) {
	t.Helper()
	store := make(map[string]*versionedEntry)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Vault-Token")
		if token == "" {
			http.Error(w, `{"errors":["missing client token"]}`, http.StatusForbidden)
			return
		}

		path := r.URL.Path
		switch {
		case strings.Contains(path, "/data/"):
			handleVersionedData(w, r, path, store)
		default:
			http.Error(w, `{"errors":["not found"]}`, http.StatusNotFound)
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, store
}

func handleVersionedData(w http.ResponseWriter, r *http.Request, path string, store map[string]*versionedEntry) {
	parts := strings.SplitN(path, "/data/", 2)
	if len(parts) < 2 {
		http.Error(w, `{"errors":["invalid path"]}`, http.StatusBadRequest)
		return
	}
	key := parts[1]

	switch r.Method {
	case http.MethodGet:
		entry, ok := store[key]
		if !ok || len(entry.versions) == 0 {
			http.Error(w, `{"errors":[]}`, http.StatusNotFound)
			return
		}

		// Check for ?version=N query param
		versionParam := r.URL.Query().Get("version")
		var data map[string]interface{}
		var version int

		if versionParam != "" {
			v, err := strconv.Atoi(versionParam)
			if err != nil || v < 1 {
				http.Error(w, `{"errors":["invalid version"]}`, http.StatusBadRequest)
				return
			}
			data = entry.getVersion(v)
			if data == nil {
				http.Error(w, `{"errors":[]}`, http.StatusNotFound)
				return
			}
			version = v
		} else {
			data = entry.current()
			version = entry.currentVersion()
		}

		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"data": data,
				"metadata": map[string]interface{}{
					"version": version,
				},
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

		if store[key] == nil {
			store[key] = &versionedEntry{}
		}
		store[key].put(payload.Data)

		newVersion := store[key].currentVersion()
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"version": newVersion,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

	default:
		http.Error(w, `{"errors":["method not allowed"]}`, http.StatusMethodNotAllowed)
	}
}

func TestVaultProvider_Rotate(t *testing.T) {
	server, store := newVersionedVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address:   server.URL,
		Token:     "test-token",
		MountPath: "secret",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	ctx := context.Background()

	// Rotate on a fresh key — should create version 1.
	newVal, err := p.Rotate(ctx, "myapp/api-key")
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if newVal == "" {
		t.Fatal("expected non-empty rotated value")
	}
	// 32 bytes hex-encoded = 64 chars
	if len(newVal) != 64 {
		t.Errorf("expected 64-char hex value, got %d chars: %q", len(newVal), newVal)
	}

	entry, ok := store["myapp/api-key"]
	if !ok {
		t.Fatal("expected key to exist in store")
	}
	if entry.currentVersion() != 1 {
		t.Errorf("expected version 1 after first rotate, got %d", entry.currentVersion())
	}

	// Rotate again — should create version 2.
	newVal2, err := p.Rotate(ctx, "myapp/api-key")
	if err != nil {
		t.Fatalf("Rotate (second): %v", err)
	}
	if newVal2 == newVal {
		t.Error("expected different value after second rotate")
	}
	if entry.currentVersion() != 2 {
		t.Errorf("expected version 2 after second rotate, got %d", entry.currentVersion())
	}
}

func TestVaultProvider_Rotate_EmptyKey(t *testing.T) {
	server, _ := newVersionedVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	_, err = p.Rotate(context.Background(), "")
	if err != ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey, got %v", err)
	}
}

func TestVaultProvider_GetPrevious(t *testing.T) {
	server, store := newVersionedVaultServer(t)

	// Pre-populate two versions directly in the store.
	store["myapp/db-pass"] = &versionedEntry{
		versions: []map[string]interface{}{
			{"value": "old-secret"},
			{"value": "new-secret"},
		},
	}

	p, err := NewVaultProvider(VaultConfig{
		Address:   server.URL,
		Token:     "test-token",
		MountPath: "secret",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	ctx := context.Background()

	// GetPrevious should return version 1 (old-secret).
	prev, err := p.GetPrevious(ctx, "myapp/db-pass#value")
	if err != nil {
		t.Fatalf("GetPrevious: %v", err)
	}
	if prev != "old-secret" {
		t.Errorf("expected 'old-secret', got %q", prev)
	}
}

func TestVaultProvider_GetPrevious_NoHistory(t *testing.T) {
	server, store := newVersionedVaultServer(t)

	// Only one version exists — GetPrevious should return ErrNotFound.
	store["myapp/only-one"] = &versionedEntry{
		versions: []map[string]interface{}{
			{"value": "only"},
		},
	}

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	_, err = p.GetPrevious(context.Background(), "myapp/only-one")
	if err == nil {
		t.Fatal("expected error when only one version exists")
	}
}

func TestVaultProvider_GetPrevious_EmptyKey(t *testing.T) {
	server, _ := newVersionedVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	_, err = p.GetPrevious(context.Background(), "")
	if err != ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey, got %v", err)
	}
}

func TestVaultProvider_GetPrevious_NotFound(t *testing.T) {
	server, _ := newVersionedVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	_, err = p.GetPrevious(context.Background(), "nonexistent/key")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestVaultProvider_Rotate_ThenGetPrevious(t *testing.T) {
	server, _ := newVersionedVaultServer(t)

	p, err := NewVaultProvider(VaultConfig{
		Address:   server.URL,
		Token:     "test-token",
		MountPath: "secret",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}

	ctx := context.Background()

	// First rotation creates version 1.
	val1, err := p.Rotate(ctx, "svc/token")
	if err != nil {
		t.Fatalf("first Rotate: %v", err)
	}

	// Second rotation creates version 2.
	_, err = p.Rotate(ctx, "svc/token")
	if err != nil {
		t.Fatalf("second Rotate: %v", err)
	}

	// GetPrevious should return the version 1 value (stored as "value" field).
	prev, err := p.GetPrevious(ctx, "svc/token#value")
	if err != nil {
		t.Fatalf("GetPrevious: %v", err)
	}
	if prev != val1 {
		t.Errorf("expected previous value %q, got %q", val1, prev)
	}
}
