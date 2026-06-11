package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/GoCodeAlone/workflow/capability/inventory"
)

func runCapability(args []string) error {
	return runCapabilityWithOutput(args, os.Stdout)
}

func runCapabilityWithOutput(args []string, out io.Writer) error {
	if len(args) == 0 {
		printCapabilityUsage(out)
		return errors.New("capability subcommand is required")
	}
	switch args[0] {
	case "ecosystem":
		return runCapabilityEcosystem(args[1:], out)
	case "catalog":
		return runCapabilityCatalog(args[1:], out)
	case "crossrefs":
		return runCapabilityCrossrefs(args[1:], out)
	case "app":
		return runCapabilityApp(args[1:], out)
	case "check":
		return runCapabilityCheck(args[1:], out)
	case "-h", "--help", "help":
		printCapabilityUsage(out)
		return nil
	default:
		printCapabilityUsage(out)
		return fmt.Errorf("unknown capability subcommand %q", args[0])
	}
}

func printCapabilityUsage(out io.Writer) {
	fmt.Fprintln(out, `Usage: wfctl capability <subcommand> [options]

Subcommands:
  ecosystem  Generate Workflow ecosystem capability inventory
  catalog    Generate docs-facing Workflow capability catalog
  crossrefs  Generate plugin/provider capability cross-reference index
  app        Generate capability profile for an application
  check      Print detected capabilities and findings for an application

Use "wfctl capability <subcommand> -h" for subcommand options.`)
}

func runCapabilityEcosystem(args []string, out io.Writer) error {
	inv, format, outputPath, err := collectCapabilityEcosystemFromFlags("capability ecosystem", args, out)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	switch format {
	case "json":
		if err := writeCapabilityJSON(&buf, inv); err != nil {
			return err
		}
	case "md", "markdown":
		renderEcosystemMarkdown(&buf, inv)
	default:
		return fmt.Errorf("unsupported ecosystem format %q", format)
	}
	return writeCapabilityOutput(out, outputPath, buf.Bytes())
}

func runCapabilityCatalog(args []string, out io.Writer) error {
	inv, format, outputPath, err := collectCapabilityEcosystemFromFlags("capability catalog", args, out)
	if err != nil {
		return err
	}
	catalog := inventory.BuildCatalog(inv)
	var buf bytes.Buffer
	switch format {
	case "json":
		if err := writeCapabilityJSON(&buf, catalog); err != nil {
			return err
		}
	case "md", "markdown":
		renderCatalogMarkdown(&buf, catalog)
	default:
		return fmt.Errorf("unsupported catalog format %q", format)
	}
	return writeCapabilityOutput(out, outputPath, buf.Bytes())
}

func runCapabilityCrossrefs(args []string, out io.Writer) error {
	inv, format, outputPath, err := collectCapabilityEcosystemFromFlags("capability crossrefs", args, out)
	if err != nil {
		return err
	}
	refs := inventory.BuildCapabilityCrossrefs(inv)
	var buf bytes.Buffer
	switch format {
	case "json":
		if err := writeCapabilityJSON(&buf, refs); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported crossrefs format %q", format)
	}
	return writeCapabilityOutput(out, outputPath, buf.Bytes())
}

func collectCapabilityEcosystemFromFlags(name string, args []string, out io.Writer) (*inventory.Inventory, string, string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(out)
	var registryDir, repoRoot, taxonomyPath, format, outputPath string
	fs.StringVar(&registryDir, "registry", defaultCapabilityRegistryPath(), "registry directory")
	fs.StringVar(&repoRoot, "repo-root", "..", "workspace root containing workflow-plugin-* repos")
	fs.StringVar(&taxonomyPath, "taxonomy", defaultCapabilityTaxonomyPath(), "capability taxonomy YAML")
	fs.StringVar(&format, "format", "json", "output format: json or md")
	fs.StringVar(&outputPath, "output", "", "write output to path instead of stdout")
	if err := fs.Parse(args); err != nil {
		return nil, "", "", err
	}
	inv, err := inventory.CollectEcosystem(inventory.EcosystemOptions{
		RegistryDir:     registryDir,
		RepoRoot:        repoRoot,
		TaxonomyPath:    taxonomyPath,
		GeneratedAt:     time.Now().UTC(),
		WorkflowVersion: version,
	})
	if err != nil {
		return nil, "", "", err
	}
	return inv, format, outputPath, nil
}

