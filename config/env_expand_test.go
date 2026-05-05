package config

import (
	"testing"
)

func TestExpandEnvInMap(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		if got := ExpandEnvInMap(nil); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("single-level map with ${VAR}", func(t *testing.T) {
		t.Setenv("TEST_TOKEN", "tok_abc123")
		input := map[string]any{
			"token": "${TEST_TOKEN}",
			"count": 42,
		}
		got := ExpandEnvInMap(input)
		if got["token"] != "tok_abc123" {
			t.Errorf("token: want tok_abc123, got %v", got["token"])
		}
		if got["count"] != 42 {
			t.Errorf("count: want 42, got %v", got["count"])
		}
	})

	t.Run("single-level map with $VAR no braces", func(t *testing.T) {
		t.Setenv("TEST_REGION", "us-east-1")
		input := map[string]any{"region": "$TEST_REGION"}
		got := ExpandEnvInMap(input)
		if got["region"] != "us-east-1" {
			t.Errorf("region: want us-east-1, got %v", got["region"])
		}
	})

	t.Run("nested map 3 levels deep", func(t *testing.T) {
		t.Setenv("TEST_HOST", "db.internal")
		t.Setenv("TEST_PORT", "5432")
		t.Setenv("TEST_PASS", "secret")
		input := map[string]any{
			"db": map[string]any{
				"host": "${TEST_HOST}",
				"conn": map[string]any{
					"port":     "${TEST_PORT}",
					"password": "${TEST_PASS}",
				},
			},
		}
		got := ExpandEnvInMap(input)
		db := got["db"].(map[string]any)
		if db["host"] != "db.internal" {
			t.Errorf("db.host: want db.internal, got %v", db["host"])
		}
		conn := db["conn"].(map[string]any)
		if conn["port"] != "5432" {
			t.Errorf("db.conn.port: want 5432, got %v", conn["port"])
		}
		if conn["password"] != "secret" {
			t.Errorf("db.conn.password: want secret, got %v", conn["password"])
		}
	})

	t.Run("slice containing strings and maps", func(t *testing.T) {
		t.Setenv("TEST_SVC", "api")
		input := map[string]any{
			"items": []any{
				"${TEST_SVC}",
				42,
				map[string]any{"name": "${TEST_SVC}-v2"},
			},
		}
		got := ExpandEnvInMap(input)
		items := got["items"].([]any)
		if items[0] != "api" {
			t.Errorf("items[0]: want api, got %v", items[0])
		}
		if items[1] != 42 {
			t.Errorf("items[1]: want 42, got %v", items[1])
		}
		nested := items[2].(map[string]any)
		if nested["name"] != "api-v2" {
			t.Errorf("items[2].name: want api-v2, got %v", nested["name"])
		}
	})

	t.Run("unset var expands to empty string", func(t *testing.T) {
		// os.ExpandEnv behaviour: unset vars become "".
		// This is intentional — callers should ensure vars are set.
		t.Setenv("TEST_UNSET_VAR_DEFINITELYNOTSET", "") // ensure not accidentally set
		input := map[string]any{"key": "${TEST_UNSET_VAR_DEFINITELYNOTSET}"}
		got := ExpandEnvInMap(input)
		if got["key"] != "" {
			t.Errorf("key: want empty string for unset var, got %v", got["key"])
		}
	})

	t.Run("non-string types preserved", func(t *testing.T) {
		input := map[string]any{
			"b":   true,
			"i":   int64(99),
			"f":   float64(3.14),
			"nil": nil,
		}
		got := ExpandEnvInMap(input)
		if got["b"] != true {
			t.Errorf("b: want true, got %v", got["b"])
		}
		if got["i"] != int64(99) {
			t.Errorf("i: want 99, got %v", got["i"])
		}
		if got["f"] != float64(3.14) {
			t.Errorf("f: want 3.14, got %v", got["f"])
		}
		if got["nil"] != nil {
			t.Errorf("nil: want nil, got %v", got["nil"])
		}
	})

	t.Run("original map not mutated", func(t *testing.T) {
		t.Setenv("TEST_IMMUTABLE", "expanded")
		original := map[string]any{"v": "${TEST_IMMUTABLE}"}
		_ = ExpandEnvInMap(original)
		// original value must remain the unexpanded literal
		if original["v"] != "${TEST_IMMUTABLE}" {
			t.Errorf("original mutated: got %v", original["v"])
		}
	})

	t.Run("table-driven mixed substitution", func(t *testing.T) {
		t.Setenv("T_A", "alpha")
		t.Setenv("T_B", "beta")
		tests := []struct {
			name  string
			key   string
			input string
			want  string
		}{
			{"braces", "k1", "${T_A}", "alpha"},
			{"no braces", "k2", "$T_B", "beta"},
			{"literal no dollar", "k3", "plain", "plain"},
			{"mixed", "k4", "${T_A}-${T_B}", "alpha-beta"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got := ExpandEnvInMap(map[string]any{tc.key: tc.input})
				if got[tc.key] != tc.want {
					t.Errorf("%s: want %q, got %q", tc.name, tc.want, got[tc.key])
				}
			})
		}
	})
}

