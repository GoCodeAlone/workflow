package capability

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
)

// Test interfaces for use in tests.
type testServer interface {
	Serve(addr string) error
}

type testBroker interface {
	Publish(topic string, data []byte) error
}

type testCache interface {
	Get(key string) ([]byte, bool)
}

// Concrete types for provider registration.
type myServer struct{}
type myBroker struct{}
type altServer struct{}

func makeServerContract() Contract {
	return Contract{
		Name:          "http-server",
		Description:   "Provides HTTP server capability",
		InterfaceType: reflect.TypeOf((*testServer)(nil)).Elem(),
		RequiredMethods: []MethodSignature{
			{Name: "Serve", Params: []string{"string"}, Returns: []string{"error"}},
		},
	}
}

func makeBrokerContract() Contract {
	return Contract{
		Name:          "message-broker",
		Description:   "Provides message broker capability",
		InterfaceType: reflect.TypeOf((*testBroker)(nil)).Elem(),
		RequiredMethods: []MethodSignature{
			{Name: "Publish", Params: []string{"string", "[]byte"}, Returns: []string{"error"}},
		},
	}
}

func TestRegisterAndRetrieveContract(t *testing.T) {
	reg := NewRegistry()
	c := makeServerContract()

	if err := reg.RegisterContract(c); err != nil {
		t.Fatalf("RegisterContract failed: %v", err)
	}

	got, ok := reg.ContractFor("http-server")
	if !ok {
		t.Fatal("ContractFor returned false for registered contract")
	}
	if got.Name != "http-server" {
		t.Errorf("expected name %q, got %q", "http-server", got.Name)
	}
	if got.InterfaceType != c.InterfaceType {
		t.Errorf("expected InterfaceType %v, got %v", c.InterfaceType, got.InterfaceType)
	}
}

func TestContractForUnregistered(t *testing.T) {
	reg := NewRegistry()

	_, ok := reg.ContractFor("nonexistent")
	if ok {
		t.Error("ContractFor should return false for unregistered capability")
	}
}

func TestRegisterContractEmptyName(t *testing.T) {
	reg := NewRegistry()
	c := Contract{Name: ""}
	if err := reg.RegisterContract(c); err == nil {
		t.Error("expected error for empty contract name")
	}
}

func TestRegisterDuplicateContractSameType(t *testing.T) {
	reg := NewRegistry()
	c := makeServerContract()

	if err := reg.RegisterContract(c); err != nil {
		t.Fatalf("first register: %v", err)
	}
	// Re-registering with the same type should be idempotent.
	if err := reg.RegisterContract(c); err != nil {
		t.Fatalf("duplicate register with same type should succeed: %v", err)
	}
}

func TestRegisterDuplicateContractDifferentType(t *testing.T) {
	reg := NewRegistry()

	c1 := makeServerContract()
	if err := reg.RegisterContract(c1); err != nil {
		t.Fatalf("first register: %v", err)
	}

	c2 := Contract{
		Name:          "http-server",
		Description:   "Different interface",
		InterfaceType: reflect.TypeOf((*testBroker)(nil)).Elem(),
	}
	if err := reg.RegisterContract(c2); err == nil {
		t.Error("expected error when registering same name with different InterfaceType")
	}
}

func TestRegisterProviderAndResolve(t *testing.T) {
	reg := NewRegistry()
	c := makeServerContract()
	if err := reg.RegisterContract(c); err != nil {
		t.Fatalf("RegisterContract: %v", err)
	}

	implType := reflect.TypeOf(myServer{})
	if err := reg.RegisterProvider("http-server", "my-plugin", 10, implType); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}

	entry, err := reg.Resolve("http-server")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if entry.PluginName != "my-plugin" {
		t.Errorf("expected plugin %q, got %q", "my-plugin", entry.PluginName)
	}
	if entry.Priority != 10 {
		t.Errorf("expected priority 10, got %d", entry.Priority)
	}
}

func TestRegisterProviderUnregisteredCapability(t *testing.T) {
	reg := NewRegistry()

	err := reg.RegisterProvider("nonexistent", "my-plugin", 10, reflect.TypeOf(myServer{}))
	if err == nil {
		t.Error("expected error when registering provider for unregistered capability")
	}
}

func TestResolveHighestPriority(t *testing.T) {
	reg := NewRegistry()
	c := makeServerContract()
	if err := reg.RegisterContract(c); err != nil {
		t.Fatalf("RegisterContract: %v", err)
	}

	_ = reg.RegisterProvider("http-server", "low-priority", 5, reflect.TypeOf(myServer{}))
	_ = reg.RegisterProvider("http-server", "high-priority", 20, reflect.TypeOf(altServer{}))
	_ = reg.RegisterProvider("http-server", "mid-priority", 10, reflect.TypeOf(myServer{}))

	entry, err := reg.Resolve("http-server")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if entry.PluginName != "high-priority" {
		t.Errorf("expected %q, got %q", "high-priority", entry.PluginName)
	}
	if entry.Priority != 20 {
		t.Errorf("expected priority 20, got %d", entry.Priority)
	}
}

func TestResolveNoProviders(t *testing.T) {
	reg := NewRegistry()
	c := makeServerContract()
	_ = reg.RegisterContract(c)

	_, err := reg.Resolve("http-server")
	if err == nil {
		t.Error("expected error when resolving capability with no providers")
	}
}

