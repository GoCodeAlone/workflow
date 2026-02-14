package main

import (
	"flag"
	"os"
	"testing"
)

func TestEnvOrFlag(t *testing.T) {
	t.Run("returns env when set", func(t *testing.T) {
		t.Setenv("TEST_ENV_OR_FLAG", "from-env")
		flagVal := "from-flag"
		got := envOrFlag("TEST_ENV_OR_FLAG", &flagVal)
		if got != "from-env" {
			t.Errorf("envOrFlag = %q, want %q", got, "from-env")
		}
	})

	t.Run("returns flag when env not set", func(t *testing.T) {
		flagVal := "from-flag"
		got := envOrFlag("UNSET_ENV_VAR_XYZ", &flagVal)
		if got != "from-flag" {
			t.Errorf("envOrFlag = %q, want %q", got, "from-flag")
		}
	})

	t.Run("returns empty when both unset", func(t *testing.T) {
		got := envOrFlag("UNSET_ENV_VAR_XYZ", nil)
		if got != "" {
			t.Errorf("envOrFlag = %q, want empty", got)
		}
	})
}

func TestApplyEnvOverrides(t *testing.T) {
	// Save and restore original flag values after test.
	origConfig := *configFile
	origAddr := *addr
	origAnthropicKey := *anthropicKey
	origAnthropicModel := *anthropicModel
	origJwtSecret := *jwtSecret
	t.Cleanup(func() {
		*configFile = origConfig
		*addr = origAddr
		*anthropicKey = origAnthropicKey
		*anthropicModel = origAnthropicModel
		*jwtSecret = origJwtSecret
	})

	t.Run("WORKFLOW_CONFIG sets config flag", func(t *testing.T) {
		*configFile = ""
		t.Setenv("WORKFLOW_CONFIG", "/etc/workflow/test.yaml")
		applyEnvOverrides()
		if *configFile != "/etc/workflow/test.yaml" {
			t.Errorf("configFile = %q, want %q", *configFile, "/etc/workflow/test.yaml")
		}
	})

	t.Run("WORKFLOW_ADDR sets addr flag", func(t *testing.T) {
		*addr = ":8080"
		t.Setenv("WORKFLOW_ADDR", ":9090")
		applyEnvOverrides()
		if *addr != ":9090" {
			t.Errorf("addr = %q, want %q", *addr, ":9090")
		}
	})

	t.Run("WORKFLOW_AI_API_KEY sets anthropic-key flag", func(t *testing.T) {
		*anthropicKey = ""
		t.Setenv("WORKFLOW_AI_API_KEY", "sk-test-key")
		applyEnvOverrides()
		if *anthropicKey != "sk-test-key" {
			t.Errorf("anthropicKey = %q, want %q", *anthropicKey, "sk-test-key")
		}
	})

	t.Run("WORKFLOW_AI_MODEL sets anthropic-model flag", func(t *testing.T) {
		*anthropicModel = ""
		t.Setenv("WORKFLOW_AI_MODEL", "claude-3-opus")
		applyEnvOverrides()
		if *anthropicModel != "claude-3-opus" {
			t.Errorf("anthropicModel = %q, want %q", *anthropicModel, "claude-3-opus")
		}
	})

	t.Run("WORKFLOW_JWT_SECRET sets jwt-secret flag", func(t *testing.T) {
		*jwtSecret = ""
		t.Setenv("WORKFLOW_JWT_SECRET", "my-secret")
		applyEnvOverrides()
		if *jwtSecret != "my-secret" {
			t.Errorf("jwtSecret = %q, want %q", *jwtSecret, "my-secret")
		}
	})

	t.Run("explicit flag not overridden by env", func(t *testing.T) {
		// Use flag.Set so the flag appears in the "visited" set, which
		// is what happens when the user passes -addr on the command line.
		_ = flag.Set("addr", ":7777")
		t.Setenv("WORKFLOW_ADDR", ":9999")

		applyEnvOverrides()
		if *addr != ":7777" {
			t.Errorf("addr = %q, want %q (explicit flag should not be overridden by env)", *addr, ":7777")
		}
	})
}

func TestEnvOverridesDoNotPanic(t *testing.T) {
	// Ensure applyEnvOverrides does not panic even when no env vars are set.
	origConfig := *configFile
	origAddr := *addr
	t.Cleanup(func() {
		*configFile = origConfig
		*addr = origAddr
	})

	// Clear all relevant env vars.
	for _, key := range []string{
		"WORKFLOW_CONFIG",
		"WORKFLOW_ADDR",
		"WORKFLOW_AI_API_KEY",
		"WORKFLOW_AI_MODEL",
		"WORKFLOW_JWT_SECRET",
		"WORKFLOW_AI_PROVIDER",
		"WORKFLOW_ENCRYPTION_KEY",
	} {
		os.Unsetenv(key)
	}

	applyEnvOverrides() // must not panic
}