func TestExpandEnvInSlice(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		if got := ExpandEnvInSlice(nil); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("expands strings in slice", func(t *testing.T) {
		t.Setenv("TEST_SL_VAR", "hello")
		got := ExpandEnvInSlice([]any{"${TEST_SL_VAR}", 7, nil})
		if got[0] != "hello" {
			t.Errorf("got[0]: want hello, got %v", got[0])
		}
		if got[1] != 7 {
			t.Errorf("got[1]: want 7, got %v", got[1])
		}
		if got[2] != nil {
			t.Errorf("got[2]: want nil, got %v", got[2])
		}
	})
}

func TestExpandEnvInValue(t *testing.T) {
	t.Run("string expanded", func(t *testing.T) {
		t.Setenv("TEST_VAL", "x")
		if got := ExpandEnvInValue("${TEST_VAL}"); got != "x" {
			t.Errorf("want x, got %v", got)
		}
	})
	t.Run("non-string passthrough", func(t *testing.T) {
		if got := ExpandEnvInValue(123); got != 123 {
			t.Errorf("want 123, got %v", got)
		}
	})
	t.Run("nil passthrough", func(t *testing.T) {
		if got := ExpandEnvInValue(nil); got != nil {
			t.Errorf("want nil, got %v", got)
		}
	})
}

func TestExpandEnvInMapPreservingKeys_PreservesEnvVarsSubmap(t *testing.T) {
	t.Setenv("MY_TOKEN", "actual-secret-value")
	t.Setenv("OTHER", "resolved-other")
	in := map[string]any{
		"name":   "myapp",
		"region": "${OTHER}", // top-level: should resolve
		"env_vars": map[string]any{
			"AUTH_TOKEN": "${MY_TOKEN}", // inside env_vars: should PRESERVE literal
			"PORT":       "8080",        // no var ref, preserved as-is
		},
		"env_vars_secret": map[string]any{
			"DB_URL": "${OTHER}", // inside env_vars_secret: PRESERVE
		},
	}
	out := ExpandEnvInMapPreservingKeys(in, []string{"env_vars", "env_vars_secret", "secret_env_vars"})
	if got := out["region"]; got != "resolved-other" {
		t.Errorf("top-level region: got %q, want resolved-other", got)
	}
	envVars := out["env_vars"].(map[string]any)
	if got := envVars["AUTH_TOKEN"]; got != "${MY_TOKEN}" {
		t.Errorf("env_vars.AUTH_TOKEN: got %q, want literal ${MY_TOKEN}", got)
	}
	envVarsSecret := out["env_vars_secret"].(map[string]any)
	if got := envVarsSecret["DB_URL"]; got != "${OTHER}" {
		t.Errorf("env_vars_secret.DB_URL: got %q, want literal ${OTHER}", got)
	}
}

func TestExpandEnvInMapPreservingKeys_NestedNonPreservedSubmapsStillResolve(t *testing.T) {
	t.Setenv("DEEP", "deep-value")
	in := map[string]any{
		"services": map[string]any{
			"api": map[string]any{
				"image": "${DEEP}", // not in preserve list: should resolve
			},
		},
	}
	out := ExpandEnvInMapPreservingKeys(in, []string{"env_vars"})
	got := out["services"].(map[string]any)["api"].(map[string]any)["image"]
	if got != "deep-value" {
		t.Errorf("services.api.image: got %q, want deep-value", got)
	}
}

func TestExpandEnvInMapPreservingKeys_EmptyPreserveListEqualsExpandEnvInMap(t *testing.T) {
	t.Setenv("V", "vv")
	in := map[string]any{"k": "${V}"}
	out := ExpandEnvInMapPreservingKeys(in, []string{})
	if out["k"] != "vv" {
		t.Errorf("with empty preserve list, behavior should equal ExpandEnvInMap; got %q", out["k"])
	}
}
