package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

func runBuildUI(args []string) error {
	fs := flag.NewFlagSet("build-ui", flag.ContinueOnError)
	uiDir := fs.String("ui-dir", "ui", "Path to the UI source directory (default: ./ui)")
	output := fs.String("output", "", "Copy dist/ contents to this directory after build")
	validate := fs.Bool("validate", false, "Validate the build output without running the build")
	configSnippet := fs.Bool("config-snippet", false, "Print the static.fileserver YAML config snippet")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl build-ui [options]

Build the application UI using the detected package manager and framework.

The command detects the UI framework (Vite, etc.), installs dependencies,
runs the build, and validates the output. Optionally copies the built
assets to a target directory and prints the YAML config to serve the UI.

Examples:
  wfctl build-ui
  wfctl build-ui --ui-dir ./ui
  wfctl build-ui --output ./module/ui_dist
  wfctl build-ui --validate
  wfctl build-ui --config-snippet

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Resolve the UI directory to an absolute path.
	absUIDir, err := filepath.Abs(*uiDir)
	if err != nil {
		return fmt.Errorf("failed to resolve ui-dir: %w", err)
	}

	if *validate {
		return validateUIBuild(absUIDir)
	}

	// Detect framework and build command.
	info, err := detectUIFramework(absUIDir)
	if err != nil {
		return err
	}

	fmt.Printf("Detected UI framework: %s\n", info.framework)
	fmt.Printf("Build command: %s %s\n", info.packageManager, info.buildCmd)

	// Install dependencies.
	fmt.Println("Installing dependencies...")
	if err := runNPMCommand(absUIDir, info.packageManager, info.installArgs...); err != nil {
		return fmt.Errorf("dependency installation failed: %w", err)
	}

	// Run build.
	fmt.Println("Building UI...")
	buildArgs := append([]string{"run"}, info.buildCmd) //nolint:gocritic // intentional append to new slice
	if err := runNPMCommand(absUIDir, info.packageManager, buildArgs...); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	// Validate the output.
	fmt.Println("Validating build output...")
	if err := validateUIBuild(absUIDir); err != nil {
		return err
	}
	fmt.Println("Build output is valid.")

	// Copy to output directory if requested.
	if *output != "" {
		absOutput, err := filepath.Abs(*output)
		if err != nil {
			return fmt.Errorf("failed to resolve output path: %w", err)
		}
		distDir := filepath.Join(absUIDir, "dist")
		fmt.Printf("Copying dist/ to %s...\n", absOutput)
		if err := copyDir(distDir, absOutput); err != nil {
			return fmt.Errorf("failed to copy output: %w", err)
		}
		fmt.Printf("Copied build output to %s\n", absOutput)
	}

	// Print config snippet if requested.
	if *configSnippet {
		printConfigSnippet(*uiDir)
	}

	return nil
}

// uiFrameworkInfo holds detected build information for a UI project.
type uiFrameworkInfo struct {
	framework      string
	packageManager string
	installArgs    []string
	buildCmd       string
}

// detectUIFramework examines the UI directory and returns build information.
func detectUIFramework(uiDir string) (*uiFrameworkInfo, error) {
	// Must have a package.json.
	pkgJSON := filepath.Join(uiDir, "package.json")
	if _, err := os.Stat(pkgJSON); os.IsNotExist(err) {
		return nil, fmt.Errorf("no package.json found in %s — is this a Node.js UI project?", uiDir)
	}

	// Detect framework by config files.
	framework := "node"
	switch {
	case fileExists(filepath.Join(uiDir, "vite.config.ts")) || fileExists(filepath.Join(uiDir, "vite.config.js")):
		framework = "vite"
	case fileExists(filepath.Join(uiDir, "next.config.js")) || fileExists(filepath.Join(uiDir, "next.config.ts")):
		framework = "next"
	case fileExists(filepath.Join(uiDir, "angular.json")):
		framework = "angular"
	}

	// Prefer npm ci (reproducible) when package-lock.json exists, else npm install.
	installArgs := []string{"install"}
	if fileExists(filepath.Join(uiDir, "package-lock.json")) {
		installArgs = []string{"ci"}
	}

	return &uiFrameworkInfo{
		framework:      framework,
		packageManager: "npm",
		installArgs:    installArgs,
		buildCmd:       "build",
	}, nil
}

// runNPMCommand executes an npm (or compatible) command inside uiDir.
func runNPMCommand(uiDir, packageManager string, args ...string) error {
	cmd := exec.Command(packageManager, args...) //nolint:gosec // G204: uiDir and packageManager come from validated user input
	cmd.Dir = uiDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// validateUIBuild checks that the dist/ directory contains the expected output.
func validateUIBuild(uiDir string) error {
	distDir := filepath.Join(uiDir, "dist")
	if _, err := os.Stat(distDir); os.IsNotExist(err) {
		return fmt.Errorf("dist/ directory not found in %s — run 'wfctl build-ui' first", uiDir)
	}

	indexHTML := filepath.Join(distDir, "index.html")
	if _, err := os.Stat(indexHTML); os.IsNotExist(err) {
		return fmt.Errorf("dist/index.html not found — the build output is incomplete")
	}

	assetsDir := filepath.Join(distDir, "assets")
	if _, err := os.Stat(assetsDir); os.IsNotExist(err) {
		return fmt.Errorf("dist/assets/ directory not found — the build output is incomplete")
	}

	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		return fmt.Errorf("failed to read dist/assets/: %w", err)
	}

	var hasJS, hasCSS bool
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch filepath.Ext(e.Name()) {
		case ".js":
			hasJS = true
		case ".css":
			hasCSS = true
		}
	}
	if !hasJS {
		return fmt.Errorf("dist/assets/ contains no .js files — the build output is incomplete")
	}
	if !hasCSS {
		return fmt.Errorf("dist/assets/ contains no .css files — the build output is incomplete")
	}

	return nil
}

// printConfigSnippet prints the YAML config for serving the UI with static.fileserver.
func printConfigSnippet(uiDir string) {
	distPath := filepath.Join(uiDir, "dist")
	fmt.Printf(`
# Add this to your workflow YAML config to serve the UI:
modules:
  - name: "app-ui"
    type: "static.fileserver"
    config:
      root: "%s"
      prefix: "/"
      spaFallback: true
      cacheMaxAge: 3600
`, distPath)
}

// copyDir recursively copies the contents of src into dst.
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0750); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

// copyFile copies a single file from src to dst, preserving permissions.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src) //nolint:gosec // G304: src is constructed from validated user-supplied path
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) //nolint:gosec // G304: dst is constructed from validated user-supplied path
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// fileExists returns true if the file exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