func TestResolveUnregisteredCapability(t *testing.T) {
	reg := NewRegistry()

	_, err := reg.Resolve("nonexistent")
	if err == nil {
		t.Error("expected error when resolving unregistered capability")
	}
}

func TestHasProvider(t *testing.T) {
	reg := NewRegistry()
	c := makeServerContract()
	_ = reg.RegisterContract(c)

	if reg.HasProvider("http-server") {
		t.Error("HasProvider should return false before any providers are registered")
	}

	_ = reg.RegisterProvider("http-server", "my-plugin", 10, reflect.TypeOf(myServer{}))

	if !reg.HasProvider("http-server") {
		t.Error("HasProvider should return true after provider registration")
	}

	if reg.HasProvider("nonexistent") {
		t.Error("HasProvider should return false for unregistered capability")
	}
}

func TestListCapabilitiesSorted(t *testing.T) {
	reg := NewRegistry()
	_ = reg.RegisterContract(Contract{
		Name:          "zebra",
		InterfaceType: reflect.TypeOf((*testServer)(nil)).Elem(),
	})
	_ = reg.RegisterContract(Contract{
		Name:          "alpha",
		InterfaceType: reflect.TypeOf((*testBroker)(nil)).Elem(),
	})
	_ = reg.RegisterContract(Contract{
		Name:          "middle",
		InterfaceType: reflect.TypeOf((*testCache)(nil)).Elem(),
	})

	caps := reg.ListCapabilities()
	if len(caps) != 3 {
		t.Fatalf("expected 3 capabilities, got %d", len(caps))
	}
	expected := []string{"alpha", "middle", "zebra"}
	for i, name := range caps {
		if name != expected[i] {
			t.Errorf("position %d: expected %q, got %q", i, expected[i], name)
		}
	}
}

func TestListCapabilitiesEmpty(t *testing.T) {
	reg := NewRegistry()
	caps := reg.ListCapabilities()
	if len(caps) != 0 {
		t.Errorf("expected empty list, got %v", caps)
	}
}

func TestListProviders(t *testing.T) {
	reg := NewRegistry()
	c := makeServerContract()
	_ = reg.RegisterContract(c)

	_ = reg.RegisterProvider("http-server", "plugin-a", 5, reflect.TypeOf(myServer{}))
	_ = reg.RegisterProvider("http-server", "plugin-b", 15, reflect.TypeOf(altServer{}))

	providers := reg.ListProviders("http-server")
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}

	names := map[string]bool{}
	for _, p := range providers {
		names[p.PluginName] = true
	}
	if !names["plugin-a"] || !names["plugin-b"] {
		t.Errorf("expected both plugin-a and plugin-b, got %v", providers)
	}
}

func TestListProvidersEmpty(t *testing.T) {
	reg := NewRegistry()
	providers := reg.ListProviders("nonexistent")
	if providers != nil {
		t.Errorf("expected nil for nonexistent capability, got %v", providers)
	}
}

func TestListProvidersReturnsCopy(t *testing.T) {
	reg := NewRegistry()
	c := makeServerContract()
	_ = reg.RegisterContract(c)
	_ = reg.RegisterProvider("http-server", "plugin-a", 5, reflect.TypeOf(myServer{}))

	providers := reg.ListProviders("http-server")
	providers[0].PluginName = "mutated"

	// Original should be unchanged.
	original := reg.ListProviders("http-server")
	if original[0].PluginName != "plugin-a" {
		t.Error("ListProviders should return a copy; original was mutated")
	}
}

func TestConcurrentAccess(t *testing.T) {
	reg := NewRegistry()
	c := makeServerContract()
	_ = reg.RegisterContract(c)

	var wg sync.WaitGroup
	const goroutines = 50

	// Concurrent provider registrations.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("plugin-%d", n)
			_ = reg.RegisterProvider("http-server", name, n, reflect.TypeOf(myServer{}))
		}(i)
	}

	// Concurrent reads while writes are happening.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = reg.ListCapabilities()
			_ = reg.HasProvider("http-server")
			_, _ = reg.Resolve("http-server")
			_ = reg.ListProviders("http-server")
			_, _ = reg.ContractFor("http-server")
		}()
	}

	wg.Wait()

	// Verify all providers were registered.
	providers := reg.ListProviders("http-server")
	if len(providers) != goroutines {
		t.Errorf("expected %d providers, got %d", goroutines, len(providers))
	}
}

func TestMultipleCapabilities(t *testing.T) {
	reg := NewRegistry()

	server := makeServerContract()
	broker := makeBrokerContract()
	_ = reg.RegisterContract(server)
	_ = reg.RegisterContract(broker)

	_ = reg.RegisterProvider("http-server", "web-plugin", 10, reflect.TypeOf(myServer{}))
	_ = reg.RegisterProvider("message-broker", "mq-plugin", 10, reflect.TypeOf(myBroker{}))

	caps := reg.ListCapabilities()
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(caps))
	}

	serverEntry, err := reg.Resolve("http-server")
	if err != nil {
		t.Fatalf("resolve http-server: %v", err)
	}
	if serverEntry.PluginName != "web-plugin" {
		t.Errorf("expected %q, got %q", "web-plugin", serverEntry.PluginName)
	}

	brokerEntry, err := reg.Resolve("message-broker")
	if err != nil {
		t.Fatalf("resolve message-broker: %v", err)
	}
	if brokerEntry.PluginName != "mq-plugin" {
		t.Errorf("expected %q, got %q", "mq-plugin", brokerEntry.PluginName)
	}
}
