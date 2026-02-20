package bundle

import "github.com/GoCodeAlone/workflow/config"

// BundleFormatVersion is the current bundle format version.
const BundleFormatVersion = "1.0"

// Manifest describes the contents of a workflow bundle.
type Manifest struct {
	Version     string                 `json:"version"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Files       []string               `json:"files"`
	Requires    *config.RequiresConfig `json:"requires,omitempty"`
}
