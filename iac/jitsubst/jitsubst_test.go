package jitsubst

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// envFn is a tiny helper for building envLookup closures from a static map.
func envFn(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

// TestResolveSpec_NoRefs_PassesThrough verifies that a spec with no ${...}
// references is returned with Config equal to the input — but as a deep copy
// so caller mutation cannot poison the input.
func TestResolveSpec_NoRefs_PassesThrough(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Name: "vpc",
		Type: "infra.vpc",
		Config: map[string]any{
			"cidr": "10.0.0.0/16",
			"tag":  "prod",
		},
	}
	got, err := ResolveSpec(spec, nil, nil, nil)
	if err != nil {
		t.Fatalf("ResolveSpec: unexpected error: %v", err)
	}
	if got.Config["cidr"] != "10.0.0.0/16" || got.Config["tag"] != "prod" {
		t.Errorf("expected Config preserved; got %v", got.Config)
	}
	// Confirm deep copy: mutating returned Config must not touch input.
	got.Config["cidr"] = "mutated"
	if spec.Config["cidr"] != "10.0.0.0/16" {
		t.Errorf("input Config was mutated: %v", spec.Config)
	}
}

// TestResolveSpec_EnvVarReferenceResolved verifies that a bare ${VAR} (no dot)
// is resolved through envLookup.
func TestResolveSpec_EnvVarReferenceResolved(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Name: "pg",
		Config: map[string]any{
			"password": "${PG_PASSWORD}",
		},
	}
	env := envFn(map[string]string{"PG_PASSWORD": "s3cret"})
	got, err := ResolveSpec(spec, nil, nil, env)
	if err != nil {
		t.Fatalf("ResolveSpec: unexpected error: %v", err)
	}
	if got.Config["password"] != "s3cret" {
		t.Errorf("password: got %q want %q", got.Config["password"], "s3cret")
	}
}

// TestResolveSpec_EnvVarUnset_Errors verifies that a ${VAR} whose name has no
// dot AND whose value is missing from envLookup returns an unresolved-reference
// error — JIT semantics demand strictness, unlike os.ExpandEnv.
func TestResolveSpec_EnvVarUnset_Errors(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Config: map[string]any{"x": "${MISSING}"},
	}
	_, err := ResolveSpec(spec, nil, nil, envFn(nil))
	if err == nil {
		t.Fatalf("expected error for unset env var, got nil")
	}
	if !strings.Contains(err.Error(), "MISSING") {
		t.Errorf("error should mention the missing var name; got %q", err)
	}
}

// TestResolveSpec_NilEnvLookup_TreatsAllEnvVarsAsUnset verifies that callers
// that legitimately have no env-var source (e.g., test fixtures) get a clear
// error — not a nil-deref panic — when a ${VAR} is encountered.
func TestResolveSpec_NilEnvLookup_TreatsAllEnvVarsAsUnset(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Config: map[string]any{"x": "${FOO}"},
	}
	_, err := ResolveSpec(spec, nil, nil, nil)
	if err == nil {
		t.Fatalf("expected error for nil envLookup with ${VAR} ref")
	}
}

// TestResolveSpec_ModuleField_ResolvedFromSyncedOutputs verifies that a
// ${MODULE.field} reference (non-id field) is resolved against the synced
// outputs of the named module.
func TestResolveSpec_ModuleField_ResolvedFromSyncedOutputs(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Name: "app",
		Config: map[string]any{
			"db_host": "${pg.private_ip}",
		},
	}
	synced := map[string]map[string]any{
		"pg": {"private_ip": "10.0.0.5"},
	}
	got, err := ResolveSpec(spec, nil, synced, nil)
	if err != nil {
		t.Fatalf("ResolveSpec: unexpected error: %v", err)
	}
	if got.Config["db_host"] != "10.0.0.5" {
		t.Errorf("db_host: got %q want %q", got.Config["db_host"], "10.0.0.5")
	}
}

// TestResolveSpec_ModuleID_PrefersReplaceIDMap verifies that ${MODULE.id}
// resolves from the replaceIDMap first — even if syncedOutputs also has an
// `id` field. This makes cascade-replace ProviderID propagation authoritative
// over potentially-stale state outputs.
func TestResolveSpec_ModuleID_PrefersReplaceIDMap(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Config: map[string]any{"vpc_uuid": "${vpc.id}"},
	}
	replace := map[string]string{"vpc": "new-uuid-after-replace"}
	synced := map[string]map[string]any{
		"vpc": {"id": "old-uuid-from-state"},
	}
	got, err := ResolveSpec(spec, replace, synced, nil)
	if err != nil {
		t.Fatalf("ResolveSpec: unexpected error: %v", err)
	}
	if got.Config["vpc_uuid"] != "new-uuid-after-replace" {
		t.Errorf("vpc_uuid: got %q want replace-map value", got.Config["vpc_uuid"])
	}
}

