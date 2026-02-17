package dockercompose

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/platform"
)

func TestProviderName(t *testing.T) {
	p := NewProvider()
	if p.Name() != "docker-compose" {
		t.Errorf("expected name %q, got %q", "docker-compose", p.Name())
	}
}

func TestProviderVersion(t *testing.T) {
	p := NewProvider()
	if p.Version() != "0.1.0" {
		t.Errorf("expected version %q, got %q", "0.1.0", p.Version())
	}
}

func TestProviderInitialize(t *testing.T) {
	tmpDir := t.TempDir()
	mock := &MockExecutor{}

	p := NewProviderWithExecutor(mock)
	err := p.Initialize(context.Background(), map[string]any{
		"project_dir": tmpDir,
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if !p.initialized {
		t.Error("expected provider to be initialized")
	}

	if p.projectDir != tmpDir {
		t.Errorf("expected projectDir %q, got %q", tmpDir, p.projectDir)
	}

	// Verify state directory was created
	stateDir := filepath.Join(tmpDir, ".platform-state")
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		t.Errorf("expected state directory %q to exist", stateDir)
	}
}

func TestProviderInitializeFailsWithoutDocker(t *testing.T) {
	mock := &MockExecutor{
		IsAvailableFn: func(ctx context.Context) error {
			return context.DeadlineExceeded
		},
	}

	p := NewProviderWithExecutor(mock)
	err := p.Initialize(context.Background(), map[string]any{
		"project_dir": t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error when Docker is not available")
	}
}

func TestProviderCapabilities(t *testing.T) {
	p := NewProvider()
	caps := p.Capabilities()

	if len(caps) == 0 {
		t.Fatal("expected non-empty capabilities list")
	}

	// Verify key capability types are present
	capNames := make(map[string]bool)
	for _, c := range caps {
		capNames[c.Name] = true
	}

	required := []string{"container_runtime", "database", "message_queue", "network",
		"kubernetes_cluster", "load_balancer", "persistent_volume"}
	for _, name := range required {
		if !capNames[name] {
			t.Errorf("missing capability %q", name)
		}
	}
}

func TestProviderMapCapabilityBeforeInit(t *testing.T) {
	p := NewProviderWithExecutor(&MockExecutor{})

	_, err := p.MapCapability(context.Background(), platform.CapabilityDeclaration{
		Name: "test",
		Type: "container_runtime",
	}, nil)

	if err != platform.ErrProviderNotInitialized {
		t.Errorf("expected ErrProviderNotInitialized, got %v", err)
	}
}

func TestProviderMapCapabilityContainerRuntime(t *testing.T) {
	p := initTestProvider(t)

	plans, err := p.MapCapability(context.Background(), platform.CapabilityDeclaration{
		Name: "web-app",
		Type: "container_runtime",
		Properties: map[string]any{
			"image":    "myapp:latest",
			"replicas": 3,
			"memory":   "512M",
		},
	}, nil)

	if err != nil {
		t.Fatalf("MapCapability failed: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	if plans[0].ResourceType != "docker-compose.service" {
		t.Errorf("expected resource type %q, got %q", "docker-compose.service", plans[0].ResourceType)
	}
	if plans[0].Name != "web-app" {
		t.Errorf("expected name %q, got %q", "web-app", plans[0].Name)
	}
}

func TestProviderMapCapabilityDatabase(t *testing.T) {
	p := initTestProvider(t)

	plans, err := p.MapCapability(context.Background(), platform.CapabilityDeclaration{
		Name: "main-db",
		Type: "database",
		Properties: map[string]any{
			"engine":     "postgresql",
			"version":    "15",
			"storage_gb": 50,
		},
	}, nil)

	if err != nil {
		t.Fatalf("MapCapability failed: %v", err)
	}
	// Database produces a service + a volume
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans (service + volume), got %d", len(plans))
	}
	if plans[0].ResourceType != "docker-compose.service" {
		t.Errorf("expected first plan type %q, got %q", "docker-compose.service", plans[0].ResourceType)
	}
	if plans[1].ResourceType != "docker-compose.volume" {
		t.Errorf("expected second plan type %q, got %q", "docker-compose.volume", plans[1].ResourceType)
	}
}

func TestProviderMapCapabilityKubernetesCluster(t *testing.T) {
	p := initTestProvider(t)

	plans, err := p.MapCapability(context.Background(), platform.CapabilityDeclaration{
		Name: "primary-cluster",
		Type: "kubernetes_cluster",
		Properties: map[string]any{
			"version": "1.29",
		},
	}, nil)

	if err != nil {
		t.Fatalf("MapCapability failed: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	if plans[0].ResourceType != "docker-compose.stub" {
		t.Errorf("expected stub resource type, got %q", plans[0].ResourceType)
	}
}

func TestProviderMapCapabilityUnsupported(t *testing.T) {
	p := initTestProvider(t)

	_, err := p.MapCapability(context.Background(), platform.CapabilityDeclaration{
		Name: "test",
		Type: "lambda_function",
	}, nil)

	if err == nil {
		t.Fatal("expected error for unsupported capability")
	}
	var unsupported *platform.CapabilityUnsupportedError
	if ue, ok := err.(*platform.CapabilityUnsupportedError); !ok {
		t.Errorf("expected CapabilityUnsupportedError, got %T", err)
	} else {
		unsupported = ue
		_ = unsupported
	}
}

func TestProviderResourceDriver(t *testing.T) {
	p := initTestProvider(t)

	tests := []struct {
		resourceType string
		wantErr      bool
	}{
		{"docker-compose.service", false},
		{"docker-compose.network", false},
		{"docker-compose.volume", false},
		{"docker-compose.stub", false},
		{"docker-compose.nonexistent", true},
	}

	for _, tt := range tests {
		driver, err := p.ResourceDriver(tt.resourceType)
		if tt.wantErr {
			if err == nil {
				t.Errorf("expected error for resource type %q", tt.resourceType)
			}
		} else {
			if err != nil {
				t.Errorf("unexpected error for resource type %q: %v", tt.resourceType, err)
			}
			if driver.ResourceType() != tt.resourceType {
				t.Errorf("expected driver type %q, got %q", tt.resourceType, driver.ResourceType())
			}
		}
	}
}

func TestProviderCredentialBroker(t *testing.T) {
	p := NewProvider()
	if p.CredentialBroker() != nil {
		t.Error("Docker Compose provider should return nil credential broker")
	}
}

func TestProviderHealthy(t *testing.T) {
	mock := &MockExecutor{}
	p := NewProviderWithExecutor(mock)

	err := p.Healthy(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProviderGenerateComposeFile(t *testing.T) {
	p := initTestProvider(t)

	plans := []platform.ResourcePlan{
		{
			ResourceType: "docker-compose.service",
			Name:         "web",
			Properties: map[string]any{
				"image":    "nginx:latest",
				"replicas": 2,
				"ports":    []map[string]any{{"container_port": 80, "host_port": 8080}},
			},
		},
		{
			ResourceType: "docker-compose.network",
			Name:         "app-net",
			Properties: map[string]any{
				"driver": "bridge",
			},
		},
		{
			ResourceType: "docker-compose.volume",
			Name:         "data-vol",
			Properties: map[string]any{
				"driver": "local",
			},
		},
	}

	cf, err := p.GenerateComposeFile(plans)
	if err != nil {
		t.Fatalf("GenerateComposeFile failed: %v", err)
	}

	if len(cf.Services) != 1 {
		t.Errorf("expected 1 service, got %d", len(cf.Services))
	}
	if len(cf.Networks) != 1 {
		t.Errorf("expected 1 network, got %d", len(cf.Networks))
	}
	if len(cf.Volumes) != 1 {
		t.Errorf("expected 1 volume, got %d", len(cf.Volumes))
	}
}

func TestProviderWriteComposeFile(t *testing.T) {
	p := initTestProvider(t)

	cf := NewComposeFile()
	cf.AddService("web", &ComposeService{
		Image: "nginx:latest",
		Ports: []string{"8080:80"},
	})

	err := p.WriteComposeFile(cf)
	if err != nil {
		t.Fatalf("WriteComposeFile failed: %v", err)
	}

	// Verify file was written
	path := p.ComposeFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read compose file: %v", err)
	}
	content := string(data)
	if len(content) == 0 {
		t.Error("compose file is empty")
	}
	if !containsImpl(content, "nginx:latest") {
		t.Error("compose file does not contain expected image")
	}
}

func TestProviderFidelityReports(t *testing.T) {
	p := initTestProvider(t)

	decls := []platform.CapabilityDeclaration{
		{Name: "cluster", Type: "kubernetes_cluster"},
		{Name: "web", Type: "container_runtime", Properties: map[string]any{"ingress": map[string]any{}, "health_check": map[string]any{}}},
		{Name: "db", Type: "database", Properties: map[string]any{"multi_az": true}},
		{Name: "vpc", Type: "network", Properties: map[string]any{"availability_zones": 3}},
		{Name: "ns", Type: "namespace"},
	}

	reports := p.FidelityReports(decls)
	if len(reports) == 0 {
		t.Error("expected fidelity reports for Docker Compose provider")
	}

	// Verify kubernetes_cluster produces a stub report
	foundK8s := false
	for _, r := range reports {
		if r.Capability == "kubernetes_cluster" {
			foundK8s = true
			if r.Fidelity != platform.FidelityStub {
				t.Errorf("expected k8s fidelity %q, got %q", platform.FidelityStub, r.Fidelity)
			}
		}
	}
	if !foundK8s {
		t.Error("expected fidelity report for kubernetes_cluster")
	}
}

func TestProviderUpDown(t *testing.T) {
	mock := &MockExecutor{}
	p := NewProviderWithExecutor(mock)

	err := p.Initialize(context.Background(), map[string]any{
		"project_dir": t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	_, err = p.Up(context.Background())
	if err != nil {
		t.Fatalf("Up failed: %v", err)
	}

	_, err = p.Down(context.Background())
	if err != nil {
		t.Fatalf("Down failed: %v", err)
	}

	// Verify Up and Down were called
	upCalled := false
	downCalled := false
	for _, call := range mock.Calls {
		if call.Method == "Up" {
			upCalled = true
		}
		if call.Method == "Down" {
			downCalled = true
		}
	}
	if !upCalled {
		t.Error("expected Up to be called on executor")
	}
	if !downCalled {
		t.Error("expected Down to be called on executor")
	}
}

func TestProviderStateStore(t *testing.T) {
	p := initTestProvider(t)

	store := p.StateStore()
	if store == nil {
		t.Fatal("expected non-nil state store")
	}

	// Test save and read
	ctx := context.Background()
	output := &platform.ResourceOutput{
		Name:         "test-resource",
		Type:         "container_runtime",
		ProviderType: "docker-compose.service",
		Status:       platform.ResourceStatusActive,
		Properties:   map[string]any{"image": "nginx:latest"},
	}

	err := store.SaveResource(ctx, "acme/dev", output)
	if err != nil {
		t.Fatalf("SaveResource failed: %v", err)
	}

	got, err := store.GetResource(ctx, "acme/dev", "test-resource")
	if err != nil {
		t.Fatalf("GetResource failed: %v", err)
	}
	if got.Name != "test-resource" {
		t.Errorf("expected name %q, got %q", "test-resource", got.Name)
	}
}

func initTestProvider(t *testing.T) *DockerComposeProvider {
	t.Helper()
	tmpDir := t.TempDir()
	mock := &MockExecutor{}

	p := NewProviderWithExecutor(mock)
	err := p.Initialize(context.Background(), map[string]any{
		"project_dir": tmpDir,
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	return p
}
