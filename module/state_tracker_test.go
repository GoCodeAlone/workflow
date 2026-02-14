package module

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestNewStateTracker(t *testing.T) {
	st := NewStateTracker("my-tracker")
	if st.Name() != "my-tracker" {
		t.Errorf("expected name 'my-tracker', got %q", st.Name())
	}
}

func TestNewStateTracker_DefaultName(t *testing.T) {
	st := NewStateTracker("")
	if st.Name() != StateTrackerName {
		t.Errorf("expected default name %q, got %q", StateTrackerName, st.Name())
	}
}

func TestStateTracker_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	st := NewStateTracker("tracker")
	if err := st.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestStateTracker_StartStop(t *testing.T) {
	st := NewStateTracker("tracker")
	ctx := context.Background()
	if err := st.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := st.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestStateTracker_GetState_NotFound(t *testing.T) {
	st := NewStateTracker("tracker")
	_, exists := st.GetState("orders", "123")
	if exists {
		t.Error("expected state not to exist")
	}
}

func TestStateTracker_SetAndGetState(t *testing.T) {
	st := NewStateTracker("tracker")
	data := map[string]any{"amount": 100}
	st.SetState("orders", "order-1", "pending", data)

	info, exists := st.GetState("orders", "order-1")
	if !exists {
		t.Fatal("expected state to exist")
	}
	if info.CurrentState != "pending" {
		t.Errorf("expected state 'pending', got %q", info.CurrentState)
	}
	if info.ID != "order-1" {
		t.Errorf("expected ID 'order-1', got %q", info.ID)
	}
	if info.ResourceType != "orders" {
		t.Errorf("expected ResourceType 'orders', got %q", info.ResourceType)
	}
	if info.PreviousState != "" {
		t.Errorf("expected empty previous state, got %q", info.PreviousState)
	}
	if info.Data["amount"] != 100 {
		t.Errorf("expected data amount=100, got %v", info.Data["amount"])
	}
}

func TestStateTracker_SetState_UpdatesPreviousState(t *testing.T) {
	st := NewStateTracker("tracker")
	st.SetState("orders", "order-1", "pending", nil)
	st.SetState("orders", "order-1", "processing", nil)

	info, _ := st.GetState("orders", "order-1")
	if info.CurrentState != "processing" {
		t.Errorf("expected state 'processing', got %q", info.CurrentState)
	}
	if info.PreviousState != "pending" {
		t.Errorf("expected previous state 'pending', got %q", info.PreviousState)
	}
}

func TestStateTracker_SetState_LastUpdate(t *testing.T) {
	st := NewStateTracker("tracker")
	before := time.Now()
	st.SetState("orders", "order-1", "new", nil)
	after := time.Now()

	info, _ := st.GetState("orders", "order-1")
	if info.LastUpdate.Before(before) || info.LastUpdate.After(after) {
		t.Errorf("expected LastUpdate between %v and %v, got %v", before, after, info.LastUpdate)
	}
}

func TestStateTracker_AddStateChangeListener(t *testing.T) {
	st := NewStateTracker("tracker")

	var mu sync.Mutex
	var events []string

	st.AddStateChangeListener("orders", func(prev, next, resourceID string, data map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, prev+"->"+next+":"+resourceID)
	})

	st.SetState("orders", "order-1", "pending", nil)
	st.SetState("orders", "order-1", "shipped", nil)

	// Listeners are called in goroutines, give them time
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %v", len(events), events)
	}
	// Events may arrive in any order since listeners run in goroutines
	expected := map[string]bool{
		"->pending:order-1":        false,
		"pending->shipped:order-1": false,
	}
	for _, e := range events {
		if _, ok := expected[e]; ok {
			expected[e] = true
		} else {
			t.Errorf("unexpected event: %q", e)
		}
	}
	for e, found := range expected {
		if !found {
			t.Errorf("missing expected event: %q", e)
		}
	}
}

func TestStateTracker_ListenerNotCalledWhenStateSame(t *testing.T) {
	st := NewStateTracker("tracker")

	var mu sync.Mutex
	callCount := 0

	st.AddStateChangeListener("orders", func(prev, next, resourceID string, data map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		callCount++
	})

	st.SetState("orders", "order-1", "pending", nil)
	st.SetState("orders", "order-1", "pending", nil) // same state, no notification

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if callCount != 1 {
		t.Errorf("expected 1 listener call, got %d", callCount)
	}
}

func TestStateTracker_WildcardListener(t *testing.T) {
	st := NewStateTracker("tracker")

	var mu sync.Mutex
	var events []string

	st.AddStateChangeListener("*", func(prev, next, resourceID string, data map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, resourceID)
	})

	st.SetState("orders", "order-1", "pending", nil)
	st.SetState("users", "user-1", "active", nil)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("expected 2 events from wildcard listener, got %d", len(events))
	}
}

func TestStateTracker_ProvidesServices(t *testing.T) {
	st := NewStateTracker("my-tracker")
	svcs := st.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "my-tracker" {
		t.Errorf("expected service name 'my-tracker', got %q", svcs[0].Name)
	}
	if svcs[0].Instance != st {
		t.Error("expected service instance to be the tracker")
	}
}

func TestStateTracker_RequiresServices(t *testing.T) {
	st := NewStateTracker("tracker")
	deps := st.RequiresServices()
	if deps != nil {
		t.Errorf("expected nil dependencies, got %v", deps)
	}
}

func TestStateTracker_ConcurrentAccess(t *testing.T) {
	st := NewStateTracker("tracker")
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("item-%d", i)
			st.SetState("items", id, "active", nil)
			st.GetState("items", id)
		}(i)
	}

	wg.Wait()
}
