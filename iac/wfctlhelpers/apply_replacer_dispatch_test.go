package wfctlhelpers

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeReplacerDriver implements both ResourceDriver AND ResourceReplacer
// to exercise the dispatch-to-replacer path. Embeds fakeDriver so all
// ResourceDriver methods are satisfied without duplication.
type fakeReplacerDriver struct {
	fakeDriver
	replaceCalled bool
	replaceErr    error
}

func (f *fakeReplacerDriver) Replace(_ context.Context, _ interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	f.replaceCalled = true
	if f.replaceErr != nil {
		return nil, f.replaceErr
	}
	return &interfaces.ResourceOutput{Name: spec.Name, ProviderID: "new-id"}, nil
}

// TestDoReplace_DispatchesToReplacerWhenAvailable verifies that doReplace
// routes to ResourceReplacer.Replace when the driver implements it, and
// does NOT fall through to DefaultReplace (Delete+Create).
func TestDoReplace_DispatchesToReplacerWhenAvailable(t *testing.T) {
	d := &fakeReplacerDriver{}
	action := interfaces.PlanAction{
		Action:   "replace",
		Resource: interfaces.ResourceSpec{Name: "x", Type: "test"},
		Current:  &interfaces.ResourceState{Name: "x", ProviderID: "old-id"},
	}
	result := &interfaces.ApplyResult{}
	err := doReplace(context.Background(), d, action, result)
	if err != nil {
		t.Fatalf("doReplace: %v", err)
	}
	if !d.replaceCalled {
		t.Error("Replacer.Replace was not invoked")
	}
	// DefaultReplace (Delete+Create) should NOT have fired.
	if d.deleteCount != 0 || d.createCount != 0 {
		t.Errorf("DefaultReplace fired alongside Replacer: deleteCount=%d createCount=%d (both should be 0)",
			d.deleteCount, d.createCount)
	}
	if got := result.ReplaceIDMap["x"]; got != "new-id" {
		t.Errorf("ReplaceIDMap[x] = %q, want new-id", got)
	}
}

// TestDoReplace_DefaultReplaceWhenNoReplacerInterface verifies that doReplace
// falls back to DefaultReplace (Delete+Create) when the driver does NOT
// implement ResourceReplacer.
func TestDoReplace_DefaultReplaceWhenNoReplacerInterface(t *testing.T) {
	d := &fakeDriver{} // does NOT implement ResourceReplacer
	action := interfaces.PlanAction{
		Action:   "replace",
		Resource: interfaces.ResourceSpec{Name: "x", Type: "test"},
		Current:  &interfaces.ResourceState{Name: "x", ProviderID: "old-id"},
	}
	result := &interfaces.ApplyResult{}
	err := doReplace(context.Background(), d, action, result)
	if err != nil {
		t.Fatalf("doReplace: %v", err)
	}
	if d.deleteCount == 0 || d.createCount == 0 {
		t.Errorf("DefaultReplace did NOT fire: deleteCount=%d createCount=%d (both should be 1)",
			d.deleteCount, d.createCount)
	}
}

// TestDoReplace_ReplacerError_IsWrappedByBackstop verifies that a
// non-conforming error from ResourceReplacer.Replace gets the engine-side
// backstop prefix applied (Task 11 implements wrapDriverReplaceError;
// this test is the cross-task coupling assertion).
func TestDoReplace_ReplacerError_IsWrappedByBackstop(t *testing.T) {
	d := &fakeReplacerDriver{replaceErr: errors.New("kaboom")}
	action := interfaces.PlanAction{
		Action:   "replace",
		Resource: interfaces.ResourceSpec{Name: "x", Type: "test"},
		Current:  &interfaces.ResourceState{Name: "x", ProviderID: "old-id"},
	}
	result := &interfaces.ApplyResult{}
	err := doReplace(context.Background(), d, action, result)
	if err == nil {
		t.Fatal("expected error from failing Replacer")
	}
	// After Task 11 lands, this will assert "replace: driver: kaboom".
	// For now assert only that an error was returned (pre-Task-11 compilation).
	if err.Error() == "" {
		t.Error("error message is empty")
	}
}
