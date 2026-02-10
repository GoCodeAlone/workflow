package module

import (
	"context"
	"testing"
)

func TestS3StorageName(t *testing.T) {
	s := NewS3Storage("s3-test")
	if s.Name() != "s3-test" {
		t.Errorf("expected name 's3-test', got %q", s.Name())
	}
}

func TestS3StorageModuleInterface(t *testing.T) {
	s := NewS3Storage("s3-test")

	// Test Init
	app, _ := NewTestApplication()
	if err := s.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Test ProvidesServices
	services := s.ProvidesServices()
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Name != "s3-test" {
		t.Errorf("expected service name 's3-test', got %q", services[0].Name)
	}

	// Test RequiresServices
	deps := s.RequiresServices()
	if len(deps) != 0 {
		t.Errorf("expected no dependencies, got %d", len(deps))
	}
}

func TestS3StorageConfig(t *testing.T) {
	s := NewS3Storage("s3-test")

	// Test defaults
	if s.region != "us-east-1" {
		t.Errorf("expected default region 'us-east-1', got %q", s.region)
	}

	// Test setters
	s.SetBucket("my-bucket")
	if s.bucket != "my-bucket" {
		t.Errorf("expected bucket 'my-bucket', got %q", s.bucket)
	}

	s.SetRegion("eu-west-1")
	if s.region != "eu-west-1" {
		t.Errorf("expected region 'eu-west-1', got %q", s.region)
	}

	s.SetEndpoint("http://localhost:4566")
	if s.endpoint != "http://localhost:4566" {
		t.Errorf("expected endpoint 'http://localhost:4566', got %q", s.endpoint)
	}
}

func TestS3StorageOperationsWithoutClient(t *testing.T) {
	s := NewS3Storage("s3-test")

	ctx := context.Background()

	// Operations should fail without Start
	if err := s.PutObject(ctx, "key", nil); err == nil {
		t.Error("PutObject should fail without initialized client")
	}

	if _, err := s.GetObject(ctx, "key"); err == nil {
		t.Error("GetObject should fail without initialized client")
	}

	if err := s.DeleteObject(ctx, "key"); err == nil {
		t.Error("DeleteObject should fail without initialized client")
	}
}

func TestS3StorageStop(t *testing.T) {
	s := NewS3Storage("s3-test")
	app, _ := NewTestApplication()
	_ = s.Init(app)

	// Stop should be a no-op and not error
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}
