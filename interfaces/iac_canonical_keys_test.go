package interfaces_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestCanonicalKeys_AllPresent(t *testing.T) {
	required := []string{
		"name", "region", "image", "http_port", "internal_ports", "protocol",
		"instance_count", "size", "env_vars", "env_vars_secret", "vpc_ref",
		"autoscaling", "routes", "cors", "domains", "health_check",
		"liveness_check", "ingress", "egress", "alerts", "log_destinations",
		"termination", "maintenance", "jobs", "workers", "static_sites",
		"sidecars", "build_command", "run_command", "dockerfile_path",
		"source_dir", "provider_specific",
	}
	for _, k := range required {
		if !interfaces.IsCanonicalKey(k) {
			t.Errorf("canonical key %q missing from IsCanonicalKey", k)
		}
	}
}

func TestCanonicalKeys_List(t *testing.T) {
	keys := interfaces.CanonicalKeys()
	if len(keys) == 0 {
		t.Fatal("CanonicalKeys() returned empty slice")
	}
	// All returned keys must be self-recognized
	for _, k := range keys {
		if !interfaces.IsCanonicalKey(k) {
			t.Errorf("key %q from CanonicalKeys() not recognized by IsCanonicalKey", k)
		}
	}
}

func TestCanonicalKeys_UnknownReturnsFalse(t *testing.T) {
	if interfaces.IsCanonicalKey("not_a_canonical_key") {
		t.Error("IsCanonicalKey returned true for unknown key")
	}
}
