package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	_ "github.com/lib/pq"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

// runTenant is the entry point for `wfctl tenant <subcommand>`.
// It opens a PostgreSQL connection from the --dsn flag and delegates to
// runTenantWithRegistry, which can be replaced in tests with a fake registry.
func runTenant(args []string) error {
	// Find --dsn before passing args to the subcommand handlers.
	dsn := os.Getenv("WFCTL_TENANT_DSN")
	for i, a := range args {
		if a == "--dsn" && i+1 < len(args) {
			dsn = args[i+1]
			args = append(args[:i], args[i+2:]...)
			break
		}
		if strings.HasPrefix(a, "--dsn=") {
			dsn = strings.TrimPrefix(a, "--dsn=")
			args = append(args[:i], args[i+1:]...)
			break
		}
	}

	if dsn == "" {
		return fmt.Errorf("tenant: --dsn <postgres-dsn> or WFCTL_TENANT_DSN env var is required")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("tenant: open database: %w", err)
	}
	defer db.Close()

	reg, err := module.NewSQLTenantRegistry(module.SQLTenantRegistryConfig{DB: db})
	if err != nil {
		return fmt.Errorf("tenant: create registry: %w", err)
	}
	return runTenantWithRegistry(args, os.Stdout, reg)
}

// runTenantWithRegistry executes the tenant subcommand against the given registry.
// Extracted for testability.
func runTenantWithRegistry(args []string, w io.Writer, reg interfaces.TenantRegistry) error {
	if len(args) == 0 {
		return tenantUsage(w)
	}

	// Extract global --format flag before routing.
	format := "table"
	filteredArgs := args[:0:0]
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--format" && i+1 < len(args) {
			format = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(a, "--format=") {
			format = strings.TrimPrefix(a, "--format=")
			continue
		}
		filteredArgs = append(filteredArgs, a)
	}

	subcmd := filteredArgs[0]
	rest := filteredArgs[1:]

	switch subcmd {
	case "ensure":
		return tenantEnsure(rest, w, format, reg)
	case "list":
		return tenantList(rest, w, format, reg)
	case "get":
		return tenantGet(rest, w, format, reg)
	case "update":
		return tenantUpdate(rest, w, format, reg)
	case "disable":
		return tenantDisable(rest, w, reg)
	default:
		return fmt.Errorf("tenant: unknown subcommand %q (allowed: ensure, list, get, update, disable)", subcmd)
	}
}

func tenantUsage(w io.Writer) error {
	fmt.Fprintf(w, `Usage: wfctl tenant <subcommand> [options]

Manage tenants in the registry.

Subcommands:
  ensure    Create a tenant if it doesn't exist, or return the existing one
  list      List tenants with optional filtering
  get       Get a tenant by domain, slug, or ID
  update    Apply a partial patch to an existing tenant
  disable   Soft-delete a tenant (set is_active=false)

Global options:
  --dsn <postgres-dsn>   PostgreSQL DSN (or WFCTL_TENANT_DSN env var)
  --format <table|json|yaml>  Output format (default: table)
`)
	return fmt.Errorf("subcommand required")
}

// ── ensure ─────────────────────────────────────────────────────────────────────

func tenantEnsure(args []string, w io.Writer, format string, reg interfaces.TenantRegistry) error {
	var name, slug string
	var domains []string

	rest := args
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--name":
			if i+1 < len(rest) {
				name = rest[i+1]
				i++
			}
		case "--slug":
			if i+1 < len(rest) {
				slug = rest[i+1]
				i++
			}
		case "--domain":
			if i+1 < len(rest) {
				domains = append(domains, rest[i+1])
				i++
			}
		}
	}

	if name == "" {
		return fmt.Errorf("tenant ensure: --name is required")
	}
	if slug == "" {
		return fmt.Errorf("tenant ensure: --slug is required")
	}

	tenant, err := reg.Ensure(interfaces.TenantSpec{Name: name, Slug: slug, Domains: domains})
	if err != nil {
		return fmt.Errorf("tenant ensure: %w", err)
	}
	return printTenants(w, format, []interfaces.Tenant{tenant})
}

