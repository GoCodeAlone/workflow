package config

import (
	"strings"
	"testing"
)

func TestValidate_ContainerMethod(t *testing.T) {
	t.Run("dockerfile method requires dockerfile field", func(t *testing.T) {
		c := &CIConfig{Build: &CIBuildConfig{
			Containers: []CIContainerTarget{
				{Name: "img", Method: "dockerfile"},
			},
		}}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "dockerfile") {
			t.Errorf("expected dockerfile required error, got %v", err)
		}
	})

	t.Run("ko method requires ko_package", func(t *testing.T) {
		c := &CIConfig{Build: &CIBuildConfig{
			Containers: []CIContainerTarget{
				{Name: "img", Method: "ko"},
			},
		}}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "ko_package") {
			t.Errorf("expected ko_package required error, got %v", err)
		}
	})

	t.Run("unknown method rejected", func(t *testing.T) {
		c := &CIConfig{Build: &CIBuildConfig{
			Containers: []CIContainerTarget{
				{Name: "img", Method: "buildpacks", Dockerfile: "Dockerfile"},
			},
		}}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "method") {
			t.Errorf("expected unknown method error, got %v", err)
		}
	})

	t.Run("empty method defaults to dockerfile (no error)", func(t *testing.T) {
		c := &CIConfig{Build: &CIBuildConfig{
			Containers: []CIContainerTarget{
				{Name: "img", Method: "", Dockerfile: "Dockerfile"},
			},
		}}
		if err := c.Validate(); err != nil {
			t.Errorf("unexpected error for empty method: %v", err)
		}
	})
}

func TestValidate_Registry(t *testing.T) {
	t.Run("duplicate registry names rejected", func(t *testing.T) {
		c := &CIConfig{
			Registries: []CIRegistry{
				{Name: "docr", Type: "do", Path: "registry.digitalocean.com/x"},
				{Name: "docr", Type: "ghcr", Path: "ghcr.io/x"},
			},
		}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Errorf("expected duplicate registry name error, got %v", err)
		}
	})

	t.Run("unknown registry type rejected", func(t *testing.T) {
		c := &CIConfig{
			Registries: []CIRegistry{
				{Name: "x", Type: "quay", Path: "quay.io/x"},
			},
		}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "type") {
			t.Errorf("expected unknown type error, got %v", err)
		}
	})

	t.Run("push_to references undeclared registry", func(t *testing.T) {
		c := &CIConfig{
			Registries: []CIRegistry{
				{Name: "docr", Type: "do", Path: "registry.digitalocean.com/x"},
			},
			Build: &CIBuildConfig{
				Containers: []CIContainerTarget{
					{Name: "img", Method: "dockerfile", Dockerfile: "Dockerfile", PushTo: []string{"docr", "ghcr"}},
				},
			},
		}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "push_to") {
			t.Errorf("expected push_to undeclared error, got %v", err)
		}
	})

	t.Run("valid registry passes", func(t *testing.T) {
		c := &CIConfig{
			Registries: []CIRegistry{
				{Name: "docr", Type: "do", Path: "registry.digitalocean.com/x"},
			},
			Build: &CIBuildConfig{
				Containers: []CIContainerTarget{
					{Name: "img", Method: "dockerfile", Dockerfile: "Dockerfile", PushTo: []string{"docr"}},
				},
			},
		}
		if err := c.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestValidate_Retention(t *testing.T) {
	t.Run("keep_latest must be ≥ 1", func(t *testing.T) {
		c := &CIConfig{
			Registries: []CIRegistry{
				{Name: "docr", Type: "do", Path: "x", Retention: &CIRegistryRetention{KeepLatest: 0}},
			},
		}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "keep_latest") {
			t.Errorf("expected keep_latest error, got %v", err)
		}
	})

	t.Run("invalid untagged_ttl rejected", func(t *testing.T) {
		c := &CIConfig{
			Registries: []CIRegistry{
				{Name: "docr", Type: "do", Path: "x", Retention: &CIRegistryRetention{KeepLatest: 10, UntaggedTTL: "notaduration"}},
			},
		}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "untagged_ttl") {
			t.Errorf("expected untagged_ttl error, got %v", err)
		}
	})
}

func TestValidate_CITarget(t *testing.T) {
	t.Run("unknown target type rejected", func(t *testing.T) {
		c := &CIConfig{Build: &CIBuildConfig{
			Targets: []CITarget{
				{Name: "t", Type: "cobol", Path: "./cmd"},
			},
		}}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "type") {
			t.Errorf("expected unknown type error, got %v", err)
		}
	})

	t.Run("known types pass", func(t *testing.T) {
		for _, typ := range []string{"go", "nodejs", "rust", "python", "custom"} {
			c := &CIConfig{Build: &CIBuildConfig{
				Targets: []CITarget{
					{Name: "t", Type: typ, Path: "./cmd"},
				},
			}}
			if err := c.Validate(); err != nil {
				t.Errorf("type %q should be valid, got: %v", typ, err)
			}
		}
	})
}
