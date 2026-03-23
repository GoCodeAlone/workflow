package wftest

import (
	"context"
	"fmt"
	"sync"
)

// TriggerAdapter allows external plugins to add test injection support for
// custom trigger types. Register an adapter with RegisterTriggerAdapter, then
// inject events via Harness.InjectTrigger.
type TriggerAdapter interface {
	// Name returns the unique adapter identifier (e.g. "websocket", "grpc").
	Name() string
	// Inject simulates a trigger firing and returns the pipeline result.
	Inject(ctx context.Context, h *Harness, event string, data map[string]any) (*Result, error)
}

var (
	adapterMu sync.RWMutex
	adapters  = map[string]TriggerAdapter{}
)

// RegisterTriggerAdapter registers a TriggerAdapter by name. Call this from
// plugin init() or TestMain to make the adapter available to all harnesses.
func RegisterTriggerAdapter(adapter TriggerAdapter) {
	adapterMu.Lock()
	defer adapterMu.Unlock()
	adapters[adapter.Name()] = adapter
}

// InjectTrigger fires the named trigger adapter with the given event and data,
// returning the pipeline result. Fails the test if no adapter is registered
// with that name.
func (h *Harness) InjectTrigger(name, event string, data map[string]any) *Result {
	h.t.Helper()
	adapterMu.RLock()
	adapter, ok := adapters[name]
	adapterMu.RUnlock()
	if !ok {
		h.t.Fatalf("wftest: no TriggerAdapter registered with name %q; call wftest.RegisterTriggerAdapter first", name)
		return &Result{}
	}
	result, err := adapter.Inject(h.t.Context(), h, event, data)
	if err != nil {
		return &Result{Error: err}
	}
	return result
}

// UnregisterTriggerAdapter removes a previously registered adapter. Useful in
// test cleanup to avoid cross-test pollution.
func UnregisterTriggerAdapter(name string) {
	adapterMu.Lock()
	defer adapterMu.Unlock()
	delete(adapters, name)
}

// TriggerAdapterFunc adapts a plain function to TriggerAdapter.
type TriggerAdapterFunc struct {
	AdapterName string
	Fn          func(ctx context.Context, h *Harness, event string, data map[string]any) (*Result, error)
}

// Name returns the adapter name.
func (f *TriggerAdapterFunc) Name() string { return f.AdapterName }

// Inject calls the underlying function.
func (f *TriggerAdapterFunc) Inject(ctx context.Context, h *Harness, event string, data map[string]any) (*Result, error) {
	if f.Fn == nil {
		return &Result{Error: fmt.Errorf("TriggerAdapterFunc %q has no Fn set", f.AdapterName)}, nil
	}
	return f.Fn(ctx, h, event, data)
}
