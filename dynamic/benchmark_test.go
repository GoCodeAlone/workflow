package dynamic

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// benchComponentSource is a simple component used across benchmarks.
const benchComponentSource = `package component

import (
	"context"
	"fmt"
)

func Name() string { return "bench-component" }

func Init(services map[string]interface{}) error { return nil }

func Start(ctx context.Context) error { return nil }

func Stop(ctx context.Context) error { return nil }

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, _ := params["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("missing name")
	}
	return map[string]interface{}{
		"greeting": "hello " + name,
	}, nil
}
`

func BenchmarkInterpreterCreation(b *testing.B) {
	pool := NewInterpreterPool()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		interp, err := pool.NewInterpreter()
		if err != nil {
			b.Fatalf("NewInterpreter failed: %v", err)
		}
		_ = interp
	}
}

func BenchmarkComponentLoad(b *testing.B) {
	pool := NewInterpreterPool()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		comp := NewDynamicComponent("bench", pool)
		if err := comp.LoadFromSource(benchComponentSource); err != nil {
			b.Fatalf("LoadFromSource failed: %v", err)
		}
	}
}

func BenchmarkComponentExecute(b *testing.B) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("bench", pool)
	if err := comp.LoadFromSource(benchComponentSource); err != nil {
		b.Fatalf("LoadFromSource failed: %v", err)
	}
	if err := comp.Init(nil); err != nil {
		b.Fatalf("Init failed: %v", err)
	}
	if err := comp.Start(context.Background()); err != nil {
		b.Fatalf("Start failed: %v", err)
	}

	ctx := context.Background()
	params := map[string]any{"name": "world"}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		result, err := comp.Execute(ctx, params)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if result["greeting"] != "hello world" {
			b.Fatalf("unexpected result: %v", result)
		}
	}
}

func BenchmarkPoolContention(b *testing.B) {
	poolSizes := []int{1, 2, 4, 8, 16}

	for _, numWorkers := range poolSizes {
		b.Run(fmt.Sprintf("workers-%d", numWorkers), func(b *testing.B) {
			pool := NewInterpreterPool()

			// Pre-load components, one per worker
			components := make([]*DynamicComponent, numWorkers)
			for i := range numWorkers {
				comp := NewDynamicComponent(fmt.Sprintf("bench-%d", i), pool)
				if err := comp.LoadFromSource(benchComponentSource); err != nil {
					b.Fatalf("LoadFromSource failed: %v", err)
				}
				if err := comp.Init(nil); err != nil {
					b.Fatalf("Init failed: %v", err)
				}
				if err := comp.Start(context.Background()); err != nil {
					b.Fatalf("Start failed: %v", err)
				}
				components[i] = comp
			}

			ctx := context.Background()
			params := map[string]any{"name": "world"}

			b.ReportAllocs()
			b.ResetTimer()

			b.RunParallel(func(pb *testing.PB) {
				// Each goroutine picks a component based on its index
				var mu sync.Mutex
				var idx int
				mu.Lock()
				localIdx := idx % numWorkers
				mu.Unlock()

				comp := components[localIdx]
				for pb.Next() {
					_, err := comp.Execute(ctx, params)
					if err != nil {
						b.Errorf("Execute failed: %v", err)
						return
					}
				}
			})
		})
	}
}

func BenchmarkComponentLifecycle(b *testing.B) {
	pool := NewInterpreterPool()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		// Create
		comp := NewDynamicComponent("lifecycle", pool)

		// Load
		if err := comp.LoadFromSource(benchComponentSource); err != nil {
			b.Fatalf("LoadFromSource failed: %v", err)
		}

		// Init
		if err := comp.Init(nil); err != nil {
			b.Fatalf("Init failed: %v", err)
		}

		// Start
		ctx := context.Background()
		if err := comp.Start(ctx); err != nil {
			b.Fatalf("Start failed: %v", err)
		}

		// Execute
		result, err := comp.Execute(ctx, map[string]any{"name": "bench"})
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if result["greeting"] != "hello bench" {
			b.Fatalf("unexpected result: %v", result)
		}

		// Stop
		if err := comp.Stop(ctx); err != nil {
			b.Fatalf("Stop failed: %v", err)
		}
	}
}

func BenchmarkSourceValidation(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if err := ValidateSource(benchComponentSource); err != nil {
			b.Fatalf("ValidateSource failed: %v", err)
		}
	}
}

func BenchmarkRegistryConcurrent(b *testing.B) {
	pool := NewInterpreterPool()
	registry := NewComponentRegistry()

	// Pre-register some components
	for i := range 10 {
		comp := NewDynamicComponent(fmt.Sprintf("comp-%d", i), pool)
		if err := registry.Register(fmt.Sprintf("comp-%d", i), comp); err != nil {
			b.Fatalf("Register failed: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			id := fmt.Sprintf("comp-%d", i%10)
			_, _ = registry.Get(id)
			_ = registry.List()
			i++
		}
	})
}

func BenchmarkLoaderLoadFromString(b *testing.B) {
	pool := NewInterpreterPool()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reg := NewComponentRegistry()
		loader := NewLoader(pool, reg)
		_, err := loader.LoadFromString(fmt.Sprintf("bench-%d", i), benchComponentSource)
		if err != nil {
			b.Fatalf("LoadFromString failed: %v", err)
		}
	}
}
