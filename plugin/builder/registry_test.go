package builder_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/plugin/builder"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	builder.Reset() // start clean for this test

	m := &mockBuilder{name: "test-reg"}
	builder.Register(m)

	got, ok := builder.Get("test-reg")
	if !ok {
		t.Fatal("want ok=true after register")
	}
	if got.Name() != "test-reg" {
		t.Fatalf("want test-reg, got %q", got.Name())
	}
}

func TestRegistry_UnknownReturnsFalse(t *testing.T) {
	builder.Reset()
	_, ok := builder.Get("does-not-exist")
	if ok {
		t.Fatal("want ok=false for unknown builder")
	}
}

func TestRegistry_List(t *testing.T) {
	builder.Reset()
	builder.Register(&mockBuilder{name: "a"})
	builder.Register(&mockBuilder{name: "b"})

	list := builder.List()
	if len(list) != 2 {
		t.Fatalf("want 2 builders, got %d", len(list))
	}
}

func TestRegistry_DuplicateRegisterOverwrites(t *testing.T) {
	builder.Reset()
	builder.Register(&mockBuilder{name: "dup"})
	builder.Register(&mockBuilder{name: "dup"})
	list := builder.List()
	if len(list) != 1 {
		t.Fatalf("want 1 after duplicate register, got %d", len(list))
	}
}
