package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeProviderEnumeratorAll is a test double implementing
// interfaces.EnumeratorAll. It returns a fixed list of *ResourceOutput per
// the workflow contract — full metadata so the audit-keys CLI can render
// without re-reading from the cloud.
type fakeProviderEnumeratorAll struct {
	keys []*interfaces.ResourceOutput
	// lastType records the resourceType passed to EnumerateAll so tests can
	// assert the CLI forwarded the --type flag correctly.
	lastType string
}

func (f *fakeProviderEnumeratorAll) EnumerateAll(_ context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
	f.lastType = resourceType
	return f.keys, nil
}

// TestInfraAuditKeys_ListsAll verifies that `wfctl infra audit-keys --type
// <T>` delegates to the provider's EnumeratorAll, then renders every
// returned key's identifying fields (Name, ProviderID/access_key) into the
// writer. This is the failing test for Task 16 of the spaces-key-iac-resource
// plan (PR5). Until Task 17 implements runInfraAuditKeys + the registration
// of `wfctl infra audit-keys`, this test fails with `undefined:
// runInfraAuditKeys`.
func TestInfraAuditKeys_ListsAll(t *testing.T) {
	fakeProv := &fakeProviderEnumeratorAll{
		keys: []*interfaces.ResourceOutput{
			{
				Name:       "key-a",
				Type:       "infra.spaces_key",
				ProviderID: "AK_A",
				Outputs: map[string]any{
					"name":       "key-a",
					"access_key": "AK_A",
					"created_at": "2026-05-01T00:00:00Z",
				},
			},
			{
				Name:       "key-b",
				Type:       "infra.spaces_key",
				ProviderID: "AK_B",
				Outputs: map[string]any{
					"name":       "key-b",
					"access_key": "AK_B",
					"created_at": "2026-05-02T00:00:00Z",
				},
			},
		},
	}

	var out bytes.Buffer
	exitCode := runInfraAuditKeys([]string{"--type", "infra.spaces_key"}, fakeProv, &out)
	if exitCode != 0 {
		t.Fatalf("expected zero exit; got %d\nout=%s", exitCode, out.String())
	}
	if !strings.Contains(out.String(), "key-a") || !strings.Contains(out.String(), "key-b") {
		t.Errorf("expected both keys in output; got: %s", out.String())
	}
	if !strings.Contains(out.String(), "AK_A") || !strings.Contains(out.String(), "AK_B") {
		t.Errorf("expected access_keys in output; got: %s", out.String())
	}
	// CLI must have forwarded the --type flag to the enumerator.
	if fakeProv.lastType != "infra.spaces_key" {
		t.Errorf("EnumerateAll resourceType = %q, want %q", fakeProv.lastType, "infra.spaces_key")
	}
}
