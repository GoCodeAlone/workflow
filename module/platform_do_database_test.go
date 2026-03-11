package module

import "testing"

func TestPlatformDODatabase_MockBackend(t *testing.T) {
	m := &PlatformDODatabase{
		name: "test-db",
		config: map[string]any{
			"provider":  "mock",
			"engine":    "pg",
			"version":   "16",
			"size":      "db-s-1vcpu-1gb",
			"region":    "nyc1",
			"num_nodes": 1,
			"name":      "test-db",
		},
		state: &DODatabaseState{
			Name:     "test-db",
			Engine:   "pg",
			Version:  "16",
			Size:     "db-s-1vcpu-1gb",
			Region:   "nyc1",
			NumNodes: 1,
			Status:   "pending",
		},
		backend: &doDatabaseMockBackend{},
	}

	// Test PlatformProvider interface
	var _ PlatformProvider = m

	// Plan
	plan, err := m.Plan()
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if plan.Provider != "digitalocean" {
		t.Errorf("expected provider digitalocean, got %s", plan.Provider)
	}
	if plan.Resource != "managed_database" {
		t.Errorf("expected resource managed_database, got %s", plan.Resource)
	}

	// Apply
	result, err := m.Apply()
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}

	// Status
	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	if st == nil {
		t.Error("expected non-nil status")
	}

	// Destroy
	if err := m.Destroy(); err != nil {
		t.Fatalf("Destroy() error: %v", err)
	}
}
