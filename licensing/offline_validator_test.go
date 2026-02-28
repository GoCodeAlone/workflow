package licensing_test

import (
	"context"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/licensing"
	"github.com/GoCodeAlone/workflow/pkg/license"
)

// buildSignedToken creates a key pair and a signed token for use in tests.
func buildSignedToken(t *testing.T, tier string, features []string, expiresIn time.Duration) (pubPEM []byte, tokenStr string) {
	t.Helper()
	pub, priv, err := license.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	tok := &license.LicenseToken{
		LicenseID:    "lic-123",
		TenantID:     "tenant-abc",
		Organization: "TestOrg",
		Tier:         tier,
		Features:     features,
		MaxWorkflows: 10,
		MaxPlugins:   20,
		IssuedAt:     time.Now().Unix(),
		ExpiresAt:    time.Now().Add(expiresIn).Unix(),
	}
	tokenStr, err = tok.Sign(priv)
	if err != nil {
		t.Fatal(err)
	}
	return license.MarshalPublicKeyPEM(pub), tokenStr
}

func TestOfflineValidator_ValidToken(t *testing.T) {
	pubPEM, tokenStr := buildSignedToken(t, "enterprise", []string{"my-plugin", "other-feature"}, time.Hour)

	v, err := licensing.NewOfflineValidator(pubPEM, tokenStr)
	if err != nil {
		t.Fatalf("NewOfflineValidator: %v", err)
	}

	result, err := v.Validate(context.Background(), tokenStr)
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected valid result, got: %s", result.Error)
	}
	if result.License.Tier != "enterprise" {
		t.Errorf("Tier: got %q, want enterprise", result.License.Tier)
	}
	if result.License.Organization != "TestOrg" {
		t.Errorf("Organization: got %q, want TestOrg", result.License.Organization)
	}
}

func TestOfflineValidator_CheckFeature(t *testing.T) {
	pubPEM, tokenStr := buildSignedToken(t, "enterprise", []string{"my-plugin", "audit-log"}, time.Hour)

	v, err := licensing.NewOfflineValidator(pubPEM, tokenStr)
	if err != nil {
		t.Fatal(err)
	}

	if !v.CheckFeature("my-plugin") {
		t.Error(`CheckFeature("my-plugin") should return true`)
	}
	if !v.CheckFeature("audit-log") {
		t.Error(`CheckFeature("audit-log") should return true`)
	}
	if v.CheckFeature("nonexistent") {
		t.Error(`CheckFeature("nonexistent") should return false`)
	}
}

func TestOfflineValidator_ValidatePlugin_Success(t *testing.T) {
	pubPEM, tokenStr := buildSignedToken(t, "enterprise", []string{"my-plugin"}, time.Hour)

	v, err := licensing.NewOfflineValidator(pubPEM, tokenStr)
	if err != nil {
		t.Fatal(err)
	}

	if err := v.ValidatePlugin("my-plugin"); err != nil {
		t.Errorf("ValidatePlugin should succeed: %v", err)
	}
}

func TestOfflineValidator_ValidatePlugin_ExpiredToken(t *testing.T) {
	pubPEM, tokenStr := buildSignedToken(t, "enterprise", []string{"my-plugin"}, -time.Hour)

	v, err := licensing.NewOfflineValidator(pubPEM, tokenStr)
	if err != nil {
		t.Fatal(err)
	}

	if err := v.ValidatePlugin("my-plugin"); err == nil {
		t.Error("ValidatePlugin should fail for expired token")
	}
}

func TestOfflineValidator_ValidatePlugin_WrongTier(t *testing.T) {
	pubPEM, tokenStr := buildSignedToken(t, "starter", []string{"my-plugin"}, time.Hour)

	v, err := licensing.NewOfflineValidator(pubPEM, tokenStr)
	if err != nil {
		t.Fatal(err)
	}

	if err := v.ValidatePlugin("my-plugin"); err == nil {
		t.Error("ValidatePlugin should fail for starter tier")
	}
}

func TestOfflineValidator_ValidatePlugin_MissingFeature(t *testing.T) {
	pubPEM, tokenStr := buildSignedToken(t, "enterprise", []string{"other-plugin"}, time.Hour)

	v, err := licensing.NewOfflineValidator(pubPEM, tokenStr)
	if err != nil {
		t.Fatal(err)
	}

	if err := v.ValidatePlugin("my-plugin"); err == nil {
		t.Error("ValidatePlugin should fail when plugin not in feature list")
	}
}

func TestOfflineValidator_GetLicenseInfo_Expired(t *testing.T) {
	pubPEM, tokenStr := buildSignedToken(t, "enterprise", []string{}, -time.Hour)

	v, err := licensing.NewOfflineValidator(pubPEM, tokenStr)
	if err != nil {
		t.Fatal(err)
	}

	if info := v.GetLicenseInfo(); info != nil {
		t.Error("GetLicenseInfo should return nil for expired token")
	}
}

