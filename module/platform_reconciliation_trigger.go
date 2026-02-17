package module

import (
	"context"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/platform"
)

const (
	// ReconciliationTriggerName is the standard name for reconciliation triggers.
	ReconciliationTriggerName = "trigger.reconciliation"
)

// ReconciliationTrigger implements the Trigger interface for periodic
// drift detection. It launches a platform.Reconciler in a background
// goroutine that compares stored state with live provider state.
type ReconciliationTrigger struct {
	name     string
	interval time.Duration
	provider platform.Provider
	store    platform.StateStore
	ctxPath  string
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewReconciliationTrigger creates a new reconciliation trigger.
func NewReconciliationTrigger() *ReconciliationTrigger {
	return &ReconciliationTrigger{
		name: ReconciliationTriggerName,
	}
}

// Name returns the trigger name.
func (t *ReconciliationTrigger) Name() string {
	return t.name
}

// Dependencies returns nil; the trigger discovers services at configure time.
func (t *ReconciliationTrigger) Dependencies() []string {
	return nil
}

// Init registers the trigger as a service.
func (t *ReconciliationTrigger) Init(app modular.Application) error {
	return app.RegisterService(t.name, t)
}

// Configure sets up the trigger from its YAML configuration.
// Expected config keys:
//   - interval: duration string (e.g., "5m", "30s")
//   - context_path: the platform context path to reconcile
//   - provider_service: optional service name of the provider to use
func (t *ReconciliationTrigger) Configure(app modular.Application, triggerConfig any) error {
	config, ok := triggerConfig.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid reconciliation trigger configuration format")
	}

	// Parse interval
	intervalStr, _ := config["interval"].(string)
	if intervalStr == "" {
		intervalStr = "5m" // default to 5 minutes
	}
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		return fmt.Errorf("invalid reconciliation interval %q: %w", intervalStr, err)
	}
	t.interval = interval

	// Parse context path
	t.ctxPath, _ = config["context_path"].(string)
	if t.ctxPath == "" {
		return fmt.Errorf("context_path is required for reconciliation trigger")
	}

	// Find the provider service
	providerServiceName, _ := config["provider_service"].(string)
	if providerServiceName == "" {
		providerServiceName = "platform.provider"
	}

	var providerSvc any
	if err := app.GetService(providerServiceName, &providerSvc); err != nil {
		// Try scanning all services for a provider
		for _, svc := range app.SvcRegistry() {
			if p, ok := svc.(platform.Provider); ok {
				t.provider = p
				break
			}
		}
		if t.provider == nil {
			return fmt.Errorf("platform provider service %q not found: %w", providerServiceName, err)
		}
	} else if p, ok := providerSvc.(platform.Provider); ok {
		t.provider = p
	} else {
		return fmt.Errorf("service %q does not implement platform.Provider", providerServiceName)
	}

	// Find the state store service
	stateStoreServiceName, _ := config["state_store_service"].(string)
	if stateStoreServiceName == "" {
		stateStoreServiceName = "platform.state_store"
	}

	var storeSvc any
	if err := app.GetService(stateStoreServiceName, &storeSvc); err != nil {
		// Try scanning for a state store
		for _, svc := range app.SvcRegistry() {
			if s, ok := svc.(platform.StateStore); ok {
				t.store = s
				break
			}
		}
		if t.store == nil {
			// Fall back to the provider's state store
			t.store = t.provider.StateStore()
		}
		if t.store == nil {
			return fmt.Errorf("state store service not found")
		}
	} else if s, ok := storeSvc.(platform.StateStore); ok {
		t.store = s
	} else {
		return fmt.Errorf("service %q does not implement platform.StateStore", stateStoreServiceName)
	}

	return nil
}

// Start launches the reconciliation loop in a background goroutine.
func (t *ReconciliationTrigger) Start(ctx context.Context) error {
	if t.provider == nil || t.store == nil {
		// No platform config — reconciliation is a no-op.
		return nil
	}

	childCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	t.done = make(chan struct{})

	reconciler := platform.NewReconciler(t.provider, t.store, t.ctxPath, t.interval)

	go func() {
		defer close(t.done)
		_ = reconciler.Start(childCtx)
	}()

	return nil
}

// Stop cancels the reconciliation loop and waits for it to finish.
func (t *ReconciliationTrigger) Stop(_ context.Context) error {
	if t.cancel != nil {
		t.cancel()
	}
	if t.done != nil {
		<-t.done
	}
	return nil
}
