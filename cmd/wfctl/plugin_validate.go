package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func runPluginValidate(args []string) error {
	fs := flag.NewFlagSet("plugin validate", flag.ContinueOnError)
	filePath := fs.String("file", "", "Validate a local manifest file instead of fetching from registry")
	all := fs.Bool("all", false, "Validate all plugins in the configured registries")
	verifyURLs := fs.Bool("verify-urls", false, "HEAD-check download URLs for reachability")
	strictContracts := fs.Bool("strict-contracts", false, "Require strict contract descriptors for advertised module, step, trigger, and service method types")
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
	contractOpts := pluginAuditOptions{StrictContracts: *strictContracts}

	// Validate a local file
	if *filePath != "" {
		return validateLocalManifest(*filePath, opts, contractOpts)
	}

	// Validate all plugins across configured registries
	if *all {
		return validateAllPlugins(*cfgPath, opts, contractOpts)
	}

	// Validate a single plugin by name
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("plugin name, --file, or --all is required")
	}

	pluginName := fs.Arg(0)
	return validateSinglePlugin(pluginName, *cfgPath, opts, contractOpts)
}

func validateLocalManifest(path string, opts ValidationOptions, contractOpts pluginAuditOptions) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var m RegistryManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	errs := ValidateManifest(&m, opts)
	errs = append(errs, validateStrictContractManifest(path, data, contractOpts)...)
	if len(errs) == 0 {
		fmt.Printf("OK  %s v%s (%s)\n", m.Name, m.Version, path)
		return nil
	}
	fmt.Printf("FAIL  %s v%s (%s)\n", m.Name, m.Version, path)
	fmt.Print(FormatValidationErrors(errs))
	return fmt.Errorf("%d validation error(s)", len(errs))
}

func validateSinglePlugin(name, cfgPath string, opts ValidationOptions, contractOpts pluginAuditOptions) error {
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
	errs = append(errs, validateStrictContractRegistryManifest(manifest, contractOpts)...)
	if len(errs) == 0 {
		fmt.Printf("OK  %s v%s (from %s)\n", manifest.Name, manifest.Version, source)
		return nil
	}
	fmt.Printf("FAIL  %s v%s (from %s)\n", manifest.Name, manifest.Version, source)
	fmt.Print(FormatValidationErrors(errs))
	return fmt.Errorf("%d validation error(s)", len(errs))
}

func validateAllPlugins(cfgPath string, opts ValidationOptions, contractOpts pluginAuditOptions) error {
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
		errs = append(errs, validateStrictContractRegistryManifest(manifest, contractOpts)...)
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

func validateStrictContractManifest(path string, data []byte, opts pluginAuditOptions) []ValidationError {
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil
	}
	repoPath := filepathOrCurrentDir(path)
	result := pluginAuditResult{RepoPath: repoPath}
	addPluginContractFindings(&result, manifest, opts)
	return validationErrorsFromContractFindings(result.Findings)
}

func validateStrictContractRegistryManifest(manifest *RegistryManifest, opts pluginAuditOptions) []ValidationError {
	if !opts.StrictContracts || manifest == nil || manifest.Capabilities == nil {
		return nil
	}
	generic := map[string]any{
		"capabilities": map[string]any{
			"moduleTypes":    stringsFromStrings(manifest.Capabilities.ModuleTypes),
			"stepTypes":      stringsFromStrings(manifest.Capabilities.StepTypes),
			"triggerTypes":   stringsFromStrings(manifest.Capabilities.TriggerTypes),
			"serviceMethods": stringsFromStrings(manifest.Capabilities.ServiceMethods),
		},
		"contracts": manifest.Contracts,
	}
	result := pluginAuditResult{RepoPath: "."}
	addPluginContractFindings(&result, generic, opts)
	return validationErrorsFromContractFindings(result.Findings)
}

func validationErrorsFromContractFindings(findings []planFinding) []ValidationError {
	var errs []ValidationError
	for _, finding := range findings {
		if finding.Level != "ERROR" {
			continue
		}
		errs = append(errs, ValidationError{
			Field:   "contracts",
			Message: fmt.Sprintf("%s: %s", finding.Code, finding.Message),
		})
	}
	return errs
}

func filepathOrCurrentDir(path string) string {
	dir := filepath.Dir(path)
	if dir == "" {
		return "."
	}
	return dir
}

func stringsFromStrings(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
