package licensing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// DefaultCacheTTL is the default time to cache a valid license result.
const DefaultCacheTTL = 1 * time.Hour

// DefaultGracePeriod is how long offline operation is allowed after the last
// successful validation before the license is considered expired.
const DefaultGracePeriod = 72 * time.Hour

// DefaultRefreshInterval is how often the background goroutine re-validates.
const DefaultRefreshInterval = 1 * time.Hour

// HTTPValidator validates licenses against a remote server with local caching
// and a grace period for offline operation.
type HTTPValidator struct {
	serverURL       string
	licenseKey      string
	cacheTTL        time.Duration
	gracePeriod     time.Duration
	refreshInterval time.Duration
	httpClient      *http.Client
	logger          *slog.Logger

	mu            sync.RWMutex
	cachedResult  *ValidationResult
	lastValidated time.Time // time of last successful remote validation
	stopRefresh   chan struct{}
}

// ValidatorConfig holds the configuration for creating an HTTPValidator.
type ValidatorConfig struct {
	ServerURL       string
	LicenseKey      string
	CacheTTL        time.Duration
	GracePeriod     time.Duration
	RefreshInterval time.Duration
}

// NewHTTPValidator creates a new HTTPValidator with the given config.
func NewHTTPValidator(cfg ValidatorConfig, logger *slog.Logger) *HTTPValidator {
	if logger == nil {
		logger = slog.Default()
	}
	cacheTTL := cfg.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = DefaultCacheTTL
	}
	gracePeriod := cfg.GracePeriod
	if gracePeriod <= 0 {
		gracePeriod = DefaultGracePeriod
	}
	refreshInterval := cfg.RefreshInterval
	if refreshInterval <= 0 {
		refreshInterval = DefaultRefreshInterval
	}
	return &HTTPValidator{
		serverURL:       cfg.ServerURL,
		licenseKey:      cfg.LicenseKey,
		cacheTTL:        cacheTTL,
		gracePeriod:     gracePeriod,
		refreshInterval: refreshInterval,
		httpClient:      &http.Client{Timeout: 10 * time.Second},
		logger:          logger,
		stopRefresh:     make(chan struct{}),
	}
}

// Start performs an initial validation and starts a background refresh goroutine.
func (v *HTTPValidator) Start(ctx context.Context) error {
	result, err := v.remoteValidate(ctx, v.licenseKey)
	if err != nil {
		v.logger.Warn("Initial license validation failed; will operate in grace period",
			"error", err, "grace_period", v.gracePeriod)
		// Set a failed cached result — will be re-attempted by background refresh
		v.mu.Lock()
		v.cachedResult = &ValidationResult{
			Valid:       false,
			Error:       err.Error(),
			CachedUntil: time.Now().Add(v.cacheTTL),
		}
		v.mu.Unlock()
	} else {
		v.mu.Lock()
		v.cachedResult = result
		v.lastValidated = time.Now()
		v.mu.Unlock()
	}

	go v.backgroundRefresh()
	return nil
}

// Stop signals the background refresh goroutine to exit.
func (v *HTTPValidator) Stop(_ context.Context) {
	close(v.stopRefresh)
}

// backgroundRefresh periodically re-validates the license.
func (v *HTTPValidator) backgroundRefresh() {
	ticker := time.NewTicker(v.refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-v.stopRefresh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			result, err := v.remoteValidate(ctx, v.licenseKey)
			cancel()
			if err != nil {
				v.logger.Warn("Background license refresh failed", "error", err)
				// Keep using cached result; grace period logic handled in Validate
				continue
			}
			v.mu.Lock()
			v.cachedResult = result
			v.lastValidated = time.Now()
			v.mu.Unlock()
			v.logger.Info("License refreshed", "valid", result.Valid, "tier", result.License.Tier)
		}
	}
}

