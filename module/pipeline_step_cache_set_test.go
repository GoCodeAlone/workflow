package module

import (
	"context"
	"errors"
	"testing"
)

func TestCacheSetStep_Basic(t *testing.T) {
	cm := newMockCacheModule()
	app := mockAppWithCache("cache", cm)

	factory := NewCacheSetStepFactory()
	step, err := factory("set-user", map[string]any{
		"cache": "cache",
		"key":   "user:{{.user_id}}",
		"value": "{{.profile}}",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"user_id": "42",
		"profile": `{"name":"Alice"}`,
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["cached"] != true {
		t.Errorf("expected cached=true, got %v", result.Output["cached"])
	}
	if cm.data["user:42"] != `{"name":"Alice"}` {
		t.Errorf("expected stored value, got %v", cm.data["user:42"])
	}
}

func TestCacheSetStep_WithTTL(t *testing.T) {
	cm := newMockCacheModule()
	app := mockAppWithCache("cache", cm)

	factory := NewCacheSetStepFactory()
	step, err := factory("set-ttl", map[string]any{
		"cache": "cache",
		"key":   "k",
		"value": "v",
		"ttl":   "30m",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["cached"] != true {
		t.Errorf("expected cached=true")
	}
	if cm.data["k"] != "v" {
		t.Errorf("expected stored value %q, got %q", "v", cm.data["k"])
	}
}

func TestCacheSetStep_InvalidTTL(t *testing.T) {
	factory := NewCacheSetStepFactory()
	_, err := factory("bad-ttl", map[string]any{
		"cache": "cache",
		"key":   "k",
		"value": "v",
		"ttl":   "notaduration",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid TTL")
	}
}

func TestCacheSetStep_SetError(t *testing.T) {
	cm := newMockCacheModule()
	cm.setErr = errors.New("redis unavailable")
	app := mockAppWithCache("cache", cm)

	factory := NewCacheSetStepFactory()
	step, err := factory("set-err", map[string]any{
		"cache": "cache",
		"key":   "k",
		"value": "v",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error from underlying Set")
	}
}

func TestCacheSetStep_MissingCache(t *testing.T) {
	factory := NewCacheSetStepFactory()
	_, err := factory("bad", map[string]any{"key": "k", "value": "v"}, nil)
	if err == nil {
		t.Fatal("expected error for missing cache")
	}
}

func TestCacheSetStep_MissingKey(t *testing.T) {
	factory := NewCacheSetStepFactory()
	_, err := factory("bad", map[string]any{"cache": "c", "value": "v"}, nil)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestCacheSetStep_MissingValue(t *testing.T) {
	factory := NewCacheSetStepFactory()
	_, err := factory("bad", map[string]any{"cache": "c", "key": "k"}, nil)
	if err == nil {
		t.Fatal("expected error for missing value")
	}
}

func TestCacheSetStep_ServiceNotFound(t *testing.T) {
	app := NewMockApplication()
	factory := NewCacheSetStepFactory()
	step, err := factory("set-missing-svc", map[string]any{
		"cache": "nonexistent",
		"key":   "k",
		"value": "v",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for missing service")
	}
}
