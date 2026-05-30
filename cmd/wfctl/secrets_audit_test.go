package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// TestSecretsAuditRecord verifies that:
//   - exactly one JSONL line is written per writeSecretsAuditRecord call
//   - the line contains the expected fields (ts, secret_name, store)
//   - the secret VALUE is absent from the file bytes
func TestSecretsAuditRecord(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	if err := writeSecretsAuditRecord("MY_SECRET", "localfs"); err != nil {
		t.Fatalf("writeSecretsAuditRecord: %v", err)
	}

	auditPath := filepath.Join(tmp, "wfctl", "plugins", "wfctl", "secrets-audit.jsonl")
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}

	// Must contain exactly one non-empty line.
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		if l := strings.TrimSpace(scanner.Text()); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 JSONL line, got %d: %s", len(lines), data)
	}

	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("unmarshal JSONL: %v (%s)", err, lines[0])
	}

	// Required fields.
	checkField := func(key string) {
		t.Helper()
		v, ok := rec[key]
		if !ok || v == "" {
			t.Errorf("audit record missing or empty field %q: %v", key, rec)
		}
	}
	checkField("ts")
	checkField("secret_name")
	checkField("store")

	if rec["secret_name"] != "MY_SECRET" {
		t.Errorf("secret_name = %v, want MY_SECRET", rec["secret_name"])
	}
	if rec["store"] != "localfs" {
		t.Errorf("store = %v, want localfs", rec["store"])
	}

	// The raw file bytes must never contain a secret value (only the name is stored).
	const forbiddenValue = "hunter2"
	if strings.Contains(string(data), forbiddenValue) {
		t.Errorf("audit file contains forbidden value %q: %s", forbiddenValue, data)
	}
}

// TestSecretsAuditRecord_AppendOnSecondWrite verifies appending behaviour.
func TestSecretsAuditRecord_AppendOnSecondWrite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	if err := writeSecretsAuditRecord("A", "store1"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := writeSecretsAuditRecord("B", "store2"); err != nil {
		t.Fatalf("second write: %v", err)
	}

	auditPath := filepath.Join(tmp, "wfctl", "plugins", "wfctl", "secrets-audit.jsonl")
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		if l := strings.TrimSpace(scanner.Text()); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %s", len(lines), data)
	}
}

// TestResolveSetupStorePriority exercises the store-resolver priority chain
// (5 cases).
func TestResolveSetupStorePriority(t *testing.T) {
	oneStore := map[string]*config.SecretStoreConfig{
		"only-store": {Provider: "file"},
	}
	twoStores := map[string]*config.SecretStoreConfig{
		"s1": {Provider: "file"},
		"s2": {Provider: "env"},
	}

	tests := []struct {
		name         string
		storeFlag    string
		defaultStore string
		stores       map[string]*config.SecretStoreConfig
		interactive  bool
		wantStore    string
		wantErr      bool
	}{
		{
			name:      "1. --store flag wins",
			storeFlag: "flagged",
			wantStore: "flagged",
		},
		{
			name:         "2. config.defaultStore",
			defaultStore: "configured",
			wantStore:    "configured",
		},
		{
			name:      "3. exactly-one secretStores entry",
			stores:    oneStore,
			wantStore: "only-store",
		},
		{
			name:        "4. multiple stores, non-interactive → error",
			stores:      twoStores,
			interactive: false,
			wantErr:     true,
		},
		{
			name:      "5. no store anywhere → env fallback",
			wantStore: "env",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store, err := resolveSetupStoreName(tc.storeFlag, tc.defaultStore, tc.stores, tc.interactive)
			if tc.wantErr {
				if err == nil {
					t.Errorf("want error, got store=%q", store)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if store != tc.wantStore {
				t.Errorf("store = %q, want %q", store, tc.wantStore)
			}
		})
	}
}