// TestResolveSpec_ModuleID_FallsBackToSyncedOutputs verifies that when the
// replaceIDMap has no entry for the module (the common case — module was
// created, not replaced, in this apply), ${MODULE.id} falls back to
// syncedOutputs[module]["id"].
func TestResolveSpec_ModuleID_FallsBackToSyncedOutputs(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Config: map[string]any{"vpc_uuid": "${vpc.id}"},
	}
	synced := map[string]map[string]any{
		"vpc": {"id": "vpc-from-state-12345"},
	}
	got, err := ResolveSpec(spec, nil, synced, nil)
	if err != nil {
		t.Fatalf("ResolveSpec: unexpected error: %v", err)
	}
	if got.Config["vpc_uuid"] != "vpc-from-state-12345" {
		t.Errorf("vpc_uuid: got %q want syncedOutputs.id", got.Config["vpc_uuid"])
	}
}

// TestResolveSpec_ModuleID_UnknownModule_Errors verifies that ${MODULE.id}
// for a module absent from BOTH replaceIDMap and syncedOutputs returns an
// unresolved-reference error.
func TestResolveSpec_ModuleID_UnknownModule_Errors(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Config: map[string]any{"x": "${ghost.id}"},
	}
	_, err := ResolveSpec(spec, nil, nil, nil)
	if err == nil {
		t.Fatalf("expected error for unknown module ${ghost.id}")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should mention the missing module; got %q", err)
	}
}

// TestResolveSpec_ModuleField_UnknownModule_Errors verifies that
// ${MODULE.field} (non-id) for an unknown module errors clearly.
func TestResolveSpec_ModuleField_UnknownModule_Errors(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Config: map[string]any{"x": "${ghost.private_ip}"},
	}
	_, err := ResolveSpec(spec, nil, map[string]map[string]any{}, nil)
	if err == nil {
		t.Fatalf("expected error for unknown module ${ghost.private_ip}")
	}
}

// TestResolveSpec_ModuleField_UnknownField_Errors verifies that
// ${MODULE.field} for a known module but unknown field errors clearly.
func TestResolveSpec_ModuleField_UnknownField_Errors(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Config: map[string]any{"x": "${pg.nonexistent}"},
	}
	synced := map[string]map[string]any{"pg": {"private_ip": "10.0.0.5"}}
	_, err := ResolveSpec(spec, nil, synced, nil)
	if err == nil {
		t.Fatalf("expected error for unknown field ${pg.nonexistent}")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention the missing field; got %q", err)
	}
}

// TestResolveSpec_NestedMapsAndSlices_RecursivelyResolved verifies that
// substitution walks nested map[string]any and []any structures.
func TestResolveSpec_NestedMapsAndSlices_RecursivelyResolved(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Config: map[string]any{
			"env": map[string]any{
				"DATABASE_URL": "postgres://${pg.private_ip}/db",
			},
			"args": []any{"--vpc=${vpc.id}", "--port=5432"},
		},
	}
	replace := map[string]string{"vpc": "vpc-abc"}
	synced := map[string]map[string]any{
		"pg": {"private_ip": "10.0.0.5"},
	}
	got, err := ResolveSpec(spec, replace, synced, nil)
	if err != nil {
		t.Fatalf("ResolveSpec: unexpected error: %v", err)
	}
	envMap, ok := got.Config["env"].(map[string]any)
	if !ok {
		t.Fatalf("env: not a map: %T", got.Config["env"])
	}
	if envMap["DATABASE_URL"] != "postgres://10.0.0.5/db" {
		t.Errorf("DATABASE_URL: got %q", envMap["DATABASE_URL"])
	}
	args, ok := got.Config["args"].([]any)
	if !ok {
		t.Fatalf("args: not a slice: %T", got.Config["args"])
	}
	if args[0] != "--vpc=vpc-abc" {
		t.Errorf("args[0]: got %q", args[0])
	}
	if args[1] != "--port=5432" {
		t.Errorf("args[1] (no refs) should pass through; got %q", args[1])
	}
}

// TestResolveSpec_DoesNotMutateInputConfig is a defensive double-check that
// the deep-copy contract holds even when nested structures are involved.
func TestResolveSpec_DoesNotMutateInputConfig(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Config: map[string]any{
			"env": map[string]any{"X": "${FOO}"},
		},
	}
	env := envFn(map[string]string{"FOO": "resolved"})
	_, err := ResolveSpec(spec, nil, nil, env)
	if err != nil {
		t.Fatalf("ResolveSpec: %v", err)
	}
	envMap := spec.Config["env"].(map[string]any)
	if envMap["X"] != "${FOO}" {
		t.Errorf("input nested map mutated: X = %q", envMap["X"])
	}
}

