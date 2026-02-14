package scale

import (
	"fmt"
	"hash/crc32"
	"sort"
	"sync"
)

// ConsistentHash implements a consistent hashing ring for partitioning work
// by tenant or conversation ID across a set of nodes.
type ConsistentHash struct {
	mu       sync.RWMutex
	ring     []uint32          // sorted hash values
	nodes    map[uint32]string // hash -> node name
	replicas int               // virtual nodes per real node
	members  map[string]bool   // set of real node names
}

// NewConsistentHash creates a new consistent hash ring.
// replicas controls the number of virtual nodes per physical node (higher = more even distribution).
func NewConsistentHash(replicas int) *ConsistentHash {
	if replicas <= 0 {
		replicas = 100
	}
	return &ConsistentHash{
		nodes:    make(map[uint32]string),
		replicas: replicas,
		members:  make(map[string]bool),
	}
}

// AddNode adds a node to the hash ring.
func (h *ConsistentHash) AddNode(node string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.members[node] {
		return
	}

	h.members[node] = true

	for i := 0; i < h.replicas; i++ {
		hash := h.hash(fmt.Sprintf("%s-%d", node, i))
		h.ring = append(h.ring, hash)
		h.nodes[hash] = node
	}

	sort.Slice(h.ring, func(i, j int) bool {
		return h.ring[i] < h.ring[j]
	})
}

// RemoveNode removes a node from the hash ring.
func (h *ConsistentHash) RemoveNode(node string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.members[node] {
		return
	}

	delete(h.members, node)

	// Remove all virtual nodes for this node
	newRing := make([]uint32, 0, len(h.ring))
	for _, hashVal := range h.ring {
		if h.nodes[hashVal] == node {
			delete(h.nodes, hashVal)
		} else {
			newRing = append(newRing, hashVal)
		}
	}
	h.ring = newRing
}

// GetNode returns the node responsible for the given key.
func (h *ConsistentHash) GetNode(key string) (string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.ring) == 0 {
		return "", fmt.Errorf("empty hash ring")
	}

	hash := h.hash(key)

	// Binary search for the first hash >= key hash
	idx := sort.Search(len(h.ring), func(i int) bool {
		return h.ring[i] >= hash
	})

	// Wrap around to the first node if past the end
	if idx >= len(h.ring) {
		idx = 0
	}

	return h.nodes[h.ring[idx]], nil
}

// GetNodes returns the N distinct nodes responsible for the given key,
// in ring order. Useful for replication.
func (h *ConsistentHash) GetNodes(key string, count int) ([]string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.ring) == 0 {
		return nil, fmt.Errorf("empty hash ring")
	}

	memberCount := len(h.members)
	if count > memberCount {
		count = memberCount
	}

	hash := h.hash(key)
	idx := sort.Search(len(h.ring), func(i int) bool {
		return h.ring[i] >= hash
	})

	result := make([]string, 0, count)
	seen := make(map[string]bool, count)

	for len(result) < count {
		if idx >= len(h.ring) {
			idx = 0
		}
		node := h.nodes[h.ring[idx]]
		if !seen[node] {
			seen[node] = true
			result = append(result, node)
		}
		idx++
	}

	return result, nil
}

// Members returns the set of nodes in the ring.
func (h *ConsistentHash) Members() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]string, 0, len(h.members))
	for node := range h.members {
		result = append(result, node)
	}
	sort.Strings(result)
	return result
}

// Size returns the number of nodes in the ring.
func (h *ConsistentHash) Size() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.members)
}

func (h *ConsistentHash) hash(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}
