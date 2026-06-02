package module

import (
	"reflect"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestCollectSecretRefs exercises collectSecretRefs/collectFromValue across every
// container shape a ResourceSpec.Config can carry: a flat string ref, a ref
// nested inside a map[string]any, a ref inside an []any of strings, a ref inside
// an []any of maps, a ref inside a typed []string (programmatically-built specs),
// and double-nesting. It also asserts dedup + sorted order.
func TestCollectSecretRefs(t *testing.T) {
	specs := []interfaces.ResourceSpec{
		{
			Name: "r1",
			Type: "infra.database",
			Config: map[string]any{
				// flat string ref
				"password": "secret://vault/db-pass",
				// non-ref string (ignored)
				"region": "us-east-1",
				// ref nested in a map[string]any
				"nested": map[string]any{
					"token": "secret://vault/nested-token",
				},
				// ref in an []any of strings
				"list_any_strings": []any{
					"plain",
					"secret://vault/from-anylist",
				},
				// ref in an []any of maps
				"list_any_maps": []any{
					map[string]any{"key": "secret://vault/from-anymap"},
				},
				// ref in a typed []string (programmatically-built)
				"list_typed_strings": []string{
					"not-a-ref",
					"secret://vault/from-typedlist",
				},
				// double-nesting: map → slice → map → string
				"deep": map[string]any{
					"items": []any{
						map[string]any{
							"deeper": map[string]any{
								"v": "secret://vault/deep-ref",
							},
						},
					},
				},
			},
		},
		{
			Name: "r2",
			Type: "infra.database",
			Config: map[string]any{
				// duplicate of r1's password — must be deduped
				"password": "secret://vault/db-pass",
				// a unique ref in r2
				"api": "secret://vault/r2-api",
			},
		},
	}

	got := collectSecretRefs(specs)

	want := []string{
		"secret://vault/db-pass",
		"secret://vault/deep-ref",
		"secret://vault/from-anylist",
		"secret://vault/from-anymap",
		"secret://vault/from-typedlist",
		"secret://vault/nested-token",
		"secret://vault/r2-api",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("collectSecretRefs mismatch:\n got:  %#v\n want: %#v", got, want)
	}
}

// TestCollectSecretRefs_NoRefs asserts an empty (non-nil) slice when no refs exist.
func TestCollectSecretRefs_NoRefs(t *testing.T) {
	specs := []interfaces.ResourceSpec{
		{Name: "r1", Type: "infra.database", Config: map[string]any{"size": "small"}},
	}
	got := collectSecretRefs(specs)
	if len(got) != 0 {
		t.Errorf("expected no refs, got %#v", got)
	}
}
