package licensing

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/workflow/pkg/license"
)

// OfflineValidator validates a license token using a local Ed25519 public key,
// with no network calls required after construction.
type OfflineValidator struct {
	tokenStr string
	token    *license.LicenseToken
}

// NewOfflineValidator parses publicKeyPEM and tokenStr, verifies the token
// signature, and returns an OfflineValidator ready for use.
func NewOfflineValidator(publicKeyPEM []byte, tokenStr string) (*OfflineValidator, error) {
	pub, err := license.UnmarshalPublicKeyPEM(publicKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	tok, err := license.Parse(tokenStr)
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	if err := tok.Verify(pub); err != nil {
		return nil, fmt.Errorf("verify token signature: %w", err)
	}
	return &OfflineValidator{tokenStr: tokenStr, token: tok}, nil
}

// Validate implements licensing.Validator. It returns a valid result when key
// matches the stored token string, and an invalid result otherwise.
func (v *OfflineValidator) Validate(_ context.Context, key string) (*ValidationResult, error) {
	if key != v.tokenStr {
		return &ValidationResult{
			Valid:       false,
			Error:       "license key does not match token",
			CachedUntil: time.Now().Add(DefaultCacheTTL),
		}, nil
	}
	return &ValidationResult{
		Valid:       true,
		License:     *v.licenseInfo(),
		CachedUntil: time.Now().Add(DefaultCacheTTL),
	}, nil
}

// CheckFeature implements licensing.Validator.
func (v *OfflineValidator) CheckFeature(feature string) bool {
	return v.token.HasFeature(feature)
}

// GetLicenseInfo implements licensing.Validator. Returns nil if the token is expired.
func (v *OfflineValidator) GetLicenseInfo() *LicenseInfo {
	if v.token.IsExpired() {
		return nil
	}
	info := v.licenseInfo()
	return info
}

// ValidatePlugin implements plugin.LicenseValidator. It returns an error if the
// token is expired, the tier is not professional or enterprise, or the plugin name
// is not listed in the token's feature set.
func (v *OfflineValidator) ValidatePlugin(pluginName string) error {
	if v.token.IsExpired() {
		return fmt.Errorf("license token is expired")
	}
	if v.token.Tier != "professional" && v.token.Tier != "enterprise" {
		return fmt.Errorf("license tier %q does not permit premium plugins", v.token.Tier)
	}
	if !v.token.HasFeature(pluginName) {
		return fmt.Errorf("plugin %q is not licensed", pluginName)
	}
	return nil
}

// CanLoadPlugin returns true when the given plugin tier is permitted by the license.
// Core and community plugins are always allowed. Premium plugins require a
// professional or enterprise tier that is not expired.
func (v *OfflineValidator) CanLoadPlugin(tier string) bool {
	switch tier {
	case "core", "community":
		return true
	case "premium":
		if v.token.IsExpired() {
			return false
		}
		return v.token.Tier == "professional" || v.token.Tier == "enterprise"
	default:
		return false
	}
}

// licenseInfo converts the stored token fields into a LicenseInfo struct.
func (v *OfflineValidator) licenseInfo() *LicenseInfo {
	return &LicenseInfo{
		Key:          v.token.LicenseID,
		Tier:         v.token.Tier,
		Organization: v.token.Organization,
		ExpiresAt:    time.Unix(v.token.ExpiresAt, 0),
		MaxWorkflows: v.token.MaxWorkflows,
		MaxPlugins:   v.token.MaxPlugins,
		Features:     v.token.Features,
	}
}
