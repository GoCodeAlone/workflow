package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

func runGenerate(args []string) error {
	if len(args) < 1 {
		return generateUsage()
	}
	switch args[0] {
	case "github-actions":
		return runGenerateGithubActions(args[1:])
	default:
		return generateUsage()
	}
}

func generateUsage() error {
	fmt.Fprintf(os.Stderr, `Usage: wfctl generate <subcommand> [options]

Generate files for your workflow project.

Subcommands:
  github-actions   Generate GitHub Actions CI/CD workflow files

Examples:
  wfctl generate github-actions workflow.yaml
  wfctl generate github-actions -output .github/workflows/ -registry ghcr.io workflow.yaml
`)
	return fmt.Errorf("subcommand is required (github-actions)")
}

// projectFeatures captures what was detected in the workflow config and project directory.
type projectFeatures struct {
	hasUI       bool
	hasAuth     bool
	hasDatabase bool
	hasPlugin   bool
	hasHTTP     bool
	configFile  string
}

const (
	githubActionsCheckoutRef  = "actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3"
	githubActionsSetupGoRef   = "actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0"
	githubActionsSetupNodeRef = "actions/setup-node@48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e # v6.4.0"
	// #nosec G101 -- action commit SHA, not a credential.
	githubActionsSetupWfctlRef        = "GoCodeAlone/setup-wfctl@526e23ee7d3cae9ba8ba09d87090879e04c7aab2 # v1"
	githubActionsDockerLoginRef       = "docker/login-action@c94ce9fb468520275223c153574b00df6fe4bcc9 # v3"
	githubActionsDockerSetupBuildxRef = "docker/setup-buildx-action@f7ce87c1d6bead3e36075b2ce75da1f6cc28aaca # v3.9.0"
	githubActionsDockerBuildPushRef   = "docker/build-push-action@4f58ea79222b3b9dc2c8bbdd6debcef730109a75 # v6.9.0"
)

func runGenerateGithubActions(args []string) error {
	fs := flag.NewFlagSet("generate github-actions", flag.ContinueOnError)
	output := fs.String("output", ".github/workflows/", "Output directory for generated workflow files")
	genCI := fs.Bool("ci", true, "Generate CI workflow (lint, test, validate)")
	genCD := fs.Bool("cd", true, "Generate CD workflow (build, deploy)")
	registry := fs.String("registry", "ghcr.io", "Container registry for Docker images")
	platforms := fs.String("platforms", "linux/amd64,linux/arm64", "Platforms to build for")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl generate github-actions [options] <config.yaml>

Generate GitHub Actions CI/CD workflow files based on config analysis.

Options:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("config file path is required")
	}

	configFile := fs.Arg(0)
	features, err := detectProjectFeatures(configFile)
	if err != nil {
		return fmt.Errorf("failed to analyze config: %w", err)
	}

	if err := os.MkdirAll(*output, 0750); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", *output, err)
	}

	var generated []string

	if *genCI {
		ciPath := filepath.Join(*output, "ci.yml")
		if err := writeCIWorkflow(ciPath, features); err != nil {
			return fmt.Errorf("failed to generate CI workflow: %w", err)
		}
		generated = append(generated, ciPath)
		fmt.Printf("  create  %s\n", ciPath)
	}

	if *genCD {
		cdPath := filepath.Join(*output, "cd.yml")
		if err := writeCDWorkflow(cdPath, features, *registry, *platforms); err != nil {
			return fmt.Errorf("failed to generate CD workflow: %w", err)
		}
		generated = append(generated, cdPath)
		fmt.Printf("  create  %s\n", cdPath)
	}

	if features.hasPlugin {
		relPath := filepath.Join(*output, "release.yml")
		if err := writeReleaseWorkflow(relPath); err != nil {
			return fmt.Errorf("failed to generate release workflow: %w", err)
		}
		generated = append(generated, relPath)
		fmt.Printf("  create  %s\n", relPath)
	}

	fmt.Printf("\nGenerated %d GitHub Actions workflow file(s).\n", len(generated))
	return nil
}

// detectProjectFeatures reads the config file and surrounding project to determine what features are present.
func detectProjectFeatures(configFile string) (*projectFeatures, error) {
	features := &projectFeatures{configFile: configFile}

	cfg, err := config.LoadFromFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config %s: %w", configFile, err)
	}

	for _, mod := range cfg.Modules {
		t := strings.ToLower(mod.Type)
		switch {
		case strings.HasPrefix(t, "static.") || t == "static.fileserver":
			features.hasUI = true
		case strings.HasPrefix(t, "auth.") || strings.Contains(t, "jwt") || strings.Contains(t, "auth"):
			features.hasAuth = true
		case strings.HasPrefix(t, "storage.") || strings.HasPrefix(t, "database.") ||
			strings.Contains(t, "sqlite") || strings.Contains(t, "postgres") || strings.Contains(t, "mysql"):
			features.hasDatabase = true
		case strings.HasPrefix(t, "http.server") || strings.HasPrefix(t, "http.router"):
			features.hasHTTP = true
		}
	}

	// Check for plugin.json in the same directory as the config file
	configDir := filepath.Dir(configFile)
	if _, err := os.Stat(filepath.Join(configDir, "plugin.json")); err == nil {
		features.hasPlugin = true
	}

	return features, nil
}

