package bdd

import (
	"encoding/json"
	"fmt"
)

// mapToJSON serialises a map to a compact JSON string.
func mapToJSON(m map[string]any) (string, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("mapToJSON: %w", err)
	}
	return string(b), nil
}