// ── list ───────────────────────────────────────────────────────────────────────

func tenantList(args []string, w io.Writer, format string, reg interfaces.TenantRegistry) error {
	filter := interfaces.TenantFilter{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--active-only":
			filter.ActiveOnly = true
		case "--domain":
			if i+1 < len(args) {
				filter.Domain = args[i+1]
				i++
			}
		case "--slug":
			if i+1 < len(args) {
				filter.Slug = args[i+1]
				i++
			}
		}
	}

	tenants, err := reg.List(filter)
	if err != nil {
		return fmt.Errorf("tenant list: %w", err)
	}
	return printTenants(w, format, tenants)
}

// ── get ────────────────────────────────────────────────────────────────────────

func tenantGet(args []string, w io.Writer, format string, reg interfaces.TenantRegistry) error {
	var domain, slug, id string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--domain":
			if i+1 < len(args) {
				domain = args[i+1]
				i++
			}
		case "--slug":
			if i+1 < len(args) {
				slug = args[i+1]
				i++
			}
		case "--id":
			if i+1 < len(args) {
				id = args[i+1]
				i++
			}
		}
	}

	var tenant interfaces.Tenant
	var err error
	switch {
	case domain != "":
		tenant, err = reg.GetByDomain(domain)
	case slug != "":
		tenant, err = reg.GetBySlug(slug)
	case id != "":
		tenant, err = reg.GetByID(id)
	default:
		return fmt.Errorf("tenant get: one of --domain, --slug, or --id is required")
	}
	if err != nil {
		return fmt.Errorf("tenant get: %w", err)
	}
	return printTenants(w, format, []interfaces.Tenant{tenant})
}

// ── update ─────────────────────────────────────────────────────────────────────

func tenantUpdate(args []string, w io.Writer, format string, reg interfaces.TenantRegistry) error {
	var id, name string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--id":
			if i+1 < len(args) {
				id = args[i+1]
				i++
			}
		case "--name":
			if i+1 < len(args) {
				name = args[i+1]
				i++
			}
		}
	}

	if id == "" {
		return fmt.Errorf("tenant update: --id is required")
	}

	patch := interfaces.TenantPatch{}
	if name != "" {
		patch.Name = &name
	}

	tenant, err := reg.Update(id, patch)
	if err != nil {
		return fmt.Errorf("tenant update: %w", err)
	}
	return printTenants(w, format, []interfaces.Tenant{tenant})
}

// ── disable ────────────────────────────────────────────────────────────────────

func tenantDisable(args []string, w io.Writer, reg interfaces.TenantRegistry) error {
	var id string
	for i := 0; i < len(args); i++ {
		if args[i] == "--id" && i+1 < len(args) {
			id = args[i+1]
			i++
		}
	}

	if id == "" {
		return fmt.Errorf("tenant disable: --id is required")
	}

	if err := reg.Disable(id); err != nil {
		return fmt.Errorf("tenant disable: %w", err)
	}
	fmt.Fprintf(w, "tenant %s disabled\n", id)
	return nil
}

// ── output formatting ──────────────────────────────────────────────────────────

func printTenants(w io.Writer, format string, tenants []interfaces.Tenant) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(tenants)
	case "yaml":
		for _, t := range tenants {
			fmt.Fprintf(w, "- id: %s\n  name: %s\n  slug: %s\n  is_active: %v\n", t.ID, t.Name, t.Slug, t.IsActive)
			if len(t.Domains) > 0 {
				fmt.Fprintf(w, "  domains: [%s]\n", strings.Join(t.Domains, ", "))
			}
		}
		return nil
	default: // table
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tSLUG\tACTIVE\tDOMAINS")
		for _, t := range tenants {
			domains := strings.Join(t.Domains, ",")
			fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%s\n", t.ID, t.Name, t.Slug, t.IsActive, domains)
		}
		return tw.Flush()
	}
}
