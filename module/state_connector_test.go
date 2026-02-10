package module

import (
	"context"
	"testing"
)

func TestNewStateMachineStateConnector(t *testing.T) {
	c := NewStateMachineStateConnector("my-connector")
	if c.Name() != "my-connector" {
		t.Errorf("expected name 'my-connector', got %q", c.Name())
	}
}

func TestNewStateMachineStateConnector_DefaultName(t *testing.T) {
	c := NewStateMachineStateConnector("")
	if c.Name() != StateMachineStateConnectorName {
		t.Errorf("expected default name %q, got %q", StateMachineStateConnectorName, c.Name())
	}
}

func TestStateMachineStateConnector_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	c := NewStateMachineStateConnector("connector")
	if err := c.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestStateMachineStateConnector_Configure(t *testing.T) {
	c := NewStateMachineStateConnector("connector")
	mappings := []ResourceStateMapping{
		{ResourceType: "orders", StateMachine: "order-flow", InstanceIDKey: "orderID"},
	}
	if err := c.Configure(mappings); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	if len(c.mappings) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(c.mappings))
	}
	if c.mappings[0].ResourceType != "orders" {
		t.Errorf("expected ResourceType 'orders', got %q", c.mappings[0].ResourceType)
	}
}

func TestStateMachineStateConnector_RegisterMapping(t *testing.T) {
	c := NewStateMachineStateConnector("connector")
	c.RegisterMapping("users", "user-workflow", "userID")

	if len(c.mappings) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(c.mappings))
	}
	if c.mappings[0].StateMachine != "user-workflow" {
		t.Errorf("expected StateMachine 'user-workflow', got %q", c.mappings[0].StateMachine)
	}
}

func TestStateMachineStateConnector_ProvidesServices(t *testing.T) {
	c := NewStateMachineStateConnector("my-conn")
	svcs := c.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "my-conn" {
		t.Errorf("expected service name 'my-conn', got %q", svcs[0].Name)
	}
}

func TestStateMachineStateConnector_RequiresServices(t *testing.T) {
	c := NewStateMachineStateConnector("connector")
	deps := c.RequiresServices()
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(deps))
	}
	if deps[0].Name != StateTrackerName {
		t.Errorf("expected dependency %q, got %q", StateTrackerName, deps[0].Name)
	}
	if !deps[0].Required {
		t.Error("expected required dependency")
	}
}

func TestStateMachineStateConnector_GetEngineForResourceType(t *testing.T) {
	c := NewStateMachineStateConnector("connector")
	c.RegisterMapping("orders", "order-flow", "orderID")
	c.RegisterMapping("users", "user-flow", "userID")

	name, found := c.GetEngineForResourceType("orders")
	if !found {
		t.Fatal("expected to find engine for orders")
	}
	if name != "order-flow" {
		t.Errorf("expected 'order-flow', got %q", name)
	}

	_, found = c.GetEngineForResourceType("missing")
	if found {
		t.Error("expected not to find engine for missing type")
	}
}

func TestStateMachineStateConnector_FindStateMachineByName(t *testing.T) {
	c := NewStateMachineStateConnector("connector")

	engine1 := NewStateMachineEngine("ns.order-engine")
	engine2 := NewStateMachineEngine("user-engine")
	c.stateMachines["ns.order-engine"] = engine1
	c.stateMachines["user-engine"] = engine2

	// Exact match
	found, ok := c.findStateMachineByName("user-engine")
	if !ok {
		t.Fatal("expected exact match")
	}
	if found != engine2 {
		t.Error("expected engine2")
	}

	// Suffix match
	found, ok = c.findStateMachineByName("order-engine")
	if !ok {
		t.Fatal("expected suffix match")
	}
	if found != engine1 {
		t.Error("expected engine1")
	}

	// No match
	_, ok = c.findStateMachineByName("nonexistent")
	if ok {
		t.Error("expected no match")
	}
}

