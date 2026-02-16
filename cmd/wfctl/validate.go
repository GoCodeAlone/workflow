package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	strict := fs.Bool("strict", false, "Enable strict validation (no empty modules allowed)")
	skipUnknownTypes := fs.Bool("skip-unknown-types", false, "Skip unknown module/workflow/trigger type checks")
	allowNoEntryPoints := fs.Bool("allow-no-entry-points", false, "Allow configs with no entry points (triggers, routes, subscriptions, jobs)")
	dir := fs.String("dir", "", "Validate all .yaml/.yml files in a directory (recursive)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl validate [options] <config.yaml> [config2.yaml ...]

Validate one or more workflow configuration files.

Examples:
  wfctl validate config.yaml
  wfctl validate example/*.yaml
  wfctl validate --dir ./example/
  wfctl validate --strict admin/config.yaml
  wfctl validate --skip-unknown-types example/*.yaml

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Collect files to validate
	var files []string

	if *dir != "" {
		found, err := findYAMLFiles(*dir)
		if err != nil {
			return fmt.Errorf("failed to scan directory %s: %w", *dir, err)
		}
		files = append(files, found...)
	}

	files = append(files, fs.Args()...)

	if len(files) == 0 {
		fs.Usage()
		return fmt.Errorf("at least one config file or --dir is required")
	}

	// Validate each file
	var (
		passed int
		failed int
		errors []string
	)

	for _, f := range files {
		if err := validateFile(f, *strict, *skipUnknownTypes, *allowNoEntryPoints); err != nil {
			failed++
			errors = append(errors, fmt.Sprintf("  FAIL %s\n       %s", f, indentError(err)))
		} else {
			passed++
		}
	}

	// Print summary
	total := passed + failed
	if total > 1 {
		fmt.Printf("\n--- Validation Summary ---\n")
		fmt.Printf("  %d/%d configs passed\n", passed, total)
		if failed > 0 {
			fmt.Printf("  %d/%d configs failed:\n", failed, total)
			for _, e := range errors {
				fmt.Println(e)
			}
		}
		fmt.Println()
	}

	if failed > 0 {
		return fmt.Errorf("%d config(s) failed validation", failed)
	}
	return nil
}

func validateFile(cfgPath string, strict, skipUnknownTypes, allowNoEntryPoints bool) error {
	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load: %w", err)
	}

	var opts []schema.ValidationOption
	if !strict {
		opts = append(opts, schema.WithAllowEmptyModules())
	}
	if skipUnknownTypes {
		opts = append(opts, schema.WithSkipModuleTypeCheck())
		opts = append(opts, schema.WithSkipWorkflowTypeCheck())
		opts = append(opts, schema.WithSkipTriggerTypeCheck())
	} else {
		// Still skip workflow/trigger type checks by default (dynamic dispatch)
		opts = append(opts, schema.WithSkipWorkflowTypeCheck(), schema.WithSkipTriggerTypeCheck())
	}
	if allowNoEntryPoints {
		opts = append(opts, schema.WithAllowNoEntryPoints())
	}

	if err := schema.ValidateConfig(cfg, opts...); err != nil {
		return err
	}

	fmt.Printf("  PASS %s (%d modules, %d workflows, %d triggers)\n",
		cfgPath, len(cfg.Modules), len(cfg.Workflows), len(cfg.Triggers))
	return nil
}

// skipDirs are directory names that should be excluded from recursive scanning.
var skipDirs = map[string]bool{
	".playwright-cli": true,
	"node_modules":    true,
	".git":            true,
	"vendor":          true,
	"observability":   true,
}

// skipFiles are filename patterns that are not workflow configs.
var skipFiles = map[string]bool{
	"docker-compose.yml":  true,
	"docker-compose.yaml": true,
	"prometheus.yml":      true,
	"prometheus.yaml":     true,
	"datasource.yml":      true,
	"datasource.yaml":     true,
	"dashboard.yml":       true,
	"dashboard.yaml":      true,
}

func findYAMLFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		if skipFiles[info.Name()] {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

func indentError(err error) string {
	return strings.ReplaceAll(err.Error(), "\n", "\n       ")
}
