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
	return "inventory-checker"
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

// Execute checks inventory availability for order items.
// Params should contain "items" (array of order items).
// Returns availability status with reservation details.
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Simulate inventory lookup delay (100-300ms)
	delay := time.Duration(100+r.Intn(200)) * time.Millisecond
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	items, _ := params["items"].([]interface{})
	if len(items) == 0 {
		// Build items from productId/quantity if no explicit items array
		if productId, ok := params["productId"].(string); ok && productId != "" {
			qty := 1
			if q, ok := params["quantity"].(float64); ok {
				qty = int(q)
			}
			items = []interface{}{
				map[string]interface{}{
					"productId": productId,
					"quantity":  qty,
				},
			}
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("missing required parameter: items")
	}

	// 90% success rate
	if r.Float64() < 0.9 {
		checkedItems := make([]interface{}, len(items))
		for i, item := range items {
			checkedItems[i] = fmt.Sprintf("%v", item)
		}
		return map[string]interface{}{
			"available":     true,
			"checked_items": checkedItems,
			"reserved_until": time.Now().Add(15 * time.Minute).Format(time.RFC3339),
		}, nil
	}

	// Inventory failure — business result, not a Go error
	firstItem := fmt.Sprintf("%v", items[0])
	return map[string]interface{}{
		"available":   false,
		"reason":      "out_of_stock",
		"failed_item": firstItem,
	}, nil
}