func runCapabilityApp(args []string, out io.Writer) error {
	opts, format, outputPath, err := parseCapabilityAppFlags("capability app", args, out)
	if err != nil {
		return err
	}
	profile, err := inventory.CollectApp(context.Background(), opts)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	switch format {
	case "json":
		if err := writeCapabilityJSON(&buf, profile); err != nil {
			return err
		}
	case "md", "markdown":
		renderAppMarkdown(&buf, profile)
	default:
		return fmt.Errorf("unsupported app format %q", format)
	}
	return writeCapabilityOutput(out, outputPath, buf.Bytes())
}

func runCapabilityCheck(args []string, out io.Writer) error {
	opts, format, outputPath, findingsOnly, err := parseCapabilityCheckFlags(args, out)
	if err != nil {
		return err
	}
	profile, err := inventory.CollectApp(context.Background(), opts)
	if err != nil {
		return err
	}
	findings := inventory.CheckApp(profile)
	var buf bytes.Buffer
	switch format {
	case "text", "":
		if findingsOnly {
			renderFindingsText(&buf, findings)
		} else {
			renderCapabilityCheckText(&buf, profile, findings)
		}
	case "json":
		if err := writeCapabilityJSON(&buf, findings); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported check format %q", format)
	}
	return writeCapabilityOutput(out, outputPath, buf.Bytes())
}

func parseCapabilityCheckFlags(args []string, out io.Writer) (inventory.AppOptions, string, string, bool, error) {
	fs := flag.NewFlagSet("capability check", flag.ContinueOnError)
	fs.SetOutput(out)
	var workflows capabilityStringListFlag
	var manifestPath, pluginDir, lockfilePath, taxonomyPath, format, outputPath string
	var findingsOnly bool
	fs.StringVar(&manifestPath, "manifest", "wfctl.yaml", "wfctl project manifest")
	fs.Var(&workflows, "workflow", "workflow config path; repeat for multiple files")
	fs.StringVar(&pluginDir, "plugin-dir", ".wfctl/plugins", "installed plugin directory")
	fs.StringVar(&lockfilePath, "lock-file", ".wfctl-lock.yaml", "wfctl lockfile")
	fs.StringVar(&taxonomyPath, "taxonomy", defaultCapabilityTaxonomyPath(), "capability taxonomy YAML")
	fs.StringVar(&format, "format", "text", "output format")
	fs.StringVar(&outputPath, "output", "", "write output to path instead of stdout")
	fs.BoolVar(&findingsOnly, "findings-only", false, "print only warning/error findings")
	if err := fs.Parse(args); err != nil {
		return inventory.AppOptions{}, "", "", false, err
	}
	workflowPaths := []string(workflows)
	if len(workflowPaths) == 0 {
		workflowPaths = []string{"workflow.yaml"}
	}
	return inventory.AppOptions{
		ManifestPath:  manifestPath,
		WorkflowPaths: workflowPaths,
		PluginDir:     pluginDir,
		LockfilePath:  lockfilePath,
		TaxonomyPath:  taxonomyPath,
		GeneratedAt:   time.Now().UTC(),
	}, format, outputPath, findingsOnly, nil
}

