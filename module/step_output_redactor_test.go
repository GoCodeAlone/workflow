package module

import (
	"testing"
)

func TestRedactStepOutput_SensitiveFields(t *testing.T) {
	cases := []struct {
		key          string
		wantRedacted bool
	}{
		{"password", true},
		{"Password", true},
		{"PASSWORD", true},
		{"user_password", true},
		{"secret", true},
		{"api_secret", true},
		{"token", true},
		{"access_token", true},
		{"credential", true},
		{"api_key", true},
		{"apikey", true},
		{"ApiKey", true},
		{"private_key", true},
		{"access_key", true},
		{"backup_code", true},
		{"totp_secret", true},
		{"mfa_secret", true},
		// safe fields
		{"username", false},
		{"email", false},
		{"status", false},
		{"result", false},
		// _display suffix exemption
		{"password_display", false},
		{"token_display", false},
		{"secret_display", false},
	}

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			output := map[string]any{tc.key: "sensitive-value"}
			got := RedactStepOutput(output)
			val := got[tc.key]
			if tc.wantRedacted {
				if val != RedactionPlaceholder {
					t.Errorf("field %q: expected %q, got %v", tc.key, RedactionPlaceholder, val)
				}
			} else {
				if val == RedactionPlaceholder {
					t.Errorf("field %q: should not be redacted, but was", tc.key)
				}
			}
		})
	}
}

func TestRedactStepOutput_NestedMaps(t *testing.T) {
	output := map[string]any{
		"user": map[string]any{
			"name":     "alice",
			"password": "hunter2",
			"profile": map[string]any{
				"bio":    "developer",
				"secret": "shh",
			},
		},
		"status": "ok",
	}

	got := RedactStepOutput(output)

	user, ok := got["user"].(map[string]any)
	if !ok {
		t.Fatal("user field should be a map")
	}
	if user["name"] != "alice" {
		t.Errorf("name should be preserved, got %v", user["name"])
	}
	if user["password"] != RedactionPlaceholder {
		t.Errorf("password should be redacted, got %v", user["password"])
	}

	profile, ok := user["profile"].(map[string]any)
	if !ok {
		t.Fatal("profile should be a map")
	}
	if profile["bio"] != "developer" {
		t.Errorf("bio should be preserved, got %v", profile["bio"])
	}
	if profile["secret"] != RedactionPlaceholder {
		t.Errorf("nested secret should be redacted, got %v", profile["secret"])
	}

	if got["status"] != "ok" {
		t.Errorf("status should be preserved, got %v", got["status"])
	}
}

func TestRedactStepOutput_OriginalNotModified(t *testing.T) {
	original := map[string]any{
		"password": "hunter2",
		"user":     "alice",
	}
	_ = RedactStepOutput(original)

	if original["password"] != "hunter2" {
		t.Error("original map should not be modified by RedactStepOutput")
	}
}

func TestRedactStepOutputWithPatterns_ExtraPatterns(t *testing.T) {
	output := map[string]any{
		"card_number": "4111111111111111",
		"cvv":         "123",
		"name":        "Alice",
	}
	got := RedactStepOutputWithPatterns(output, []string{"card_number", "cvv"})

	if got["card_number"] != RedactionPlaceholder {
		t.Errorf("card_number should be redacted, got %v", got["card_number"])
	}
	if got["cvv"] != RedactionPlaceholder {
		t.Errorf("cvv should be redacted, got %v", got["cvv"])
	}
	if got["name"] != "Alice" {
		t.Errorf("name should be preserved, got %v", got["name"])
	}
}

func TestRedactStepOutput_DisplaySuffixExemption(t *testing.T) {
	output := map[string]any{
		"token":         "secret-token",
		"token_display": "tok***en",
	}
	got := RedactStepOutput(output)

	if got["token"] != RedactionPlaceholder {
		t.Errorf("token should be redacted, got %v", got["token"])
	}
	if got["token_display"] == RedactionPlaceholder {
		t.Error("token_display should not be redacted due to _display suffix")
	}
}

func TestRedactStepOutput_NonStringValues(t *testing.T) {
	output := map[string]any{
		"count":    42,
		"enabled":  true,
		"password": 99999, // numeric password field
	}
	got := RedactStepOutput(output)

	if got["count"] != 42 {
		t.Errorf("count should be preserved, got %v", got["count"])
	}
	if got["enabled"] != true {
		t.Errorf("enabled should be preserved, got %v", got["enabled"])
	}
	if got["password"] != RedactionPlaceholder {
		t.Errorf("numeric password field should still be redacted, got %v", got["password"])
	}
}

func TestRedactStepOutput_EmptyMap(t *testing.T) {
	got := RedactStepOutput(map[string]any{})
	if len(got) != 0 {
		t.Errorf("empty map should return empty map, got %v", got)
	}
}
