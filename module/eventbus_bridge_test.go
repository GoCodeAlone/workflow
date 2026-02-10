package module

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/eventbus"
)

// setupEventBusForBridge creates a working EventBusModule backed by the in-memory engine.
// It uses a mock modular.Application to register config, init, and start the module.
func setupEventBusForBridge(t *testing.T) *eventbus.EventBusModule {
	t.Helper()

	app := newEventBusBridgeMockApp()
	eb := eventbus.NewModule().(*eventbus.EventBusModule)

	if err := eb.RegisterConfig(app); err != nil {
		t.Fatalf("RegisterConfig: %v", err)
	}
	if err := eb.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := eb.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := eb.Stop(context.Background()); err != nil {
			t.Logf("Stop: %v", err)
		}
	})
	return eb
}

// bridgeMockApp satisfies modular.Application for the eventbus module init.
type bridgeMockApp struct {
	configSections map[string]modular.ConfigProvider
	services       map[string]interface{}
	modules        map[string]modular.Module
	logger         modular.Logger
}

func newEventBusBridgeMockApp() *bridgeMockApp {
	return &bridgeMockApp{
		configSections: make(map[string]modular.ConfigProvider),
		services:       make(map[string]interface{}),
		modules:        make(map[string]modular.Module),
		logger:         &noopLogger{},
	}
}

func (a *bridgeMockApp) RegisterConfigSection(name string, cp modular.ConfigProvider) {
	a.configSections[name] = cp
}
func (a *bridgeMockApp) GetConfigSection(name string) (modular.ConfigProvider, error) {
	return a.configSections[name], nil
}
func (a *bridgeMockApp) ConfigSections() map[string]modular.ConfigProvider {
	return a.configSections
}
func (a *bridgeMockApp) Logger() modular.Logger         { return a.logger }
func (a *bridgeMockApp) SetLogger(l modular.Logger)     { a.logger = l }
func (a *bridgeMockApp) ConfigProvider() modular.ConfigProvider { return nil }
func (a *bridgeMockApp) SvcRegistry() modular.ServiceRegistry {
	return a.services
}
func (a *bridgeMockApp) RegisterModule(m modular.Module) {
	a.modules[m.Name()] = m
}
func (a *bridgeMockApp) RegisterService(name string, svc any) error {
	a.services[name] = svc
	return nil
}
func (a *bridgeMockApp) GetService(name string, target any) error { return nil }
func (a *bridgeMockApp) Init() error                              { return nil }
func (a *bridgeMockApp) Start() error                             { return nil }
func (a *bridgeMockApp) Stop() error                              { return nil }
func (a *bridgeMockApp) Run() error                               { return nil }
func (a *bridgeMockApp) IsVerboseConfig() bool                    { return false }
func (a *bridgeMockApp) SetVerboseConfig(bool)                    {}
func (a *bridgeMockApp) Context() context.Context                 { return context.Background() }
func (a *bridgeMockApp) GetServicesByModule(string) []string      { return nil }
func (a *bridgeMockApp) GetServiceEntry(string) (*modular.ServiceRegistryEntry, bool) {
	return nil, false
}
func (a *bridgeMockApp) GetServicesByInterface(_ reflect.Type) []*modular.ServiceRegistryEntry {
	return nil
}
func (a *bridgeMockApp) GetModule(name string) modular.Module       { return a.modules[name] }
func (a *bridgeMockApp) GetAllModules() map[string]modular.Module   { return a.modules }
func (a *bridgeMockApp) StartTime() time.Time                       { return time.Time{} }
func (a *bridgeMockApp) OnConfigLoaded(func(modular.Application) error) {}

// testMessageHandler is a simple MessageHandler for tests.
type testMessageHandler struct {
	mu       sync.Mutex
	messages [][]byte
	ch       chan []byte
}

func newTestMessageHandler() *testMessageHandler {
	return &testMessageHandler{
		messages: make([][]byte, 0),
		ch:       make(chan []byte, 10),
	}
}

func (h *testMessageHandler) HandleMessage(message []byte) error {
	h.mu.Lock()
	h.messages = append(h.messages, message)
	h.mu.Unlock()
	h.ch <- message
	return nil
}

// --- Tests ---

func TestEventBusBridge_NewAndName(t *testing.T) {
	bridge := NewEventBusBridge("my.bridge")
	if bridge.Name() != "my.bridge" {
		t.Fatalf("expected name %q, got %q", "my.bridge", bridge.Name())
	}
}

