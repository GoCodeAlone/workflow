// Package sensitiveinputs validates provider-declared sensitive Config leaves.
package sensitiveinputs

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/GoCodeAlone/workflow/iac/sensitive"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
	"github.com/go-openapi/jsonpointer"
)

var referencePattern = regexp.MustCompile(`^\$\{[^{}[:space:]]+\}$`)

// ValidatePaths requires non-root canonical RFC 6901 pointers. Escapes are
// validated by re-encoding the parser's decoded tokens; malformed '~' escapes
// must not alias an otherwise valid key.
func ValidatePaths(paths []string) error {
	for _, path := range paths {
		p, err := jsonpointer.New(path)
		if err != nil || p.IsEmpty() {
			return fmt.Errorf("%w: sensitive input path must be a non-root JSON pointer", interfaces.ErrValidation)
		}
		var canonical strings.Builder
		for _, token := range p.DecodedTokens() {
			canonical.WriteByte('/')
			canonical.WriteString(jsonpointer.Escape(token))
		}
		if canonical.String() != path {
			return fmt.Errorf("%w: sensitive input path has noncanonical escaping", interfaces.ErrValidation)
		}
	}
	return nil
}

// Expand returns deterministic unique concrete leaf pointers. Missing optional
// fields have no match; present containers or scalar traversal are errors.
func Expand(config map[string]any, paths []string) ([]string, error) {
	var result []string
	err := walkLeaves(config, paths, func(path string, _ reflect.Value) error {
		result = append(result, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(result)
	return slices.Compact(result), nil
}

func walkLeaves(config map[string]any, paths []string, visitLeaf func(string, reflect.Value) error) error {
	if err := ValidatePaths(paths); err != nil {
		return err
	}
	for _, path := range paths {
		p, _ := jsonpointer.New(path) // Already validated above.
		if err := expand(reflect.ValueOf(config), p.DecodedTokens(), "", visitLeaf); err != nil {
			return err
		}
	}
	return nil
}

func expand(node reflect.Value, tokens []string, path string, visitLeaf func(string, reflect.Value) error) error {
	for node.IsValid() && node.Kind() == reflect.Interface {
		node = node.Elem()
	}
	if len(tokens) == 0 {
		if node.IsValid() && (node.Kind() == reflect.Map || node.Kind() == reflect.Slice || node.Kind() == reflect.Array) {
			return fmt.Errorf("%w: sensitive input %s is not a leaf", interfaces.ErrValidation, path)
		}
		return visitLeaf(path, node)
	}
	if !node.IsValid() {
		return fmt.Errorf("%w: sensitive input %s traverses a null value", interfaces.ErrValidation, path)
	}
	segment := tokens[0]
	visit := func(key string, child reflect.Value) error {
		return expand(child, tokens[1:], path+"/"+jsonpointer.Escape(key), visitLeaf)
	}
	switch node.Kind() {
	case reflect.Map:
		if node.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("%w: sensitive input %s requires string map keys", interfaces.ErrValidation, path)
		}
		if segment == "*" {
			keys := make([]string, 0, node.Len())
			for _, key := range node.MapKeys() {
				keys = append(keys, key.String())
			}
			slices.Sort(keys)
			for _, key := range keys {
				if err := visit(key, node.MapIndex(reflect.ValueOf(key).Convert(node.Type().Key()))); err != nil {
					return err
				}
			}
			return nil
		}
		child := node.MapIndex(reflect.ValueOf(segment).Convert(node.Type().Key()))
		if !child.IsValid() {
			return nil
		}
		return visit(segment, child)
	case reflect.Slice, reflect.Array:
		if segment == "*" {
			for i := 0; i < node.Len(); i++ {
				if err := visit(strconv.Itoa(i), node.Index(i)); err != nil {
					return err
				}
			}
			return nil
		}
		index, err := strconv.Atoi(segment)
		if err != nil || index < 0 || strconv.Itoa(index) != segment {
			return fmt.Errorf("%w: sensitive input %s has a noncanonical list index", interfaces.ErrValidation, path)
		}
		if index >= node.Len() {
			return nil
		}
		return visit(segment, node.Index(index))
	default:
		return fmt.Errorf("%w: sensitive input %s traverses a scalar", interfaces.ErrValidation, path)
	}
}

// ValidateReferences rejects literal sensitive values without including them
// in diagnostics. It validates declarative inputs, never consumer artifacts.
func ValidateReferences(config map[string]any, paths []string) error {
	return walkLeaves(config, paths, func(path string, value reflect.Value) error {
		if !value.IsValid() || value.Kind() != reflect.String || !isReference(value.String()) {
			return fmt.Errorf("%w: sensitive input %s must be a reference", interfaces.ErrValidation, path)
		}
		return nil
	})
}

func isReference(s string) bool {
	for _, prefix := range []string{secrets.SecretPrefix, sensitive.PlaceholderPrefix} {
		if strings.HasPrefix(s, prefix) {
			return len(s) > len(prefix) && !strings.ContainsAny(s, "\r\n\t ")
		}
	}
	if !referencePattern.MatchString(s) {
		return false
	}
	body := s[2 : len(s)-1]
	module, field, dotted := strings.Cut(body, ".")
	return !dotted || (module != "" && field != "")
}
