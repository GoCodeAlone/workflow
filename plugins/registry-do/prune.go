package registrydo

import (
	"fmt"
	"regexp"
	"strings"
)

var pruneComponent = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$`)

// validatePrunePath requires an exact registry and image repository. It never
// infers a repository from a registry root or accepts a tag/digest as a scope.
func validatePrunePath(path string) error {
	parts := strings.Split(path, "/")
	if len(parts) < 3 || parts[0] != "registry.digitalocean.com" {
		return fmt.Errorf("do prune requires an exact registry.digitalocean.com/<registry>/<repository> path")
	}
	for _, part := range parts[1:] {
		if !pruneComponent.MatchString(part) {
			return fmt.Errorf("do prune requires an exact registry.digitalocean.com/<registry>/<repository> path")
		}
	}
	return nil
}
