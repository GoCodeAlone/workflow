package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/dns/intent"
	"gopkg.in/yaml.v3"
)

var (
	dnsIntentRunValidate   = runValidate
	dnsIntentRunInfraPlan  = runInfraPlan
	dnsIntentRunInfraApply = runInfraApply
)

func runDNS(args []string) error {
	if len(args) < 1 {
		return dnsUsage()
	}
	switch args[0] {
	case "intent":
		return runDNSIntent(args[1:])
	case "-h", "--help", "help":
		_ = dnsUsage()
		return flag.ErrHelp
	default:
		return fmt.Errorf("dns: unknown subcommand %q", args[0])
	}
}

func dnsUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl dns <subcommand> [flags]

DNS orchestration helpers.

Subcommands:
  intent   Compile domain intent into infra resources and reports
`)
	return fmt.Errorf("missing or unknown subcommand")
}

func runDNSIntent(args []string) error {
	if len(args) < 1 {
		return dnsIntentUsage()
	}
	switch args[0] {
	case "compile":
		return runDNSIntentCompile(args[1:])
	case "reconcile":
		return runDNSIntentReconcile(args[1:])
	case "-h", "--help", "help":
		_ = dnsIntentUsage()
		return flag.ErrHelp
	default:
		return fmt.Errorf("dns intent: unknown subcommand %q", args[0])
	}
}

func dnsIntentUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl dns intent <subcommand> [flags]

Subcommands:
  compile   Compile domain intent JSON and DNS portfolio exports
  reconcile Compile, validate, and plan/apply domain intent
`)
	return fmt.Errorf("missing or unknown subcommand")
}

func runDNSIntentCompile(args []string) error {
	fs := flag.NewFlagSet("dns intent compile", flag.ContinueOnError)
	var intentPath, portfolioCSV, domain, outputPath, reportPath, bundlePath, stateDir string
	fs.StringVar(&intentPath, "intent", "domains.json", "Domain intent JSON file")
	fs.StringVar(&portfolioCSV, "portfolio", "zones/*.portfolio.json", "Comma-separated DNS portfolio JSON paths or globs")
	fs.StringVar(&domain, "domain", "", "Optional single domain to compile")
	fs.StringVar(&outputPath, "output", "infra/domain-reconcile.generated.wfctl.yaml", "Generated wfctl config path")
	fs.StringVar(&reportPath, "report", "reports/domain-reconcile-report.json", "Generated JSON report path")
	fs.StringVar(&bundlePath, "bundle", "", "Optional combined JSON bundle path")
	fs.StringVar(&stateDir, "state-dir", ".state/domain-intent/", "Filesystem state directory for generated iac.state")
	if err := fs.Parse(args); err != nil {
		return err
	}
	bundle, err := compileDNSIntentBundle(intentPath, portfolioCSV, domain, outputPath, reportPath, bundlePath, stateDir)
	if err != nil {
		return err
	}
	if bundle.Report.BlockedDomains > 0 {
		return fmt.Errorf("%d domain(s) blocked", bundle.Report.BlockedDomains)
	}
	return nil
}

