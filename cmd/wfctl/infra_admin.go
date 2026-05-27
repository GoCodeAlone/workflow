package main

// wfctl infra admin — CLI mirror of the host-side infra.admin module's
// typed RPC surface. Each subcommand resolves the iac.state backend +
// iac.provider modules from a workflow config via wfctlhelpers (T1-T3),
// invokes the shared handler library (T5/T6), and renders the output.
//
// The CLI is the second half of the design's "handler library imported
// by both module HTTP routes and wfctl CLI subcommands" contract — the
// same handler functions back HTTP and CLI so behavior cannot drift.
//
// Subcommands (per plan §Task 19):
//
//	wfctl infra admin list-resources    [--type T] [--provider P]
//	                                    [--app-context CTX] [--env E]
//	                                    [--format json|table] [-c FILE]
//	wfctl infra admin get-resource NAME [--env E] [--format json|table]
//	                                    [-c FILE]
//	wfctl infra admin list-types        [--provider P]
//	                                    [--format json|json-schema|table]
//	                                    [-c FILE]
//	wfctl infra admin list-providers    [--env E] [-c FILE]
//	wfctl infra admin generate-config   --type T --name N
//	                                    --provider P [--field K=V...]
//	                                    [-c FILE]
//	wfctl infra admin audit-tail        --base-url URL [--since DUR]
//	                                    [--format json|table]
//
// Authz: the CLI runs as the operator with full filesystem access to
// the workflow config; AdminAuthzEvidence is implicitly satisfied
// (authz_checked=true, authz_allowed=true). The handler library still
// runs its default-deny guard, so we ALWAYS populate evidence — the
// authz layer above the CLI is the OS filesystem permission on the
// config file, not a separate token check.
//
// Plan §Task 20 covers the parity test asserting JSON output decodes
// as the matching adminpb proto type.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// infraAdminEvidence returns the AdminAuthzEvidence stamp every
// CLI-side invocation needs. CLI operators have already been vetted by
// filesystem ACL on the config file; the handler library's default-
// deny guard still requires the evidence to be populated.
//
// Subject preference per implementer-1's T19 guidance: the OS-side
// USER env var when present, falling back to a static "wfctl-cli"
// sentinel. The env var is best-effort (operators can spoof it); its
// only purpose is improving the audit-log breadcrumb for routine
// CLI use, NOT as an authz primitive. Authz is the filesystem ACL on
// the config file.
func infraAdminEvidence() *adminpb.AdminAuthzEvidence {
	subject := os.Getenv("USER")
	if subject == "" {
		subject = "wfctl-cli"
	}
	return &adminpb.AdminAuthzEvidence{
		AuthzChecked:       true,
		AuthzAllowed:       true,
		Subject:            subject,
		GrantedPermissions: []string{"infra:read"},
	}
}

// infraAdminFormat is the shared --format value type. Accepts a small
// allowlist to keep usage predictable.
type infraAdminFormat string

const (
	infraAdminFormatJSON       infraAdminFormat = "json"
	infraAdminFormatJSONSchema infraAdminFormat = "json-schema"
	infraAdminFormatTable      infraAdminFormat = "table"
)

func runInfraAdmin(args []string) error {
	if len(args) < 1 {
		return infraAdminUsage()
	}
	switch args[0] {
	case "list-resources":
		return runInfraAdminListResources(args[1:])
	case "get-resource":
		return runInfraAdminGetResource(args[1:])
	case "list-types":
		return runInfraAdminListTypes(args[1:])
	case "list-providers":
		return runInfraAdminListProviders(args[1:])
	case "generate-config":
		return runInfraAdminGenerateConfig(args[1:])
	case "audit-tail":
		return runInfraAdminAuditTail(args[1:])
	case "--help", "-h", "help":
		return infraAdminUsage()
	default:
		fmt.Fprintf(os.Stderr, "wfctl infra admin: unknown subcommand %q\n\n", args[0])
		return infraAdminUsage()
	}
}

func infraAdminUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl infra admin <subcommand> [options]

Mirror of the infra.admin host-side module's typed RPC surface. Reads
the iac.state backend + iac.provider modules from a workflow config
and renders results.

Subcommands:
  list-resources    [--type T] [--provider P] [--app-context CTX] [--env E]
                    [--format json|table] [-c FILE]
  get-resource NAME [--env E] [--format json|table] [-c FILE]
  list-types        [--provider P] [--format json|json-schema|table] [-c FILE]
  list-providers    [--env E] [-c FILE]
  generate-config   --type T --name N --provider P [--field K=V ...]
                    [-c FILE]
  audit-tail        --base-url URL [--since DUR] [--format json|table]

