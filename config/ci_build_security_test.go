package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCIBuildSecurity_Unmarshal(t *testing.T) {
	src := `
ci:
  build:
    security:
      hardened: true
      sbom: true
      provenance: slsa-3
      sign: true
      non_root: true
      base_image_policy:
        allow_prefixes:
          - gcr.io/distroless
          - alpine
        deny_prefixes:
          - scratch
`
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.CI == nil || cfg.CI.Build == nil || cfg.CI.Build.Security == nil {
		t.Fatalf("security missing")
	}
	s := cfg.CI.Build.Security
	if !s.Hardened || !s.SBOM || s.Provenance != "slsa-3" || !s.Sign || !s.NonRoot {
		t.Fatalf("unexpected security: %+v", s)
	}
	if s.BaseImagePolicy == nil || len(s.BaseImagePolicy.AllowPrefixes) != 2 {
		t.Fatalf("base_image_policy missing: %+v", s.BaseImagePolicy)
	}
}

func TestCIBuildSecurity_ApplyDefaults_NilSecurity(t *testing.T) {
	var s *CIBuildSecurity
	result := s.ApplyDefaults()
	if !result.Hardened || !result.SBOM || result.Provenance != "slsa-3" || !result.NonRoot {
		t.Fatalf("nil security should default to hardened: %+v", result)
	}
}

func TestCIBuildSecurity_ApplyDefaults_PreservesExplicitFalse(t *testing.T) {
	s := &CIBuildSecurity{Hardened: false, SBOM: true}
	result := s.ApplyDefaults()
	if result.Hardened {
		t.Fatal("explicit Hardened=false should be preserved")
	}
	if !result.SBOM {
		t.Fatal("SBOM should be preserved")
	}
}
