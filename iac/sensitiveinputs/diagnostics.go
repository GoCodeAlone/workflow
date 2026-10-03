package sensitiveinputs

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// DiagnosticRedactor masks only declared resolved inputs in control-plane
// diagnostics. It must not be applied to provider outputs or consumer payloads.
func DiagnosticRedactor(config map[string]any, paths []string) (func(string) string, error) {
	var values []string
	err := walkLeaves(config, paths, func(path string, value reflect.Value) error {
		if !value.IsValid() || value.Kind() != reflect.String {
			return fmt.Errorf("%w: resolved sensitive input %s must be a string", interfaces.ErrValidation, path)
		}
		if value.String() != "" {
			values = append(values, value.String())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(values, func(a, b string) int {
		if order := cmp.Compare(len(b), len(a)); order != 0 {
			return order
		}
		return strings.Compare(a, b)
	})
	var pairs []string
	for _, value := range slices.Compact(values) {
		pairs = append(pairs, value, "[redacted]")
	}
	return strings.NewReplacer(pairs...).Replace, nil
}
