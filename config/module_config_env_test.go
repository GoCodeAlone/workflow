package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestModuleConfig_UnmarshalEnvironments(t *testing.T) {
	const src = `
name: bmw-database
type: infra.database
config:
  size: db-s-1vcpu-1gb
environments:
  staging:
    config:
      size: db-s-1vcpu-1gb
  prod:
    config:
      size: db-s-2vcpu-4gb
`
	var m ModuleConfig
	if err := yaml.Unmarshal([]byte(src), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := len(m.Environments); got != 2 {
		t.Fatalf("want 2 environments, got %d", got)
	}
	if m.Environments["prod"].Config["size"] != "db-s-2vcpu-4gb" {
		t.Fatalf("prod size override not applied: %+v", m.Environments["prod"].Config)
	}
}
