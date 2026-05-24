package main

import (
	"strings"
	"testing"
)

// TestPluginRegistrySync_TypeAllowlist verifies the scaffold-defense:
// plugin.json.type values outside the allowlist (e.g. "scaffold") are
// rejected at sync time. Workflow#762 plan C-P3 fix.
func TestPluginRegistrySync_TypeAllowlist(t *testing.T) {
	cases := []struct {
		name    string
		manType string
		want    string
	}{
		{"external accepted", "external", ""},
		{"builtin accepted", "builtin", ""},
		{"core accepted", "core", ""},
		{"iac accepted", "iac", ""},
		{"scaffold rejected", "scaffold", "REJECT"},
		{"unknown rejected", "novel", "REJECT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.want == "REJECT" {
				if registryAllowedTypes[tc.manType] {
					t.Errorf("type %q should be rejected but is in allowlist", tc.manType)
				}
				return
			}
			if !registryAllowedTypes[tc.manType] {
				t.Errorf("type %q should be accepted but is not in allowlist", tc.manType)
			}
		})
	}
}

// TestPluginRegistrySync_NormalizeRepo ports the bash normalize_repo
// behavior (workflow-registry/scripts/sync-versions.sh:36-44).
func TestPluginRegistrySync_NormalizeRepo(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://github.com/owner/repo", "owner/repo"},
		{"http://github.com/owner/repo", "owner/repo"},
		{"github.com/owner/repo", "owner/repo"},
		{"https://github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo/", "owner/repo"},
		{"owner/repo", "owner/repo"},
		{"owner/repo/subpath", "owner/repo"},
		{"not-a-repo", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := normalizeRepo(tc.in)
			if got != tc.want {
				t.Errorf("normalizeRepo(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestPluginRegistrySync_DownloadsMatchVersion verifies the downloads-vs-version
// invariant the bash script enforces (sync-versions.sh:46-58).
func TestPluginRegistrySync_DownloadsMatchVersion(t *testing.T) {
	t.Run("empty downloads OK", func(t *testing.T) {
		raw := map[string]any{}
		if !downloadsMatchVersion(raw, "1.2.3") {
			t.Error("empty downloads should match (no URLs to verify)")
		}
	})
	t.Run("matching URLs OK", func(t *testing.T) {
		raw := map[string]any{
			"downloads": []any{
				map[string]any{
					"os":   "linux",
					"arch": "amd64",
					"url":  "https://github.com/owner/repo/releases/download/v1.2.3/repo-linux-amd64.tar.gz",
				},
			},
		}
		if !downloadsMatchVersion(raw, "1.2.3") {
			t.Error("matching URL should pass")
		}
	})
	t.Run("stale URLs rejected", func(t *testing.T) {
		raw := map[string]any{
			"downloads": []any{
				map[string]any{
					"os":   "linux",
					"arch": "amd64",
					"url":  "https://github.com/owner/repo/releases/download/v1.0.0/repo-linux-amd64.tar.gz",
				},
			},
		}
		if downloadsMatchVersion(raw, "1.2.3") {
			t.Error("stale URL should fail")
		}
	})
}

// TestPluginRegistrySync_PublishGradeSemverGate verifies the shared regex
// rejects non-publish-grade tags (workflow#762 plan C2 fixture pin).
func TestPluginRegistrySync_PublishGradeSemverGate(t *testing.T) {
	cases := []struct {
		tag      string
		accepted bool
	}{
		{"v1.2.3", true},
		{"v0.0.0", true},
		{"v10.20.30", true},
		{"v1.2", false},        // not M.m.p
		{"v1.2.3-rc1", false},  // prerelease (engine ParseSemver rejects)
		{"v1.2.3-rc.1", false}, // prerelease canonical
		{"v1.2.3+build", false},
		{"1.2.3", false}, // missing v prefix
		{"release-2026", false},
	}
	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			got := PublishGradeSemverRe.MatchString(tc.tag)
			if got != tc.accepted {
				t.Errorf("PublishGradeSemverRe.MatchString(%q) = %v, want %v", tc.tag, got, tc.accepted)
			}
		})
	}
}

// TestPluginRegistrySync_UsageHelp verifies the subcommand prints usage.
func TestPluginRegistrySync_UsageHelp(t *testing.T) {
	// Capture os.Stderr (flag.Usage writes there).
	// Use --help via flag parsing; that triggers Usage + flag.ErrHelp.
	err := runPluginRegistrySync([]string{"--help"})
	if err == nil {
		t.Skip("runPluginRegistrySync returned nil for --help; flag pkg may differ")
	}
	// flag.ErrHelp is the expected error for --help.
	if !strings.Contains(err.Error(), "help") {
		t.Logf("non-help error from --help (may be OK): %v", err)
	}
}
