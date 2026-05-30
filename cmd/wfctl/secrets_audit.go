package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
)

// secretsAuditRecord is the shape of one line in secrets-audit.jsonl.
// It MUST NOT contain a secret value — only the name and metadata.
type secretsAuditRecord struct {
	Ts         string `json:"ts"`
	SecretName string `json:"secret_name"`
	Store      string `json:"store"`
	Scope      string `json:"scope,omitempty"`
	Actor      string `json:"actor,omitempty"`
}

// secretsAuditPath returns the path to the audit JSONL file.
// It honours $XDG_STATE_HOME; falls back to $HOME/.local/state.
func secretsAuditPath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		u, err := user.Current()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		base = filepath.Join(u.HomeDir, ".local", "state")
	}
	return filepath.Join(base, "wfctl", "plugins", "wfctl", "secrets-audit.jsonl"), nil
}

// writeSecretsAuditRecord appends a single JSON line to the audit log
// for a successful secret Set. It NEVER writes the secret value.
func writeSecretsAuditRecord(name, store string) error {
	path, err := secretsAuditPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("audit: create dir: %w", err)
	}

	actor := os.Getenv("USER")
	if actor == "" {
		if u, err := user.Current(); err == nil {
			actor = u.Username
		}
	}

	rec := secretsAuditRecord{
		Ts:         time.Now().UTC().Format(time.RFC3339),
		SecretName: name,
		Store:      store,
		Actor:      actor,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("audit: marshal: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("audit: open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

// resolveSetupStoreName implements the 5-step store resolution priority:
//
//  1. storeFlag (--store CLI flag) → use it directly.
//  2. defaultStore (config.secrets.defaultStore) → use it.
//  3. exactly-one entry in secretStores → use its key.
//  4. interactive=true → caller must invoke prompt.Select (handled at call site).
//  5. → error (non-interactive with no resolved store).
//
// When case 4 applies, this function returns ("", nil) so the caller can
// prompt the user. Callers should check for ("", nil) and prompt accordingly.
func resolveSetupStoreName(
	storeFlag string,
	defaultStore string,
	stores map[string]*config.SecretStoreConfig,
	interactive bool,
) (string, error) {
	// 1. Explicit --store flag.
	if storeFlag != "" {
		return storeFlag, nil
	}
	// 2. Config default.
	if defaultStore != "" {
		return defaultStore, nil
	}
	// 3. Exactly-one store in the map.
	if len(stores) == 1 {
		for k := range stores {
			return k, nil
		}
	}
	// 4/5. Multiple stores or none.
	if len(stores) > 1 {
		if !interactive {
			return "", fmt.Errorf("multiple secret stores configured; set secrets.defaultStore or pass --store (available: %s)",
				storeNames(stores))
		}
		// Caller must prompt — signal with empty string + no error.
		return "", nil
	}
	// No stores configured → fall back to "env" (legacy).
	return "env", nil
}

// storeNames returns a comma-separated, sorted list of store names for error
// messages (sorted for deterministic output).
func storeNames(stores map[string]*config.SecretStoreConfig) string {
	names := make([]string, 0, len(stores))
	for k := range stores {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
