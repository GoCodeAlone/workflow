package refreshoutputs

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// countingProvider is a stress-test IaCProvider that hands out a single
// shared countingDriver. Every Read call increments the per-ProviderID
// counter so the test can assert "exactly once per resource" after
// Refresh returns.
type countingProvider struct {
	driver *countingDriver
}

func (p *countingProvider) Name() string    { panic("not used") }
func (p *countingProvider) Version() string { panic("not used") }
func (p *countingProvider) Initialize(context.Context, map[string]any) error {
	panic("not used")
}
func (p *countingProvider) Capabilities() []interfaces.IaCCapabilityDeclaration {
	panic("not used")
}
func (p *countingProvider) Plan(context.Context, []interfaces.ResourceSpec, []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	panic("not used")
}
func (p *countingProvider) Apply(context.Context, *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	panic("not used")
}
func (p *countingProvider) Destroy(context.Context, []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	panic("not used")
}
func (p *countingProvider) Status(context.Context, []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	panic("not used")
}
func (p *countingProvider) DetectDrift(context.Context, []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	panic("not used")
}
func (p *countingProvider) Import(context.Context, string, string) (*interfaces.ResourceState, error) {
	panic("not used")
}
func (p *countingProvider) ResolveSizing(string, interfaces.Size, *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	panic("not used")
}
func (p *countingProvider) SupportedCanonicalKeys() []string { panic("not used") }
func (p *countingProvider) BootstrapStateBackend(context.Context, map[string]any) (*interfaces.BootstrapResult, error) {
	panic("not used")
}
func (p *countingProvider) Close() error { return nil }

func (p *countingProvider) ResourceDriver(string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}

// countingDriver atomically tracks how many times Read was called for
// each ProviderID and how many goroutines are inside Read at once.
// concurrentPeak gives the test a way to assert that the bounded
// semaphore actually enforced its cap.
type countingDriver struct {
	mu             sync.Mutex
	callsByID      map[string]int
	inFlight       atomic.Int32
	concurrentPeak atomic.Int32
}

func (d *countingDriver) Create(context.Context, interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	panic("not used")
}
func (d *countingDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	now := d.inFlight.Add(1)
	defer d.inFlight.Add(-1)
	for {
		peak := d.concurrentPeak.Load()
		if now <= peak || d.concurrentPeak.CompareAndSwap(peak, now) {
			break
		}
	}

	// Hold each call long enough that, with N=100 and Concurrency=8, the
	// bounded pool will queue up: every dispatched call MUST overlap with
	// at least one other or the test's concurrency-cap check is vacuous.
	// 5ms × 100 / 8 ≈ 63ms total wall time at the cap, well under the
	// watchdog's 10s budget.
	time.Sleep(5 * time.Millisecond)

	d.mu.Lock()
	d.callsByID[ref.ProviderID]++
	d.mu.Unlock()
	return &interfaces.ResourceOutput{
		Name:       ref.Name,
		Type:       ref.Type,
		ProviderID: ref.ProviderID,
		// Add a "id" field so Refresh sees a diff and copies the new
		// Outputs into out[i].Outputs — gives the test a positive
		// per-resource signal that every refresh propagated.
		Outputs: map[string]any{"id": ref.ProviderID, "fresh": true},
	}, nil
}
func (d *countingDriver) Update(context.Context, interfaces.ResourceRef, interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	panic("not used")
}
func (d *countingDriver) Delete(context.Context, interfaces.ResourceRef) error {
	panic("not used")
}
func (d *countingDriver) Diff(context.Context, interfaces.ResourceSpec, *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	panic("not used")
}
func (d *countingDriver) HealthCheck(context.Context, interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	panic("not used")
}
func (d *countingDriver) Scale(context.Context, interfaces.ResourceRef, int) (*interfaces.ResourceOutput, error) {
	panic("not used")
}
func (d *countingDriver) SensitiveKeys() []string { return nil }

// TestRefresh_ConcurrencyStress_NoDeadlock_AllRefreshed_OnceEach exercises
// Refresh against 100 resources with Concurrency=8 and asserts:
//
//  1. No deadlock — Refresh returns within a generous watchdog budget.
//     Caught by `done` channel + 10s timeout `select`.
//  2. Read was called exactly once per resource — no double-dispatch
//     hidden bugs in the semaphore acquire/release pairing.
//  3. The resulting state slice carries the refreshed Outputs for every
//     entry — no goroutine wrote into the wrong out[i] under concurrency.
//  4. Concurrent peak in flight is between 2 and the requested
//     concurrency cap. ≥2 confirms the bounded pool actually parallelised
//     work; ≤cap confirms the semaphore enforced its limit.
func TestRefresh_ConcurrencyStress_NoDeadlock_AllRefreshed_OnceEach(t *testing.T) {
	const (
		nResources  = 100
		concurrency = 8
		watchdog    = 10 * time.Second
	)

	states := make([]interfaces.ResourceState, nResources)
	for i := range states {
		id := fmt.Sprintf("uuid-%03d", i)
		states[i] = interfaces.ResourceState{
			Name:       fmt.Sprintf("vpc-%03d", i),
			Type:       "infra.vpc",
			ProviderID: id,
			Outputs:    map[string]any{"ip_range": "10.0.0.0/16"},
		}
	}

	driver := &countingDriver{callsByID: make(map[string]int, nResources)}
	provider := &countingProvider{driver: driver}

	done := make(chan struct{})
	var refreshed []interfaces.ResourceState
	var refreshErr error
	go func() {
		defer close(done)
		refreshed, refreshErr = Refresh(context.Background(), provider, states, Options{Concurrency: concurrency})
	}()
	select {
	case <-done:
		// Refresh returned — proceed with assertions.
	case <-time.After(watchdog):
		t.Fatalf("Refresh did not return within %s — possible deadlock", watchdog)
	}

	if refreshErr != nil {
		t.Fatalf("Refresh: %v", refreshErr)
	}
	if len(refreshed) != nResources {
		t.Fatalf("expected %d refreshed states, got %d", nResources, len(refreshed))
	}

	// Each ProviderID should have been Read exactly once.
	driver.mu.Lock()
	defer driver.mu.Unlock()
	if len(driver.callsByID) != nResources {
		t.Errorf("expected reads for %d distinct ProviderIDs, got %d", nResources, len(driver.callsByID))
	}
	for id, n := range driver.callsByID {
		if n != 1 {
			t.Errorf("ProviderID %q: Read called %d times, want exactly 1", id, n)
		}
	}

	// Every state in the result must carry the refreshed Outputs map
	// (driver returns "id" + "fresh": true on every Read).
	for i, s := range refreshed {
		if got, _ := s.Outputs["id"].(string); got != fmt.Sprintf("uuid-%03d", i) {
			t.Errorf("refreshed[%d]: Outputs[\"id\"]=%q, want uuid-%03d", i, got, i)
		}
		if got, _ := s.Outputs["fresh"].(bool); !got {
			t.Errorf("refreshed[%d]: Outputs[\"fresh\"]=%v, want true", i, got)
		}
	}

	// Concurrency-cap invariants: peak inflight must have been >1 to
	// prove parallelism happened, and ≤concurrency to prove the
	// semaphore enforced its limit.
	peak := int(driver.concurrentPeak.Load())
	if peak < 2 {
		t.Errorf("concurrent peak in flight = %d; expected >=2 (parallelism not exercised)", peak)
	}
	if peak > concurrency {
		t.Errorf("concurrent peak in flight = %d; expected <=%d (semaphore cap exceeded)", peak, concurrency)
	}
}
