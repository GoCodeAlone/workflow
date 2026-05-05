package interfaces_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestEnumerator_TypeAssertionCompiles verifies the optional-interface
// pattern for Enumerator: (1) a type CAN implement Enumerator, and (2)
// a type that implements IaCProvider is NOT required to implement
// Enumerator. The opt-in nature is what lets `wfctl infra cleanup --tag`
// degrade gracefully across providers that have no tag-query API:
// non-implementers are skipped with a structured stdout log.
//
// If a future change accidentally moved EnumerateByTag onto IaCProvider
// (or made Enumerator a required embedded interface), this runtime
// assertion would fail and the cleanup subcommand's skip-on-missing
// behaviour would silently break for plugins that haven't been updated.
func TestEnumerator_TypeAssertionCompiles(t *testing.T) {
	// (1) fakeEnumerator implements Enumerator.
	var _ interfaces.Enumerator = (*fakeEnumerator)(nil)

	// (2) mockProvider implements IaCProvider but NOT Enumerator.
	// mockProvider is defined in iac_test.go (same package).
	var p interfaces.IaCProvider = (*mockProvider)(nil)
	if _, ok := p.(interfaces.Enumerator); ok {
		t.Errorf("mockProvider should not satisfy Enumerator; the optional-interface idiom requires the negative case to assert false")
	}
}

type fakeEnumerator struct{}

func (f *fakeEnumerator) EnumerateByTag(ctx context.Context, tag string) ([]interfaces.ResourceRef, error) {
	return []interfaces.ResourceRef{{Name: "test", Type: "infra.compute"}}, nil
}