func TestEventBusBridge_ProducerConsumer(t *testing.T) {
	bridge := NewEventBusBridge(EventBusBridgeName)
	if bridge.Producer() != bridge {
		t.Fatal("Producer() should return the bridge itself")
	}
	if bridge.Consumer() != bridge {
		t.Fatal("Consumer() should return the bridge itself")
	}
}

func TestEventBusBridge_NilEventBusNoOp(t *testing.T) {
	bridge := NewEventBusBridge(EventBusBridgeName)

	// SendMessage should be a no-op
	if err := bridge.SendMessage("topic", []byte(`{"key":"val"}`)); err != nil {
		t.Fatalf("SendMessage with nil EventBus should be no-op, got: %v", err)
	}

	// Subscribe should be a no-op
	handler := newTestMessageHandler()
	if err := bridge.Subscribe("topic", handler); err != nil {
		t.Fatalf("Subscribe with nil EventBus should be no-op, got: %v", err)
	}

	// Unsubscribe should be a no-op (no subscription exists)
	if err := bridge.Unsubscribe("topic"); err != nil {
		t.Fatalf("Unsubscribe with no subscription should be no-op, got: %v", err)
	}
}

func TestEventBusBridge_StartStop(t *testing.T) {
	bridge := NewEventBusBridge(EventBusBridgeName)
	ctx := context.Background()

	if err := bridge.Start(ctx); err != nil {
		t.Fatalf("Start should be no-op, got: %v", err)
	}
	if err := bridge.Stop(ctx); err != nil {
		t.Fatalf("Stop should succeed, got: %v", err)
	}
}