func parseCapabilityAppFlags(name string, args []string, out io.Writer) (inventory.AppOptions, string, string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(out)
	var workflows capabilityStringListFlag
	var manifestPath, pluginDir, lockfilePath, taxonomyPath, format, outputPath string
	fs.StringVar(&manifestPath, "manifest", "wfctl.yaml", "wfctl project manifest")
	fs.Var(&workflows, "workflow", "workflow config path; repeat for multiple files")
	fs.StringVar(&pluginDir, "plugin-dir", ".wfctl/plugins", "installed plugin directory")
	fs.StringVar(&lockfilePath, "lock-file", ".wfctl-lock.yaml", "wfctl lockfile")
	fs.StringVar(&taxonomyPath, "taxonomy", defaultCapabilityTaxonomyPath(), "capability taxonomy YAML")
	fs.StringVar(&format, "format", defaultCapabilityFormat(name), "output format")
	fs.StringVar(&outputPath, "output", "", "write output to path instead of stdout")
	if err := fs.Parse(args); err != nil {
		return inventory.AppOptions{}, "", "", err
	}
	workflowPaths := []string(workflows)
	if len(workflowPaths) == 0 {
		workflowPaths = []string{"workflow.yaml"}
	}
	return inventory.AppOptions{
		ManifestPath:  manifestPath,
		WorkflowPaths: workflowPaths,
		PluginDir:     pluginDir,
		LockfilePath:  lockfilePath,
		TaxonomyPath:  taxonomyPath,
		GeneratedAt:   time.Now().UTC(),
	}, format, outputPath, nil
}

func defaultCapabilityFormat(name string) string {
	if strings.Contains(name, "check") {
		return "text"
	}
	return "json"
}

type capabilityStringListFlag []string

func (f *capabilityStringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *capabilityStringListFlag) Set(value string) error {
	if strings.TrimSpace(value) != "" {
		*f = append(*f, value)
	}
	return nil
}

func writeCapabilityJSON(out io.Writer, value any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func writeCapabilityOutput(out io.Writer, outputPath string, data []byte) error {
	if outputPath == "" {
		_, err := out.Write(data)
		return err
	}
	return os.WriteFile(outputPath, data, 0o600)
}

func renderEcosystemMarkdown(out io.Writer, inv *inventory.Inventory) {
	fmt.Fprintln(out, "# Workflow Capability Matrix")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "- Generated: %s\n", inv.Metadata.GeneratedAt)
	fmt.Fprintf(out, "- Workflow version: %s\n", inv.Metadata.WorkflowVersion)
	fmt.Fprintf(out, "- Taxonomy: %s\n", inv.Metadata.TaxonomyVersion)
	fmt.Fprintln(out)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "| Capability\t| Category\t| Lifecycle\t| Providers\t|")
	fmt.Fprintln(tw, "|---\t|---\t|---\t|---\t|")
	for i := range inv.Capabilities {
		cap := &inv.Capabilities[i]
		fmt.Fprintf(tw, "| %s\t| %s\t| %s\t| %s\t|\n", cap.ID, cap.Category, cap.Lifecycle, providerSummary(cap.Providers))
	}
	_ = tw.Flush()
	if len(inv.Findings) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "## Findings")
		renderFindingTable(out, inv.Findings)
	}
}

func renderCatalogMarkdown(out io.Writer, catalog *inventory.Catalog) {
	fmt.Fprintln(out, "# Workflow Capability Catalog")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "- Generated: %s\n", catalog.Metadata.GeneratedAt)
	fmt.Fprintf(out, "- Workflow version: %s\n", catalog.Metadata.WorkflowVersion)
	fmt.Fprintf(out, "- Taxonomy: %s\n", catalog.Metadata.TaxonomyVersion)
	if hidden := catalog.Metadata.Counts["hiddenUncategorized"]; hidden > 0 {
		fmt.Fprintf(out, "- Hidden uncategorized rows: %d\n", hidden)
	}
	fmt.Fprintln(out)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "| Capability\t| Name\t| Category\t| Providers\t|")
	fmt.Fprintln(tw, "|---\t|---\t|---\t|---\t|")
	for i := range catalog.Capabilities {
		cap := &catalog.Capabilities[i]
		fmt.Fprintf(tw, "| %s\t| %s\t| %s\t| %s\t|\n", cap.ID, cap.Name, cap.Category, providerSummary(cap.Providers))
	}
	_ = tw.Flush()
	if len(catalog.Findings) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "## Findings")
		renderFindingTable(out, catalog.Findings)
	}
}

