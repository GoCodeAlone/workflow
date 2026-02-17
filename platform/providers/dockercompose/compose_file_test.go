package dockercompose

import (
	"strings"
	"testing"
)

func TestNewComposeFile(t *testing.T) {
	cf := NewComposeFile()
	if cf.Services == nil {
		t.Error("expected Services to be initialized")
	}
	if cf.Networks == nil {
		t.Error("expected Networks to be initialized")
	}
	if cf.Volumes == nil {
		t.Error("expected Volumes to be initialized")
	}
}

func TestComposeFileMarshalYAMLBasicService(t *testing.T) {
	cf := NewComposeFile()
	cf.AddService("web", &ComposeService{
		Image: "nginx:latest",
		Ports: []string{"8080:80"},
	})

	yaml, err := cf.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML failed: %v", err)
	}

	assertContains(t, yaml, "services:")
	assertContains(t, yaml, "web:")
	assertContains(t, yaml, "image: nginx:latest")
	assertContains(t, yaml, "\"8080:80\"")
}

func TestComposeFileMarshalYAMLFullService(t *testing.T) {
	cf := NewComposeFile()
	cf.AddService("app", &ComposeService{
		Image:         "myapp:v1",
		ContainerName: "my-app",
		Command:       "./start.sh",
		Restart:       "unless-stopped",
		Ports:         []string{"3000:3000"},
		Environment: map[string]string{
			"NODE_ENV": "production",
			"PORT":     "3000",
		},
		Volumes:   []string{"app-data:/data"},
		Networks:  []string{"app-net"},
		DependsOn: []string{"db", "cache"},
		Deploy: &DeployConfig{
			Replicas: 3,
			Resources: &ResourcesConfig{
				Limits: &ResourceSpec{
					CPUs:   "0.5",
					Memory: "512M",
				},
			},
		},
		Healthcheck: &HealthcheckConfig{
			Test:        []string{"CMD-SHELL", "curl -f http://localhost:3000/health || exit 1"},
			Interval:    "30s",
			Timeout:     "10s",
			Retries:     3,
			StartPeriod: "15s",
		},
		Labels: map[string]string{
			"com.example.app": "myapp",
		},
	})

	yaml, err := cf.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML failed: %v", err)
	}

	assertContains(t, yaml, "image: myapp:v1")
	assertContains(t, yaml, "container_name: my-app")
	assertContains(t, yaml, "command: ./start.sh")
	assertContains(t, yaml, "restart: unless-stopped")
	assertContains(t, yaml, "replicas: 3")
	assertContains(t, yaml, "cpus: \"0.5\"")
	assertContains(t, yaml, "memory: 512M")
	assertContains(t, yaml, "NODE_ENV")
	assertContains(t, yaml, "healthcheck:")
	assertContains(t, yaml, "interval: 30s")
	assertContains(t, yaml, "retries: 3")
	assertContains(t, yaml, "depends_on:")
	assertContains(t, yaml, "- db")
	assertContains(t, yaml, "- cache")
	assertContains(t, yaml, "labels:")
	assertContains(t, yaml, "com.example.app")
}

func TestComposeFileMarshalYAMLNetwork(t *testing.T) {
	cf := NewComposeFile()
	cf.AddNetwork("app-net", &ComposeNetwork{
		Driver: "bridge",
		IPAM: &IPAMConfig{
			Config: []IPAMPoolConfig{
				{Subnet: "172.28.0.0/16"},
			},
		},
		Labels: map[string]string{
			"env": "dev",
		},
	})

	yaml, err := cf.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML failed: %v", err)
	}

	assertContains(t, yaml, "networks:")
	assertContains(t, yaml, "app-net:")
	assertContains(t, yaml, "driver: bridge")
	assertContains(t, yaml, "subnet: 172.28.0.0/16")
}

func TestComposeFileMarshalYAMLVolume(t *testing.T) {
	cf := NewComposeFile()
	cf.AddVolume("db-data", &ComposeVolume{
		Driver: "local",
		Labels: map[string]string{
			"backup": "true",
		},
	})

	yaml, err := cf.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML failed: %v", err)
	}

	assertContains(t, yaml, "volumes:")
	assertContains(t, yaml, "db-data:")
	assertContains(t, yaml, "driver: local")
}

func TestComposeFileMarshalYAMLMultipleServices(t *testing.T) {
	cf := NewComposeFile()
	cf.AddService("api", &ComposeService{
		Image: "api:v1",
		Ports: []string{"8080:8080"},
	})
	cf.AddService("worker", &ComposeService{
		Image:     "worker:v1",
		DependsOn: []string{"api"},
	})
	cf.AddNetwork("default", &ComposeNetwork{
		Driver: "bridge",
	})
	cf.AddVolume("shared", &ComposeVolume{
		Driver: "local",
	})

	yaml, err := cf.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML failed: %v", err)
	}

	// Services should be in alphabetical order
	apiIdx := strings.Index(yaml, "api:")
	workerIdx := strings.Index(yaml, "worker:")
	if apiIdx >= workerIdx {
		t.Error("expected 'api' before 'worker' in sorted output")
	}

	assertContains(t, yaml, "services:")
	assertContains(t, yaml, "networks:")
	assertContains(t, yaml, "volumes:")
}

func TestComposeFileExternalNetwork(t *testing.T) {
	cf := NewComposeFile()
	cf.AddNetwork("external-net", &ComposeNetwork{
		External: true,
	})

	yaml, err := cf.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML failed: %v", err)
	}

	assertContains(t, yaml, "external: true")
}

func TestComposeFileDriverOpts(t *testing.T) {
	cf := NewComposeFile()
	cf.AddNetwork("overlay-net", &ComposeNetwork{
		Driver: "overlay",
		DriverOpts: map[string]string{
			"encrypted": "true",
		},
	})

	yaml, err := cf.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML failed: %v", err)
	}

	assertContains(t, yaml, "driver_opts:")
	assertContains(t, yaml, "encrypted")
}

func TestComposeFileEmptyFile(t *testing.T) {
	cf := NewComposeFile()
	yaml, err := cf.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML failed: %v", err)
	}

	if yaml != "" {
		t.Errorf("expected empty output for empty compose file, got %q", yaml)
	}
}

func TestComposeFileResourceReservations(t *testing.T) {
	cf := NewComposeFile()
	cf.AddService("heavy", &ComposeService{
		Image: "heavy:latest",
		Deploy: &DeployConfig{
			Resources: &ResourcesConfig{
				Limits: &ResourceSpec{
					CPUs:   "2",
					Memory: "4G",
				},
				Reservations: &ResourceSpec{
					CPUs:   "1",
					Memory: "2G",
				},
			},
		},
	})

	yaml, err := cf.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML failed: %v", err)
	}

	assertContains(t, yaml, "limits:")
	assertContains(t, yaml, "reservations:")
	assertContains(t, yaml, "memory: 4G")
	assertContains(t, yaml, "memory: 2G")
}

func assertContains(t *testing.T, content, expected string) {
	t.Helper()
	if !strings.Contains(content, expected) {
		t.Errorf("expected output to contain %q, got:\n%s", expected, content)
	}
}