// TestResolveSpec_NonStringScalars_PreservedAsIs verifies that ints, bools,
// and other non-string scalars in Config are passed through unchanged.
func TestResolveSpec_NonStringScalars_PreservedAsIs(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Config: map[string]any{
			"port":    5432,
			"enabled": true,
			"ratio":   0.5,
		},
	}
	got, err := ResolveSpec(spec, nil, nil, nil)
	if err != nil {
		t.Fatalf("ResolveSpec: %v", err)
	}
	if got.Config["port"] != 5432 || got.Config["enabled"] != true || got.Config["ratio"] != 0.5 {
		t.Errorf("scalars not preserved: %v", got.Config)
	}
}

// TestResolveSpec_NonStringOutputValue_StringifiedFmtV verifies that when an
// output value in syncedOutputs is non-string (e.g., int port), it's
// stringified via fmt.Sprintf("%v", v) before substitution.
func TestResolveSpec_NonStringOutputValue_StringifiedFmtV(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Config: map[string]any{"x": "port=${pg.port}"},
	}
	synced := map[string]map[string]any{"pg": {"port": 5432}}
	got, err := ResolveSpec(spec, nil, synced, nil)
	if err != nil {
		t.Fatalf("ResolveSpec: %v", err)
	}
	if got.Config["x"] != "port=5432" {
		t.Errorf("x: got %q want %q", got.Config["x"], "port=5432")
	}
}

// TestResolveSpec_MultipleRefsInSingleString_AllResolved verifies that a
// single string with multiple ${...} refs has every ref substituted.
func TestResolveSpec_MultipleRefsInSingleString_AllResolved(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Config: map[string]any{
			"url": "postgres://user:${PG_PASSWORD}@${pg.private_ip}:${pg.port}/db",
		},
	}
	env := envFn(map[string]string{"PG_PASSWORD": "s3cret"})
	synced := map[string]map[string]any{"pg": {"private_ip": "10.0.0.5", "port": 5432}}
	got, err := ResolveSpec(spec, nil, synced, env)
	if err != nil {
		t.Fatalf("ResolveSpec: %v", err)
	}
	want := "postgres://user:s3cret@10.0.0.5:5432/db"
	if got.Config["url"] != want {
		t.Errorf("url: got %q want %q", got.Config["url"], want)
	}
}

// TestResolveSpec_MalformedRef_EmptyBody_Errors verifies that ${} (empty body)
// is rejected as a malformed reference rather than silently substituting "".
func TestResolveSpec_MalformedRef_EmptyBody_Errors(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Config: map[string]any{"x": "${}"},
	}
	_, err := ResolveSpec(spec, nil, nil, nil)
	if err == nil {
		t.Fatalf("expected error for malformed ${} ref")
	}
}

// TestResolveSpec_MalformedRef_DotOnly_Errors verifies that ${.} or
// ${module.} or ${.field} are rejected.
func TestResolveSpec_MalformedRef_DotOnly_Errors(t *testing.T) {
	cases := []string{"${.}", "${module.}", "${.field}"}
	for _, ref := range cases {
		spec := interfaces.ResourceSpec{
			Config: map[string]any{"x": ref},
		}
		if _, err := ResolveSpec(spec, nil, nil, nil); err == nil {
			t.Errorf("expected error for malformed ref %q", ref)
		}
	}
}

// TestResolveSpec_NilConfig_NoOp verifies that a spec with nil Config is
// returned unchanged with no error.
func TestResolveSpec_NilConfig_NoOp(t *testing.T) {
	spec := interfaces.ResourceSpec{Name: "x", Type: "infra.x"}
	got, err := ResolveSpec(spec, nil, nil, nil)
	if err != nil {
		t.Fatalf("ResolveSpec: %v", err)
	}
	if got.Config != nil {
		t.Errorf("Config should remain nil; got %v", got.Config)
	}
	if got.Name != "x" || got.Type != "infra.x" {
		t.Errorf("identity fields not preserved: %+v", got)
	}
}

// TestResolveSpec_OnError_ReturnsInputSpecUnchanged verifies the error
// contract: when substitution fails, the returned ResourceSpec is the
// original (untouched) input — callers MUST NOT use a partially-resolved
// spec since some fields may have substituted and others not.
func TestResolveSpec_OnError_ReturnsInputSpecUnchanged(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Name: "app",
		Config: map[string]any{
			"good": "${KNOWN}",
			"bad":  "${UNKNOWN}",
		},
	}
	env := envFn(map[string]string{"KNOWN": "ok"})
	got, err := ResolveSpec(spec, nil, nil, env)
	if err == nil {
		t.Fatalf("expected error; got %+v", got)
	}
	// The returned spec must be the input — same Config map identity-wise
	// is not required (callers may rely on either), but the values must
	// match the unresolved originals.
	if got.Config["good"] != "${KNOWN}" || got.Config["bad"] != "${UNKNOWN}" {
		t.Errorf("error path leaked partial substitution: %+v", got.Config)
	}
}
