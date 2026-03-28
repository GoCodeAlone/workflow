package module

import (
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// TestSSETracerConcurrent verifies no deadlock or data race with many wildcard
// subscribers and concurrent Publish/Subscribe/Unsubscribe calls.
func TestSSETracerConcurrent(t *testing.T) {
	tracer := NewSSETracer(slog.Default())

	const wildcardSubs = 50
	const publishers = 10
	const eventsEach = 20

	// Register wildcard subscribers upfront.
	unsubs := make([]func(), wildcardSubs)
	for i := range wildcardSubs {
		_, unsub := tracer.Subscribe("*")
		unsubs[i] = unsub
	}
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()

	var wg sync.WaitGroup

	// Concurrent publishers.
	for p := range publishers {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			for e := range eventsEach {
				tracer.Publish(fmt.Sprintf("exec-%d", p), SSEEvent{
					ID:    fmt.Sprintf("p%d-e%d", p, e),
					Event: "step.started",
					Data:  "{}",
				})
			}
		}(p)
	}

	// Concurrent Subscribe/Unsubscribe during publishing.
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("dyn-%d", i)
			_, unsub := tracer.Subscribe(id)
			time.Sleep(time.Millisecond)
			unsub()
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// No deadlock.
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock detected: concurrent Publish/Subscribe/Unsubscribe did not complete")
	}
}
