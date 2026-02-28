package module

import (
	"context"
	"testing"
	"time"
)

func TestTokenBlacklistMemory_AddAndCheck(t *testing.T) {
	bl := NewTokenBlacklistModule("test-bl", "memory", "", time.Minute)

	if bl.IsBlacklisted("jti-1") {
		t.Fatal("expected jti-1 to not be blacklisted initially")
	}

	bl.Add("jti-1", time.Now().Add(time.Hour))
	if !bl.IsBlacklisted("jti-1") {
		t.Fatal("expected jti-1 to be blacklisted after Add")
	}

	if bl.IsBlacklisted("jti-unknown") {
		t.Fatal("expected jti-unknown to not be blacklisted")
	}
}

func TestTokenBlacklistMemory_ExpiredEntry(t *testing.T) {
	bl := NewTokenBlacklistModule("test-bl", "memory", "", time.Minute)

	// Add a JTI that has already expired.
	bl.Add("jti-expired", time.Now().Add(-time.Second))
	if bl.IsBlacklisted("jti-expired") {
		t.Fatal("expected already-expired JTI to not be blacklisted")
	}
}

func TestTokenBlacklistMemory_Cleanup(t *testing.T) {
	bl := NewTokenBlacklistModule("test-bl", "memory", "", 50*time.Millisecond)

	app := NewMockApplication()
	if err := bl.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := bl.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = bl.Stop(context.Background()) }()

	// Add a JTI that expires in 10ms.
	bl.Add("jti-cleanup", time.Now().Add(10*time.Millisecond))

	// Give it time to expire and be cleaned up by the cleanup goroutine.
	time.Sleep(300 * time.Millisecond)

	if bl.IsBlacklisted("jti-cleanup") {
		t.Fatal("expected cleaned-up JTI to no longer be blacklisted")
	}
}

func TestTokenBlacklistModule_StopIdempotent(t *testing.T) {
	bl := NewTokenBlacklistModule("test-bl", "memory", "", time.Minute)
	app := NewMockApplication()
	if err := bl.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := bl.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Calling Stop twice must not panic.
	if err := bl.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := bl.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestTokenBlacklistModule_ProvidesServices(t *testing.T) {
	bl := NewTokenBlacklistModule("my-bl", "memory", "", time.Minute)
	svcs := bl.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "my-bl" {
		t.Fatalf("expected service name 'my-bl', got %q", svcs[0].Name)
	}
	if _, ok := svcs[0].Instance.(TokenBlacklist); !ok {
		t.Fatal("expected service instance to implement TokenBlacklist")
	}
}
