// Package refreshoutputs implements read-only state refresh — it reads
// current Outputs from providers and updates the persisted state when fields
// differ. It never invokes Update or Replace at the cloud level; the contract
// is strictly "Read and reconcile in-memory state".
//
// Refresh is the foundation for two consumers in W-2:
//
//   - wfctl infra refresh-outputs (T2.2): explicit operator-driven refresh.
//   - wfctl infra apply pre-step (T2.3): opt-in via WFCTL_REFRESH_OUTPUTS to
//     keep stale outputs from poisoning the planner.
package refreshoutputs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"sync"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// defaultConcurrency is the worker count used when Options.Concurrency is
// non-positive.
const defaultConcurrency = 8

// Options tunes Refresh behaviour. The zero value is valid and uses
// defaultConcurrency.
type Options struct {
	// Concurrency is the maximum number of concurrent Read calls. Values < 1
	// fall back to defaultConcurrency (8).
	Concurrency int
}

// Refresh issues a bounded-concurrency Read against p for each entry in
// states and returns a copy with Outputs reconciled to the live values.
// Resources whose live Outputs are deeply equal to the persisted Outputs are
// returned unchanged (callers can rely on this to skip writes).
//
// Refresh never mutates the input slice. The returned slice is a fresh copy
// of states with possibly-updated Outputs maps; on any Read or
// ResourceDriver failure, Refresh returns (nil, err) and discards partial
// progress so callers don't half-persist a refresh.
//
// Aliasing: for entries whose live Outputs match the persisted Outputs,
// out[i].Outputs is the same map as states[i].Outputs (unchanged maps are
// not cloned). Callers must not mutate Outputs maps in the returned slice
// in place.
func Refresh(ctx context.Context, p interfaces.IaCProvider, states []interfaces.ResourceState, opts Options) ([]interfaces.ResourceState, error) {
	if opts.Concurrency < 1 {
		opts.Concurrency = defaultConcurrency
	}
	out := make([]interfaces.ResourceState, len(states))
	copy(out, states)

	sem := make(chan struct{}, opts.Concurrency)
	errs := make([]error, len(states))
	var wg sync.WaitGroup

	for i := range states {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			errs[i] = refreshOne(ctx, p, &out[i], states[i])
		})
	}
	wg.Wait()

	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}

// refreshOne performs a single resource Read and writes the live Outputs
// into dst when they differ from src.Outputs. It returns nil on success or
// the error otherwise.
//
// Ghost handling: if the provider reports ErrResourceNotFound, the resource
// exists in local state but cloud has no record of it (deleted out-of-band).
// refreshOne skips the ghost silently — it preserves dst.Outputs == src.Outputs
// and returns nil so the batch continues. Ghost-prune (wfctl infra apply
// --refresh) remains the canonical mechanism for removing stale state entries.
// This separates "refresh outputs of live resources" from "remove state entries
// for gone resources" — they are orthogonal concerns.
func refreshOne(ctx context.Context, p interfaces.IaCProvider, dst *interfaces.ResourceState, src interfaces.ResourceState) error {
	d, err := p.ResourceDriver(src.Type)
	if err != nil {
		return fmt.Errorf("could not resolve driver for %q (%s): %w", src.Name, src.Type, err)
	}
	ref := interfaces.ResourceRef{Name: src.Name, Type: src.Type, ProviderID: src.ProviderID}
	live, err := d.Read(ctx, ref)
	if err != nil {
		if errors.Is(err, interfaces.ErrResourceNotFound) {
			// Ghost: cloud reports the resource does not exist. Explicitly keep
			// dst.Outputs aligned with src so refreshOne is self-contained and
			// does not rely on the caller having pre-copied src into dst.
			// The caller's --refresh phase (or operator) handles ghost-prune
			// separately; refresh-outputs is non-mutating for ghosts.
			dst.Outputs = src.Outputs
			return nil
		}
		return fmt.Errorf("could not refresh %q: %w", src.Name, err)
	}
	if !reflect.DeepEqual(live.Outputs, src.Outputs) {
		dst.Outputs = cloneMap(live.Outputs)
	}
	return nil
}

// cloneMap returns an independent shallow copy of m. Callers receive a map
// they can mutate without aliasing the live driver output.
func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	c := make(map[string]any, len(m))
	maps.Copy(c, m)
	return c
}
