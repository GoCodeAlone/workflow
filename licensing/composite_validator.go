package licensing

import (
	"context"
)

// CompositeValidator tries an offline validator first, falling back to an HTTP
// validator. This enables air-gapped or low-latency license checks while keeping
// the HTTP validator as a fallback for online validation.
type CompositeValidator struct {
	offline *OfflineValidator
	http    *HTTPValidator
}

// NewCompositeValidator creates a CompositeValidator from an offline and an HTTP
// validator. Either may be nil (though at least one should be non-nil).
func NewCompositeValidator(offline *OfflineValidator, http *HTTPValidator) *CompositeValidator {
	return &CompositeValidator{offline: offline, http: http}
}

// Validate implements licensing.Validator. It tries the offline validator first;
// if the result is valid it is returned immediately. Otherwise it falls back to
// the HTTP validator.
func (c *CompositeValidator) Validate(ctx context.Context, key string) (*ValidationResult, error) {
	if c.offline != nil {
		result, err := c.offline.Validate(ctx, key)
		if err == nil && result.Valid {
			return result, nil
		}
	}
	if c.http != nil {
		return c.http.Validate(ctx, key)
	}
	return &ValidationResult{Valid: false, Error: "no validator configured"}, nil
}

// CheckFeature implements licensing.Validator. It uses the offline validator when
// available, otherwise falls back to the HTTP validator.
func (c *CompositeValidator) CheckFeature(feature string) bool {
	if c.offline != nil {
		return c.offline.CheckFeature(feature)
	}
	if c.http != nil {
		return c.http.CheckFeature(feature)
	}
	return false
}

// GetLicenseInfo implements licensing.Validator. It returns the offline license
// info when available (non-nil), otherwise falls back to the HTTP validator.
func (c *CompositeValidator) GetLicenseInfo() *LicenseInfo {
	if c.offline != nil {
		if info := c.offline.GetLicenseInfo(); info != nil {
			return info
		}
	}
	if c.http != nil {
		return c.http.GetLicenseInfo()
	}
	return nil
}

// ValidatePlugin implements plugin.LicenseValidator. It delegates to the offline
// validator, which performs the authoritative check without network calls.
func (c *CompositeValidator) ValidatePlugin(pluginName string) error {
	if c.offline != nil {
		return c.offline.ValidatePlugin(pluginName)
	}
	return nil
}

// CanLoadPlugin returns true when the offline validator permits the tier, or when
// there is no offline validator and the HTTP validator permits it.
func (c *CompositeValidator) CanLoadPlugin(tier string) bool {
	if c.offline != nil {
		return c.offline.CanLoadPlugin(tier)
	}
	if c.http != nil {
		return c.http.CanLoadPlugin(tier)
	}
	return false
}