func TestEventBusBridge_SendMessageThenReceiveViaEventBus(t *testing.T) {
	eb := setupEventBusForBridge(t)
	bridge := NewEventBusBridge(EventBusBridgeName)
	bridge.SetEventBus(eb)

	// Subscribe directly on the EventBus to verify the bridge publishes correctly.
	received := make(chan eventbus.Event, 1)
	sub, err := eb.Subscribe(context.Background(), "test.send", func(_ context.Context, event eventbus.Event) error {
		received <- event
		return nil
	})
	if err != nil {
		t.Fatalf("EventBus Subscribe: %v", err)
	}
	defer func() { _ = sub.Cancel() }()

	payload := map[string]interface{}{"hello": "world"}
	msg, _ := json.Marshal(payload)
	if err := bridge.SendMessage("test.send", msg); err != nil {
		t.Fatalf("bridge.SendMessage: %v", err)
	}

	select {
	case evt := <-received:
		if evt.Topic != "test.send" {
			t.Fatalf("expected topic %q, got %q", "test.send", evt.Topic)
		}
		// The payload went through JSON unmarshal in SendMessage then was published.
		payloadMap, ok := evt.Payload.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map payload, got %T", evt.Payload)
		}
		if payloadMap["hello"] != "world" {
			t.Fatalf("expected hello=world, got %v", payloadMap["hello"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event on EventBus")
	}
}

func TestEventBusBridge_EventBusPublishThenReceiveViaBridge(t *testing.T) {
	eb := setupEventBusForBridge(t)
	bridge := NewEventBusBridge(EventBusBridgeName)
	bridge.SetEventBus(eb)

	handler := newTestMessageHandler()
	if err := bridge.Subscribe("test.receive", handler); err != nil {
		t.Fatalf("bridge.Subscribe: %v", err)
	}

	// Publish via the EventBus directly.
	payload := map[string]interface{}{"foo": "bar"}
	if err := eb.Publish(context.Background(), "test.receive", payload); err != nil {
		t.Fatalf("EventBus Publish: %v", err)
	}

	select {
	case msg := <-handler.ch:
		var got map[string]interface{}
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal handler message: %v", err)
		}
		if got["foo"] != "bar" {
			t.Fatalf("expected foo=bar, got %v", got["foo"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message via bridge handler")
	}
}

func TestEventBusBridge_JSONRoundTrip(t *testing.T) {
	eb := setupEventBusForBridge(t)
	bridge := NewEventBusBridge(EventBusBridgeName)
	bridge.SetEventBus(eb)

	handler := newTestMessageHandler()
	if err := bridge.Subscribe("roundtrip", handler); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	original := map[string]interface{}{
		"count":  float64(42),
		"name":   "test",
		"nested": map[string]interface{}{"a": float64(1)},
	}
	msg, _ := json.Marshal(original)

	if err := bridge.SendMessage("roundtrip", msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	select {
	case received := <-handler.ch:
		var got map[string]interface{}
		if err := json.Unmarshal(received, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got["count"] != float64(42) {
			t.Fatalf("expected count=42, got %v", got["count"])
		}
		if got["name"] != "test" {
			t.Fatalf("expected name=test, got %v", got["name"])
		}
		nested, ok := got["nested"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected nested map, got %T", got["nested"])
		}
		if nested["a"] != float64(1) {
			t.Fatalf("expected nested.a=1, got %v", nested["a"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for round-trip message")
	}
}

func TestEventBusBridge_SendInvalidJSON(t *testing.T) {
	eb := setupEventBusForBridge(t)
	bridge := NewEventBusBridge(EventBusBridgeName)
	bridge.SetEventBus(eb)

	// Subscribe on EventBus to verify the raw bytes are published as payload.
	received := make(chan eventbus.Event, 1)
	sub, err := eb.Subscribe(context.Background(), "raw", func(_ context.Context, event eventbus.Event) error {
		received <- event
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Cancel() }()

	// Send invalid JSON
	rawMsg := []byte("this is not json")
	if err := bridge.SendMessage("raw", rawMsg); err != nil {
		t.Fatalf("SendMessage with raw bytes: %v", err)
	}

	select {
	case evt := <-received:
		// Payload should be the raw bytes since JSON unmarshal failed.
		b, ok := evt.Payload.([]byte)
		if !ok {
			t.Fatalf("expected []byte payload for invalid JSON, got %T", evt.Payload)
		}
		if string(b) != "this is not json" {
			t.Fatalf("expected raw message, got %q", string(b))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for raw event")
	}
}

func TestEventBusBridge_Unsubscribe(t *testing.T) {
	eb := setupEventBusForBridge(t)
	bridge := NewEventBusBridge(EventBusBridgeName)
	bridge.SetEventBus(eb)

	handler := newTestMessageHandler()
	if err := bridge.Subscribe("unsub.topic", handler); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Unsubscribe
	if err := bridge.Unsubscribe("unsub.topic"); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	// Publish a message -- it should NOT reach the handler.
	if err := eb.Publish(context.Background(), "unsub.topic", "after-unsub"); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Brief wait to confirm no delivery.
	select {
	case <-handler.ch:
		t.Fatal("handler received a message after unsubscribe")
	case <-time.After(200 * time.Millisecond):
		// expected
	}
}

func TestEventBusBridge_StopCancelsSubscriptions(t *testing.T) {
	eb := setupEventBusForBridge(t)
	bridge := NewEventBusBridge(EventBusBridgeName)
	bridge.SetEventBus(eb)

	handler := newTestMessageHandler()
	if err := bridge.Subscribe("stop.topic", handler); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Stop the bridge
	if err := bridge.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Publish after stop -- handler should NOT receive it.
	if err := eb.Publish(context.Background(), "stop.topic", "after-stop"); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case <-handler.ch:
		t.Fatal("handler received a message after bridge Stop")
	case <-time.After(200 * time.Millisecond):
		// expected
	}

	// Subscriptions map should be empty.
	bridge.mu.RLock()
	count := len(bridge.subscriptions)
	bridge.mu.RUnlock()
	if count != 0 {
		t.Fatalf("expected 0 subscriptions after Stop, got %d", count)
	}
}

func TestEventBusBridge_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	eb := setupEventBusForBridge(t)
	bridge := NewEventBusBridge(EventBusBridgeName)
	bridge.SetEventBus(eb)

	var wg sync.WaitGroup
	topics := 20

	// Concurrently subscribe to many topics.
	for i := 0; i < topics; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			topic := "concurrent." + string(rune('A'+idx))
			h := newTestMessageHandler()
			if err := bridge.Subscribe(topic, h); err != nil {
				t.Errorf("Subscribe(%s): %v", topic, err)
			}
		}(i)
	}
	wg.Wait()

	// Concurrently unsubscribe from all topics.
	for i := 0; i < topics; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			topic := "concurrent." + string(rune('A'+idx))
			if err := bridge.Unsubscribe(topic); err != nil {
				t.Errorf("Unsubscribe(%s): %v", topic, err)
			}
		}(i)
	}
	wg.Wait()

	bridge.mu.RLock()
	remaining := len(bridge.subscriptions)
	bridge.mu.RUnlock()
	if remaining != 0 {
		t.Fatalf("expected 0 subscriptions after concurrent unsub, got %d", remaining)
	}
}

func TestEventBusBridge_InitRegistersService(t *testing.T) {
	bridge := NewEventBusBridge(EventBusBridgeName)
	app := newEventBusBridgeMockApp()

	if err := bridge.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	svc, exists := app.services[EventBusBridgeName]
	if !exists {
		t.Fatal("expected bridge to be registered as a service")
	}
	if svc != bridge {
		t.Fatal("registered service should be the bridge instance")
	}
}
