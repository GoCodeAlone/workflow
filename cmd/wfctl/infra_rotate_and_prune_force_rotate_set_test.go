package main

import (
	"strings"
	"testing"
)

// TestBuildRotateAndPruneForceRotateSet exercises the strict-contract
// validation added per Copilot review on PR #594:
//   - Exactly one matching gen entry (zero or two-plus reject)
//   - Match must be type=provider_credential
//   - Returns map[Key]bool with exactly one entry
//
// Without these guards, the staging-rotation false-negative class returns:
// multi-match → multi-rotate → fails len(rotations)==1 after side effects;
// non-provider_credential match → rotates without appending RotationResult.
func TestBuildRotateAndPruneForceRotateSet(t *testing.T) {
	cases := []struct {
		name        string
		argName     string
		gens        []SecretGen
		wantKeys    []string // empty = expect error
		wantErrFrag string   // substring expected in error
	}{
		{
			name:    "happy_path_match_by_name",
			argName: "coredump-deploy-key",
			gens: []SecretGen{
				{Key: "SPACES", Type: "provider_credential", Name: "coredump-deploy-key"},
			},
			wantKeys: []string{"SPACES"},
		},
		{
			name:    "happy_path_fallback_match_by_key",
			argName: "SPACES",
			gens: []SecretGen{
				{Key: "SPACES", Type: "provider_credential"},
			},
			wantKeys: []string{"SPACES"},
		},
		{
			name:        "empty_name_rejected",
			argName:     "",
			gens:        []SecretGen{{Key: "SPACES", Type: "provider_credential"}},
			wantErrFrag: "--name is required",
		},
		{
			name:        "no_generate_entries",
			argName:     "anything",
			gens:        nil,
			wantErrFrag: "no secrets.generate entries",
		},
		{
			name:        "no_match",
			argName:     "ghost-name",
			gens:        []SecretGen{{Key: "SPACES", Type: "provider_credential", Name: "coredump-deploy-key"}},
			wantErrFrag: "no secrets.generate entry matches",
		},
		{
			name:    "multi_match_by_name_rejected",
			argName: "shared-name",
			gens: []SecretGen{
				{Key: "SPACES_A", Type: "provider_credential", Name: "shared-name"},
				{Key: "SPACES_B", Type: "provider_credential", Name: "shared-name"},
			},
			wantErrFrag: "matches multiple secrets.generate entries",
		},
		{
			name:    "non_provider_credential_rejected",
			argName: "session-secret",
			gens: []SecretGen{
				{Key: "SESSION_SECRET", Type: "random_hex", Name: "session-secret"},
			},
			wantErrFrag: "rotate-and-prune only operates on provider_credential",
		},
		{
			name:    "name_match_takes_precedence_over_key_match",
			argName: "SPACES",
			gens: []SecretGen{
				{Key: "OTHER", Type: "provider_credential", Name: "SPACES"}, // matched (Pass 1 by Name)
				{Key: "SPACES", Type: "provider_credential"},                // not matched (Pass 1 hit, Pass 2 skipped)
			},
			wantKeys: []string{"OTHER"},
		},
		{
			// Defense-in-depth: even when --name picks a single gen, if the
			// matched gen's Key is shared with another gen, forceRotate[key]
			// would rotate both → fails len(rotations)==1. Reject up-front.
			name:    "matched_key_shared_with_another_gen_rejected",
			argName: "primary-key",
			gens: []SecretGen{
				{Key: "SHARED_KEY", Type: "provider_credential", Name: "primary-key"},   // matched
				{Key: "SHARED_KEY", Type: "provider_credential", Name: "secondary-key"}, // collides on Key
			},
			wantErrFrag: "rotate-and-prune requires Key uniqueness",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &SecretsConfig{Generate: tc.gens}
			got, err := buildRotateAndPruneForceRotateSet(tc.argName, cfg)

			if tc.wantErrFrag != "" {
				if err == nil {
					t.Fatalf("expected error containing %q; got nil (got=%v)", tc.wantErrFrag, got)
				}
				if !strings.Contains(err.Error(), tc.wantErrFrag) {
					t.Fatalf("expected error containing %q; got %q", tc.wantErrFrag, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.wantKeys) {
				t.Fatalf("expected %d keys, got %d (%v)", len(tc.wantKeys), len(got), got)
			}
			for _, k := range tc.wantKeys {
				if !got[k] {
					t.Fatalf("expected key %q in result, got %v", k, got)
				}
			}
		})
	}
}
