package builder

import "sync"

var (
	mu       sync.RWMutex
	registry = map[string]Builder{}
)

// Register adds b to the registry, overwriting any prior registration with the same name.
func Register(b Builder) {
	mu.Lock()
	defer mu.Unlock()
	registry[b.Name()] = b
}

// Get returns the Builder registered under name, or (nil, false) if not found.
func Get(name string) (Builder, bool) {
	mu.RLock()
	defer mu.RUnlock()
	b, ok := registry[name]
	return b, ok
}

// List returns all registered builders in an unspecified order.
func List() []Builder {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Builder, 0, len(registry))
	for _, b := range registry {
		out = append(out, b)
	}
	return out
}

// Reset clears all registrations. Intended for tests only.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	registry = map[string]Builder{}
}
