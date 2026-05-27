package main

import (
	"reflect"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
)

// TestProviderResolverInit_WiresLoader guards against accidental
// deletion or refactor breakage of provider_resolver_init.go. The
// production wiring happens at package init time; if the init() goes
// missing, every `wfctl infra apply` returns the UnregisteredResolver
// "no IaCProviderResolver registered" error — a graceful failure but
// only discoverable by running a real command. This test catches it
// at `go test`. Per code-reviewer I-3 on commit 63129d65f.
//
// The assertion uses a function-pointer comparison: after init() runs,
// wfctlhelpers.Resolver must point at a different func than
// wfctlhelpers.UnregisteredResolver. The looser "is not Unregistered"
// shape is acceptable per the reviewer's note — if a sibling test
// already swapped Resolver via t.Cleanup, the comparison still holds
// (the swap target is also non-Unregistered).
func TestProviderResolverInit_WiresLoader(t *testing.T) {
	got := reflect.ValueOf(wfctlhelpers.Resolver).Pointer()
	want := reflect.ValueOf(wfctlhelpers.UnregisteredResolver).Pointer()
	if got == want {
		t.Fatal("cmd/wfctl init() did not register a resolver; provider_resolver_init.go missing or its init() broken — wfctlhelpers.Resolver is still the UnregisteredResolver default")
	}
}
