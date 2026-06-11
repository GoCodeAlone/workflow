package config

import (
	"reflect"
	"testing"
)

func TestDesiredEnvironmentNamesCollectsConfiguredEnvironmentDeclarations(t *testing.T) {
	cfg := &WorkflowConfig{
		Environments: map[string]*EnvironmentConfig{
			"staging":    {},
			"production": {},
		},
		CI: &CIConfig{
			Deploy: &CIDeployConfig{
				Environments: map[string]*CIDeployEnvironment{
					"preview": {},
					"staging": {},
				},
			},
		},
		Platform: map[string]any{
			"environment": "canary",
		},
		SecretStores: map[string]*SecretStoreConfig{
			"github": {
				Provider: "github",
				Config: map[string]any{
					"environment": "production",
				},
			},
		},
	}

	got := DesiredEnvironmentNames(cfg)
	want := []string{"canary", "preview", "production", "staging"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DesiredEnvironmentNames = %#v, want %#v", got, want)
	}
}

func TestDesiredEnvironmentNamesSkipsRuntimePlaceholders(t *testing.T) {
	cfg := &WorkflowConfig{
		Platform: map[string]any{
			"environment": "${WORKFLOW_ENV}",
		},
		SecretStores: map[string]*SecretStoreConfig{
			"github": {
				Provider: "github",
				Config: map[string]any{
					"environment": "$DEPLOY_ENV",
				},
			},
		},
	}

	if got := DesiredEnvironmentNames(cfg); len(got) != 0 {
		t.Fatalf("DesiredEnvironmentNames = %#v, want empty", got)
	}
}
