package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/GoCodeAlone/workflow/scaffold"
)

// runUI dispatches `wfctl ui <subcommand> [args]`.
func runUI(args []string) error {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, `Usage: wfctl ui <subcommand> [options]

Subcommands:
  scaffold   Generate a Vite+React+TypeScript SPA from an OpenAPI spec
  build      Build the application UI (npm install + npm run build + validate)

Run 'wfctl ui <subcommand> -h' for subcommand-specific help.
`)
		return fmt.Errorf("subcommand required")
	}

	sub := args[0]
	rest := args[1:]
	switch sub {
	case "scaffold":
		return runUIScaffold(rest)
	case "build":
		return runBuildUI(rest)
	default:
		return fmt.Errorf("unknown ui subcommand %q — use 'scaffold' or 'build'", sub)
	}
}

// runUIScaffold implements `wfctl ui scaffold`.
func runUIScaffold(args []string) error {
	fs := flag.NewFlagSet("ui scaffold", flag.ContinueOnError)
	specFile := fs.String("spec", "", "Path to OpenAPI spec file (JSON or YAML); reads stdin if not set")
	output := fs.String("output", "ui", "Output directory for the scaffolded UI")
	title := fs.String("title", "", "Application title (extracted from spec if not provided)")
	auth := fs.Bool("auth", false, "Include login/register pages (auto-detected if not set)")
	theme := fs.String("theme", "auto", "Color theme: light, dark, auto")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl ui scaffold [options]

Generate a complete Vite+React+TypeScript SPA from an OpenAPI 3.0 spec.

The generated UI is immediately buildable with:
  cd <output> && npm install && npm run build

Examples:
  wfctl ui scaffold -spec openapi.yaml -output ui
  cat openapi.json | wfctl ui scaffold -output ./frontend
  wfctl ui scaffold -spec api.yaml -title "My App" -auth -theme dark

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Read spec.
	var specBytes []byte
	var err error
	if *specFile != "" {
		specBytes, err = os.ReadFile(*specFile) //nolint:gosec // user-supplied path
		if err != nil {
			return fmt.Errorf("failed to read spec file: %w", err)
		}
	} else {
		specBytes, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read spec from stdin: %w", err)
		}
	}

	// Parse and analyze spec.
	opts := scaffold.Options{
		Title: *title,
		Theme: *theme,
		Auth:  *auth,
	}
	data, err := scaffold.AnalyzeOnly(specBytes, opts)
	if err != nil {
		return fmt.Errorf("failed to parse OpenAPI spec: %w", err)
	}

	// Resolve output directory.
	absOutput, err := filepath.Abs(*output)
	if err != nil {
		return fmt.Errorf("failed to resolve output path: %w", err)
	}

	// Generate files.
	if err := scaffold.GenerateScaffold(absOutput, *data); err != nil {
		return fmt.Errorf("scaffold generation failed: %w", err)
	}

	fmt.Printf("\nUI scaffold generated in %s/\n\n", absOutput)
	fmt.Println("Next steps:")
	fmt.Printf("  cd %s\n", *output)
	fmt.Println("  npm install")
	fmt.Println("  npm run dev      # start dev server with API proxy")
	fmt.Println("  npm run build    # production build")
	fmt.Println()
	return nil
}
