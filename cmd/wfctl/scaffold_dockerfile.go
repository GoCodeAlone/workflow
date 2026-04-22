package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed templates/Dockerfile.prebuilt.generic.tmpl
var dockerfileGenericTmpl string

//go:embed templates/Dockerfile.prebuilt.library.tmpl
var dockerfileLibraryTmpl string

// distrolessDefault is the canonical hardened base image for workflow apps.
// Digest-pinned to prevent supply-chain substitution attacks.
// Pin updated via: crane digest gcr.io/distroless/base-debian12:nonroot
const distrolessDefault = "gcr.io/distroless/base-debian12:nonroot@sha256:b55b14b75c7c1a1a74cf948e8a5c3c0618fa9c1f1a32f6e77d60a2d9bf6eba0c"

// shellContainingBases is the set of base images known to include a shell.
// Using these without --allow-shell is blocked by policy.
var shellContainingBases = []string{
	"ubuntu", "debian", "fedora", "centos", "rhel", "amazonlinux",
	"amazonlinux2", "oraclelinux", "busybox",
}

// glibcWarningBases are allowed but trigger a warning about attack surface.
var glibcWarningBases = []string{
	"alpine", "scratch",
}

// scaffoldDockerfileArgs holds parsed arguments for scaffoldDockerfile.
type scaffoldDockerfileArgs struct {
	mode       string // "generic" (default) | "library"
	binary     string // required for library mode
	baseImage  string // overrides distrolessDefault when non-empty
	allowShell bool   // bypass shell-containing base check
	outputDir  string // where to write Dockerfile.prebuilt (default: ".")
}

// runScaffold dispatches wfctl scaffold subcommands.
func runScaffold(args []string) error {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, `Usage: wfctl scaffold <subcommand> [options]

Subcommands:
  dockerfile   Generate a hardened Dockerfile.prebuilt

Run 'wfctl scaffold <subcommand> -h' for subcommand-specific help.
`)
		return fmt.Errorf("subcommand required")
	}
	switch args[0] {
	case "dockerfile":
		return runScaffoldDockerfile(args[1:])
	default:
		return fmt.Errorf("unknown scaffold subcommand %q", args[0])
	}
}

// runScaffoldDockerfile implements `wfctl scaffold dockerfile`.
func runScaffoldDockerfile(args []string) error {
	fs := flag.NewFlagSet("scaffold dockerfile", flag.ContinueOnError)
	mode := fs.String("mode", "generic", "Template mode: generic | library")
	binary := fs.String("binary", "", "Binary name for library mode (required when --mode=library)")
	baseImage := fs.String("base-image", "", "Override base image (default: distroless/base-debian12:nonroot@digest)")
	allowShell := fs.Bool("allow-shell", false, "Allow shell-containing base images (bypasses security block)")
	outputDir := fs.String("output", ".", "Output directory for Dockerfile.prebuilt")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl scaffold dockerfile [options]

Generate a hardened Dockerfile.prebuilt for use with wfctl build.

Examples:
  wfctl scaffold dockerfile
  wfctl scaffold dockerfile --mode=library --binary=bmw-server
  wfctl scaffold dockerfile --base-image=gcr.io/distroless/static:nonroot

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	return scaffoldDockerfile(scaffoldDockerfileArgs{
		mode:       *mode,
		binary:     *binary,
		baseImage:  *baseImage,
		allowShell: *allowShell,
		outputDir:  *outputDir,
	})
}

// scaffoldDockerfile generates Dockerfile.prebuilt from the given arguments.
func scaffoldDockerfile(a scaffoldDockerfileArgs) error {
	// Default mode to generic.
	if a.mode == "" {
		a.mode = "generic"
	}

	// Validate mode.
	switch a.mode {
	case "generic", "library":
		// valid
	default:
		return fmt.Errorf("unknown --mode %q: must be generic or library", a.mode)
	}

	// Library mode requires a binary name.
	if a.mode == "library" && a.binary == "" {
		return fmt.Errorf("--binary is required for library mode")
	}

	// Resolve base image.
	base := distrolessDefault
	if a.baseImage != "" {
		base = a.baseImage
		if err := validateBaseImage(base, a.allowShell); err != nil {
			return err
		}
	}

	// Select template.
	var tmplSrc string
	switch a.mode {
	case "generic":
		tmplSrc = dockerfileGenericTmpl
	case "library":
		tmplSrc = dockerfileLibraryTmpl
	}

	// Render template.
	tmpl, err := template.New("dockerfile").Parse(tmplSrc)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	data := struct {
		BaseImage string
		Binary    string
	}{
		BaseImage: base,
		Binary:    a.binary,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	// Write output.
	outputDir := a.outputDir
	if outputDir == "" {
		outputDir = "."
	}
	outPath := filepath.Join(outputDir, "Dockerfile.prebuilt")
	if err := os.WriteFile(outPath, buf.Bytes(), 0600); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}

	fmt.Printf("Wrote %s\n", outPath)
	return nil
}

// imageBaseName returns the last path segment of an image reference,
// stripped of any tag or digest suffix. This handles both short refs
// ("ubuntu:22.04") and fully-qualified refs ("docker.io/library/ubuntu:22.04").
func imageBaseName(ref string) string {
	// Strip digest first.
	if idx := strings.Index(ref, "@"); idx != -1 {
		ref = ref[:idx]
	}
	// A colon after the last slash is a tag separator; a colon before the last
	// slash (e.g. registry host:port) is part of the address.
	if idx := strings.LastIndex(ref, ":"); idx != -1 && !strings.Contains(ref[idx:], "/") {
		ref = ref[:idx]
	}
	// Take last path segment.
	if idx := strings.LastIndex(ref, "/"); idx != -1 {
		ref = ref[idx+1:]
	}
	return strings.ToLower(ref)
}

// validateBaseImage checks the base image against security policy.
// Warning images (alpine) continue; shell-containing images are blocked unless
// --allow-shell is passed.
func validateBaseImage(base string, allowShell bool) error {
	name := imageBaseName(base)

	for _, s := range shellContainingBases {
		if name == strings.ToLower(s) {
			if !allowShell {
				return fmt.Errorf("base image %q contains a shell — use a distroless image or pass --allow-shell to override", base)
			}
			return nil
		}
	}

	for _, w := range glibcWarningBases {
		if name == strings.ToLower(w) {
			fmt.Fprintf(os.Stderr, "warning: base image %q has a large attack surface; consider gcr.io/distroless/base-debian12:nonroot\n", base)
			return nil
		}
	}

	return nil
}
