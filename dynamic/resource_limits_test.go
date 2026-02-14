package dynamic

import (
	"context"
	"testing"
	"time"
)

func TestDefaultResourceLimits(t *testing.T) {
	limits := DefaultResourceLimits()
	if limits.MaxExecutionTime != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", limits.MaxExecutionTime)
	}
	if limits.MaxOutputSize != 1000 {
		t.Errorf("expected 1000 max output, got %d", limits.MaxOutputSize)
	}
}

func TestExecuteWithLimits_Timeout(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("timeout-test", pool)

	// Load a component that sleeps longer than the timeout
	source := `package component

import (
	"context"
	"time"
)

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Second):
		return map[string]interface{}{"done": true}, nil
	}
}
`
	if err := comp.LoadFromSource(source); err != nil {
		t.Fatalf("LoadFromSource failed: %v", err)
	}

	limits := ResourceLimits{
		MaxExecutionTime: 100 * time.Millisecond,
		MaxOutputSize:    100,
	}

	ctx := context.Background()
	_, err := ExecuteWithLimits(ctx, comp, nil, limits)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestExecuteWithLimits_Success(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("success-test", pool)

	source := `package component

import "context"

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"result": "ok"}, nil
}
`
	if err := comp.LoadFromSource(source); err != nil {
		t.Fatalf("LoadFromSource failed: %v", err)
	}

	limits := ResourceLimits{
		MaxExecutionTime: 5 * time.Second,
		MaxOutputSize:    100,
	}

	ctx := context.Background()
	result, err := ExecuteWithLimits(ctx, comp, nil, limits)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["result"] != "ok" {
		t.Errorf("expected result 'ok', got %v", result["result"])
	}
}

func TestExecuteWithLimits_OutputSizeExceeded(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("output-test", pool)

	// Component returns a large map
	source := `package component

import (
	"context"
	"fmt"
)

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	for i := 0; i < 50; i++ {
		result[fmt.Sprintf("key_%d", i)] = i
	}
	return result, nil
}
`
	if err := comp.LoadFromSource(source); err != nil {
		t.Fatalf("LoadFromSource failed: %v", err)
	}

	limits := ResourceLimits{
		MaxExecutionTime: 5 * time.Second,
		MaxOutputSize:    10, // Only allow 10 keys
	}

	ctx := context.Background()
	_, err := ExecuteWithLimits(ctx, comp, nil, limits)
	if err == nil {
		t.Fatal("expected output size error, got nil")
	}
}

func TestExecuteWithLimits_NoLimits(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("nolimit-test", pool)

	source := `package component

import "context"

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"ok": true}, nil
}
`
	if err := comp.LoadFromSource(source); err != nil {
		t.Fatalf("LoadFromSource failed: %v", err)
	}

	// Zero limits = no enforcement
	limits := ResourceLimits{}

	ctx := context.Background()
	result, err := ExecuteWithLimits(ctx, comp, nil, limits)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["ok"] != true {
		t.Errorf("expected ok=true, got %v", result["ok"])
	}
}

func TestParseResourceLimitsFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		cfg      map[string]any
		wantTime time.Duration
		wantSize int
	}{
		{
			name:     "defaults",
			cfg:      map[string]any{},
			wantTime: 30 * time.Second,
			wantSize: 1000,
		},
		{
			name:     "duration string",
			cfg:      map[string]any{"maxExecutionTime": "10s"},
			wantTime: 10 * time.Second,
			wantSize: 1000,
		},
		{
			name:     "seconds float64",
			cfg:      map[string]any{"maxExecutionTimeSeconds": float64(15)},
			wantTime: 15 * time.Second,
			wantSize: 1000,
		},
		{
			name:     "seconds int",
			cfg:      map[string]any{"maxExecutionTimeSeconds": 20},
			wantTime: 20 * time.Second,
			wantSize: 1000,
		},
		{
			name:     "output size float64",
			cfg:      map[string]any{"maxOutputSize": float64(500)},
			wantTime: 30 * time.Second,
			wantSize: 500,
		},
		{
			name:     "output size int",
			cfg:      map[string]any{"maxOutputSize": 200},
			wantTime: 30 * time.Second,
			wantSize: 200,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			limits := ParseResourceLimitsFromConfig(tc.cfg)
			if limits.MaxExecutionTime != tc.wantTime {
				t.Errorf("MaxExecutionTime: got %v, want %v", limits.MaxExecutionTime, tc.wantTime)
			}
			if limits.MaxOutputSize != tc.wantSize {
				t.Errorf("MaxOutputSize: got %d, want %d", limits.MaxOutputSize, tc.wantSize)
			}
		})
	}
}
