package sdk

import (
	"fmt"
	"runtime/debug"
)

// ResolveBuildVersion returns the operator-visible build-version string.
//
// When declared is non-empty AND not a known dev sentinel ("", "dev",
// "(devel)"), returns declared as-is. This is the typical path for
// goreleaser-built plugin binaries where the ldflag injects the release
// tag into a package-level Version var.
//
// Otherwise consults runtime/debug.ReadBuildInfo() as fallback:
//   - "(devel) [@ shortsha[.dirty]]" when vcs.revision is set
//   - "(devel)" when no VCS info
//
// Intended call sites (plugin author chooses ANY package-level Version var):
//
//	var Version = "dev"   // ldflag-injected at release time
//
//	sdk.ServeIaCPlugin(srv, sdk.IaCServeOptions{
//	    BuildVersion: sdk.ResolveBuildVersion(internal.Version),
//	})
//	sdk.Serve(p, sdk.WithManifestProvider(m),
//	    sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)))
//
// Goreleaser config provides the tag:
//
//	ldflags:
//	  - -X github.com/<...>/internal.Version={{.Version}}
//
// Mirrors the wfctl pattern at cmd/wfctl/main.go (var version = "dev" +
// debug.ReadBuildInfo() fallback). Closes workflow#758.
func ResolveBuildVersion(declared string) string {
	switch declared {
	case "", "dev", "(devel)":
		return buildInfoVersion()
	}
	return declared
}

func buildInfoVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(devel)"
	}
	var sha, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				sha = s.Value[:7]
			} else {
				sha = s.Value
			}
		case "vcs.modified":
			if s.Value == "true" {
				modified = ".dirty"
			}
		}
	}
	if sha == "" {
		return "(devel)"
	}
	return fmt.Sprintf("(devel) [@ %s%s]", sha, modified)
}