func runDNSIntentReconcile(args []string) error {
	fs := flag.NewFlagSet("dns intent reconcile", flag.ContinueOnError)
	var opts dnsIntentReconcileOptions
	fs.StringVar(&opts.intentPath, "intent", "domains.json", "Domain intent JSON file")
	fs.StringVar(&opts.portfolioCSV, "portfolio", "zones/*.portfolio.json", "Comma-separated DNS portfolio JSON paths or globs")
	fs.StringVar(&opts.domain, "domain", "", "Optional single domain to reconcile")
	fs.StringVar(&opts.outputPath, "output", "infra/domain-reconcile.generated.wfctl.yaml", "Generated wfctl config path")
	fs.StringVar(&opts.reportPath, "report", "reports/domain-reconcile-report.json", "Generated JSON report path")
	fs.StringVar(&opts.bundlePath, "bundle", "", "Optional combined JSON bundle path")
	fs.StringVar(&opts.stateDir, "state-dir", ".state/domain-intent/", "Filesystem state directory for generated iac.state")
	fs.StringVar(&opts.planPath, "plan-output", "reports/domain-reconcile-plan.json", "Generated infra plan JSON path")
	fs.StringVar(&opts.pluginDir, "plugin-dir", "", "Plugin directory passed to validate/plan/apply")
	fs.StringVar(&opts.mode, "mode", "plan", "Reconcile mode: plan or apply")
	fs.BoolVar(&opts.autoApprove, "auto-approve", false, "Pass --auto-approve to infra apply (required with --mode apply)")
	fs.BoolVar(&opts.allowEmpty, "allow-empty", false, "Allow intent with zero generated actions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runDNSIntentReconcileWithOptions(opts)
}

type dnsIntentReconcileOptions struct {
	intentPath   string
	portfolioCSV string
	domain       string
	outputPath   string
	reportPath   string
	bundlePath   string
	stateDir     string
	planPath     string
	pluginDir    string
	mode         string
	autoApprove  bool
	allowEmpty   bool
}

func runDNSIntentReconcileWithOptions(opts dnsIntentReconcileOptions) error {
	switch opts.mode {
	case "plan":
	case "apply":
		if !opts.autoApprove {
			return fmt.Errorf("--mode apply requires --auto-approve")
		}
	default:
		return fmt.Errorf("unsupported reconcile mode %q (want plan or apply)", opts.mode)
	}
	bundle, err := compileDNSIntentBundle(
		opts.intentPath,
		opts.portfolioCSV,
		opts.domain,
		opts.outputPath,
		opts.reportPath,
		opts.bundlePath,
		opts.stateDir,
	)
	if err != nil {
		return err
	}
	if bundle.Report.ActionCount == 0 && !opts.allowEmpty {
		return fmt.Errorf("domain intent produced no actions; use --allow-empty to accept a no-op")
	}
	validateArgs := []string{"--allow-no-entry-points"}
	if opts.pluginDir != "" {
		validateArgs = append(validateArgs, "--plugin-dir", opts.pluginDir)
	}
	validateArgs = append(validateArgs, opts.outputPath)
	if err := dnsIntentRunValidate(validateArgs); err != nil {
		return fmt.Errorf("validate generated domain intent config: %w", err)
	}
	planArgs := []string{"--config", opts.outputPath}
	if opts.pluginDir != "" {
		planArgs = append(planArgs, "--plugin-dir", opts.pluginDir)
	}
	if opts.planPath != "" {
		planArgs = append(planArgs, "--output", opts.planPath)
	}
	if err := dnsIntentRunInfraPlan(planArgs); err != nil {
		return fmt.Errorf("plan domain intent: %w", err)
	}
	switch opts.mode {
	case "plan":
		return nil
	case "apply":
		applyArgs := []string{"--config", opts.outputPath, "--auto-approve"}
		if opts.pluginDir != "" {
			applyArgs = append(applyArgs, "--plugin-dir", opts.pluginDir)
		}
		if err := dnsIntentRunInfraApply(applyArgs); err != nil {
			return fmt.Errorf("apply domain intent: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported reconcile mode %q (want plan or apply)", opts.mode)
	}
}

func compileDNSIntentBundle(intentPath, portfolioCSV, domain, outputPath, reportPath, bundlePath, stateDir string) (*intent.Bundle, error) {
	portfolios := splitDNSIntentCSV(portfolioCSV)
	bundle, err := intent.Compile(intent.Options{
		IntentPath:     intentPath,
		PortfolioGlobs: portfolios,
		DomainFilter:   domain,
		StateDir:       stateDir,
	})
	if err != nil {
		return nil, err
	}
	configBytes, err := yaml.Marshal(bundle.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal generated config: %w", err)
	}
	reportBytes, err := json.MarshalIndent(bundle.Report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal report: %w", err)
	}
	reportBytes = append(reportBytes, '\n')
	if err := writeFileCreatingParents(outputPath, configBytes); err != nil {
		return nil, err
	}
	if err := writeFileCreatingParents(reportPath, reportBytes); err != nil {
		return nil, err
	}
	if bundlePath != "" {
		bundleBytes, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal bundle: %w", err)
		}
		bundleBytes = append(bundleBytes, '\n')
		if err := writeFileCreatingParents(bundlePath, bundleBytes); err != nil {
			return nil, err
		}
	}
	printDNSIntentSummary(bundle)
	return bundle, nil
}

func printDNSIntentSummary(bundle *intent.Bundle) {
	for i := range bundle.Report.Domains {
		domainReport := &bundle.Report.Domains[i]
		if len(domainReport.Blockers) == 0 {
			actionTypes := make([]string, 0, len(domainReport.Actions))
			for j := range domainReport.Actions {
				action := &domainReport.Actions[j]
				actionTypes = append(actionTypes, action.Type)
			}
			fmt.Printf("%s: %s\n", domainReport.Domain, strings.Join(actionTypes, ","))
		} else {
			fmt.Printf("%s: blocked: %s\n", domainReport.Domain, strings.Join(domainReport.Blockers, "; "))
		}
	}
}

func writeFileCreatingParents(path string, data []byte) error {
	if path == "" || path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create parent directory for %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

func splitDNSIntentCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
