package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/cigen"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/internal/legacyaws"
	"github.com/GoCodeAlone/workflow/internal/legacydo"
	"github.com/GoCodeAlone/workflow/schema"
	"github.com/GoCodeAlone/workflow/validation"
	"gopkg.in/yaml.v3"
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
	platform := fs.String("platform", "", "Validate rendered CI artifact platform: github_actions, gitlab_ci, jenkins, circleci")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl ci validate [options] <config.yaml>
       wfctl ci validate --platform <platform> <ci-file>

Run full validation suite on a workflow config for CI pipelines. Includes
structural validation, immutability checks, and pipeline reference analysis.
With --platform, validates rendered provider CI artifacts instead.

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

	if *platform != "" {
		return runCIValidateArtifacts(*platform, files, *format)
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
		if err := enc.Encode(map[string]any{"results": results, "passed": allPassed}); err != nil {
			return err
		}
		if !allPassed {
			return fmt.Errorf("%d file(s) failed ci validate", ciCountFailed(results))
		}
		return nil
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

func runCIValidateArtifacts(platform string, files []string, format string) error {
	rendered := make(map[string]string, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			rendered[file] = ""
			continue
		}
		rendered[file] = string(data)
	}
	findings := cigen.ValidateRenderedFiles(platform, rendered)
	for _, file := range files {
		if _, err := os.Stat(file); err != nil {
			findings = append(findings, cigen.ValidationFinding{
				Path:    file,
				Code:    "read_ci_artifact",
				Message: fmt.Sprintf("read CI artifact: %v", err),
			})
		}
	}
	passed := len(findings) == 0
	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"platform": platform, "findings": findings, "passed": passed}); err != nil {
			return err
		}
	default:
		for _, file := range files {
			fmt.Printf("  VALIDATE %s (%s)\n", file, platform)
		}
		for _, finding := range findings {
			fmt.Printf("       %s: %s\n", finding.Code, finding.Message)
		}
	}
	if !passed {
		return fmt.Errorf("%d file(s) failed ci validate", len(files))
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
	// Pass legacy DO module types through schema so the migration error fires
	// below instead of a generic "unknown module type" (issue #617).
	for t := range legacydo.ModuleTypes {
		opts = append(opts, schema.WithExtraModuleTypes(t))
	}
	// Same for legacy AWS module types removed in issue #653.
	for t := range legacyaws.ModuleTypes {
		opts = append(opts, schema.WithExtraModuleTypes(t))
	}
	if err := schema.ValidateConfig(cfg, opts...); err != nil {
		errs = append(errs, fmt.Errorf("schema: %w", err))
	}

	// Post-validate sweep: accumulate legacy DO and AWS module/step errors
	// (issues #617, #653).
	for _, m := range cfg.Modules {
		if legacydo.IsModuleType(m.Type) {
			errs = append(errs, legacydo.FormatModuleError(m.Type, m.Name, false))
		}
		if legacyaws.IsModuleType(m.Type) {
			errs = append(errs, legacyaws.FormatModuleError(m.Type, m.Name, false))
		}
	}
	for _, rawPipeline := range cfg.Pipelines {
		yamlBytes, err := yaml.Marshal(rawPipeline)
		if err != nil {
			continue
		}
		var pipeCfg config.PipelineConfig
		if err := yaml.Unmarshal(yamlBytes, &pipeCfg); err != nil {
			continue
		}
		for _, s := range pipeCfg.Steps {
			if legacydo.IsStepType(s.Type) {
				errs = append(errs, legacydo.FormatStepError(s.Type, false))
			}
			if legacyaws.IsStepType(s.Type) {
				errs = append(errs, legacyaws.FormatStepError(s.Type, false))
			}
		}
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
		return fmt.Errorf(
			"immutable-sections: unknown section %q (allowed: modules, workflows, pipelines, triggers)",
			section,
		)
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
