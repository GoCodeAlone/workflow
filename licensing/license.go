// Package licensing provides license validation and feature gating for the workflow engine.
package licensing

import (
	"context"
	"time"
)

// LicenseInfo contains the details of a validated license.
type LicenseInfo struct {
	Key          string    `json:"key"`
	Tier         string    `json:"tier"` // starter, professional, enterprise
	Organization string    `json:"organization"`
	ExpiresAt    time.Time `json:"expires_at"`
	MaxWorkflows int       `json:"max_workflows"`
	MaxPlugins   int       `json:"max_plugins"`
	Features     []string  `json:"features"`
}

// ValidationResult holds the outcome of a license validation attempt.
type ValidationResult struct {
	Valid       bool        `json:"valid"`
	License     LicenseInfo `json:"license,omitempty"`
	Error       string      `json:"error,omitempty"`
	CachedUntil time.Time   `json:"cached_until"`
}

// Validator is the interface for license validation and feature checking.
type Validator interface {
	Validate(ctx context.Context, key string) (*ValidationResult, error)
	CheckFeature(feature string) bool
	GetLicenseInfo() *LicenseInfo
}
