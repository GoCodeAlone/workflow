//go:build e2e_dns_import

// End-to-end smoke test for `wfctl infra import-all` against a real
// workflow-plugin-digitalocean plugin loaded from disk. Gated by:
//
//   - build tag `e2e_dns_import`
//   - WFCTL_E2E_DNS_IMPORT=1 env var
//   - DIGITALOCEAN_TOKEN env var (read access to /v2/domains)
//
// Optional:
//   - WFCTL_E2E_DO_PLUGIN_DIR — directory containing the
//     workflow-plugin-digitalocean binary; defaults to ../../data/plugins.
//
// Run locally:
//
//	WFCTL_E2E_DNS_IMPORT=1 DIGITALOCEAN_TOKEN=$TOKEN \
//	  GOWORK=off go test -tags e2e_dns_import \
//	    -run TestInfraImportAll_e2e_DO ./cmd/wfctl/...

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInfraImportAll_e2e_DO(t *testing.T) {
	if os.Getenv("WFCTL_E2E_DNS_IMPORT") != "1" {
		t.Skip("set WFCTL_E2E_DNS_IMPORT=1 + DIGITALOCEAN_TOKEN to run")
	}
	token := os.Getenv("DIGITALOCEAN_TOKEN")
	if token == "" {
		t.Skip("DIGITALOCEAN_TOKEN not set; cannot run live e2e")
	}

	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	cfgPath := filepath.Join(dir, "infra.yaml")
	cfgContent := `
modules:
  - name: do-prod
    type: iac.provider
    config:
      provider: digitalocean
      token: ${DIGITALOCEAN_TOKEN}
      region: nyc3
  - name: local-state
    type: iac.state
    config:
      backend: filesystem
      directory: ` + stateDir + `
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	pluginDir := os.Getenv("WFCTL_E2E_DO_PLUGIN_DIR")
	// baseArgs is the canonical flag set without --dry-run; each pass
	// builds its own slice off this base so the dry-run vs. real-run
	// transition does NOT depend on flag ordering inside `args`. Earlier
	// revisions mutated the last element + ran dropFlag, which broke when
	// WFCTL_E2E_DO_PLUGIN_DIR was set (the last element became the plugin
	// directory path, not --dry-run, and the in-place rewrite corrupted
	// the args list).
	baseArgs := []string{"--config", cfgPath, "--provider", "do-prod", "--type", "infra.dns"}
	if pluginDir != "" {
		baseArgs = append(baseArgs, "--plugin-dir", pluginDir)
	}

	dryRunArgs := append([]string(nil), baseArgs...)
	dryRunArgs = append(dryRunArgs, "--dry-run")
	if err := runInfraImportAll(dryRunArgs); err != nil {
		t.Fatalf("e2e import-all dry-run: %v", err)
	}

	// Second pass: real import. Tightest contract the e2e can assert is:
	// dry-run succeeded; reading the state directory after a real import
	// shows non-zero rows (or zero if the account has no DNS zones).
	realArgs := append([]string(nil), baseArgs...)
	if err := runInfraImportAll(realArgs); err != nil {
		t.Fatalf("e2e import-all real: %v", err)
	}
	// Snapshot the state-store contents by listing the filesystem backend.
	store, err := resolveStateStore(cfgPath, "")
	if err != nil {
		t.Fatalf("resolve state: %v", err)
	}
	resources, err := store.ListResources(context.Background())
	if err != nil {
		t.Fatalf("list state: %v", err)
	}
	if len(resources) == 0 {
		t.Log("note: account has zero DNS zones; e2e validated dispatch + flag plumbing only")
		return
	}
	for _, r := range resources {
		if r.Type != "infra.dns" {
			t.Errorf("state resource %q has Type=%q; want infra.dns", r.Name, r.Type)
		}
		if r.Provider != "digitalocean" {
			t.Errorf("state resource %q has Provider=%q; want digitalocean", r.Name, r.Provider)
		}
		if r.ProviderID == "" {
			t.Errorf("state resource %q has empty ProviderID", r.Name)
		}
	}
	t.Logf("e2e: imported %d DNS zones into local state store", len(resources))
}
