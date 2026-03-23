package wftest_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/wftest"
)

func TestStateStore_SeedAndGet(t *testing.T) {
	s := wftest.NewStateStore()

	s.Seed("players", map[string]any{
		"p1": map[string]any{"hp": 100},
		"p2": map[string]any{"hp": 80},
	})

	v, ok := s.Get("players", "p1")
	if !ok {
		t.Fatal("expected key p1 to exist")
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", v)
	}
	if m["hp"] != 100 {
		t.Errorf("expected hp=100, got %v", m["hp"])
	}

	_, ok = s.Get("players", "p99")
	if ok {
		t.Error("expected p99 to not exist")
	}

	_, ok = s.Get("nonexistent", "p1")
	if ok {
		t.Error("expected nonexistent store to return false")
	}
}

func TestStateStore_Set(t *testing.T) {
	s := wftest.NewStateStore()
	s.Set("inventory", "sword", map[string]any{"damage": 10})

	v, ok := s.Get("inventory", "sword")
	if !ok {
		t.Fatal("expected sword to exist")
	}
	m := v.(map[string]any)
	if m["damage"] != 10 {
		t.Errorf("expected damage=10, got %v", m["damage"])
	}
}

func TestStateStore_GetAll(t *testing.T) {
	s := wftest.NewStateStore()
	s.Seed("scores", map[string]any{"alice": 42, "bob": 7})

	all := s.GetAll("scores")
	if len(all) != 2 {
		t.Errorf("expected 2 entries, got %d", len(all))
	}
	if all["alice"] != 42 {
		t.Errorf("expected alice=42, got %v", all["alice"])
	}

	if s.GetAll("missing") != nil {
		t.Error("expected nil for missing store")
	}
}

func TestStateStore_LoadFixture(t *testing.T) {
	s := wftest.NewStateStore()
	if err := s.LoadFixture("testdata/fixture.json", "chars"); err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}

	v, ok := s.Get("chars", "player1")
	if !ok {
		t.Fatal("expected player1 to exist after LoadFixture")
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", v)
	}
	// JSON numbers unmarshal as float64.
	if m["level"] != float64(5) {
		t.Errorf("expected level=5, got %v", m["level"])
	}
}

func TestStateStore_LoadFixture_NotFound(t *testing.T) {
	s := wftest.NewStateStore()
	err := s.LoadFixture("testdata/nonexistent.json", "x")
	if err == nil {
		t.Error("expected error for missing fixture file")
	}
}

func TestStateStore_Assert_Match(t *testing.T) {
	s := wftest.NewStateStore()
	s.Seed("session", map[string]any{
		"turn":  "p1",
		"round": 3,
	})

	if err := s.Assert("session", map[string]any{"turn": "p1"}); err != nil {
		t.Errorf("unexpected mismatch: %v", err)
	}
}

func TestStateStore_Assert_Mismatch(t *testing.T) {
	s := wftest.NewStateStore()
	s.Seed("session", map[string]any{"turn": "p1"})

	if err := s.Assert("session", map[string]any{"turn": "p2"}); err == nil {
		t.Error("expected mismatch error, got nil")
	}
}

func TestStateStore_Assert_MissingKey(t *testing.T) {
	s := wftest.NewStateStore()
	s.Seed("session", map[string]any{"turn": "p1"})

	if err := s.Assert("session", map[string]any{"score": 0}); err == nil {
		t.Error("expected error for missing key, got nil")
	}
}