Common flags:
  -c FILE           Path to workflow config (default: workflow.yaml).
                    Required for all subcommands except audit-tail.
  --env NAME        Per-environment backend overrides applied to iac.state.
  --format VAL      Output format (json or table). audit-tail emits ndjson
                    on stdout when --format=json.
`)
	return nil
}

// --- dependency resolution ------------------------------------------------

// adminDeps bundles the three things every read subcommand needs to
// invoke the handler library: state backend, provider map (keyed by
// host YAML module name), the catalog triple, and the
// providerTypeByModule lookup populated from the YAML config at
// resolve-time (per design cycle-5/6 + spec-reviewer T6 F1 — the
// YAML-config `provider:` field is the stable identifier the
// catalogs key against; provider.Name() returns the plugin's
// display name and is NOT a stable identifier).
type adminDeps struct {
	store                interfaces.IaCStateStore
	providers            map[string]interfaces.IaCProvider
	providerTypeByModule map[string]string
	closers              []io.Closer
	fieldCatalog         *catalog.FieldSpecCatalog
	regionCatalog        *catalog.RegionCatalog
	engineCatalog        *catalog.EngineCatalog
}

// resolveAdminDeps loads the workflow config and instantiates everything
// the handler library needs. envName is forwarded to ResolveStateStore
// for per-env backend overrides.
func resolveAdminDeps(ctx context.Context, cfgFile, envName string) (*adminDeps, error) {
	if cfgFile == "" {
		return nil, errors.New("config file path required (-c FILE)")
	}
	store, err := wfctlhelpers.ResolveStateStore(cfgFile, envName, currentInfraPluginDir)
	if err != nil {
		return nil, fmt.Errorf("resolve state store: %w", err)
	}
	providers, closers, err := wfctlhelpers.LoadAllIaCProvidersFromConfig(ctx, cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load providers: %w", err)
	}
	providerTypeByModule, err := loadProviderTypeByModule(cfgFile)
	if err != nil {
		// Roll back the loaded providers so we don't leak subprocesses.
		for _, c := range closers {
			_ = c.Close()
		}
		return nil, fmt.Errorf("load providerTypeByModule: %w", err)
	}
	return &adminDeps{
		store:                store,
		providers:            providers,
		providerTypeByModule: providerTypeByModule,
		closers:              closers,
		fieldCatalog:         catalog.New(),
		regionCatalog:        catalog.NewRegionCatalog(),
		engineCatalog:        catalog.NewEngineCatalog(),
	}, nil
}

// loadProviderTypeByModule walks cfgFile's modules and returns a map
// of {iac.provider module name -> YAML `provider:` string}. Per
// spec-reviewer T6 F1 + design cycle-5/6: this is the captured-at-
// Init contract handler.ListProviders relies on for the
// provider_type field in AdminProviderSummary. T15 (host module)
// will populate the same map from its app.GetService-resolved
// modules; the CLI side reads it from disk.
func loadProviderTypeByModule(cfgFile string) (map[string]string, error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", cfgFile, err)
	}
	out := map[string]string{}
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if m.Type != "iac.provider" {
			continue
		}
		modCfg := config.ExpandEnvInMap(m.Config)
		pt, _ := modCfg["provider"].(string)
		if pt == "" {
			// Skip silently — matches LoadAllIaCProvidersFromConfig's
			// same-shape behavior; misconfigured module already won't
			// have a provider entry in the providers map either.
			continue
		}
		out[m.Name] = pt
	}
	return out, nil
}

func (d *adminDeps) Close() {
	for _, c := range d.closers {
		if c != nil {
			_ = c.Close()
		}
	}
}

// --- subcommand: list-resources -------------------------------------------

func runInfraAdminListResources(args []string) error {
	fs := flag.NewFlagSet("infra admin list-resources", flag.ContinueOnError)
	cfg := fs.String("c", "workflow.yaml", "config file path")
	typeFilter := fs.String("type", "", "filter by resource type (e.g. infra.vpc)")
	providerFilter := fs.String("provider", "", "filter by provider module name")
	appContext := fs.String("app-context", "", "filter by app_context label")
	envName := fs.String("env", "", "per-env backend overrides")
	format := fs.String("format", string(infraAdminFormatJSON), "json or table")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()
	deps, err := resolveAdminDeps(ctx, *cfg, *envName)
	if err != nil {
		return err
	}
	defer deps.Close()

	in := &adminpb.AdminListResourcesInput{
		TypeFilter:       *typeFilter,
		ProviderFilter:   *providerFilter,
		AppContextFilter: *appContext,
		EnvName:          *envName,
		Evidence:         infraAdminEvidence(),
	}
	out, err := handler.ListResources(ctx, deps.store, deps.providers, deps.fieldCatalog, in)
	if err != nil {
		return err
	}
	if out.Error != "" {
		return errors.New(out.Error)
	}

	switch infraAdminFormat(*format) {
	case infraAdminFormatJSON:
		return emitJSON(out)
	case infraAdminFormatTable:
		return renderResourcesTable(out.Resources)
	default:
		return fmt.Errorf("--format %q not supported (use json or table)", *format)
	}
}

func renderResourcesTable(rows []*adminpb.AdminResourceSummary) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	defer func() { _ = w.Flush() }()
	fmt.Fprintln(w, "NAME\tTYPE\tPROVIDER\tSTATUS\tUPDATED")
	for _, r := range rows {
		updated := ""
		if r.UpdatedAtUnix > 0 {
			updated = time.Unix(r.UpdatedAtUnix, 0).UTC().Format(time.RFC3339)
		}
		providerCell := r.ProviderModule
		if r.ProviderType != "" {
			providerCell = providerCell + " / " + r.ProviderType
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			r.Name, r.Type, providerCell, r.Status, updated)
	}
	return nil
}

// --- subcommand: get-resource ---------------------------------------------

func runInfraAdminGetResource(args []string) error {
	fs := flag.NewFlagSet("infra admin get-resource", flag.ContinueOnError)
	cfg := fs.String("c", "workflow.yaml", "config file path")
	envName := fs.String("env", "", "per-env backend overrides")
	format := fs.String("format", string(infraAdminFormatJSON), "json or table")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("get-resource: NAME positional argument required")
	}
	name := fs.Arg(0)

	ctx := context.Background()
	deps, err := resolveAdminDeps(ctx, *cfg, *envName)
	if err != nil {
		return err
	}
	defer deps.Close()

	in := &adminpb.AdminGetResourceInput{
		Name:     name,
		EnvName:  *envName,
		Evidence: infraAdminEvidence(),
	}
	out, err := handler.GetResource(ctx, deps.store, in)
	if err != nil {
		return err
	}
	if out.Error != "" {
		return errors.New(out.Error)
	}

	switch infraAdminFormat(*format) {
	case infraAdminFormatJSON:
		return emitJSON(out)
	case infraAdminFormatTable:
		return renderResourceDetail(out.Resource)
	default:
		return fmt.Errorf("--format %q not supported (use json or table)", *format)
	}
}

func renderResourceDetail(r *adminpb.AdminResourceDetail) error {
	if r == nil {
		fmt.Println("(empty)")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	s := r.Summary
	if s != nil {
		fmt.Fprintf(w, "Name:\t%s\n", s.Name)
		fmt.Fprintf(w, "Type:\t%s\n", s.Type)
		fmt.Fprintf(w, "Provider module:\t%s\n", s.ProviderModule)
		fmt.Fprintf(w, "Provider type:\t%s\n", s.ProviderType)
		fmt.Fprintf(w, "Provider id:\t%s\n", s.ProviderId)
		fmt.Fprintf(w, "Status:\t%s\n", s.Status)
		fmt.Fprintf(w, "App context:\t%s\n", s.AppContext)
		if s.UpdatedAtUnix > 0 {
			fmt.Fprintf(w, "Updated:\t%s\n", time.Unix(s.UpdatedAtUnix, 0).UTC().Format(time.RFC3339))
		}
		fmt.Fprintf(w, "Dependencies:\t%s\n", strings.Join(s.Dependencies, ", "))
	}
	fmt.Fprintf(w, "Config hash:\t%s\n", r.ConfigHash)
	if len(r.SensitiveOutputsRedacted) > 0 {
		fmt.Fprintf(w, "Redacted outputs:\t%s\n", strings.Join(r.SensitiveOutputsRedacted, ", "))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if len(r.AppliedConfigJson) > 0 {
		fmt.Println("\nApplied config:")
		fmt.Println(string(r.AppliedConfigJson))
	}
	if len(r.OutputsJson) > 0 {
		fmt.Println("\nOutputs (redacted):")
		fmt.Println(string(r.OutputsJson))
	}
	return nil
}

// --- subcommand: list-types -----------------------------------------------

func runInfraAdminListTypes(args []string) error {
	fs := flag.NewFlagSet("infra admin list-types", flag.ContinueOnError)
	cfg := fs.String("c", "workflow.yaml", "config file path")
	providerFilter := fs.String("provider", "", "filter by provider")
	envName := fs.String("env", "", "per-env backend overrides")
	format := fs.String("format", string(infraAdminFormatJSON), "json, json-schema, or table")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()
	deps, err := resolveAdminDeps(ctx, *cfg, *envName)
	if err != nil {
		return err
	}
	defer deps.Close()

	in := &adminpb.AdminListResourceTypesInput{
		ProviderFilter: *providerFilter,
		Evidence:       infraAdminEvidence(),
	}
	out, err := handler.ListResourceTypes(ctx, deps.fieldCatalog, deps.providers, in)
	if err != nil {
		return err
	}
	if out.Error != "" {
		return errors.New(out.Error)
	}

	switch infraAdminFormat(*format) {
	case infraAdminFormatJSON, infraAdminFormatJSONSchema:
		return emitJSON(out)
	case infraAdminFormatTable:
		return renderTypesTable(out.Types)
	default:
		return fmt.Errorf("--format %q not supported (use json, json-schema, or table)", *format)
	}
}

func renderTypesTable(types []*adminpb.AdminResourceTypeMetadata) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	defer func() { _ = w.Flush() }()
	fmt.Fprintln(w, "TYPE\tFIELDS\tFQN")
	for _, t := range types {
		names := make([]string, 0, len(t.Fields))
		for _, f := range t.Fields {
			names = append(names, f.Name)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", t.Type, strings.Join(names, ","), t.ConfigMessageFqn)
	}
	return nil
}

// --- subcommand: list-providers -------------------------------------------

func runInfraAdminListProviders(args []string) error {
	fs := flag.NewFlagSet("infra admin list-providers", flag.ContinueOnError)
	cfg := fs.String("c", "workflow.yaml", "config file path")
	envName := fs.String("env", "", "per-env backend overrides")
	format := fs.String("format", string(infraAdminFormatJSON), "json or table")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()
	deps, err := resolveAdminDeps(ctx, *cfg, *envName)
	if err != nil {
		return err
	}
	defer deps.Close()

	in := &adminpb.AdminListProvidersInput{
		EnvName:  *envName,
		Evidence: infraAdminEvidence(),
	}
	out, err := handler.ListProviders(ctx, deps.providers, deps.providerTypeByModule, deps.fieldCatalog, deps.regionCatalog, deps.engineCatalog, in)
	if err != nil {
		return err
	}
	if out.Error != "" {
		return errors.New(out.Error)
	}

	switch infraAdminFormat(*format) {
	case infraAdminFormatJSON:
		return emitJSON(out)
	case infraAdminFormatTable:
		return renderProvidersTable(out.Providers)
	default:
		return fmt.Errorf("--format %q not supported (use json or table)", *format)
	}
}

func renderProvidersTable(providers []*adminpb.AdminProviderSummary) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	defer func() { _ = w.Flush() }()
	fmt.Fprintln(w, "MODULE\tTYPE\tREGIONS\tENGINES\tREGIONS_SOURCE")
	for _, p := range providers {
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\n",
			p.ModuleName, p.ProviderType,
			len(p.SupportedRegions), len(p.SupportedEngines),
			p.RegionsSource)
	}
	return nil
}

// --- subcommand: generate-config ------------------------------------------

// fieldFlag implements flag.Value for repeated --field KEY=VALUE flags.
// Multiple invocations append to the captured map; duplicate keys are
// last-write-wins (consistent with form-builder submit behavior where
// later inputs override earlier).
type fieldFlag struct {
	values map[string]string
}

func newFieldFlag() *fieldFlag {
	return &fieldFlag{values: map[string]string{}}
}

func (f *fieldFlag) String() string {
	if f == nil || len(f.values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(f.values))
	for k := range f.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+f.values[k])
	}
	return strings.Join(parts, ",")
}

func (f *fieldFlag) Set(s string) error {
	i := strings.Index(s, "=")
	if i <= 0 {
		return fmt.Errorf("--field must be KEY=VALUE, got %q", s)
	}
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[s[:i]] = s[i+1:]
	return nil
}

func runInfraAdminGenerateConfig(args []string) error {
	fs := flag.NewFlagSet("infra admin generate-config", flag.ContinueOnError)
	cfg := fs.String("c", "workflow.yaml", "config file path")
	rType := fs.String("type", "", "resource type (e.g. infra.vpc)")
	rName := fs.String("name", "", "resource name")
	provider := fs.String("provider", "", "provider module name")
	envName := fs.String("env", "", "per-env backend overrides")
	fields := newFieldFlag()
	fs.Var(fields, "field", "field KEY=VALUE (repeatable; array values are JSON-encoded e.g. --field ports='[80,443]')")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rType == "" || *rName == "" || *provider == "" {
		return errors.New("generate-config: --type, --name, --provider all required")
	}

	ctx := context.Background()
	deps, err := resolveAdminDeps(ctx, *cfg, *envName)
	if err != nil {
		return err
	}
	defer deps.Close()

	in := &adminpb.AdminGenerateConfigInput{
		ResourceType:   *rType,
		ResourceName:   *rName,
		ProviderModule: *provider,
		FieldValues:    fields.values,
		Evidence:       infraAdminEvidence(),
	}
	out, err := handler.GenerateConfig(ctx, deps.fieldCatalog, in)
	if err != nil {
		return err
	}
	if out.Error != "" {
		return errors.New(out.Error)
	}
	if len(out.ValidationErrors) > 0 {
		for _, e := range out.ValidationErrors {
			fmt.Fprintf(os.Stderr, "validation error: %s\n", e)
		}
		return fmt.Errorf("generate-config returned %d validation error(s)", len(out.ValidationErrors))
	}
	fmt.Print(out.YamlSnippet)
	if !strings.HasSuffix(out.YamlSnippet, "\n") {
		fmt.Println()
	}
	return nil
}

// --- subcommand: audit-tail -----------------------------------------------

// runInfraAdminAuditTail does NOT load a config — it talks HTTP to a
// running infra.admin module instance via its /api/infra-admin/audit
// endpoint. Per design §Security Review row "Access logging".
//
// The endpoint streams newline-delimited AdminAuditEntry proto-JSON.
// We pass --format=json through unchanged (forward the body bytes);
// --format=table parses each line and renders a compact view.
func runInfraAdminAuditTail(args []string) error {
	fs := flag.NewFlagSet("infra admin audit-tail", flag.ContinueOnError)
	baseURL := fs.String("base-url", "", "host base URL (e.g. https://admin.example.com)")
	since := fs.Duration("since", 0, "tail entries newer than this duration (e.g. 1h, 24h)")
	format := fs.String("format", string(infraAdminFormatJSON), "json or table")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *baseURL == "" {
		return errors.New("audit-tail: --base-url required")
	}

	u, err := url.Parse(*baseURL)
	if err != nil {
		return fmt.Errorf("--base-url: %w", err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/infra-admin/audit"
	q := u.Query()
	if *since > 0 {
		q.Set("since", fmt.Sprintf("%d", time.Now().Add(-*since).Unix()))
	}
	u.RawQuery = q.Encode()

	resp, err := http.Get(u.String()) //nolint:gosec // operator-supplied base URL
	if err != nil {
		return fmt.Errorf("audit-tail GET: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("audit-tail: HTTP %d", resp.StatusCode)
	}

	switch infraAdminFormat(*format) {
	case infraAdminFormatJSON:
		_, err := io.Copy(os.Stdout, resp.Body)
		return err
	case infraAdminFormatTable:
		return renderAuditTable(resp.Body)
	default:
		return fmt.Errorf("--format %q not supported (use json or table)", *format)
	}
}

func renderAuditTable(body io.Reader) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	defer func() { _ = w.Flush() }()
	fmt.Fprintln(w, "TS\tSUBJECT\tACTION\tRESULT\tTARGETS")
	dec := json.NewDecoder(body)
	for dec.More() {
		var entry adminpb.AdminAuditEntry
		if err := dec.Decode(&entry); err != nil {
			return fmt.Errorf("decode audit entry: %w", err)
		}
		ts := ""
		if entry.TsUnix > 0 {
			ts = time.Unix(entry.TsUnix, 0).UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			ts, entry.Subject, entry.Action, entry.Result, strings.Join(entry.Targets, ","))
	}
	return nil
}

// --- shared output helper -------------------------------------------------

// emitJSON writes the proto message as indented JSON to stdout. Used
// by every --format=json branch. We use encoding/json with Go struct
// tags (snake_case via the proto-generated json tags) for the
// AdminListResourcesOutput / etc. shapes. The parity test (T20) asserts
// json.Unmarshal of stdout into the matching adminpb type round-trips.
//
// We intentionally do NOT use protojson here. The CLI's wire is JSON
// over stdout; UseProtoNames vs camelCase divergence isn't a concern
// because both the encoder (Go's encoding/json with proto-generated
// snake_case tags) and the parity-test decoder (same) operate on the
// same struct tag set. The HTTP module on the host side uses protojson
// because there the wire is HTTP and the JS client expects snake_case;
// here the wire is the same Go process round-trip and using Go's
// stdlib keeps the dependency surface minimal.
func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
