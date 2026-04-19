package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// GenerateSBOM produces a CycloneDX SBOM for imageRef when security.SBOM is true.
//
// Strategy (in order of preference):
//  1. In-process via github.com/anchore/syft/syft (not yet a module dep; skipped).
//  2. Shell out to the `syft` binary if it is on PATH.
//
// In dry-run mode (WFCTL_BUILD_DRY_RUN=1) it prints the planned command without
// executing it and returns nil.
func GenerateSBOM(ctx context.Context, imageRef string, sec *config.CIBuildSecurity, out io.Writer) error {
	if sec == nil || !sec.SBOM {
		return nil
	}

	sbomPath := sbomFilePath(imageRef)

	if os.Getenv("WFCTL_BUILD_DRY_RUN") == "1" {
		fmt.Fprintf(out, "[dry-run] syft %s -o cyclonedx-json > %s\n", imageRef, sbomPath)
		return nil
	}

	if err := runSyft(ctx, imageRef, sbomPath, out); err != nil {
		return fmt.Errorf("generate SBOM for %s: %w", imageRef, err)
	}

	fmt.Fprintf(out, "SBOM written to %s\n", sbomPath)
	return AttachSBOM(ctx, imageRef, sbomPath, out)
}

// AttachSBOM attaches a local SBOM file to imageRef as an OCI artifact.
// It tries oras first, then cosign, then logs a warning if neither is available.
func AttachSBOM(ctx context.Context, imageRef, sbomPath string, out io.Writer) error {
	switch {
	case os.Getenv("WFCTL_BUILD_DRY_RUN") == "1" && orasAvailable():
		fmt.Fprintf(out, "[dry-run] oras attach %s --artifact-type application/vnd.cyclonedx+json %s:application/vnd.cyclonedx+json\n", imageRef, sbomPath)
		return nil
	case os.Getenv("WFCTL_BUILD_DRY_RUN") == "1" && cosignAvailable():
		fmt.Fprintf(out, "[dry-run] cosign attach sbom --sbom %s --type cyclonedx %s\n", sbomPath, imageRef)
		return nil
	case os.Getenv("WFCTL_BUILD_DRY_RUN") == "1":
		fmt.Fprintf(out, "[dry-run] SBOM attachment skipped (neither oras nor cosign found on PATH)\n")
		return nil
	case orasAvailable():
		return attachWithOras(ctx, imageRef, sbomPath, out)
	case cosignAvailable():
		return attachWithCosign(ctx, imageRef, sbomPath, out)
	default:
		fmt.Fprintf(out, "warning: SBOM generated but not attached — install oras or cosign to enable OCI attachment\n")
		return nil
	}
}

func runSyft(ctx context.Context, imageRef, sbomPath string, out io.Writer) error {
	syftBin, err := exec.LookPath("syft")
	if err != nil {
		return fmt.Errorf("syft not found on PATH — install syft (https://github.com/anchore/syft) or add github.com/anchore/syft as a module dep")
	}

	f, err := os.Create(sbomPath) //nolint:gosec
	if err != nil {
		return fmt.Errorf("create SBOM file: %w", err)
	}
	defer f.Close()

	cmd := exec.CommandContext(ctx, syftBin, imageRef, "-o", "cyclonedx-json") //nolint:gosec
	cmd.Stdout = f
	cmd.Stderr = out
	return cmd.Run()
}

func attachWithOras(ctx context.Context, imageRef, sbomPath string, out io.Writer) error {
	args := []string{
		"attach", imageRef,
		"--artifact-type", "application/vnd.cyclonedx+json",
		sbomPath + ":application/vnd.cyclonedx+json",
	}
	cmd := exec.CommandContext(ctx, "oras", args...) //nolint:gosec
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("oras attach: %w", err)
	}
	fmt.Fprintf(out, "SBOM attached to %s via oras\n", imageRef)
	return nil
}

func attachWithCosign(ctx context.Context, imageRef, sbomPath string, out io.Writer) error {
	args := []string{"attach", "sbom", "--sbom", sbomPath, "--type", "cyclonedx", imageRef}
	cmd := exec.CommandContext(ctx, "cosign", args...) //nolint:gosec
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cosign attach sbom: %w", err)
	}
	fmt.Fprintf(out, "SBOM attached to %s via cosign\n", imageRef)
	return nil
}

// sbomFilePath returns a local path for storing the SBOM JSON.
// It sanitises the image ref to produce a valid filename.
func sbomFilePath(imageRef string) string {
	// Replace characters invalid in filenames.
	safe := strings.NewReplacer("/", "_", ":", "_", "@", "_").Replace(imageRef)
	return filepath.Join(".", safe+"-sbom.json")
}

func orasAvailable() bool {
	_, err := exec.LookPath("oras")
	return err == nil
}

func cosignAvailable() bool {
	_, err := exec.LookPath("cosign")
	return err == nil
}
