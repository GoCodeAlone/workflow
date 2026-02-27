package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
)

func runPluginValidate(args []string) error {
	fs := flag.NewFlagSet("plugin validate", flag.ContinueOnError)
	filePath := fs.String("file", "", "Validate a local manifest file instead of fetching from registry")
	all := fs.Bool("all", false, "Validate all plugins in the configured registries")
	verifyURLs := fs.Bool("verify-urls", false, "HEAD-check download URLs for reachability")
	cfgPath := fs.String("config", "", "Registry config file path")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin validate [options] [<name>]\n\nValidate a plugin manifest from the registry or a local file.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	opts := ValidationOptions{
		VerifyURLs: *verifyURLs,
		TargetOS:   runtime.GOOS,
		TargetArch: runtime.GOARCH,
	}

	// Validate a local file
	if *filePath != "" {
		return validateLocalManifest(*filePath, opts)
	}

	// Validate all plugins across configured registries
	if *all {
		return validateAllPlugins(*cfgPath, opts)
	}

	// Validate a single plugin by name
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("plugin name, --file, or --all is required")
	}

	pluginName := fs.Arg(0)
	return validateSinglePlugin(pluginName, *cfgPath, opts)
}

func validateLocalManifest(path string, opts ValidationOptions) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var m RegistryManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	errs := ValidateManifest(&m, opts)
	if len(errs) == 0 {
		fmt.Printf("OK  %s v%s (%s)\n", m.Name, m.Version, path)
		return nil
	}
	fmt.Printf("FAIL  %s v%s (%s)\n", m.Name, m.Version, path)
	fmt.Print(FormatValidationErrors(errs))
	return fmt.Errorf("%d validation error(s)", len(errs))
}

func validateSinglePlugin(name, cfgPath string, opts ValidationOptions) error {
	cfg, err := LoadRegistryConfig(cfgPath)
	if err != nil {
		return err
	}
	mr := NewMultiRegistry(cfg)
	manifest, source, err := mr.FetchManifest(name)
	if err != nil {
		return err
	}

	errs := ValidateManifest(manifest, opts)
	if len(errs) == 0 {
		fmt.Printf("OK  %s v%s (from %s)\n", manifest.Name, manifest.Version, source)
		return nil
	}
	fmt.Printf("FAIL  %s v%s (from %s)\n", manifest.Name, manifest.Version, source)
	fmt.Print(FormatValidationErrors(errs))
	return fmt.Errorf("%d validation error(s)", len(errs))
}

func validateAllPlugins(cfgPath string, opts ValidationOptions) error {
	cfg, err := LoadRegistryConfig(cfgPath)
	if err != nil {
		return err
	}
	mr := NewMultiRegistry(cfg)

	names, err := mr.ListPlugins()
	if err != nil {
		return fmt.Errorf("list plugins: %w", err)
	}

	var totalErrs int
	passCount := 0
	failCount := 0

	for _, name := range names {
		manifest, source, fetchErr := mr.FetchManifest(name)
		if fetchErr != nil {
			fmt.Printf("SKIP  %s (fetch error: %v)\n", name, fetchErr)
			continue
		}
		errs := ValidateManifest(manifest, opts)
		if len(errs) == 0 {
			fmt.Printf("OK    %s v%s (from %s)\n", manifest.Name, manifest.Version, source)
			passCount++
		} else {
			fmt.Printf("FAIL  %s v%s (from %s)\n", manifest.Name, manifest.Version, source)
			fmt.Print(FormatValidationErrors(errs))
			failCount++
			totalErrs += len(errs)
		}
	}

	fmt.Printf("\n%d passed, %d failed (%d total errors)\n", passCount, failCount, totalErrs)
	if failCount > 0 {
		return fmt.Errorf("%d plugin(s) failed validation", failCount)
	}
	return nil
}
