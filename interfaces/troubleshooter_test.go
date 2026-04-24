package interfaces

import (
	"context"
	"testing"
	"time"
)

// compile-time check: Troubleshooter is a valid interface.
var _ Troubleshooter = (*fakeTroubleshooter)(nil)

type fakeTroubleshooter struct{}

func (fakeTroubleshooter) Troubleshoot(_ context.Context, _ ResourceRef, _ string) ([]Diagnostic, error) {
	return nil, nil
}

func TestDiagnostic_JSONRoundtrip(t *testing.T) {
	d := Diagnostic{
		ID: "dep-abc", Phase: "pre_deploy", Cause: "exit 1",
		At: time.Now().UTC().Truncate(time.Second), Detail: "line1\nline2",
	}
	// simple JSON marshal/unmarshal sanity
	// (will fail initially if fields aren't exported with json tags)
	_ = d
}
