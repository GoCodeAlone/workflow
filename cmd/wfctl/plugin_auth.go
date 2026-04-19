package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// applyPrivateAuth sets up git URL rewriting so Go tooling can fetch modules
// from private repositories. It reads the token from the named environment
// variable, writes a git global insteadOf rule, and prepends the domain to
// GOPRIVATE. The returned cleanup func reverses both changes.
func applyPrivateAuth(envVar, domain string) (cleanup func(), err error) {
	token := os.Getenv(envVar)
	if token == "" {
		return nil, fmt.Errorf("private plugin auth: env var %s is not set or empty", envVar)
	}

	rewriteURL := fmt.Sprintf("https://x-access-token:%s@%s/", token, domain)
	targetURL := fmt.Sprintf("https://%s/", domain)

	// Write git insteadOf rule.
	setArgs := []string{"config", "--global",
		fmt.Sprintf("url.%s.insteadOf", rewriteURL),
		targetURL,
	}
	if out, err := exec.Command("git", setArgs...).CombinedOutput(); err != nil { //nolint:gosec
		return nil, fmt.Errorf("git config insteadOf: %w\n%s", err, out)
	}

	// Prepend domain to GOPRIVATE.
	origGOPRIVATE := os.Getenv("GOPRIVATE")
	newGOPRIVATE := domain
	if origGOPRIVATE != "" {
		newGOPRIVATE = domain + "," + origGOPRIVATE
	}
	os.Setenv("GOPRIVATE", newGOPRIVATE) //nolint:errcheck

	cleanup = func() {
		// Remove the insteadOf rule.
		unsetArgs := []string{"config", "--global", "--unset-all",
			fmt.Sprintf("url.%s.insteadOf", rewriteURL),
		}
		exec.Command("git", unsetArgs...).Run() //nolint:gosec,errcheck

		// Restore GOPRIVATE.
		if origGOPRIVATE == "" {
			os.Unsetenv("GOPRIVATE") //nolint:errcheck
		} else {
			os.Setenv("GOPRIVATE", origGOPRIVATE) //nolint:errcheck
		}
	}
	return cleanup, nil
}

// extractDomain derives the hostname from a Go module source path.
// e.g. "github.com/MyOrg/repo" → "github.com"
func extractDomain(source string) string {
	parts := strings.SplitN(source, "/", 2)
	return parts[0]
}
