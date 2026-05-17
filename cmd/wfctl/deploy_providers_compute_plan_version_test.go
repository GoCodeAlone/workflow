package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFindIaCPluginDir_ComputePlanVersionValidation pins the workflow#693
// (Phase 2.1 follow-up to #640) manifest validation gate: invalid values
// of iacProvider.computePlanVersion on the matching plugin manifest must
// hard-fail at findIaCPluginDir so operators see misconfiguration loudly
// instead of silently routing through the v1 dispatch fallback (which
// would break the Phase 2 hard-cutover contract per ADR 0024 + ADR 0040).
func TestFindIaCPluginDir_ComputePlanVersionValidation(t *testing.T) {
	tests := []struct {
		name              string
		computePlanVer    string
		wantErrSubstring  string
		wantVersionReturn string
	}{
		{name: "empty defaults to v1 dispatch", computePlanVer: "", wantVersionReturn: ""},
		{name: "v1 explicit", computePlanVer: "v1", wantVersionReturn: "v1"},
		{name: "v2 explicit (Phase 2)", computePlanVer: "v2", wantVersionReturn: "v2"},
		{name: "typo uppercase rejected", computePlanVer: "V2", wantErrSubstring: `invalid iacProvider.computePlanVersion "V2"`},
		{name: "typo decimal rejected", computePlanVer: "v2.0", wantErrSubstring: `invalid iacProvider.computePlanVersion "v2.0"`},
		{name: "typo word rejected", computePlanVer: "two", wantErrSubstring: `invalid iacProvider.computePlanVersion "two"`},
		{name: "phase-2.3 future-tag rejected pre-introduction", computePlanVer: "v3", wantErrSubstring: `invalid iacProvider.computePlanVersion "v3"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pluginDir := t.TempDir()
			pluginName := "workflow-plugin-test-" + tt.name
			pluginName = strings.ReplaceAll(pluginName, " ", "-")
			subDir := filepath.Join(pluginDir, pluginName)
			if mkErr := os.Mkdir(subDir, 0o755); mkErr != nil {
				t.Fatalf("mkdir: %v", mkErr)
			}
			manifest := `{
				"name": "` + pluginName + `",
				"version": "1.0.0",
				"capabilities": {"iacProvider": {"name": "test-provider"}},
				"iacProvider": {"computePlanVersion": "` + tt.computePlanVer + `"}
			}`
			if writeErr := os.WriteFile(filepath.Join(subDir, "plugin.json"), []byte(manifest), 0o644); writeErr != nil {
				t.Fatalf("write manifest: %v", writeErr)
			}

			name, gotVer, _, err := findIaCPluginDir(pluginDir, "test-provider")

			if tt.wantErrSubstring != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (name=%q ver=%q)", tt.wantErrSubstring, name, gotVer)
				}
				if !strings.Contains(err.Error(), tt.wantErrSubstring) {
					t.Errorf("error mismatch:\n  got:  %v\n  want substring: %q", err, tt.wantErrSubstring)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != pluginName {
				t.Errorf("name = %q; want %q", name, pluginName)
			}
			if gotVer != tt.wantVersionReturn {
				t.Errorf("computePlanVersion = %q; want %q", gotVer, tt.wantVersionReturn)
			}
		})
	}
}
