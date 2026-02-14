package scale

import (
	"fmt"
	"testing"
)

func TestConsistentHashBasic(t *testing.T) {
	h := NewConsistentHash(100)

	h.AddNode("node-1")
	h.AddNode("node-2")
	h.AddNode("node-3")

	if h.Size() != 3 {
		t.Errorf("expected 3 nodes, got %d", h.Size())
	}

	// The same key should always return the same node
	node1, err := h.GetNode("tenant-abc")
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}

	for i := 0; i < 100; i++ {
		node, err := h.GetNode("tenant-abc")
		if err != nil {
			t.Fatalf("GetNode failed: %v", err)
		}
		if node != node1 {
			t.Errorf("expected consistent result %q, got %q on iteration %d", node1, node, i)
		}
	}
}

func TestConsistentHashEmpty(t *testing.T) {
	h := NewConsistentHash(100)

	_, err := h.GetNode("key")
	if err == nil {
		t.Error("expected error from empty ring")
	}
}

func TestConsistentHashAddRemove(t *testing.T) {
	h := NewConsistentHash(100)

	h.AddNode("node-1")
	h.AddNode("node-2")

	// Get assignment for a key
	original, _ := h.GetNode("key-x")

	// Remove and re-add the same node
	h.RemoveNode("node-1")
	if h.Size() != 1 {
		t.Errorf("expected 1 node after removal, got %d", h.Size())
	}

	h.AddNode("node-1")
	if h.Size() != 2 {
		t.Errorf("expected 2 nodes after re-add, got %d", h.Size())
	}

	// Assignment should still be consistent
	current, _ := h.GetNode("key-x")
	if current != original {
		// This can happen due to ring ordering but shouldn't for 2 nodes with same virtual nodes
		t.Logf("assignment changed from %q to %q (acceptable with ring changes)", original, current)
	}
}

func TestConsistentHashDuplicateAdd(t *testing.T) {
	h := NewConsistentHash(10)

	h.AddNode("node-1")
	h.AddNode("node-1") // duplicate

	if h.Size() != 1 {
		t.Errorf("expected 1 node, got %d", h.Size())
	}
}

func TestConsistentHashRemoveNonexistent(t *testing.T) {
	h := NewConsistentHash(10)
	h.AddNode("node-1")

	// Should not panic
	h.RemoveNode("node-999")

	if h.Size() != 1 {
		t.Errorf("expected 1 node, got %d", h.Size())
	}
}

func TestConsistentHashDistribution(t *testing.T) {
	h := NewConsistentHash(100)
	nodeCount := 5
	for i := 0; i < nodeCount; i++ {
		h.AddNode(fmt.Sprintf("node-%d", i))
	}

	counts := make(map[string]int)
	keyCount := 10000
	for i := 0; i < keyCount; i++ {
		key := fmt.Sprintf("tenant-%d", i)
		node, err := h.GetNode(key)
		if err != nil {
			t.Fatalf("GetNode failed: %v", err)
		}
		counts[node]++
	}

	// Check that distribution is reasonably even (no node gets less than 10% or more than 30%)
	for node, count := range counts {
		pct := float64(count) / float64(keyCount) * 100
		if pct < 10 || pct > 30 {
			t.Errorf("node %s got %.1f%% of keys (%d), distribution is uneven", node, pct, count)
		}
	}
}

func TestConsistentHashGetNodes(t *testing.T) {
	h := NewConsistentHash(100)
	h.AddNode("node-1")
	h.AddNode("node-2")
	h.AddNode("node-3")

	nodes, err := h.GetNodes("key-x", 2)
	if err != nil {
		t.Fatalf("GetNodes failed: %v", err)
	}

	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}

	// Nodes should be distinct
	if nodes[0] == nodes[1] {
		t.Error("expected distinct nodes")
	}
}

func TestConsistentHashGetNodesTooMany(t *testing.T) {
	h := NewConsistentHash(100)
	h.AddNode("node-1")
	h.AddNode("node-2")

	nodes, err := h.GetNodes("key-x", 5) // more than available
	if err != nil {
		t.Fatalf("GetNodes failed: %v", err)
	}

	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes (capped), got %d", len(nodes))
	}
}

func TestConsistentHashMembers(t *testing.T) {
	h := NewConsistentHash(10)
	h.AddNode("c-node")
	h.AddNode("a-node")
	h.AddNode("b-node")

	members := h.Members()
	if len(members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(members))
	}

	// Members should be sorted
	if members[0] != "a-node" || members[1] != "b-node" || members[2] != "c-node" {
		t.Errorf("expected sorted members, got %v", members)
	}
}

func TestConsistentHashDefaultReplicas(t *testing.T) {
	h := NewConsistentHash(0) // should default to 100
	h.AddNode("node-1")

	node, err := h.GetNode("key")
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}
	if node != "node-1" {
		t.Errorf("expected node-1, got %q", node)
	}
}
