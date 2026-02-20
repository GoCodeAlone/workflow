package module

import (
	"fmt"
	"net"
	"sync"
)

// PortAllocator manages automatic port allocation for deployed workflows.
type PortAllocator struct {
	mu        sync.Mutex
	nextPort  int
	allocated map[int]string // port → workflow name
	excluded  map[int]string // permanently excluded ports (e.g. admin server)
}

// NewPortAllocator creates a new port allocator starting from the given base port.
func NewPortAllocator(basePort int) *PortAllocator {
	return &PortAllocator{
		nextPort:  basePort,
		allocated: make(map[int]string),
		excluded:  make(map[int]string),
	}
}

// ExcludePort marks a port as permanently taken (e.g., the admin server port).
func (p *PortAllocator) ExcludePort(port int, name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.excluded[port] = name
}

// Allocate assigns the next available port to the named workflow.
func (p *PortAllocator) Allocate(name string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	startPort := p.nextPort
	for attempts := 0; attempts < 1000; attempts++ {
		port := p.nextPort
		p.nextPort++
		if p.nextPort > 65535 {
			p.nextPort = 1024
		}

		if _, taken := p.allocated[port]; taken {
			continue
		}
		if _, excluded := p.excluded[port]; excluded {
			continue
		}

		if isPortAvailable(port) {
			p.allocated[port] = name
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available port found after scanning from %d", startPort)
}

// Release frees all ports allocated to the named workflow.
// It also resets nextPort so freed ports can be reused on the next allocation.
func (p *PortAllocator) Release(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for port, n := range p.allocated {
		if n == name {
			delete(p.allocated, port)
			if port < p.nextPort {
				p.nextPort = port
			}
		}
	}
}

// AllocatedPorts returns a copy of the current port-to-workflow mapping.
func (p *PortAllocator) AllocatedPorts() map[int]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make(map[int]string, len(p.allocated))
	for k, v := range p.allocated {
		result[k] = v
	}
	return result
}

func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