func renderAppMarkdown(out io.Writer, profile *inventory.AppProfile) {
	fmt.Fprintln(out, "# Workflow Application Capability Profile")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "- Generated: %s\n", profile.Metadata.GeneratedAt)
	fmt.Fprintf(out, "- Taxonomy: %s\n", profile.Metadata.TaxonomyVersion)
	fmt.Fprintln(out)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "| Capability\t| Mode\t| Confidence\t| Evidence\t|")
	fmt.Fprintln(tw, "|---\t|---\t|---\t|---\t|")
	for i := range profile.Usage {
		usage := &profile.Usage[i]
		fmt.Fprintf(tw, "| %s\t| %s\t| %s\t| %s\t|\n", usage.CapabilityID, usage.Mode, usage.Confidence, evidenceSummary(usage.Evidence))
	}
	_ = tw.Flush()
	if len(profile.Findings) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "## Findings")
		renderFindingTable(out, profile.Findings)
	}
}

func renderFindingsText(out io.Writer, findings []inventory.Finding) {
	if len(findings) == 0 {
		fmt.Fprintln(out, "OK no capability findings")
		return
	}
	for _, finding := range findings {
		fmt.Fprintf(out, "WARN %s %s: %s\n", finding.Code, finding.CapabilityID, finding.Message)
	}
}

func renderCapabilityCheckText(out io.Writer, profile *inventory.AppProfile, findings []inventory.Finding) {
	fmt.Fprintln(out, "Capabilities")
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Capability\tMode\tConfidence\tEvidence")
	fmt.Fprintln(tw, "----------\t----\t----------\t--------")
	if profile != nil {
		for i := range profile.Usage {
			usage := &profile.Usage[i]
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", usage.CapabilityID, usage.Mode, usage.Confidence, evidenceSummary(usage.Evidence))
		}
	}
	_ = tw.Flush()
	fmt.Fprintln(out)
	renderFindingsText(out, findings)
}

func renderFindingTable(out io.Writer, findings []inventory.Finding) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "| Level\t| Code\t| Capability\t| Message\t|")
	fmt.Fprintln(tw, "|---\t|---\t|---\t|---\t|")
	for _, finding := range findings {
		fmt.Fprintf(tw, "| %s\t| %s\t| %s\t| %s\t|\n", finding.Level, finding.Code, finding.CapabilityID, finding.Message)
	}
	_ = tw.Flush()
}

func providerSummary(providers []inventory.Provider) string {
	if len(providers) == 0 {
		return ""
	}
	names := make([]string, 0, len(providers))
	for i := range providers {
		provider := &providers[i]
		status := provider.ReleaseStatus
		if status == "" {
			status = "unknown"
		}
		names = append(names, provider.Name+" ("+status+")")
	}
	return strings.Join(names, ", ")
}

func evidenceSummary(evidence []inventory.Evidence) string {
	if len(evidence) == 0 {
		return ""
	}
	values := make([]string, 0, len(evidence))
	for _, item := range evidence {
		if item.SourcePath == "" {
			values = append(values, item.SourceKind)
			continue
		}
		values = append(values, item.SourcePath)
	}
	return strings.Join(values, ", ")
}

func defaultCapabilityTaxonomyPath() string {
	return firstExistingPath("data/capabilities/taxonomy.yaml", "../../data/capabilities/taxonomy.yaml")
}

func defaultCapabilityRegistryPath() string {
	return firstExistingPath("data/registry", "../../data/registry")
}

func firstExistingPath(paths ...string) string {
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}
