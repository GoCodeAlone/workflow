package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeProviderWithDelete is a test double implementing the narrow
// pruneProvider interface (EnumerateAll + DeleteResource). Tracks deleted
// resources by their ProviderID (the cloud-side access_key for spaces keys)
// so tests can assert exactly which resources the prune CLI removed.
type fakeProviderWithDelete struct {
	keys     []*interfaces.ResourceOutput
	deleted  []string // ProviderIDs of resources passed to DeleteResource
	lastType string
}

func (f *fakeProviderWithDelete) EnumerateAll(_ context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
	f.lastType = resourceType
	return f.keys, nil
}

func (f *fakeProviderWithDelete) DeleteResource(_ context.Context, ref interfaces.ResourceRef) error {
	f.deleted = append(f.deleted, ref.ProviderID)
	return nil
}

// pruneContains is a local helper so the test file doesn't depend on a
// shared 'contains' that might collide with another package-level helper.
func pruneContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestInfraPrune_RequiresTwoKeyOptIn locks in the two-factor opt-in for
// destructive prune: BOTH `--confirm` flag AND WFCTL_CONFIRM_PRUNE=1
// environment variable must be present, otherwise the command must reject
// the request before touching the cloud. This is the safety guard that
// prevents `prune` from running by accident.
func TestInfraPrune_RequiresTwoKeyOptIn(t *testing.T) {
	// Without --confirm flag → reject (env var not set either)
	var out bytes.Buffer
	code := runInfraPrune([]string{
		"--type", "infra.spaces_key",
		"--created-before", "2026-05-08T00:00:00Z",
		"--exclude-access-key", "AK_NEW",
	}, nil, &out)
	if code == 0 {
		t.Errorf("expected non-zero exit without --confirm; got 0; out=%s", out.String())
	}

	// With --confirm but WFCTL_CONFIRM_PRUNE not set → still reject
	out.Reset()
	code = runInfraPrune([]string{
		"--type", "infra.spaces_key",
		"--created-before", "2026-05-08T00:00:00Z",
		"--exclude-access-key", "AK_NEW",
		"--confirm",
	}, nil, &out)
	if code == 0 {
		t.Errorf("expected non-zero exit without WFCTL_CONFIRM_PRUNE=1; got 0; out=%s", out.String())
	}
}

// TestInfraPrune_RequiresExcludeAccessKey verifies the safety guard that
// prevents pruning every key in the account by accident: --exclude-access-key
// is mandatory so the operator MUST name the active credential they want to
// preserve. Without it, prune refuses even with both opt-ins.
func TestInfraPrune_RequiresExcludeAccessKey(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	var out bytes.Buffer
	code := runInfraPrune([]string{
		"--type", "infra.spaces_key",
		"--created-before", "2026-05-08T00:00:00Z",
		"--confirm",
		"--non-interactive",
	}, nil, &out)
	if code == 0 {
		t.Errorf("expected non-zero exit without --exclude-access-key; got 0")
	}
	if !strings.Contains(out.String(), "exclude-access-key") {
		t.Errorf("error message must mention --exclude-access-key requirement; got: %s", out.String())
	}
}

// TestInfraPrune_FiltersByTimeAndExcludesAccessKey is the happy-path
// regression sentinel for the prune filter: with both opt-ins satisfied
// (env + flag), --exclude-access-key naming the active credential, and
// --created-before set to "right now", prune must delete all OLD keys
// while preserving the active one. Tracks deletions by ProviderID
// (access_key) on the fake provider.
func TestInfraPrune_FiltersByTimeAndExcludesAccessKey(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	fakeProv := &fakeProviderWithDelete{
		keys: []*interfaces.ResourceOutput{
			{
				Name:       "old-1",
				Type:       "infra.spaces_key",
				ProviderID: "AK_OLD_1",
				Outputs: map[string]any{
					"access_key": "AK_OLD_1",
					"created_at": "2026-05-01T00:00:00Z",
					"name":       "old-1",
				},
			},
			{
				Name:       "old-2",
				Type:       "infra.spaces_key",
				ProviderID: "AK_OLD_2",
				Outputs: map[string]any{
					"access_key": "AK_OLD_2",
					"created_at": "2026-05-02T00:00:00Z",
					"name":       "old-2",
				},
			},
			{
				Name:       "new",
				Type:       "infra.spaces_key",
				ProviderID: "AK_NEW",
				Outputs: map[string]any{
					"access_key": "AK_NEW",
					"created_at": "2026-05-08T11:00:00Z",
					"name":       "new",
				},
			},
		},
	}
	var out bytes.Buffer
	code := runInfraPrune([]string{
		"--type", "infra.spaces_key",
		"--created-before", "2026-05-08T11:00:00Z",
		"--exclude-access-key", "AK_NEW",
		"--confirm",
		"--non-interactive",
	}, fakeProv, &out)
	if code != 0 {
		t.Fatalf("prune failed: code=%d, out=%s", code, out.String())
	}
	// AK_OLD_1, AK_OLD_2 must be deleted (older than --created-before).
	if !pruneContains(fakeProv.deleted, "AK_OLD_1") || !pruneContains(fakeProv.deleted, "AK_OLD_2") {
		t.Errorf("expected AK_OLD_1 + AK_OLD_2 deleted; got %v", fakeProv.deleted)
	}
	// AK_NEW must NOT be deleted (excluded via --exclude-access-key).
	if pruneContains(fakeProv.deleted, "AK_NEW") {
		t.Errorf("AK_NEW must NOT be deleted (excluded); got %v", fakeProv.deleted)
	}
}