func TestStateMachineStateConnector_UpdateResourceState_NoMapping(t *testing.T) {
	c := NewStateMachineStateConnector("connector")
	c.stateTracker = NewStateTracker("tracker")

	err := c.UpdateResourceState("orders", "123")
	if err == nil {
		t.Fatal("expected error for missing mapping")
	}
}

func TestStateMachineStateConnector_UpdateResourceState_NoEngine(t *testing.T) {
	c := NewStateMachineStateConnector("connector")
	c.stateTracker = NewStateTracker("tracker")
	c.RegisterMapping("orders", "order-flow", "orderID")

	err := c.UpdateResourceState("orders", "123")
	if err == nil {
		t.Fatal("expected error for missing engine")
	}
}

func TestStateMachineStateConnector_UpdateResourceState_Success(t *testing.T) {
	c := NewStateMachineStateConnector("connector")
	c.stateTracker = NewStateTracker("tracker")
	c.RegisterMapping("orders", "order-flow", "orderID")

	engine := NewStateMachineEngine("order-flow")
	def := &StateMachineDefinition{
		Name:         "order-flow",
		InitialState: "new",
		States: map[string]*State{
			"new":        {Name: "new"},
			"processing": {Name: "processing"},
		},
		Transitions: map[string]*Transition{
			"process": {Name: "process", FromState: "new", ToState: "processing"},
		},
	}
	_ = engine.RegisterDefinition(def)
	_, _ = engine.CreateWorkflow("order-flow", "order-1", map[string]interface{}{"item": "widget"})
	c.stateMachines["order-flow"] = engine

	err := c.UpdateResourceState("orders", "order-1")
	if err != nil {
		t.Fatalf("UpdateResourceState failed: %v", err)
	}

	info, exists := c.stateTracker.GetState("orders", "order-1")
	if !exists {
		t.Fatal("expected state to exist in tracker")
	}
	if info.CurrentState != "new" {
		t.Errorf("expected state 'new', got %q", info.CurrentState)
	}
}

func TestStateMachineStateConnector_GetResourceState_FromTracker(t *testing.T) {
	c := NewStateMachineStateConnector("connector")
	c.stateTracker = NewStateTracker("tracker")

	// Manually set state in tracker
	c.stateTracker.SetState("orders", "order-1", "shipped", map[string]interface{}{"carrier": "ups"})

	state, data, err := c.GetResourceState("orders", "order-1")
	if err != nil {
		t.Fatalf("GetResourceState failed: %v", err)
	}
	if state != "shipped" {
		t.Errorf("expected state 'shipped', got %q", state)
	}
	if data["carrier"] != "ups" {
		t.Errorf("expected carrier=ups, got %v", data["carrier"])
	}
}

func TestStateMachineStateConnector_GetResourceState_FallbackToEngine(t *testing.T) {
	c := NewStateMachineStateConnector("connector")
	c.stateTracker = NewStateTracker("tracker")
	c.RegisterMapping("orders", "order-flow", "orderID")

	engine := NewStateMachineEngine("order-flow")
	def := &StateMachineDefinition{
		Name:         "order-flow",
		InitialState: "new",
		States:       map[string]*State{"new": {Name: "new"}},
		Transitions:  map[string]*Transition{},
	}
	_ = engine.RegisterDefinition(def)
	_, _ = engine.CreateWorkflow("order-flow", "order-1", nil)
	c.stateMachines["order-flow"] = engine

	state, _, err := c.GetResourceState("orders", "order-1")
	if err != nil {
		t.Fatalf("GetResourceState failed: %v", err)
	}
	if state != "new" {
		t.Errorf("expected state 'new', got %q", state)
	}
}

func TestStateMachineStateConnector_GetResourceState_NotFound(t *testing.T) {
	c := NewStateMachineStateConnector("connector")
	c.stateTracker = NewStateTracker("tracker")

	_, _, err := c.GetResourceState("orders", "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing resource state")
	}
}

func TestStateMachineStateConnector_Stop(t *testing.T) {
	c := NewStateMachineStateConnector("connector")
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}
