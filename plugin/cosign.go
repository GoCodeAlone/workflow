package plugin

import (
	"fmt"
	"log/slog"
	"os/exec"
)

// CosignVerifier verifies plugin binaries using cosign keyless signatures.
// It requires the cosign CLI to be installed; if not found, verification is
// skipped with a warning to support environments without cosign installed.
type CosignVerifier struct {
	OIDCIssuer            string
	AllowedIdentityRegexp string
}

// NewCosignVerifier creates a CosignVerifier for the given OIDC issuer and
// identity regexp (e.g. "https://github.com/GoCodeAlone/.*").
func NewCosignVerifier(oidcIssuer, identityRegexp string) *CosignVerifier {
	return &CosignVerifier{
		OIDCIssuer:            oidcIssuer,
		AllowedIdentityRegexp: identityRegexp,
	}
}

// Verify runs `cosign verify-blob` to validate the signature of a plugin binary.
// If cosign is not installed, a warning is logged and nil is returned so that
// deployments without cosign are not broken.
func (v *CosignVerifier) Verify(binaryPath, sigPath, certPath string) error {
	cosignBin, err := exec.LookPath("cosign")
	if err != nil {
		slog.Warn("cosign not found — skipping binary verification", "binary", binaryPath)
		return nil
	}

	cmd := exec.Command(cosignBin,
		"verify-blob",
		"--signature", sigPath,
		"--certificate", certPath,
		"--certificate-oidc-issuer", v.OIDCIssuer,
		"--certificate-identity-regexp", v.AllowedIdentityRegexp,
		binaryPath,
	)

	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return fmt.Errorf("cosign verify-blob: %w: %s", runErr, out)
	}
	return nil
}
