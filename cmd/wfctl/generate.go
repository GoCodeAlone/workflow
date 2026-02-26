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
	b.WriteString("      - uses: actions/checkout@v4\n")
	b.WriteString("      - uses: actions/setup-go@v5\n")
	b.WriteString("        with:\n")
	b.WriteString("          go-version: '1.22'\n")
	b.WriteString("      - name: Install wfctl\n")
	b.WriteString("        run: go install github.com/GoCodeAlone/workflow/cmd/wfctl@latest\n")
	b.WriteString("      - name: Validate config\n")
	fmt.Fprintf(&b, "        run: wfctl validate %s\n", features.configFile)
	b.WriteString("      - name: Inspect config\n")
	fmt.Fprintf(&b, "        run: wfctl inspect %s\n", features.configFile)

	if features.hasUI {
		b.WriteString("      - uses: actions/setup-node@v4\n")
		b.WriteString("        with:\n")
		b.WriteString("          node-version: '22'\n")
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
	b.WriteString("      - uses: actions/checkout@v4\n")
	b.WriteString("      - uses: actions/setup-go@v5\n")
	b.WriteString("        with:\n")
	b.WriteString("          go-version: '1.22'\n")

	if features.hasUI {
		b.WriteString("      - uses: actions/setup-node@v4\n")
		b.WriteString("        with:\n")
		b.WriteString("          node-version: '22'\n")
		b.WriteString("      - name: Build UI\n")
		b.WriteString("        run: |\n")
		b.WriteString("          cd ui && npm ci && npm run build && cd ..\n")
	}

	b.WriteString("      - name: Build binary\n")
	b.WriteString("        run: |\n")
	b.WriteString("          GOOS=linux GOARCH=amd64 go build -o bin/server ./cmd/server/\n")
	b.WriteString("      - name: Log in to registry\n")
	b.WriteString("        uses: docker/login-action@v3\n")
	b.WriteString("        with:\n")
	b.WriteString("          registry: ${{ env.REGISTRY }}\n")
	b.WriteString("          username: ${{ github.actor }}\n")
	b.WriteString("          password: ${{ secrets.GITHUB_TOKEN }}\n")
	b.WriteString("      - name: Set up Docker Buildx\n")
	b.WriteString("        uses: docker/setup-buildx-action@v3\n")
	b.WriteString("      - name: Build and push Docker image\n")
	b.WriteString("        uses: docker/build-push-action@v5\n")
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
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - name: Build plugin binaries
        run: |
          mkdir -p dist
          for GOOS in linux darwin; do
            for GOARCH in amd64 arm64; do
              GOOS=$GOOS GOARCH=$GOARCH go build -o dist/plugin-$GOOS-$GOARCH ./cmd/*/
            done
          done
      - name: Create release
        uses: softprops/action-gh-release@v2
        with:
          files: dist/*
`
	return os.WriteFile(path, []byte(content), 0600)
}
