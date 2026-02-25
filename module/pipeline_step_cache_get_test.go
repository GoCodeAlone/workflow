package module

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// mockCacheModule is an in-memory CacheModule for testing pipeline steps.
type mockCacheModule struct {
	data      map[string]string
	getErr    error
	setErr    error
	deleteErr error
}

func newMockCacheModule() *mockCacheModule {
	return &mockCacheModule{data: make(map[string]string)}
}

func (m *mockCacheModule) Get(_ context.Context, key string) (string, error) {
	if m.getErr != nil {
		return "", m.getErr
	}
	v, ok := m.data[key]
	if !ok {
		return "", redis.Nil
	}
	return v, nil
}

func (m *mockCacheModule) Set(_ context.Context, key, value string, _ time.Duration) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.data[key] = value
	return nil
}

func (m *mockCacheModule) Delete(_ context.Context, key string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.data, key)
	return nil
}

// mockAppWithCache creates a MockApplication with a CacheModule service registered.
func mockAppWithCache(name string, cm CacheModule) *MockApplication {
	app := NewMockApplication()
	app.Services[name] = cm
	return app
}

// ---- tests ----

func TestCacheGetStep_Hit(t *testing.T) {
	cm := newMockCacheModule()
	cm.data["user:42"] = `{"id":42}`
	app := mockAppWithCache("cache", cm)

	factory := NewCacheGetStepFactory()
	step, err := factory("get-user", map[string]any{
		"cache":  "cache",
		"key":    "user:{{.user_id}}",
		"output": "user_data",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"user_id": "42"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["user_data"] != `{"id":42}` {
		t.Errorf("expected user_data=%q, got %v", `{"id":42}`, result.Output["user_data"])
	}
	if result.Output["cache_hit"] != true {
		t.Errorf("expected cache_hit=true, got %v", result.Output["cache_hit"])
	}
}

func TestCacheGetStep_MissOK(t *testing.T) {
	cm := newMockCacheModule()
	app := mockAppWithCache("cache", cm)

	factory := NewCacheGetStepFactory()
	step, err := factory("get-user", map[string]any{
		"cache":   "cache",
		"key":     "user:99",
		"miss_ok": true,
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["value"] != "" {
		t.Errorf("expected empty value on miss, got %v", result.Output["value"])
	}
	if result.Output["cache_hit"] != false {
		t.Errorf("expected cache_hit=false on miss, got %v", result.Output["cache_hit"])
	}
}

func TestCacheGetStep_MissNotOK(t *testing.T) {
	cm := newMockCacheModule()
	app := mockAppWithCache("cache", cm)

	factory := NewCacheGetStepFactory()
	step, err := factory("get-user", map[string]any{
		"cache":   "cache",
		"key":     "user:99",
		"miss_ok": false,
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error on cache miss with miss_ok=false")
	}
}

func TestCacheGetStep_DefaultOutput(t *testing.T) {
	cm := newMockCacheModule()
	cm.data["thekey"] = "thevalue"
	app := mockAppWithCache("cache", cm)

	factory := NewCacheGetStepFactory()
	step, err := factory("get-val", map[string]any{
		"cache": "cache",
		"key":   "thekey",
		// output not set → default "value"
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["value"] != "thevalue" {
		t.Errorf("expected output[value]=%q, got %v", "thevalue", result.Output["value"])
	}
}

func TestCacheGetStep_GetError(t *testing.T) {
	cm := newMockCacheModule()
	cm.getErr = errors.New("connection refused")
	app := mockAppWithCache("cache", cm)

	factory := NewCacheGetStepFactory()
	step, err := factory("get-err", map[string]any{
		"cache": "cache",
		"key":   "k",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error from underlying Get")
	}
}

func TestCacheGetStep_MissingCache(t *testing.T) {
	factory := NewCacheGetStepFactory()
	_, err := factory("bad", map[string]any{"key": "k"}, nil)
	if err == nil {
		t.Fatal("expected error for missing cache")
	}
}

func TestCacheGetStep_MissingKey(t *testing.T) {
	factory := NewCacheGetStepFactory()
	_, err := factory("bad", map[string]any{"cache": "c"}, nil)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestCacheGetStep_ServiceNotFound(t *testing.T) {
	app := NewMockApplication()
	factory := NewCacheGetStepFactory()
	step, err := factory("get-missing-svc", map[string]any{
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

func TestCacheGetStep_ServiceWrongType(t *testing.T) {
	app := NewMockApplication()
	app.Services["cache"] = "not-a-cache-module"

	factory := NewCacheGetStepFactory()
	step, err := factory("get-wrong-type", map[string]any{
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
