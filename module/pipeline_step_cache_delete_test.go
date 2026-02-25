package module

import (
	"context"
	"errors"
	"testing"
)

func TestCacheDeleteStep_Basic(t *testing.T) {
	cm := newMockCacheModule()
	cm.data["user:42"] = "cached"
	app := mockAppWithCache("cache", cm)

	factory := NewCacheDeleteStepFactory()
	step, err := factory("del-user", map[string]any{
		"cache": "cache",
		"key":   "user:{{.user_id}}",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"user_id": "42"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["deleted"] != true {
		t.Errorf("expected deleted=true, got %v", result.Output["deleted"])
	}
	if _, exists := cm.data["user:42"]; exists {
		t.Error("expected key to be removed from mock cache")
	}
}

func TestCacheDeleteStep_DeleteError(t *testing.T) {
	cm := newMockCacheModule()
	cm.deleteErr = errors.New("delete failed")
	app := mockAppWithCache("cache", cm)

	factory := NewCacheDeleteStepFactory()
	step, err := factory("del-err", map[string]any{
		"cache": "cache",
		"key":   "k",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error from underlying Delete")
	}
}

func TestCacheDeleteStep_MissingCache(t *testing.T) {
	factory := NewCacheDeleteStepFactory()
	_, err := factory("bad", map[string]any{"key": "k"}, nil)
	if err == nil {
		t.Fatal("expected error for missing cache")
	}
}

func TestCacheDeleteStep_MissingKey(t *testing.T) {
	factory := NewCacheDeleteStepFactory()
	_, err := factory("bad", map[string]any{"cache": "c"}, nil)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestCacheDeleteStep_ServiceNotFound(t *testing.T) {
	app := NewMockApplication()
	factory := NewCacheDeleteStepFactory()
	step, err := factory("del-missing-svc", map[string]any{
		"cache": "nonexistent",
		"key":   "k",
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

func TestCacheDeleteStep_ServiceWrongType(t *testing.T) {
	app := NewMockApplication()
	app.Services["cache"] = "not-a-cache"

	factory := NewCacheDeleteStepFactory()
	step, err := factory("del-wrong-type", map[string]any{
		"cache": "cache",
		"key":   "k",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for wrong service type")
	}
}
