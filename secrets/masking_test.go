package secrets

import (
	"testing"
)

func TestMaskSensitiveOutputs_MasksSensitiveKeys(t *testing.T) {
	outputs := map[string]any{
		"host":     "db.example.com",
		"port":     5432,
		"password": "supersecret",
		"uri":      "postgres://user:pass@host/db",
	}
	keys := MergeSensitiveKeys(nil)
	masked := MaskSensitiveOutputs(outputs, keys)

	if masked["host"] != "db.example.com" {
		t.Errorf("host should not be masked, got %v", masked["host"])
	}
	if masked["port"] != 5432 {
		t.Errorf("port should not be masked, got %v", masked["port"])
	}
	if masked["password"] != maskedValue {
		t.Errorf("password should be masked, got %v", masked["password"])
	}
	if masked["uri"] != maskedValue {
		t.Errorf("uri should be masked, got %v", masked["uri"])
	}
}

func TestMaskSensitiveOutputs_NonSensitivePassThrough(t *testing.T) {
	outputs := map[string]any{
		"endpoint": "https://api.example.com",
		"region":   "us-east-1",
		"name":     "my-db",
	}
	masked := MaskSensitiveOutputs(outputs, DefaultSensitiveKeys())

	for k, v := range outputs {
		if masked[k] != v {
			t.Errorf("key %q should pass through unchanged, got %v", k, masked[k])
		}
	}
}

func TestMaskSensitiveOutputs_EmptyOutputs(t *testing.T) {
	result := MaskSensitiveOutputs(nil, DefaultSensitiveKeys())
	if result != nil {
		t.Errorf("expected nil for nil outputs, got %v", result)
	}
	result = MaskSensitiveOutputs(map[string]any{}, DefaultSensitiveKeys())
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestMergeSensitiveKeys_CombinesDriverAndDefault(t *testing.T) {
	driverKeys := []string{"connection_uri", "admin_password", "password"}
	merged := MergeSensitiveKeys(driverKeys)

	defaults := DefaultSensitiveKeys()
	seen := make(map[string]int)
	for _, k := range merged {
		seen[k]++
		if seen[k] > 1 {
			t.Errorf("duplicate key %q in merged result", k)
		}
	}

	// all defaults present
	for _, k := range defaults {
		if _, ok := seen[k]; !ok {
			t.Errorf("default key %q missing from merged result", k)
		}
	}
	// driver-specific key present
	if _, ok := seen["connection_uri"]; !ok {
		t.Error("driver key 'connection_uri' missing from merged result")
	}
	// no duplicates for shared key "password"
	if seen["password"] != 1 {
		t.Errorf("key 'password' appears %d times, want 1", seen["password"])
	}
}

func TestDefaultSensitiveKeys_ContainsExpected(t *testing.T) {
	expected := []string{"uri", "password", "secret", "token", "connection_string",
		"dsn", "secret_key", "access_key", "private_key", "api_key"}
	keys := DefaultSensitiveKeys()
	keySet := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		keySet[k] = struct{}{}
	}
	for _, e := range expected {
		if _, ok := keySet[e]; !ok {
			t.Errorf("expected default sensitive key %q not found", e)
		}
	}
}
