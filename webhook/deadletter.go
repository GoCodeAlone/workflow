package webhook

import (
	"sort"
	"sync"
	"time"
)

// DeadLetterStats holds aggregate stats for the dead letter store.
type DeadLetterStats struct {
	Total        int            `json:"total"`
	ByStatus     map[string]int `json:"byStatus"`
	OldestEntry  *time.Time     `json:"oldestEntry,omitempty"`
	NewestEntry  *time.Time     `json:"newestEntry,omitempty"`
	TotalRetries int            `json:"totalRetries"`
}

// DeadLetterStore is an in-memory store for failed webhook deliveries.
type DeadLetterStore struct {
	mu      sync.RWMutex
	entries map[string]*Delivery
}

// NewDeadLetterStore creates a new empty DeadLetterStore.
func NewDeadLetterStore() *DeadLetterStore {
	return &DeadLetterStore{
		entries: make(map[string]*Delivery),
	}
}

// Add puts a delivery into the dead letter store.
func (s *DeadLetterStore) Add(d *Delivery) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[d.ID] = d
}

// Get retrieves a delivery by ID.
func (s *DeadLetterStore) Get(id string) (*Delivery, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.entries[id]
	return d, ok
}

// Remove removes and returns a delivery from the store.
func (s *DeadLetterStore) Remove(id string) (*Delivery, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.entries[id]
	if ok {
		delete(s.entries, id)
	}
	return d, ok
}

// List returns all dead letter entries sorted by creation time (newest first).
func (s *DeadLetterStore) List() []*Delivery {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Delivery, 0, len(s.entries))
	for _, d := range s.entries {
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

// Count returns the number of entries.
func (s *DeadLetterStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// Purge removes all entries and returns the count removed.
func (s *DeadLetterStore) Purge() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.entries)
	s.entries = make(map[string]*Delivery)
	return n
}

// Stats returns aggregate statistics about dead letter entries.
func (s *DeadLetterStore) Stats() DeadLetterStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := DeadLetterStats{
		Total:    len(s.entries),
		ByStatus: make(map[string]int),
	}

	for _, d := range s.entries {
		stats.ByStatus[string(d.Status)]++
		stats.TotalRetries += d.Attempts
		if stats.OldestEntry == nil || d.CreatedAt.Before(*stats.OldestEntry) {
			t := d.CreatedAt
			stats.OldestEntry = &t
		}
		if stats.NewestEntry == nil || d.CreatedAt.After(*stats.NewestEntry) {
			t := d.CreatedAt
			stats.NewestEntry = &t
		}
	}

	return stats
}
