package dynamic

import (
	"context"
	"fmt"
	"time"
)

// ResourceLimits configures resource constraints for dynamic component execution.
type ResourceLimits struct {
	// MaxExecutionTime is the maximum duration a single Execute call may run.
	// Zero means no timeout (not recommended for production).
	MaxExecutionTime time.Duration

	// MaxOutputSize is the maximum number of keys allowed in the Execute output map.
	// Zero means unlimited.
	MaxOutputSize int
}

// DefaultResourceLimits returns sensible defaults for production use.
func DefaultResourceLimits() ResourceLimits {
	return ResourceLimits{
		MaxExecutionTime: 30 * time.Second,
		MaxOutputSize:    1000,
	}
}

// ExecuteWithLimits wraps a DynamicComponent's Execute call with resource
// enforcement: a context-based timeout and output size validation.
func ExecuteWithLimits(
	ctx context.Context,
	comp *DynamicComponent,
	params map[string]any,
	limits ResourceLimits,
) (map[string]any, error) {
	if limits.MaxExecutionTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limits.MaxExecutionTime)
		defer cancel()
	}

	type result struct {
		data map[string]any
		err  error
	}

	ch := make(chan result, 1)
	go func() {
		data, err := comp.Execute(ctx, params)
		ch <- result{data, err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("dynamic component %q execution timed out after %v", comp.Name(), limits.MaxExecutionTime)
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		if limits.MaxOutputSize > 0 && len(res.data) > limits.MaxOutputSize {
			return nil, fmt.Errorf("dynamic component %q output size %d exceeds limit %d", comp.Name(), len(res.data), limits.MaxOutputSize)
		}
		return res.data, nil
	}
}

// ParseResourceLimitsFromConfig extracts ResourceLimits from a YAML config map.
func ParseResourceLimitsFromConfig(cfg map[string]any) ResourceLimits {
	limits := DefaultResourceLimits()

	if v, ok := cfg["maxExecutionTime"].(string); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			limits.MaxExecutionTime = d
		}
	}
	if v, ok := cfg["maxExecutionTimeSeconds"].(float64); ok {
		limits.MaxExecutionTime = time.Duration(v) * time.Second
	}
	if v, ok := cfg["maxExecutionTimeSeconds"].(int); ok {
		limits.MaxExecutionTime = time.Duration(v) * time.Second
	}

	if v, ok := cfg["maxOutputSize"].(float64); ok {
		limits.MaxOutputSize = int(v)
	}
	if v, ok := cfg["maxOutputSize"].(int); ok {
		limits.MaxOutputSize = int(v)
	}

	return limits
}
