package main

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// TestDevIntegration_GenerateDevCompose verifies that generateDevCompose
// produces a valid docker-compose YAML from a fixture config.
func TestDevIntegration_GenerateDevCompose(t *testing.T) {
	t.Run("single service with postgres", func(t *testing.T) {
		cfg := &config.WorkflowConfig{
			Modules: []config.ModuleConfig{
				{Name: "db", Type: "database.postgres"},
				{Name: "server", Type: "http.server", Config: map[string]any{"port": 8080}},
			},
		}

		out, err := generateDevCompose(cfg)
		if err != nil {
			t.Fatalf("generateDevCompose returned error: %v", err)
		}

		// Must contain the postgres service.
		if !strings.Contains(out, "postgres:16") {
			t.Errorf("expected postgres:16 image in output, got:\n%s", out)
		}

		// Must contain the default app service.
		if !strings.Contains(out, "app:") || (!strings.Contains(out, "app:dev") && !strings.Contains(out, "build:")) {
			// either image: app:dev or build: context is acceptable
			if !strings.Contains(out, "app") {
				t.Errorf("expected app service in output, got:\n%s", out)
			}
		}

		// Must contain volume for postgres persistence.
		if !strings.Contains(out, "pgdata") {
			t.Errorf("expected pgdata volume in output, got:\n%s", out)
		}

		// Must declare services: section.
		if !strings.Contains(out, "services:") {
			t.Errorf("expected 'services:' key in output, got:\n%s", out)
		}
	})

	t.Run("multi-service with redis and nats", func(t *testing.T) {
		cfg := &config.WorkflowConfig{
			Modules: []config.ModuleConfig{
				{Name: "cache", Type: "cache.redis"},
				{Name: "mq", Type: "messaging.nats"},
			},
			Services: map[string]*config.ServiceConfig{
				"api": {
					Binary: "./cmd/api",
					Expose: []config.ExposeConfig{{Port: 8080, Protocol: "http"}},
				},
				"worker": {
					Binary: "./cmd/worker",
				},
			},
		}

		out, err := generateDevCompose(cfg)
		if err != nil {
			t.Fatalf("generateDevCompose returned error: %v", err)
		}

		// Infrastructure images must be present.
		if !strings.Contains(out, "redis:7-alpine") {
			t.Errorf("expected redis:7-alpine in output, got:\n%s", out)
		}
		if !strings.Contains(out, "nats:latest") {
			t.Errorf("expected nats:latest in output, got:\n%s", out)
		}

		// Both services must appear.
		if !strings.Contains(out, "api:") {
			t.Errorf("expected 'api' service in output, got:\n%s", out)
		}
		if !strings.Contains(out, "worker:") {
			t.Errorf("expected 'worker' service in output, got:\n%s", out)
		}
	})

	t.Run("port mappings match config", func(t *testing.T) {
		cfg := &config.WorkflowConfig{
			Modules: []config.ModuleConfig{
				{Name: "db", Type: "database.postgres"},
			},
			Services: map[string]*config.ServiceConfig{
				"api": {
					Expose: []config.ExposeConfig{
						{Port: 9090, Protocol: "http"},
						{Port: 9091, Protocol: "grpc"},
					},
				},
			},
		}

		out, err := generateDevCompose(cfg)
		if err != nil {
			t.Fatalf("generateDevCompose returned error: %v", err)
		}

		// Both exposed ports must appear as port mappings.
		if !strings.Contains(out, "9090:9090") {
			t.Errorf("expected port mapping 9090:9090 in output, got:\n%s", out)
		}
		if !strings.Contains(out, "9091:9091") {
			t.Errorf("expected port mapping 9091:9091 in output, got:\n%s", out)
		}
	})

	t.Run("empty config generates minimal app service", func(t *testing.T) {
		cfg := &config.WorkflowConfig{
			Modules: []config.ModuleConfig{},
		}

		out, err := generateDevCompose(cfg)
		if err != nil {
			t.Fatalf("generateDevCompose returned error: %v", err)
		}

		// At minimum there should be an 'app' service.
		if !strings.Contains(out, "app") {
			t.Errorf("expected 'app' service in minimal output, got:\n%s", out)
		}

		// There should be no postgres, redis, or nats since no modules declared.
		if strings.Contains(out, "postgres:") {
			t.Errorf("unexpected postgres image in minimal output, got:\n%s", out)
		}
	})

	t.Run("http server port detected for app service", func(t *testing.T) {
		cfg := &config.WorkflowConfig{
			Modules: []config.ModuleConfig{
				{Name: "server", Type: "http.server", Config: map[string]any{"port": 3000}},
			},
		}

		out, err := generateDevCompose(cfg)
		if err != nil {
			t.Fatalf("generateDevCompose returned error: %v", err)
		}

		// Port 3000 should be mapped for the app service.
		if !strings.Contains(out, "3000:3000") {
			t.Errorf("expected port mapping 3000:3000 in output, got:\n%s", out)
		}
	})
}