// Validate returns the current license validation result.
// If a valid cached result exists, it is returned immediately.
// If the cached result is expired, remote validation is attempted.
// If remote validation fails, the grace period is checked.
func (v *HTTPValidator) Validate(ctx context.Context, key string) (*ValidationResult, error) {
	v.mu.RLock()
	cached := v.cachedResult
	lastValidated := v.lastValidated
	v.mu.RUnlock()

	// Use cache if it is still fresh
	if cached != nil && time.Now().Before(cached.CachedUntil) {
		return cached, nil
	}

	// Try remote validation
	result, err := v.remoteValidate(ctx, key)
	if err != nil {
		// Fall back to grace period logic
		if cached != nil && cached.Valid {
			gracePeriodExpiry := lastValidated.Add(v.gracePeriod)
			if time.Now().Before(gracePeriodExpiry) {
				v.logger.Warn("License server unreachable; operating in grace period",
					"expires_at", gracePeriodExpiry)
				// Extend the cache temporarily
				extendedResult := *cached
				extendedResult.CachedUntil = time.Now().Add(v.cacheTTL)
				v.mu.Lock()
				v.cachedResult = &extendedResult
				v.mu.Unlock()
				return &extendedResult, nil
			}
			// Grace period expired
			expired := &ValidationResult{
				Valid:       false,
				Error:       fmt.Sprintf("license server unreachable and grace period of %v expired", v.gracePeriod),
				CachedUntil: time.Now().Add(v.cacheTTL),
			}
			v.mu.Lock()
			v.cachedResult = expired
			v.mu.Unlock()
			return expired, nil
		}
		return nil, fmt.Errorf("license validation failed: %w", err)
	}

	v.mu.Lock()
	v.cachedResult = result
	v.lastValidated = time.Now()
	v.mu.Unlock()
	return result, nil
}

// CheckFeature returns true if the current license includes the given feature.
func (v *HTTPValidator) CheckFeature(feature string) bool {
	info := v.GetLicenseInfo()
	if info == nil {
		return false
	}
	for _, f := range info.Features {
		if f == feature {
			return true
		}
	}
	return false
}

// GetLicenseInfo returns the current cached license info, or nil if no valid
// license is cached.
func (v *HTTPValidator) GetLicenseInfo() *LicenseInfo {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.cachedResult == nil || !v.cachedResult.Valid {
		return nil
	}
	info := v.cachedResult.License
	return &info
}

// CanLoadPlugin returns true if the current license permits loading a plugin
// of the given tier.
//   - TierCore and TierCommunity: always allowed
//   - TierPremium: only allowed when license tier is "professional" or "enterprise"
func (v *HTTPValidator) CanLoadPlugin(tier string) bool {
	switch tier {
	case "core", "community":
		return true
	case "premium":
		info := v.GetLicenseInfo()
		if info == nil {
			return false
		}
		return info.Tier == "professional" || info.Tier == "enterprise"
	default:
		return false
	}
}

// validateRequest is the JSON payload sent to the license server.
type validateRequest struct {
	Key string `json:"key"`
}

// validateResponse is the JSON response from the license server.
type validateResponse struct {
	Valid   bool        `json:"valid"`
	License LicenseInfo `json:"license,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// remoteValidate performs an HTTP POST to the license server and returns the result.
func (v *HTTPValidator) remoteValidate(ctx context.Context, key string) (*ValidationResult, error) {
	if v.serverURL == "" {
		// No server configured — treat as a development/starter license
		return &ValidationResult{
			Valid: true,
			License: LicenseInfo{
				Key:          key,
				Tier:         "starter",
				Organization: "local",
				ExpiresAt:    time.Now().Add(365 * 24 * time.Hour),
				MaxWorkflows: 3,
				MaxPlugins:   5,
				Features:     []string{},
			},
			CachedUntil: time.Now().Add(v.cacheTTL),
		}, nil
	}

	payload, err := json.Marshal(validateRequest{Key: key})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.serverURL+"/validate", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.httpClient.Do(req) //nolint:gosec // G704: URL is admin-configured, not user-supplied
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	var vResp validateResponse
	if err := json.NewDecoder(resp.Body).Decode(&vResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("license server returned status %d: %s", resp.StatusCode, vResp.Error)
	}

	result := &ValidationResult{
		Valid:       vResp.Valid,
		License:     vResp.License,
		Error:       vResp.Error,
		CachedUntil: time.Now().Add(v.cacheTTL),
	}
	return result, nil
}
