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

func runDNS(args []string) error {
	if len(args) < 1 {
		return dnsUsage()
	}
	switch args[0] {
	case "intent":
		return runDNSIntent(args[1:])
	case "-h", "--help", "help":
		return dnsUsage()
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
	case "-h", "--help", "help":
		return dnsIntentUsage()
	default:
		return fmt.Errorf("dns intent: unknown subcommand %q", args[0])
	}
}

func dnsIntentUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl dns intent <subcommand> [flags]

Subcommands:
  compile   Compile domain intent JSON and DNS portfolio exports
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
	portfolios := splitDNSIntentCSV(portfolioCSV)
	bundle, err := intent.Compile(intent.Options{
		IntentPath:     intentPath,
		PortfolioGlobs: portfolios,
		DomainFilter:   domain,
		StateDir:       stateDir,
	})
	if err != nil {
		return err
	}
	configBytes, err := yaml.Marshal(bundle.Config)
	if err != nil {
		return fmt.Errorf("marshal generated config: %w", err)
	}
	reportBytes, err := json.MarshalIndent(bundle.Report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	reportBytes = append(reportBytes, '\n')
	if err := writeFileCreatingParents(outputPath, configBytes); err != nil {
		return err
	}
	if err := writeFileCreatingParents(reportPath, reportBytes); err != nil {
		return err
	}
	if bundlePath != "" {
		bundleBytes, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal bundle: %w", err)
		}
		bundleBytes = append(bundleBytes, '\n')
		if err := writeFileCreatingParents(bundlePath, bundleBytes); err != nil {
			return err
		}
	}
	for _, domainReport := range bundle.Report.Domains {
		if len(domainReport.Blockers) == 0 {
			actionTypes := make([]string, 0, len(domainReport.Actions))
			for _, action := range domainReport.Actions {
				actionTypes = append(actionTypes, action.Type)
			}
			fmt.Printf("%s: %s\n", domainReport.Domain, strings.Join(actionTypes, ","))
		} else {
			fmt.Printf("%s: blocked: %s\n", domainReport.Domain, strings.Join(domainReport.Blockers, "; "))
		}
	}
	if bundle.Report.BlockedDomains > 0 {
		return fmt.Errorf("%d domain(s) blocked", bundle.Report.BlockedDomains)
	}
	return nil
}

func writeFileCreatingParents(path string, data []byte) error {
	if path == "" || path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
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
