package main

import "regexp"

// PublishGradeSemverRe matches strict release-grade semver tags (flat M.m.p,
// no prerelease, no build metadata). Engine ParseSemver requires this shape.
//
// Shared by:
//   - wfctl plugin validate-contract --for-publish (operator-side gate)
//   - wfctl plugin registry-sync (registry-side gate)
//
// Single source of truth per workflow#762; eliminates the regex duplication
// between operator-side and registry-side gates.
var PublishGradeSemverRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