func writeCIWorkflow(path string, features *projectFeatures) error {
	var b strings.Builder

	b.WriteString("name: CI\n")
	b.WriteString("on:\n")
	b.WriteString("  pull_request:\n")
	b.WriteString("    branches: [main]\n")
	b.WriteString("  push:\n")
	b.WriteString("    branches: [main]\n")
	b.WriteString("\n")
	b.WriteString("jobs:\n")
	b.WriteString("  validate:\n")
	b.WriteString("    runs-on: ubuntu-latest\n")
	b.WriteString("    steps:\n")
	fmt.Fprintf(&b, "      - uses: %s\n", githubActionsCheckoutRef)
	fmt.Fprintf(&b, "      - uses: %s\n", githubActionsSetupGoRef)
	b.WriteString("        with:\n")
	b.WriteString("          go-version: '1.26.4'\n")
	b.WriteString("      - name: Install wfctl\n")
	b.WriteString("        run: go install github.com/GoCodeAlone/workflow/cmd/wfctl@latest\n")
	b.WriteString("      - name: Validate config\n")
	fmt.Fprintf(&b, "        run: wfctl validate %s\n", features.configFile)
	b.WriteString("      - name: Inspect config\n")
	fmt.Fprintf(&b, "        run: wfctl inspect %s\n", features.configFile)

	if features.hasUI {
		fmt.Fprintf(&b, "      - uses: %s\n", githubActionsSetupNodeRef)
		b.WriteString("        with:\n")
		b.WriteString("          node-version: '24'\n")
		b.WriteString("      - name: Build UI\n")
		b.WriteString("        run: wfctl build-ui --ui-dir ui\n")
	}

	if features.hasAuth {
		b.WriteString("      - name: Verify secrets setup\n")
		b.WriteString("        run: echo \"Secrets configured for auth modules\"\n")
		b.WriteString("        env:\n")
		b.WriteString("          JWT_SECRET: ${{ secrets.JWT_SECRET }}\n")
	}

	if features.hasDatabase {
		b.WriteString("      - name: Run migrations\n")
		b.WriteString("        run: wfctl migrate --config " + features.configFile + "\n")
		b.WriteString("        continue-on-error: true\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0600)
}

func writeCDWorkflow(path string, features *projectFeatures, registry, platforms string) error {
	var b strings.Builder

	b.WriteString("name: CD\n")
	b.WriteString("on:\n")
	b.WriteString("  push:\n")
	b.WriteString("    tags: ['v*']\n")
	b.WriteString("\n")
	b.WriteString("env:\n")
	fmt.Fprintf(&b, "  REGISTRY: %s\n", registry)
	b.WriteString("\n")
	b.WriteString("jobs:\n")
	b.WriteString("  build:\n")
	b.WriteString("    runs-on: ubuntu-latest\n")
	b.WriteString("    permissions:\n")
	b.WriteString("      contents: read\n")
	b.WriteString("      packages: write\n")
	b.WriteString("    steps:\n")
	fmt.Fprintf(&b, "      - uses: %s\n", githubActionsCheckoutRef)
	fmt.Fprintf(&b, "      - uses: %s\n", githubActionsSetupGoRef)
	b.WriteString("        with:\n")
	b.WriteString("          go-version: '1.26.4'\n")

	if features.hasUI {
		fmt.Fprintf(&b, "      - uses: %s\n", githubActionsSetupNodeRef)
		b.WriteString("        with:\n")
		b.WriteString("          node-version: '24'\n")
		b.WriteString("      - name: Build UI\n")
		b.WriteString("        run: |\n")
		b.WriteString("          cd ui && npm ci && npm run build && cd ..\n")
	}

	b.WriteString("      - name: Build binary\n")
	b.WriteString("        run: |\n")
	b.WriteString("          GOOS=linux GOARCH=amd64 go build -o bin/server ./cmd/server/\n")
	b.WriteString("      - name: Log in to registry\n")
	fmt.Fprintf(&b, "        uses: %s\n", githubActionsDockerLoginRef)
	b.WriteString("        with:\n")
	b.WriteString("          registry: ${{ env.REGISTRY }}\n")
	b.WriteString("          username: ${{ github.actor }}\n")
	b.WriteString("          password: ${{ secrets.GITHUB_TOKEN }}\n")
	b.WriteString("      - name: Set up Docker Buildx\n")
	fmt.Fprintf(&b, "        uses: %s\n", githubActionsDockerSetupBuildxRef)
	b.WriteString("      - name: Build and push Docker image\n")
	fmt.Fprintf(&b, "        uses: %s\n", githubActionsDockerBuildPushRef)
	b.WriteString("        with:\n")
	b.WriteString("          context: .\n")
	b.WriteString("          push: true\n")
	fmt.Fprintf(&b, "          platforms: %s\n", platforms)
	b.WriteString("          tags: |\n")
	b.WriteString("            ${{ env.REGISTRY }}/${{ github.repository }}:${{ github.ref_name }}\n")
	b.WriteString("            ${{ env.REGISTRY }}/${{ github.repository }}:latest\n")

	return os.WriteFile(path, []byte(b.String()), 0600)
}

func writeReleaseWorkflow(path string) error {
	content := `name: Release
on:
  push:
    tags: ['v*']

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3
      - uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0
        with:
          go-version: '1.26.4'
      - name: Build plugin binaries
        run: |
          mkdir -p dist
          for GOOS in linux darwin; do
            for GOARCH in amd64 arm64; do
              GOOS=$GOOS GOARCH=$GOARCH go build -o dist/plugin-$GOOS-$GOARCH ./cmd/*/
            done
          done
      - name: Create release
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          set -euo pipefail
          gh release create "${GITHUB_REF_NAME}" dist/* \
            --repo "${GITHUB_REPOSITORY}" \
            --verify-tag \
            --generate-notes
`
	return os.WriteFile(path, []byte(content), 0600)
}
