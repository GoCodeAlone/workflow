package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// alignOptions holds parsed CLI options for the align subcommand.
type alignOptions struct {
	configFile   string
	envName      string
	planFile     string
	strict       bool
	strictHealth bool
	strictCIDR   bool
	maxChanges   int
}

// runInfraAlign is the CLI entry point for `wfctl infra align`.
func runInfraAlign(args []string) error {
	fs := flag.NewFlagSet("infra align", flag.ContinueOnError)
	var opts alignOptions
	fs.StringVar(&opts.configFile, "config", "", "Config file (default: infra.yaml or config/infra.yaml)")
	fs.StringVar(&opts.configFile, "c", "", "Config file (short for --config)")
	fs.StringVar(&opts.envName, "env", "", "Environment name")
	fs.StringVar(&opts.planFile, "plan", "", "Path to plan JSON file (enables R-A7 checks)")
	fs.BoolVar(&opts.strict, "strict", false, "Treat WARNs as FAILs")
	fs.BoolVar(&opts.strictHealth, "strict-health", false, "Treat R-A2 health-check WARNs as FAILs")
	fs.BoolVar(&opts.strictCIDR, "strict-cidr", false, "Enable strict CIDR overlap checks (reserved for future use)")
	fs.IntVar(&opts.maxChanges, "max-changes", 50, "Warn when plan has more than N changes")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Resolve config file
	if opts.configFile == "" {
		f := fs.Lookup("config")
		if f != nil {
			opts.configFile = f.Value.String()
		}
	}
	if opts.configFile == "" {
		for _, candidate := range []string{"infra.yaml", "config/infra.yaml"} {
			if _, err := os.Stat(candidate); err == nil {
				opts.configFile = candidate
				break
			}
		}
	}
	if opts.configFile == "" {
		for _, arg := range fs.Args() {
			if strings.HasSuffix(arg, ".yaml") || strings.HasSuffix(arg, ".yml") {
				opts.configFile = arg
				break
			}
		}
	}
	if opts.configFile == "" {
		return fmt.Errorf("no config file specified and no infra.yaml found")
	}

	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		return err
	}

	output := renderAlignMarkdown(findings)
	fmt.Print(output)

	// Write to GitHub Step Summary when running in CI
	if summary := os.Getenv("GITHUB_STEP_SUMMARY"); summary != "" {
		if f, err := os.OpenFile(summary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			fmt.Fprint(f, output)
			f.Close()
		}
	}

	if alignExitCode(findings, opts.strict) != 0 {
		var failCount int
		for _, f := range findings {
			if f.Severity == "FAIL" || (opts.strict && f.Severity == "WARN") {
				failCount++
			}
		}
		return fmt.Errorf("align: %d finding(s) require attention", failCount)
	}
	return nil
}

// runInfraAlignChecks runs all alignment rule families and returns findings.
// This is separated from runInfraAlign to make it testable.
func runInfraAlignChecks(opts alignOptions) ([]AlignFinding, error) {
	ctx, err := buildAlignContext(opts.configFile)
	if err != nil {
		return nil, err
	}

	var findings []AlignFinding

	// R-A1: container/runtime alignment
	findings = append(findings, checkRA1(ctx)...)

	// R-A2: health-check alignment
	findings = append(findings, checkRA2(ctx, opts.strictHealth)...)

	// R-A3: service-to-service DNS alignment
	findings = append(findings, checkRA3(ctx)...)

	// R-A4: env-var resolution
	findings = append(findings, checkRA4(ctx)...)

	// R-A5: migrations alignment
	findings = append(findings, checkRA5(ctx)...)

	// R-A6: network/exposure alignment
	findings = append(findings, checkRA6(ctx)...)

	// R-A7: plan-output sanity (only when --plan provided)
	if opts.planFile != "" {
		plan, err := loadPlanJSON(opts.planFile)
		if err != nil {
			return nil, fmt.Errorf("load plan: %w", err)
		}
		findings = append(findings, checkRA7(plan, opts.maxChanges)...)
	}

	// R-A8: WebAuthn alignment
	findings = append(findings, checkRA8(ctx)...)

	return findings, nil
}

// loadPlanJSON reads and decodes a plan JSON file.
func loadPlanJSON(path string) (*interfaces.IaCPlan, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var plan interfaces.IaCPlan
	if err := json.NewDecoder(f).Decode(&plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// alignExitCode returns 0 (success) or 1 (failure) based on findings and flags.
func alignExitCode(findings []AlignFinding, strict bool) int {
	for _, f := range findings {
		if f.Severity == "FAIL" {
			return 1
		}
		if strict && f.Severity == "WARN" {
			return 1
		}
	}
	return 0
}

// renderAlignMarkdown formats findings as a markdown table with a summary line.
func renderAlignMarkdown(findings []AlignFinding) string {
	var sb strings.Builder
	sb.WriteString("## wfctl infra align\n\n")

	if len(findings) == 0 {
		sb.WriteString("No alignment issues found.\n")
		return sb.String()
	}

	sb.WriteString("| Rule | Severity | Resource | Message |\n")
	sb.WriteString("|------|----------|----------|---------|\n")
	for _, f := range findings {
		// Escape pipe characters to prevent breaking the markdown table.
		resource := strings.ReplaceAll(f.Resource, "|", "\\|")
		message := strings.ReplaceAll(f.Message, "|", "\\|")
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
			f.Rule, f.Severity, resource, message))
	}
	sb.WriteString("\n")

	var failCount, warnCount int
	for _, f := range findings {
		switch f.Severity {
		case "FAIL":
			failCount++
		case "WARN":
			warnCount++
		}
	}

	parts := []string{}
	if failCount > 0 {
		parts = append(parts, fmt.Sprintf("%d FAIL", failCount))
	}
	if warnCount > 0 {
		parts = append(parts, fmt.Sprintf("%d WARN", warnCount))
	}
	sb.WriteString(strings.Join(parts, ", "))
	sb.WriteString("\n")

	return sb.String()
}
