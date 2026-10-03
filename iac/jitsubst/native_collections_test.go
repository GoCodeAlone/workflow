package jitsubst

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestResolveSpec_ResolvedCopyPreservesNativeCollections(t *testing.T) {
	type key string
	type text string
	type texts []text
	for _, mode := range []string{"strict", "lenient"} {
		t.Run(mode, func(t *testing.T) {
			original := map[string]any{
				"map":       map[key]texts{"password": {"${TOKEN}"}},
				"array":     [1]map[string]string{{"password": "${TOKEN}"}},
				"integer":   int64(9007199254740993),
				"nil_map":   map[string]string(nil),
				"nil_slice": []string(nil),
			}
			spec := interfaces.ResourceSpec{Config: original}
			lookup := func(string) (string, bool) { return "resolved-once", true }
			var got interfaces.ResourceSpec
			var err error
			if mode == "strict" {
				got, err = ResolveSpec(spec, nil, nil, lookup)
			} else {
				got, _, err = TryResolveSpec(spec, nil, nil, lookup)
			}
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"map":       map[key]texts{"password": {"resolved-once"}},
				"array":     [1]map[string]string{{"password": "resolved-once"}},
				"integer":   int64(9007199254740993),
				"nil_map":   map[string]string(nil),
				"nil_slice": []string(nil),
			}
			if !reflect.DeepEqual(got.Config, want) {
				t.Fatalf("resolved copy/types = %#v, want %#v", got.Config, want)
			}
			got.Config["map"].(map[key]texts)["password"][0] = "driver-mutation"
			got.Config["array"].([1]map[string]string)[0]["password"] = "driver-mutation"
			if original["map"].(map[key]texts)["password"][0] != "${TOKEN}" || original["array"].([1]map[string]string)[0]["password"] != "${TOKEN}" {
				t.Fatal("driver mutation changed declarative config")
			}
		})
	}
}

func TestResolveSpec_SecretReferencesOnlyResolveInDispatchCopy(t *testing.T) {
	const marker = "known-private-store-value"
	spec := interfaces.ResourceSpec{Config: map[string]any{
		"password": "secret://credential",
		"nested":   []string{"secret_ref://credential", "${parent.password}"},
	}}
	var keys []string
	got, err := ResolveSpecWithSecretLookup(spec, nil,
		map[string]map[string]any{"parent": {"password": "secret_ref://credential"}}, nil,
		func(key string) (string, error) { keys = append(keys, key); return marker, nil })
	if err != nil || got.Config["password"] != marker || !slices.Equal(got.Config["nested"].([]string), []string{marker, marker}) || !slices.Equal(keys, []string{"credential", "credential", "credential"}) {
		t.Fatalf("runtime resolution=%#v keys=%v err=%v", got.Config, keys, err)
	}
	if spec.Config["password"] != "secret://credential" || spec.Config["nested"].([]string)[0] != "secret_ref://credential" {
		t.Fatal("secret lookup changed declarative source")
	}
	cause := fmt.Errorf("store failure includes %s", marker)
	_, err = ResolveSpecWithSecretLookup(spec, nil, nil, nil, func(string) (string, error) { return "", cause })
	if err == nil || !errors.Is(err, cause) || strings.Contains(err.Error(), marker) {
		t.Fatalf("lookup diagnostic/cause: %v", err)
	}
}

func TestTryResolveSpec_PreservesSensitivePathsInPlanningCopy(t *testing.T) {
	spec := interfaces.ResourceSpec{Config: map[string]any{
		"password": "${TOKEN}",
		"nested":   []map[string]string{{"a/b": "${parent.password}", "id": "${parent.id}"}},
	}}
	got, unresolved, err := TryResolveSpecPreservingPaths(spec, nil,
		map[string]map[string]any{"parent": {"password": "private-value", "id": "public-id"}},
		func(string) (string, bool) { return "private-value", true }, []string{"/password", "/nested/0/a~1b"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Config["password"] != "${TOKEN}" || got.Config["nested"].([]map[string]string)[0]["a/b"] != "${parent.password}" || got.Config["nested"].([]map[string]string)[0]["id"] != "public-id" {
		t.Fatalf("sensitive refs changed or nonsensitive comparison lost: %#v", got.Config)
	}
	if !slices.Equal(unresolved, []string{"TOKEN", "parent.password"}) {
		t.Fatalf("pending refs=%v", unresolved)
	}
}