func TestOfflineValidator_WrongKey(t *testing.T) {
	_, tokenStr := buildSignedToken(t, "enterprise", []string{}, time.Hour)
	// Generate a different key pair for verification
	wrongPub, _, err := license.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	wrongPubPEM := license.MarshalPublicKeyPEM(wrongPub)

	_, err = licensing.NewOfflineValidator(wrongPubPEM, tokenStr)
	if err == nil {
		t.Error("NewOfflineValidator should fail when key doesn't match token signature")
	}
}

func TestOfflineValidator_KeyMismatch_Validate(t *testing.T) {
	pubPEM, tokenStr := buildSignedToken(t, "enterprise", []string{}, time.Hour)

	v, err := licensing.NewOfflineValidator(pubPEM, tokenStr)
	if err != nil {
		t.Fatal(err)
	}

	result, err := v.Validate(context.Background(), "wrong-token-string")
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if result.Valid {
		t.Error("Validate should return invalid when key doesn't match token string")
	}
}

func TestOfflineValidator_CanLoadPlugin(t *testing.T) {
	pubPEM, tokenStr := buildSignedToken(t, "professional", []string{}, time.Hour)

	v, err := licensing.NewOfflineValidator(pubPEM, tokenStr)
	if err != nil {
		t.Fatal(err)
	}

	if !v.CanLoadPlugin("core") {
		t.Error("CanLoadPlugin(core) should return true")
	}
	if !v.CanLoadPlugin("community") {
		t.Error("CanLoadPlugin(community) should return true")
	}
	if !v.CanLoadPlugin("premium") {
		t.Error("CanLoadPlugin(premium) should return true for professional tier")
	}

	// Starter tier should not allow premium
	pubPEM2, tokenStr2 := buildSignedToken(t, "starter", []string{}, time.Hour)
	v2, err := licensing.NewOfflineValidator(pubPEM2, tokenStr2)
	if err != nil {
		t.Fatal(err)
	}
	if v2.CanLoadPlugin("premium") {
		t.Error("CanLoadPlugin(premium) should return false for starter tier")
	}
}

func TestCompositeValidator_OfflineAccepts(t *testing.T) {
	pubPEM, tokenStr := buildSignedToken(t, "enterprise", []string{"my-plugin"}, time.Hour)

	offline, err := licensing.NewOfflineValidator(pubPEM, tokenStr)
	if err != nil {
		t.Fatal(err)
	}

	cv := licensing.NewCompositeValidator(offline, nil)

	result, err := cv.Validate(context.Background(), tokenStr)
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected valid result: %s", result.Error)
	}
}

func TestCompositeValidator_OfflineRejectsHTTPFallback(t *testing.T) {
	pubPEM, tokenStr := buildSignedToken(t, "enterprise", []string{}, time.Hour)

	offline, err := licensing.NewOfflineValidator(pubPEM, tokenStr)
	if err != nil {
		t.Fatal(err)
	}

	// HTTPValidator with empty server URL returns a valid starter result
	httpV := licensing.NewHTTPValidator(licensing.ValidatorConfig{}, nil)

	cv := licensing.NewCompositeValidator(offline, httpV)

	// Use a different key — offline will reject, HTTP (no server) will return valid
	result, err := cv.Validate(context.Background(), "some-other-license-key")
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected HTTP fallback to produce valid result: %s", result.Error)
	}
}

func TestCompositeValidator_ValidatePlugin_DelegatesToOffline(t *testing.T) {
	pubPEM, tokenStr := buildSignedToken(t, "enterprise", []string{"authorized-plugin"}, time.Hour)

	offline, err := licensing.NewOfflineValidator(pubPEM, tokenStr)
	if err != nil {
		t.Fatal(err)
	}

	cv := licensing.NewCompositeValidator(offline, nil)

	if err := cv.ValidatePlugin("authorized-plugin"); err != nil {
		t.Errorf("ValidatePlugin should succeed: %v", err)
	}
	if err := cv.ValidatePlugin("unauthorized-plugin"); err == nil {
		t.Error("ValidatePlugin should fail for unlicensed plugin")
	}
}

func TestCompositeValidator_GetLicenseInfo(t *testing.T) {
	pubPEM, tokenStr := buildSignedToken(t, "enterprise", []string{}, time.Hour)

	offline, err := licensing.NewOfflineValidator(pubPEM, tokenStr)
	if err != nil {
		t.Fatal(err)
	}

	cv := licensing.NewCompositeValidator(offline, nil)

	info := cv.GetLicenseInfo()
	if info == nil {
		t.Fatal("GetLicenseInfo should return non-nil for valid offline token")
	}
	if info.Tier != "enterprise" {
		t.Errorf("Tier: got %q, want enterprise", info.Tier)
	}
}
