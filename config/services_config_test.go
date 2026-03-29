package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestServicesConfig_ParseYAML(t *testing.T) {
	yamlStr := `
services:
  api:
    description: "Public API service"
    binary: ./cmd/api
    scaling:
      replicas: 2
      min: 1
      max: 10
      metric: cpu
      target: 70
    expose:
      - port: 8080
        protocol: http
      - port: 9090
        protocol: grpc
    plugins:
      - workflow-plugin-auth
  worker:
    description: "Background worker"
    binary: ./cmd/worker
    scaling:
      replicas: 1

mesh:
  transport: nats
  discovery: dns
  nats:
    url: nats://nats:4222
    clusterId: my-cluster
  routes:
    - from: api
      to: worker
      via: nats
      subject: tasks.process
    - from: api
      to: worker
      via: http
      endpoint: /internal/jobs
`
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(cfg.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(cfg.Services))
	}

	api := cfg.Services["api"]
	if api == nil {
		t.Fatal("api service missing")
	}
	if api.Description != "Public API service" {
		t.Errorf("expected 'Public API service', got %q", api.Description)
	}
	if api.Binary != "./cmd/api" {
		t.Errorf("expected './cmd/api', got %q", api.Binary)
	}
	if api.Scaling == nil {
		t.Fatal("api scaling missing")
	}
	if api.Scaling.Replicas != 2 {
		t.Errorf("expected 2 replicas, got %d", api.Scaling.Replicas)
	}
	if api.Scaling.Min != 1 {
		t.Errorf("expected min=1, got %d", api.Scaling.Min)
	}
	if api.Scaling.Max != 10 {
		t.Errorf("expected max=10, got %d", api.Scaling.Max)
	}
	if api.Scaling.Metric != "cpu" {
		t.Errorf("expected metric=cpu, got %q", api.Scaling.Metric)
	}
	if api.Scaling.Target != 70 {
		t.Errorf("expected target=70, got %d", api.Scaling.Target)
	}
	if len(api.Expose) != 2 {
		t.Fatalf("expected 2 expose configs, got %d", len(api.Expose))
	}
	if api.Expose[0].Port != 8080 {
		t.Errorf("expected port 8080, got %d", api.Expose[0].Port)
	}
	if api.Expose[0].Protocol != "http" {
		t.Errorf("expected protocol http, got %q", api.Expose[0].Protocol)
	}
	if len(api.Plugins) != 1 || api.Plugins[0] != "workflow-plugin-auth" {
		t.Errorf("unexpected plugins: %v", api.Plugins)
	}

	worker := cfg.Services["worker"]
	if worker == nil {
		t.Fatal("worker service missing")
	}
	if worker.Scaling.Replicas != 1 {
		t.Errorf("expected worker replicas=1, got %d", worker.Scaling.Replicas)
	}

	if cfg.Mesh == nil {
		t.Fatal("mesh section missing")
	}
	if cfg.Mesh.Transport != "nats" {
		t.Errorf("expected transport=nats, got %q", cfg.Mesh.Transport)
	}
	if cfg.Mesh.NATS == nil {
		t.Fatal("mesh.nats missing")
	}
	if cfg.Mesh.NATS.URL != "nats://nats:4222" {
		t.Errorf("expected nats url, got %q", cfg.Mesh.NATS.URL)
	}
	if cfg.Mesh.NATS.ClusterID != "my-cluster" {
		t.Errorf("expected clusterId=my-cluster, got %q", cfg.Mesh.NATS.ClusterID)
	}
	if len(cfg.Mesh.Routes) != 2 {
		t.Fatalf("expected 2 mesh routes, got %d", len(cfg.Mesh.Routes))
	}
	r0 := cfg.Mesh.Routes[0]
	if r0.From != "api" || r0.To != "worker" || r0.Via != "nats" {
		t.Errorf("unexpected route[0]: %+v", r0)
	}
	if r0.Subject != "tasks.process" {
		t.Errorf("expected subject=tasks.process, got %q", r0.Subject)
	}
	r1 := cfg.Mesh.Routes[1]
	if r1.Via != "http" || r1.Endpoint != "/internal/jobs" {
		t.Errorf("unexpected route[1]: %+v", r1)
	}
}
