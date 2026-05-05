package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCIRegistry_Unmarshal(t *testing.T) {
	src := `
ci:
  registries:
    - name: docr
      type: do
      path: registry.digitalocean.com/coredump-registry
      auth:
        env: DIGITALOCEAN_TOKEN
      retention:
        keep_latest: 20
        untagged_ttl: 168h
        schedule: "0 7 * * 0"
`
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.CI == nil || len(cfg.CI.Registries) != 1 {
		t.Fatalf("expected 1 registry, got %v", cfg.CI)
	}
	r := cfg.CI.Registries[0]
	if r.Name != "docr" || r.Type != "do" || r.Auth.Env != "DIGITALOCEAN_TOKEN" {
		t.Fatalf("unexpected registry: %+v", r)
	}
	if r.Retention == nil || r.Retention.KeepLatest != 20 {
		t.Fatalf("retention missing: %+v", r)
	}
}
