package module

import (
	"fmt"
	"net"
	"testing"
)

func TestNewPortAllocator(t *testing.T) {
	pa := NewPortAllocator(9000)
	if pa == nil {
		t.Fatal("expected non-nil PortAllocator")
	}
	if pa.nextPort != 9000 {
		t.Errorf("expected nextPort=9000, got %d", pa.nextPort)
	}
	if len(pa.allocated) != 0 {
		t.Errorf("expected empty allocated map, got %d entries", len(pa.allocated))
	}
	if len(pa.excluded) != 0 {
		t.Errorf("expected empty excluded map, got %d entries", len(pa.excluded))
	}
}

func TestAllocateAndRelease(t *testing.T) {
	pa := NewPortAllocator(19000)

	port1, err := pa.Allocate("workflow-a")
	if err != nil {
		t.Fatalf("first allocate failed: %v", err)
	}
	if port1 < 19000 {
		t.Errorf("expected port >= 19000, got %d", port1)
	}

	port2, err := pa.Allocate("workflow-b")
	if err != nil {
		t.Fatalf("second allocate failed: %v", err)
	}
	if port2 == port1 {
		t.Errorf("expected different ports, both got %d", port1)
	}

	ports := pa.AllocatedPorts()
	if ports[port1] != "workflow-a" {
		t.Errorf("expected port %d -> workflow-a, got %q", port1, ports[port1])
	}
	if ports[port2] != "workflow-b" {
		t.Errorf("expected port %d -> workflow-b, got %q", port2, ports[port2])
	}

	pa.Release("workflow-a")

	ports = pa.AllocatedPorts()
	if _, ok := ports[port1]; ok {
		t.Errorf("expected port %d to be released", port1)
	}
	if ports[port2] != "workflow-b" {
		t.Errorf("expected port %d still allocated to workflow-b", port2)
	}
}

func TestExcludePort(t *testing.T) {
	pa := NewPortAllocator(20000)
	pa.ExcludePort(20000, "admin-server")

	port, err := pa.Allocate("workflow-x")
	if err != nil {
		t.Fatalf("allocate failed: %v", err)
	}
	if port == 20000 {
		t.Error("allocated excluded port 20000")
	}
}

func TestAllocateSkipsUnavailable(t *testing.T) {
	// Hold a port with a real listener
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	heldPort := ln.Addr().(*net.TCPAddr).Port

	// Create allocator starting at the held port
	pa := NewPortAllocator(heldPort)

	port, err := pa.Allocate("workflow-skip")
	if err != nil {
		t.Fatalf("allocate failed: %v", err)
	}
	if port == heldPort {
		t.Errorf("should have skipped unavailable port %d", heldPort)
	}

	t.Logf("held port %d, allocated port %d", heldPort, port)
}

func TestAllocatedPorts(t *testing.T) {
	pa := NewPortAllocator(21000)

	// Initially empty
	ports := pa.AllocatedPorts()
	if len(ports) != 0 {
		t.Errorf("expected empty map, got %d entries", len(ports))
	}

	port1, err := pa.Allocate("wf-1")
	if err != nil {
		t.Fatalf("allocate failed: %v", err)
	}
	port2, err := pa.Allocate("wf-2")
	if err != nil {
		t.Fatalf("allocate failed: %v", err)
	}

	ports = pa.AllocatedPorts()
	if len(ports) != 2 {
		t.Errorf("expected 2 entries, got %d", len(ports))
	}
	if ports[port1] != "wf-1" {
		t.Errorf("expected wf-1 at port %d", port1)
	}
	if ports[port2] != "wf-2" {
		t.Errorf("expected wf-2 at port %d", port2)
	}

	// Verify it returns a copy (mutating the returned map shouldn't affect internal state)
	ports[99999] = "rogue"
	internal := pa.AllocatedPorts()
	if _, ok := internal[99999]; ok {
		t.Error("AllocatedPorts returned a reference, not a copy")
	}

	// Verify string representation for coverage
	_ = fmt.Sprintf("%v", ports)
}
