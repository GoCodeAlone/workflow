package handlers

import (
	"testing"
)

func TestNewServiceRegistryAdapter(t *testing.T) {
	app := CreateMockApplication()
	adapter := NewServiceRegistryAdapter(app)
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
}

func TestServiceRegistryAdapter_GetService(t *testing.T) {
	app := CreateMockApplication()
	adapter := NewServiceRegistryAdapter(app)
	var dest any
	err := adapter.GetService("test", &dest)
	if err != nil {
		t.Fatalf("GetService failed: %v", err)
	}
}

func TestServiceRegistryAdapter_RegisterService(t *testing.T) {
	app := CreateMockApplication()
	adapter := NewServiceRegistryAdapter(app)
	err := adapter.RegisterService("test", "value")
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}
}

func TestServiceRegistryAdapter_SvcRegistry(t *testing.T) {
	app := NewTestServiceRegistry()
	app.services["key"] = "val"
	adapter := NewServiceRegistryAdapter(app)
	reg := adapter.SvcRegistry()
	if reg["key"] != "val" {
		t.Errorf("expected key=val, got %v", reg["key"])
	}
}

func TestServiceRegistryAdapter_Init(t *testing.T) {
	app := CreateMockApplication()
	adapter := NewServiceRegistryAdapter(app)
	err := adapter.Init()
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}
