package gate

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/GoCodeAlone/workflow/dns/policy"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type fakeReader struct {
	txtRRs []string
	err    error
}

func (f *fakeReader) GetTXT(_ context.Context, _ string) ([]string, error) {
	return f.txtRRs, f.err
}
func (f *fakeReader) UpsertTXT(_ context.Context, _ string, _ []string, _ int) error { return nil }

func TestGate_Allowed(t *testing.T) {
	reader := &fakeReader{txtRRs: []string{
		`heritage=wfinfra-v1 o=sre d=true`,
		`heritage=wfinfra-v1 o=multisite p=www,admin`,
	}}
	if err := Gate(context.Background(), reader, "z.com", "www", "A", "multisite"); err != nil {
		t.Errorf("expected pass, got %v", err)
	}
}

func TestGate_Denied(t *testing.T) {
	reader := &fakeReader{txtRRs: []string{
		`heritage=wfinfra-v1 o=sre d=true`,
		`heritage=wfinfra-v1 o=multisite p=www`,
	}}
	err := Gate(context.Background(), reader, "z.com", "bandname", "A", "multisite")
	if err == nil {
		t.Errorf("expected denial")
	}
}

func TestGate_FailClosedOnEmptyPolicy(t *testing.T) {
	reader := &fakeReader{txtRRs: []string{}}
	err := Gate(context.Background(), reader, "z.com", "www", "A", "anyone")
	if err == nil {
		t.Errorf("expected fail-closed when no policy exists")
	}
}

func TestGate_PropagatesParseError(t *testing.T) {
	reader := &fakeReader{txtRRs: []string{
		`heritage=wfinfra-v1 o=sre d=true`,
		`heritage=wfinfra-v1 o=multisite d=true p=www`, // two defaults
	}}
	err := Gate(context.Background(), reader, "z.com", "www", "A", "sre")
	if !errors.Is(err, policy.ErrMultipleDefaults) {
		t.Errorf("want ErrMultipleDefaults, got %v", err)
	}
}

type countingReader struct {
	txtRRs      []string
	callCounter *int
}

func (c *countingReader) GetTXT(_ context.Context, _ string) ([]string, error) {
	*c.callCounter++
	return c.txtRRs, nil
}
func (c *countingReader) UpsertTXT(_ context.Context, _ string, _ []string, _ int) error { return nil }

func TestCachingGate_OneGetTXTPerZone(t *testing.T) {
	calls := 0
	reader := &countingReader{txtRRs: []string{`heritage=wfinfra-v1 o=sre d=true`}, callCounter: &calls}
	g := NewCachingGate()
	for i := 0; i < 10; i++ {
		if err := g.Check(context.Background(), reader, "z.com", fmt.Sprintf("name%d", i), "A", "sre"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if calls != 1 {
		t.Errorf("want 1 GetTXT call across 10 Check invocations; got %d", calls)
	}
}

// ── DriverReader adapter tests ─────────────────────────────────────────────────

// fakeDriver implements interfaces.ResourceDriver for the DriverReader
// adapter tests. Only Read is meaningful; the other methods return
// zero-value to satisfy the full interface.
type fakeDriver struct {
	records []map[string]any
	readErr error
}

func (d *fakeDriver) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeDriver) Read(_ context.Context, _ interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	if d.readErr != nil {
		return nil, d.readErr
	}
	return &interfaces.ResourceOutput{Outputs: map[string]any{"records": d.records}}, nil
}
func (d *fakeDriver) Update(_ context.Context, _ interfaces.ResourceRef, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }
func (d *fakeDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return &interfaces.DiffResult{}, nil
}
func (d *fakeDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return &interfaces.HealthResult{Healthy: true}, nil
}
func (d *fakeDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeDriver) SensitiveKeys() []string { return nil }

// TestDriverReader_GetTXT_extractsMatchingRecord pins the adapter contract:
// only TXT records whose name matches the requested policy name are
// returned. Non-TXT and non-matching records must be silently filtered.
func TestDriverReader_GetTXT_extractsMatchingRecord(t *testing.T) {
	d := &fakeDriver{records: []map[string]any{
		{"type": "TXT", "name": "_workflow-dns-policy.z.com", "data": "heritage=wfinfra-v1 o=sre d=true"},
		{"type": "TXT", "name": "_other.z.com", "data": "noise"},
		{"type": "A", "name": "_workflow-dns-policy.z.com", "data": "10.0.0.1"},
	}}
	r := &DriverReader{Driver: d, Zone: "z.com"}
	got, err := r.GetTXT(context.Background(), "_workflow-dns-policy.z.com")
	if err != nil {
		t.Fatalf("GetTXT: %v", err)
	}
	if len(got) != 1 || got[0] != "heritage=wfinfra-v1 o=sre d=true" {
		t.Errorf("want exactly the policy TXT; got %v", got)
	}
}

// TestDriverReader_GetTXT_handlesUntypedSlice pins the structpb-roundtrip
// compatibility: when records arrive as []any (post-gRPC unmarshal) the
// adapter must still extract values. Tested at the extractTXTValues
// boundary so it does not depend on a particular fakeDriver shape.
func TestDriverReader_GetTXT_handlesUntypedSlice(t *testing.T) {
	rec := []any{
		map[string]any{"type": "TXT", "name": "_workflow-dns-policy.z.com", "data": "heritage=wfinfra-v1 o=sre d=true"},
	}
	got := extractTXTValues(map[string]any{"records": rec}, "_workflow-dns-policy.z.com")
	if len(got) != 1 || got[0] != "heritage=wfinfra-v1 o=sre d=true" {
		t.Errorf("want untyped-slice extraction; got %v", got)
	}
}

func TestDriverReader_GetTXT_returnsErrOnDriverError(t *testing.T) {
	sentinel := errors.New("simulated read failure")
	d := &fakeDriver{readErr: sentinel}
	r := &DriverReader{Driver: d, Zone: "z.com"}
	_, err := r.GetTXT(context.Background(), "_workflow-dns-policy.z.com")
	if !errors.Is(err, sentinel) {
		t.Errorf("want wrapped sentinel; got %v", err)
	}
}

func TestDriverReader_GetTXT_requiresZoneAndDriver(t *testing.T) {
	if _, err := (&DriverReader{Driver: nil, Zone: "z"}).GetTXT(context.Background(), "n"); err == nil {
		t.Error("want error for nil Driver")
	}
	if _, err := (&DriverReader{Driver: &fakeDriver{}, Zone: ""}).GetTXT(context.Background(), "n"); err == nil {
		t.Error("want error for empty Zone")
	}
}
