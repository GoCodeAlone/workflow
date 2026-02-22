package module

import (
	"context"
	"testing"
)

func TestGCSStorageName(t *testing.T) {
	g := NewGCSStorage("gcs-test")
	if g.Name() != "gcs-test" {
		t.Errorf("expected name 'gcs-test', got %q", g.Name())
	}
}

func TestGCSStorageModuleInterface(t *testing.T) {
	g := NewGCSStorage("gcs-test")

	// Test Init
	app, _ := NewTestApplication()
	if err := g.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Test ProvidesServices
	services := g.ProvidesServices()
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Name != "gcs-test" {
		t.Errorf("expected service name 'gcs-test', got %q", services[0].Name)
	}

	// Test RequiresServices
	deps := g.RequiresServices()
	if len(deps) != 0 {
		t.Errorf("expected no dependencies, got %d", len(deps))
	}
}

func TestGCSStorageConfig(t *testing.T) {
	g := NewGCSStorage("gcs-test")

	g.SetBucket("my-bucket")
	if g.bucket != "my-bucket" {
		t.Errorf("expected bucket 'my-bucket', got %q", g.bucket)
	}

	g.SetProject("my-project")
	if g.project != "my-project" {
		t.Errorf("expected project 'my-project', got %q", g.project)
	}

	g.SetCredentialsFile("/path/to/creds.json")
	if g.credentialsFile != "/path/to/creds.json" {
		t.Errorf("expected credentialsFile '/path/to/creds.json', got %q", g.credentialsFile)
	}
}

func TestGCSStorageOperationsWithoutClient(t *testing.T) {
	g := NewGCSStorage("gcs-test")

	ctx := context.Background()

	// Operations should fail without Start
	if _, err := g.List(ctx, ""); err == nil {
		t.Error("List should fail without initialized client")
	}

	if _, err := g.Get(ctx, "key"); err == nil {
		t.Error("Get should fail without initialized client")
	}

	if err := g.Put(ctx, "key", nil); err == nil {
		t.Error("Put should fail without initialized client")
	}

	if err := g.Delete(ctx, "key"); err == nil {
		t.Error("Delete should fail without initialized client")
	}

	if _, err := g.Stat(ctx, "key"); err == nil {
		t.Error("Stat should fail without initialized client")
	}
}

func TestGCSStorageStop(t *testing.T) {
	g := NewGCSStorage("gcs-test")
	app, _ := NewTestApplication()
	_ = g.Init(app)

	// Stop without Start should be safe (no-op when client is nil)
	if err := g.Stop(context.Background()); err != nil {
		t.Fatalf("Stop without Start failed: %v", err)
	}
}

func TestGCSStorageMkdirAll(t *testing.T) {
	g := NewGCSStorage("gcs-test")

	// MkdirAll is a no-op for object storage
	if err := g.MkdirAll(context.Background(), "some/path"); err != nil {
		t.Fatalf("MkdirAll should be a no-op, got error: %v", err)
	}
}
