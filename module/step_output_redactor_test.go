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
		{"Stripe-Signature", true},
		{"webhook_signature", true},
		{"request_body", true},
		{"raw_body", true},
		{"paypal_transmission_sig", true},
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

func TestRedactStepOutput_NestedSensitiveHeaders(t *testing.T) {
	output := map[string]any{
		"headers": map[string]any{
			"Authorization":           "Bearer jwt.secret.value",
			"Cookie":                  "sid=session-secret",
			"Set-Cookie":              "sid=session-secret; HttpOnly",
			"X-API-Key":               "api-secret",
			"X-Scenario90-Seed-Token": "seed-secret",
			"Content-Type":            "application/json",
		},
	}

	got := RedactStepOutput(output)
	headers, ok := got["headers"].(map[string]any)
	if !ok {
		t.Fatalf("headers should remain a map, got %T", got["headers"])
	}
	for _, key := range []string{"Authorization", "Cookie", "Set-Cookie", "X-API-Key", "X-Scenario90-Seed-Token"} {
		if headers[key] != RedactionPlaceholder {
			t.Fatalf("%s should be redacted, got %#v", key, headers[key])
		}
	}
	if headers["Content-Type"] != "application/json" {
		t.Fatalf("Content-Type should be preserved, got %#v", headers["Content-Type"])
	}
}

func TestRedactStepOutput_NestedSlices(t *testing.T) {
	output := map[string]any{
		"rows": []any{
			map[string]any{
				"name":         "alice",
				"access_token": "row-token",
			},
			"plain",
		},
	}

	got := RedactStepOutput(output)

	rows, ok := got["rows"].([]any)
	if !ok {
		t.Fatalf("rows should remain a []any, got %T", got["rows"])
	}
	first, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("first row should remain a map, got %T", rows[0])
	}
	if first["access_token"] != RedactionPlaceholder {
		t.Fatalf("nested slice token should be redacted, got %#v", first["access_token"])
	}
	if first["name"] != "alice" || rows[1] != "plain" {
		t.Fatalf("non-sensitive slice values should be preserved, got %#v", rows)
	}
}

func TestRedactStepOutput_TypedRowSlices(t *testing.T) {
	output := map[string]any{
		"rows": []map[string]any{
			{
				"name":         "alice",
				"access_token": "row-token",
			},
			{
				"name":     "bob",
				"password": "hunter2",
			},
		},
	}

	got := RedactStepOutput(output)

	rows, ok := got["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows should remain a []map[string]any, got %T", got["rows"])
	}
	if rows[0]["access_token"] != RedactionPlaceholder {
		t.Fatalf("typed row token should be redacted, got %#v", rows[0]["access_token"])
	}
	if rows[1]["password"] != RedactionPlaceholder {
		t.Fatalf("typed row password should be redacted, got %#v", rows[1]["password"])
	}
	if rows[0]["name"] != "alice" || rows[1]["name"] != "bob" {
		t.Fatalf("non-sensitive typed row values should be preserved, got %#v", rows)
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

func TestRedactCredentialsBlock(t *testing.T) {
	in := map[string]any{
		"credentials": map[string]any{
			"accessKey": "AKIAEXAMPLE",
			"secretKey": "supersecret",
		},
		"credentials_ref": "aws-creds-module",
		"bucket":          "public-bucket-name",
	}
	out := RedactStepOutput(in)
	// The credentials: block is redacted WHOLESALE — the existing "credential"
	// pattern replaces the whole sub-tree with the placeholder STRING (no
	// recursion). That is safe and is the design-sanctioned "already covered".
	if out["credentials"] != RedactionPlaceholder {
		t.Fatalf("credentials block must be wholesale-redacted, got: %#v", out["credentials"])
	}
	// credentials_ref is a module NAME, not a secret — must be PRESERVED.
	if out["credentials_ref"] != "aws-creds-module" {
		t.Fatalf("credentials_ref must NOT be redacted (it is a module reference): %#v", out["credentials_ref"])
	}
	if out["bucket"] != "public-bucket-name" {
		t.Fatalf("non-sensitive field wrongly redacted")
	}
}

// TestRedactRefSuffixDoesNotBypassValueSecrets locks in that the "_ref" suffix
// exempts ONLY structural-reference words (credentials_ref) — it must NOT be a
// blanket bypass for value-bearing secret patterns. A key like
// "bearer_token_ref" still matches "token" and must redact.
func TestRedactRefSuffixDoesNotBypassValueSecrets(t *testing.T) {
	in := map[string]any{
		"credentials_ref":  "aws-creds-module", // structural ref → preserved
		"bearer_token_ref": "tok-abc123",       // matches "token" → must redact
		"api_key_ref":      "ak-secret",        // matches "api_key" → must redact
		"secret_ref":       "shhh",             // matches "secret" → must redact
	}
	out := RedactStepOutput(in)
	if out["credentials_ref"] != "aws-creds-module" {
		t.Errorf("credentials_ref must be preserved, got %#v", out["credentials_ref"])
	}
	for _, k := range []string{"bearer_token_ref", "api_key_ref", "secret_ref"} {
		if out[k] != RedactionPlaceholder {
			t.Errorf("%s matches a value-bearing secret pattern — _ref must not bypass redaction, got %#v", k, out[k])
		}
	}
}
