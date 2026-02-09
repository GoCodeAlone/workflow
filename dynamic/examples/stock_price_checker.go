//go:build ignore

// Package component is a dynamically loaded workflow component.
// When loaded by the Yaegi interpreter, the "component" package
// prefix is used to locate exported functions.
//
// This example checks a stock price (using mock data).
package component

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// Name returns the component name.
func Name() string {
	return "stock-price-checker"
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

// Execute runs the stock price check.
// Params should contain "symbol" (string).
// Returns a map with "symbol", "price", "currency", and "timestamp".
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	symbol, _ := params["symbol"].(string)
	if symbol == "" {
		return nil, fmt.Errorf("missing required parameter: symbol")
	}

	// Mock price generation
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	price := 50.0 + r.Float64()*200.0

	return map[string]interface{}{
		"symbol":    strings.ToUpper(symbol),
		"price":     fmt.Sprintf("%.2f", price),
		"currency":  "USD",
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil
}
