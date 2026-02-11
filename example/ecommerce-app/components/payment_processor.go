//go:build ignore

package component

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// Name returns the component name.
func Name() string {
	return "payment-processor"
}

// Init initializes the component with service references.
func Init(services map[string]interface{}) error {
	return nil
}

// Start begins the component.
func Start(ctx context.Context) error {
	return nil
}

// Stop halts the component.
func Stop(ctx context.Context) error {
	return nil
}

// Execute processes a payment for an order.
// Params should contain "amount" (float), "currency" (string), "order_id" (string).
// Returns transaction result or triggers retry on transient failure.
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Simulate processing delay (50-150ms)
	delay := time.Duration(50+r.Intn(100)) * time.Millisecond
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	roll := r.Float64()

	// 85% approve
	if roll < 0.85 {
		txnID := fmt.Sprintf("txn-%d", r.Int63())
		return map[string]interface{}{
			"transaction_id": txnID,
			"status":         "approved",
			"last4":          "4242",
		}, nil
	}

	// 10% decline (permanent business failure — not a Go error)
	if roll < 0.95 {
		return map[string]interface{}{
			"status": "declined",
			"reason": "insufficient_funds",
		}, nil
	}

	// 5% transient error (Go error triggers retry)
	return nil, fmt.Errorf("payment gateway timeout")
}
