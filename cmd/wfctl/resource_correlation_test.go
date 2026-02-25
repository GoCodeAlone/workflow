package main

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// --- ClassifyModule ---

func TestClassifyModuleDatabase(t *testing.T) {
	for _, ty := range []string{"storage.sqlite", "database.workflow", "persistence.store"} {
		if got := ClassifyModule(ty); got != ResourceKindDatabase {
			t.Errorf("ClassifyModule(%q) = %q, want %q", ty, got, ResourceKindDatabase)
		}
	}
}

func TestClassifyModuleBroker(t *testing.T) {
	for _, ty := range []string{"messaging.broker", "messaging.nats", "messaging.kafka", "messaging.broker.eventbus"} {
		if got := ClassifyModule(ty); got != ResourceKindBroker {
			t.Errorf("ClassifyModule(%q) = %q, want %q", ty, got, ResourceKindBroker)
		}
	}
}

func TestClassifyModuleCache(t *testing.T) {
	if got := ClassifyModule("cache.redis"); got != ResourceKindCache {
		t.Errorf("ClassifyModule(cache.redis) = %q, want %q", got, ResourceKindCache)
	}
}

func TestClassifyModuleVolume(t *testing.T) {
	if got := ClassifyModule("static.fileserver"); got != ResourceKindVolume {
		t.Errorf("ClassifyModule(static.fileserver) = %q, want %q", got, ResourceKindVolume)
	}
}

func TestClassifyModuleStateless(t *testing.T) {
	for _, ty := range []string{
		"http.server",
		"http.router",
		"auth.jwt",
		"openapi",
		"observability.prometheus",
		"http.middleware.cors",
		"unknown.type",
		"",
	} {
		if got := ClassifyModule(ty); got != ResourceKindStateless {
			t.Errorf("ClassifyModule(%q) = %q, want %q", ty, got, ResourceKindStateless)
		}
	}
}

// --- IsStateful ---

func TestIsStatefulTrue(t *testing.T) {
	for _, ty := range []string{
		"storage.sqlite",
		"database.workflow",
		"persistence.store",
		"messaging.broker",
		"messaging.nats",
		"messaging.kafka",
		"static.fileserver",
	} {
		if !IsStateful(ty) {
			t.Errorf("IsStateful(%q) = false, want true", ty)
		}
	}
}

func TestIsStatefulFalse(t *testing.T) {
	for _, ty := range []string{
		"http.server",
		"http.router",
		"auth.jwt",
		"cache.redis", // semi-stateful but ephemeral by default
		"openapi",
		"observability.prometheus",
		"",
	} {
		if IsStateful(ty) {
			t.Errorf("IsStateful(%q) = true, want false", ty)
		}
	}
}

// --- GenerateResourceID ---

func TestGenerateResourceIDWithNamespace(t *testing.T) {
	id := GenerateResourceID("orders-db", "storage.sqlite", "prod")
	if id != "database/prod-orders-db" {
		t.Errorf("GenerateResourceID = %q, want %q", id, "database/prod-orders-db")
	}
}

func TestGenerateResourceIDWithoutNamespace(t *testing.T) {
	id := GenerateResourceID("event-broker", "messaging.broker", "")
	if id != "broker/event-broker" {
		t.Errorf("GenerateResourceID = %q, want %q", id, "broker/event-broker")
	}
}

func TestGenerateResourceIDCache(t *testing.T) {
	id := GenerateResourceID("session-cache", "cache.redis", "staging")
	if id != "cache/staging-session-cache" {
		t.Errorf("GenerateResourceID = %q, want %q", id, "cache/staging-session-cache")
	}
}

// --- DetectBreakingChanges ---

func TestDetectBreakingChangesNilInputs(t *testing.T) {
	if changes := DetectBreakingChanges(nil, nil); len(changes) != 0 {
		t.Errorf("expected no changes for nil inputs, got %d", len(changes))
	}
	mod := &config.ModuleConfig{Name: "x", Type: "storage.sqlite"}
	if changes := DetectBreakingChanges(nil, mod); len(changes) != 0 {
		t.Errorf("expected no changes when old is nil, got %d", len(changes))
	}
	if changes := DetectBreakingChanges(mod, nil); len(changes) != 0 {
		t.Errorf("expected no changes when new is nil, got %d", len(changes))
	}
}

func TestDetectBreakingChangesTypeChanged(t *testing.T) {
	old := &config.ModuleConfig{Name: "db", Type: "storage.sqlite"}
	nw := &config.ModuleConfig{Name: "db", Type: "database.workflow"}
	changes := DetectBreakingChanges(old, nw)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change for type switch, got %d", len(changes))
	}
	if changes[0].Field != "type" {
		t.Errorf("expected field=type, got %q", changes[0].Field)
	}
}

func TestDetectBreakingChangesStatefulConfigChanged(t *testing.T) {
	old := &config.ModuleConfig{
		Name:   "orders-db",
		Type:   "storage.sqlite",
		Config: map[string]any{"dbPath": "/data/old.db"},
	}
	nw := &config.ModuleConfig{
		Name:   "orders-db",
		Type:   "storage.sqlite",
		Config: map[string]any{"dbPath": "/data/new.db"},
	}
	changes := DetectBreakingChanges(old, nw)
	if len(changes) == 0 {
		t.Fatal("expected breaking change for dbPath change")
	}
	if !strings.Contains(changes[0].Message, "dbPath") {
		t.Errorf("expected message to mention dbPath, got: %s", changes[0].Message)
	}
}

func TestDetectBreakingChangesStatelessNoBreaking(t *testing.T) {
	old := &config.ModuleConfig{
		Name:   "server",
		Type:   "http.server",
		Config: map[string]any{"address": ":8080"},
	}
	nw := &config.ModuleConfig{
		Name:   "server",
		Type:   "http.server",
		Config: map[string]any{"address": ":9090"},
	}
	changes := DetectBreakingChanges(old, nw)
	if len(changes) != 0 {
		t.Errorf("expected no breaking changes for stateless module, got %d", len(changes))
	}
}

func TestDetectBreakingChangesUnchanged(t *testing.T) {
	mod := &config.ModuleConfig{
		Name:   "orders-db",
		Type:   "storage.sqlite",
		Config: map[string]any{"dbPath": "/data/orders.db"},
	}
	changes := DetectBreakingChanges(mod, mod)
	if len(changes) != 0 {
		t.Errorf("expected no changes for identical modules, got %d", len(changes))
	}
}

func TestDetectBreakingChangesDatabaseWorkflow(t *testing.T) {
	old := &config.ModuleConfig{
		Name:   "main-db",
		Type:   "database.workflow",
		Config: map[string]any{"dsn": "postgres://host1/db1"},
	}
	nw := &config.ModuleConfig{
		Name:   "main-db",
		Type:   "database.workflow",
		Config: map[string]any{"dsn": "postgres://host2/db1"},
	}
	changes := DetectBreakingChanges(old, nw)
	if len(changes) == 0 {
		t.Fatal("expected breaking change for DSN change")
	}
	if changes[0].Field != "dsn" {
		t.Errorf("expected field=dsn, got %q", changes[0].Field)
	}
}
