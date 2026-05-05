package interfaces

import (
	"context"
	"encoding/json"
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
		ID:     "dep-abc",
		Phase:  "pre_deploy",
		Cause:  "exit 1",
		At:     time.Now().UTC().Truncate(time.Second),
		Detail: "line1\nline2",
	}

	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Diagnostic
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ID != d.ID {
		t.Errorf("ID: got %q, want %q", got.ID, d.ID)
	}
	if got.Phase != d.Phase {
		t.Errorf("Phase: got %q, want %q", got.Phase, d.Phase)
	}
	if got.Cause != d.Cause {
		t.Errorf("Cause: got %q, want %q", got.Cause, d.Cause)
	}
	if got.Detail != d.Detail {
		t.Errorf("Detail: got %q, want %q", got.Detail, d.Detail)
	}
	if !got.At.Equal(d.At) {
		t.Errorf("At: got %v, want %v", got.At, d.At)
	}
}
