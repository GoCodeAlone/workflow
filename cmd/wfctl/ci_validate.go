package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
	"github.com/GoCodeAlone/workflow/validation"
)

// ciFileResult holds the outcome of validating a single config file.
type ciFileResult struct {
	File   string   `json:"file"`
	Passed bool     `json:"passed"`
	Errors []string `json:"errors,omitempty"`
}

func runCIValidate(args []string) error {
	fs := flag.NewFlagSet("ci validate", flag.ContinueOnError)
	strict := fs.Bool("strict", false, "Strict validation (no empty modules allowed)")
	immutableConfig := fs.Bool("immutable-config", false, "Fail if CI config is absent or invalid")
	immutableSections := fs.String("immutable-sections", "", "Comma-separated list of top-level sections that must not be empty")
	override := fs.String("override", "", "Override token to bypass failed checks (3-word passphrase)")
	format := fs.String("format", "text", "Output format: text or json")
	pluginDir := fs.String("plugin-dir", "", "Directory of installed external plugins")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl ci validate [options] <config.yaml>

Run full validation suite on a workflow config for CI pipelines. Includes
structural validation, immutability checks, and pipeline reference analysis.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	files := fs.Args()
	if len(files) == 0 {
		fs.Usage()
		return fmt.Errorf("at least one config file is required")
	}

	if *pluginDir != "" {
		if err := schema.LoadPluginTypesFromDir(*pluginDir); err != nil {
			return fmt.Errorf("failed to load plugins: %w", err)
		}
	}

	var results []ciFileResult
	allPassed := true

	for _, f := range files {
		res := ciFileResult{File: f}
		errs := ciValidateFile(f, *strict, *immutableConfig, *immutableSections)
		if len(errs) == 0 {
			res.Passed = true
		} else {
			res.Passed = false
			allPassed = false
			for _, e := range errs {
				res.Errors = append(res.Errors, e.Error())
			}
		}
		results = append(results, res)
	}

	// If failed but override provided, verify against a hash of the error summary.
	if !allPassed && *override != "" {
		secret := os.Getenv("WFCTL_ADMIN_SECRET")
		if secret == "" {
			fmt.Fprintln(os.Stderr, "WARNING: --override ignored: WFCTL_ADMIN_SECRET is not set")
		} else {
			rejHash := ciResultsHash(results)
			if validation.VerifyChallenge(secret, rejHash, *override, time.Now()) {
				fmt.Fprintln(os.Stderr, "WARNING: validation failures overridden by challenge token")
				allPassed = true
			}
		}
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"results": results, "passed": allPassed})
	default:
		for _, r := range results {
			if r.Passed {
				fmt.Printf("  PASS %s\n", r.File)
			} else {
				fmt.Printf("  FAIL %s\n", r.File)
				for _, e := range r.Errors {
					fmt.Printf("       %s\n", e)
				}
			}
		}
		if !allPassed {
			return fmt.Errorf("%d file(s) failed ci validate", ciCountFailed(results))
		}
	}
	return nil
}

// ciValidateFile runs all validation checks on a single config file.
func ciValidateFile(cfgPath string, strict, immutableConfig bool, immutableSections string) []error {
	var errs []error

	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		return []error{fmt.Errorf("failed to load: %w", err)}
	}

	// Structural validation.
	var opts []schema.ValidationOption
	if !strict {
		opts = append(opts, schema.WithAllowEmptyModules())
	}
	opts = append(opts, schema.WithSkipWorkflowTypeCheck(), schema.WithSkipTriggerTypeCheck())
	if err := schema.ValidateConfig(cfg, opts...); err != nil {
		errs = append(errs, fmt.Errorf("schema: %w", err))
	}

	// CI config check.
	if immutableConfig {
		if cfg.CI == nil {
			errs = append(errs, fmt.Errorf("immutable-config: ci section is absent"))
		} else if err := cfg.CI.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("ci section: %w", err))
		}
	}

	// Immutable sections check.
	if immutableSections != "" {
		for _, sec := range strings.Split(immutableSections, ",") {
			sec = strings.TrimSpace(sec)
			if sec == "" {
				continue
			}
			if err := checkImmutableSection(cfg, sec); err != nil {
				errs = append(errs, err)
			}
		}
	}

	// Pipeline reference validation.
	if cfg.Pipelines != nil {
		if refs := validation.ValidatePipelineTemplateRefs(cfg.Pipelines); refs.HasIssues() {
			for _, w := range refs.Warnings {
				errs = append(errs, fmt.Errorf("pipeline-refs warning: %s", w))
			}
			for _, e := range refs.Errors {
				errs = append(errs, fmt.Errorf("pipeline-refs error: %s", e))
			}
		}
	}

	return errs
}

// checkImmutableSection verifies that the named top-level section is non-empty.
func checkImmutableSection(cfg *config.WorkflowConfig, section string) error {
	switch section {
	case "modules":
		if len(cfg.Modules) == 0 {
			return fmt.Errorf("immutable-sections: modules section is empty")
		}
	case "workflows":
		if len(cfg.Workflows) == 0 {
			return fmt.Errorf("immutable-sections: workflows section is empty")
		}
	case "pipelines":
		if len(cfg.Pipelines) == 0 {
			return fmt.Errorf("immutable-sections: pipelines section is empty")
		}
	case "triggers":
		if len(cfg.Triggers) == 0 {
			return fmt.Errorf("immutable-sections: triggers section is empty")
		}
	default:
		// Unknown section — silently skip.
	}
	return nil
}

// ciResultsHash produces a SHA-256 hex digest of all failed file paths and
// their errors, used as the rejection hash in override token verification.
func ciResultsHash(results []ciFileResult) string {
	var sb strings.Builder
	for _, r := range results {
		if !r.Passed {
			sb.WriteString(r.File)
			sb.WriteByte('\n')
			for _, e := range r.Errors {
				sb.WriteString(e)
				sb.WriteByte('\n')
			}
		}
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return fmt.Sprintf("%x", sum)
}

func ciCountFailed(results []ciFileResult) int {
	n := 0
	for _, r := range results {
		if !r.Passed {
			n++
		}
	}
	return n
}
