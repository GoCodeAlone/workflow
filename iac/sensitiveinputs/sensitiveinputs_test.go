package sensitiveinputs

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type sensitiveInputKey string
type sensitiveInputReference string
type misleadingSensitiveInputLookup map[string]any

func (misleadingSensitiveInputLookup) JSONLookup(string) (any, error) {
	return "${TOKEN}", nil
}

func TestSensitiveInputPathsCanonical(t *testing.T) {
	for _, path := range []string{"/password", "/env/*/value", "/a~1b/~0key", "/", "/list/0"} {
		if err := ValidatePaths([]string{path}); err != nil {
			t.Errorf("valid path %q: %v", path, err)
		}
	}
	for _, path := range []string{"", "password", "#/password", "/bad~", "/bad~2"} {
		if err := ValidatePaths([]string{path}); err == nil {
			t.Errorf("noncanonical path %q accepted", path)
		}
	}
}

func TestSensitiveInputPathsExpandNestedMapsAndLists(t *testing.T) {
	config := map[string]any{
		"env": []any{
			map[string]any{"value": "${FIRST}"},
			map[string]any{"value": "${SECOND}"},
		},
		"a/b": map[string]string{"~key": "secret://key"},
		"credentials": map[string]any{
			"z": map[string]any{"token": "${LAST}"},
			"a": map[string]any{"token": "${FIRST}"},
		},
	}
	got, err := Expand(config, []string{"/env/*/value", "/a~1b/~0key", "/credentials/*/token", "/absent", "/env/0/value"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/a~1b/~0key", "/credentials/a/token", "/credentials/z/token", "/env/0/value", "/env/1/value"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths=%v want=%v", got, want)
	}
}

func TestSensitiveInputPathsRejectNonLeavesAndBadListIndexes(t *testing.T) {
	config := map[string]any{"env": []any{map[string]any{"value": "${TOKEN}"}}, "literal": "known-sensitive-marker"}
	for _, path := range []string{"/env", "/env/*", "/env/01/value", "/env/-/value", "/literal/child"} {
		_, err := Expand(config, []string{path})
		if err == nil {
			t.Errorf("path %q accepted non-leaf or malformed traversal", path)
		} else if strings.Contains(err.Error(), "known-sensitive-marker") {
			t.Fatalf("path error contains consumer value: %v", err)
		}
	}
}

func TestSensitiveInputReferencesRejectLiteralsWithoutLeaking(t *testing.T) {
	for _, value := range []any{"${TOKEN}", "${database.uri}", "secret://scope/key", "secret_ref://KEY"} {
		if err := ValidateReferences(map[string]any{"password": value}, []string{"/password"}); err != nil {
			t.Errorf("reference %q: %v", value, err)
		}
	}
	for _, value := range []any{"known-sensitive-marker", "", "${}", "${.uri}", "${database.}", "${TOKEN}known-sensitive-marker", "secret://", 7, nil} {
		err := ValidateReferences(map[string]any{"password": value}, []string{"/password"})
		if err == nil {
			t.Errorf("sensitive literal %q accepted", value)
		} else if strings.Contains(err.Error(), "known-sensitive-marker") {
			t.Fatalf("validation error leaked sensitive bytes: %v", err)
		}
	}
	if err := ValidateReferences(map[string]any{}, []string{"/password"}); err != nil {
		t.Fatalf("absent optional input: %v", err)
	}
}

func TestSensitiveInputReferencesValidateActualNativeLeaves(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config map[string]any
		path   string
	}{
		{"defined map key", map[string]any{"env": map[sensitiveInputKey]string{"token": "${TOKEN}"}}, "/env/token"},
		{"fixed array", map[string]any{"env": [1]string{"${TOKEN}"}}, "/env/*"},
		{"defined reference string", map[string]any{"password": sensitiveInputReference("${TOKEN}")}, "/password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateReferences(tc.config, []string{tc.path}); err != nil {
				t.Fatalf("valid native reference shape rejected: %v", err)
			}
		})
	}
}

func TestSensitiveInputReferencesCannotUseCustomLookupToHideLiteral(t *testing.T) {
	const marker = "private-native-literal"
	config := map[string]any{"env": misleadingSensitiveInputLookup{"token": marker}}
	data, err := json.Marshal(config)
	if err != nil || !strings.Contains(string(data), marker) {
		t.Fatalf("fixture must serialize its actual stored literal: %s err=%v", data, err)
	}
	err = ValidateReferences(config, []string{"/env/token"})
	if err == nil || strings.Contains(err.Error(), marker) {
		t.Fatalf("custom lookup approved or exposed stored literal: %v", err)
	}
}
