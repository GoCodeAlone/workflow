package handlers

import (
	"testing"
)

func TestNew(t *testing.T) {
	app := CreateMockApplication()
	helper := New(app)
	if helper == nil {
		t.Fatal("expected non-nil helper")
	}
}

func TestServiceHelper_Service(t *testing.T) {
	app := CreateMockApplication()
	helper := New(app)
	svc := helper.Service("nonexistent")
	if svc != nil {
		t.Errorf("expected nil for nonexistent service, got %v", svc)
	}
}

func TestServiceHelper_Services(t *testing.T) {
	app := NewTestServiceRegistry()
	app.services["a"] = 1
	helper := New(app)
	svcs := helper.Services()
	if len(svcs) != 1 {
		t.Errorf("expected 1 service, got %d", len(svcs))
	}
}

func TestServiceHelper_GetService(t *testing.T) {
	app := CreateMockApplication()
	helper := New(app)
	var dest any
	err := helper.GetService("something", &dest)
	if err != nil {
		t.Fatalf("GetService failed: %v", err)
	}
}

func TestServiceHelper_RegisterService(t *testing.T) {
	app := CreateMockApplication()
	helper := New(app)
	err := helper.RegisterService("test-svc", "value")
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}
}

func TestServiceHelper_SvcRegistry(t *testing.T) {
	app := NewTestServiceRegistry()
	app.services["x"] = 42
	helper := New(app)
	reg := helper.SvcRegistry()
	if reg["x"] != 42 {
		t.Errorf("expected x=42, got %v", reg["x"])
	}
}

func TestServiceHelper_Init(t *testing.T) {
	app := CreateMockApplication()
	helper := New(app)
	err := helper.Init()
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestGetServiceHelper(t *testing.T) {
	app := CreateMockApplication()
	helper := GetServiceHelper(app)
	if helper == nil {
		t.Fatal("expected non-nil helper")
	}
}

func TestGetEventProcessor(t *testing.T) {
	app := CreateMockApplication()
	proc := GetEventProcessor(app)
	if proc != nil {
		t.Errorf("expected nil, got %v", proc)
	}
}

func TestFixEventHandlerGetService(t *testing.T) {
	app := CreateMockApplication()
	svc := FixEventHandlerGetService(app, "test")
	if svc != nil {
		t.Errorf("expected nil, got %v", svc)
	}
}

func TestFixHTTPHandlerService(t *testing.T) {
	app := CreateMockApplication()
	svc := FixHTTPHandlerService(app, "test")
	if svc != nil {
		t.Errorf("expected nil, got %v", svc)
	}
}

func TestFixMessagingHandlerServices(t *testing.T) {
	app := CreateMockApplication()
	svcs := FixMessagingHandlerServices(app)
	if svcs == nil {
		t.Fatal("expected non-nil map")
	}
}
