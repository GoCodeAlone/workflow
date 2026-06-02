package specparse_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/iac/specparse"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestParseResourceSpecs_RoundTripShape verifies that a representative []any
// of spec maps parses to the expected []interfaces.ResourceSpec. Critically,
// secret:// refs inside a resource's config map must survive verbatim — no
// expansion is performed.
func TestParseResourceSpecs_RoundTripShape(t *testing.T) {
	raw := []any{
		map[string]any{
			"name": "web-server",
			"type": "droplet",
			"size": "s-1vcpu-1gb",
			"config": map[string]any{
				"region":   "nyc3",
				"password": "secret://vault/db-password",
			},
		},
		map[string]any{
			"name": "db",
			"type": "database",
			// no size, no config
		},
	}

	specs, err := specparse.ParseResourceSpecs(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}

	// First spec
	s0 := specs[0]
	if s0.Name != "web-server" {
		t.Errorf("specs[0].Name = %q, want %q", s0.Name, "web-server")
	}
	if s0.Type != "droplet" {
		t.Errorf("specs[0].Type = %q, want %q", s0.Type, "droplet")
	}
	if s0.Size != interfaces.Size("s-1vcpu-1gb") {
		t.Errorf("specs[0].Size = %q, want %q", s0.Size, "s-1vcpu-1gb")
	}
	if s0.Config == nil {
		t.Fatal("specs[0].Config is nil")
	}
	// secret:// ref must be preserved verbatim
	got, ok := s0.Config["password"].(string)
	if !ok {
		t.Fatalf("specs[0].Config[\"password\"] is not a string")
	}
	const wantRef = "secret://vault/db-password"
	if got != wantRef {
		t.Errorf("secret ref not preserved: got %q, want %q", got, wantRef)
	}

	// Second spec
	s1 := specs[1]
	if s1.Name != "db" {
		t.Errorf("specs[1].Name = %q, want %q", s1.Name, "db")
	}
	if s1.Type != "database" {
		t.Errorf("specs[1].Type = %q, want %q", s1.Type, "database")
	}
	if s1.Config != nil {
		t.Errorf("specs[1].Config should be nil, got %v", s1.Config)
	}

	// nil raw must return nil, nil (no error)
	empty, err := specparse.ParseResourceSpecs(nil)
	if err != nil {
		t.Fatalf("nil raw: unexpected error: %v", err)
	}
	if empty != nil {
		t.Errorf("nil raw: expected nil slice, got %v", empty)
	}

	// non-list must error
	_, err = specparse.ParseResourceSpecs("notalist")
	if err == nil {
		t.Error("non-list raw: expected error, got nil")
	}
}
