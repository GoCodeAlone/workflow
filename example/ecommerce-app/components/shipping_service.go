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
	return "shipping-service"
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

// Execute generates a shipping label for an order.
// Params should contain "order_id" (string) and "shipping" (address object).
// Returns tracking information on success, or a Go error on transient failure.
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Simulate label generation delay (200-500ms)
	delay := time.Duration(200+r.Intn(300)) * time.Millisecond
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// 95% success rate
	if r.Float64() < 0.95 {
		trackingNum := fmt.Sprintf("TRK-%d", r.Int63())
		estimated := time.Now().Add(5 * 24 * time.Hour).Format("2006-01-02")
		return map[string]interface{}{
			"tracking_number":    trackingNum,
			"carrier":            "USPS",
			"estimated_delivery": estimated,
		}, nil
	}

	// Transient error triggers retry
	return nil, fmt.Errorf("shipping provider unavailable")
}
