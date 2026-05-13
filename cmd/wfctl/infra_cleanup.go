package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// cleanupStdout / cleanupStderr are seam variables tests override to capture
// the subcommand's structured output without redirecting os.Stdout globally
// (other parallel tests in cmd/wfctl exercise os.Stdout for their own
// assertions; redirecting it here would race them).
var (
	cleanupStdout io.Writer = os.Stdout
	cleanupStderr io.Writer = os.Stderr
)

// cleanupLoadProviders is the seam used by runInfraCleanup to obtain the
// IaCProvider instances declared in the config's iac.provider modules. The
// default implementation walks cfg.Modules + loads each via the existing
// resolveIaCProvider plugin path; tests override this var to inject fakes
// without spinning up real plugin subprocesses.
//
// fs + cfgFile are passed through so the default implementation can defer to
// the canonical resolveInfraConfig helper (which honors --config / -c /
// auto-discovery / positional .yaml argument). envName is empty when no
// --env was passed. Tests that don't exercise the config-resolution path
// can ignore both arguments.
//
// Returned closers (one per provider, indices aligned) MAY be nil. Callers
// MUST close them after the cleanup run.
var cleanupLoadProviders = defaultCleanupLoadProviders

// runInfraCleanup is the CLI entry point for `wfctl infra cleanup --tag`.
// It enumerates resources by tag across every loaded provider that implements
// the optional interfaces.Enumerator, and either lists them (default,
// --dry-run) or deletes them (--fix). Providers that do NOT implement
// Enumerator are skipped with a structured stdout log so operators see
// the explicit skip rather than silent under-cleanup.
//
// Exit semantics:
//   - all enumerations succeed + (dry-run OR all deletes succeed): nil error
//   - one or more providers' enumerate or delete fails: non-nil error joining
//     all collected failures (other providers' work continues so a single
//     bad-provider doesn't suppress the rest of the run).
//
// Flag shape: --dry-run defaults to true; --fix is the explicit opt-in to
// mutation. Passing --fix overrides --dry-run regardless of order.
func runInfraCleanup(args []string) error { //nolint:cyclop
	fs := flag.NewFlagSet("infra cleanup", flag.ContinueOnError)
	fs.SetOutput(cleanupStderr)

	var configFile string
	fs.StringVar(&configFile, "config", "", "Config file (default: infra.yaml or config/infra.yaml)")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	var envName string
	fs.StringVar(&envName, "env", "", "Environment name for config and state resolution")
	tag := fs.String("tag", "", "tag to match resources for cleanup (required)")
	dryRun := fs.Bool("dry-run", true, "preview only; do not delete resources (default: true)")
	fix := fs.Bool("fix", false, "actually delete resources (overrides --dry-run)")
	var pluginDirFlag string
	fs.StringVar(&pluginDirFlag, "plugin-dir", "", "Plugin directory (overrides WFCTL_PLUGIN_DIR and default data/plugins)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tag == "" {
		return errors.New("infra cleanup: --tag is required")
	}
	// --fix is the explicit opt-in to mutation; if absent, --dry-run remains
	// true regardless of any explicit --dry-run=false. This keeps the safe-
	// default invariant (cleanup is destructive: never delete without --fix).
	*dryRun = !*fix

	currentInfraPluginDir = pluginDirFlag
	defer func() { currentInfraPluginDir = "" }()

	ctx := context.Background()
	providers, closers, err := cleanupLoadProviders(ctx, fs, configFile, envName)
	if err != nil {
		return fmt.Errorf("load providers: %w", err)
	}
	defer func() {
		for _, c := range closers {
			if c == nil {
				continue
			}
			if cerr := c.Close(); cerr != nil {
				fmt.Fprintf(cleanupStderr, "warning: provider shutdown: %v\n", cerr)
			}
		}
	}()

	var totalErrs []error
	for _, p := range providers {
		// Per Task 17 of the strict-contracts force-cutover (ADR-0028):
		// pure typed-pb dispatch — no interfaces.X fallback. Production
		// always yields *typedIaCAdapter via discoverAndLoadIaCProvider
		// (PR #609); test fixtures must construct one via the same
		// bufconn-backed pattern (PR #603 precedent + this PR's own
		// fixture rewrites in Task 17 deliverable).
		adapter, ok := p.(*typedIaCAdapter)
		if !ok {
			err := fmt.Errorf("%s: provider %T is not a typed IaC adapter — re-load via discoverAndLoadIaCProvider", p.Name(), p)
			fmt.Fprintln(cleanupStderr, err)
			totalErrs = append(totalErrs, err)
			continue
		}
		enumCli := adapter.Enumerator()
		if enumCli == nil {
			fmt.Fprintf(cleanupStdout, "skipped %s: provider does not implement Enumerator\n", p.Name())
			continue
		}
		resp, err := enumCli.EnumerateByTag(ctx, &pb.EnumerateByTagRequest{Tag: *tag})
		if err != nil {
			// Per code-review IMPORTANT-1 (PR 618 round 4): translate
			// codes.Unimplemented at the wire boundary to
			// interfaces.ErrProviderMethodUnimplemented so callers using
			// errors.Is downstream of the join keep the sentinel signal.
			// The error still propagates loud (cleanup is single-shot per
			// ADR-0028 §Per-site dispatch UX) — the translation just
			// preserves classification for any retry / wrapper logic.
			err = translateRPCErr(err)
			fmt.Fprintf(cleanupStderr, "%s: enumerate by tag %q: %v\n", p.Name(), *tag, err)
			totalErrs = append(totalErrs, fmt.Errorf("%s: enumerate: %w", p.Name(), err))
			continue
		}
		refs := refsFromPB(resp.GetRefs())
		if len(refs) == 0 {
			fmt.Fprintf(cleanupStdout, "%s: no resources matched tag %q\n", p.Name(), *tag)
			continue
		}
		for _, ref := range refs {
			if *dryRun {
				fmt.Fprintf(cleanupStdout, "[dry-run] would delete %s/%s (provider: %s)\n", ref.Type, ref.Name, p.Name())
				continue
			}
			drv, drvErr := p.ResourceDriver(ref.Type)
			if drvErr != nil {
				err := fmt.Errorf("%s: resolve driver for %s/%s: %w", p.Name(), ref.Type, ref.Name, drvErr)
				fmt.Fprintln(cleanupStderr, err)
				totalErrs = append(totalErrs, err)
				continue
			}
			if delErr := drv.Delete(ctx, ref); delErr != nil {
				err := fmt.Errorf("%s: delete %s/%s: %w", p.Name(), ref.Type, ref.Name, delErr)
				fmt.Fprintln(cleanupStderr, err)
				totalErrs = append(totalErrs, err)
				continue
			}
			fmt.Fprintf(cleanupStdout, "deleted %s/%s (provider: %s)\n", ref.Type, ref.Name, p.Name())
		}
	}
	if len(totalErrs) > 0 {
		return errors.Join(totalErrs...)
	}
	return nil
}

// defaultCleanupLoadProviders walks the resolved config's iac.provider modules
// and loads each via the canonical resolveIaCProvider plugin path. Mirrors the
// pattern established by defaultAlignLoadProviders (R-A10) so the cleanup
// subcommand inherits the same env-resolution + plugin-discovery contract.
//
// Config resolution honors the standard precedence: explicit --config / -c
// flag → auto-discovered infra.yaml / config/infra.yaml → positional
// .yaml/.yml argument (consistent with the other infra subcommands).
//
// Best-effort behaviour: a provider that fails to load emits a stderr warning
// and is skipped (the rest of the cleanup proceeds). This matches the align
// path and prevents a single bad iac.provider entry from turning a smoke-gate
// cleanup into a hard failure that masks other providers' resources.
func defaultCleanupLoadProviders(ctx context.Context, fs *flag.FlagSet, cfgFile, envName string) ([]interfaces.IaCProvider, []io.Closer, error) {
	resolved, resolveErr := resolveInfraConfig(fs, cfgFile)
	if resolveErr != nil {
		return nil, nil, resolveErr
	}
	cfg, err := config.LoadFromFile(resolved)
	if err != nil {
		return nil, nil, fmt.Errorf("load %s: %w", resolved, err)
	}

	var providers []interfaces.IaCProvider
	var closers []io.Closer
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if m.Type != "iac.provider" {
			continue
		}
		var modCfg map[string]any
		if envName != "" {
			r, ok := m.ResolveForEnv(envName)
			if !ok {
				continue // disabled for this env
			}
			modCfg = config.ExpandEnvInMapPreservingKeys(r.Config, infraPreserveKeys)
		} else {
			modCfg = config.ExpandEnvInMapPreservingKeys(m.Config, infraPreserveKeys)
		}
		providerType, _ := modCfg["provider"].(string)
		if providerType == "" {
			continue
		}
		p, closer, loadErr := resolveIaCProvider(ctx, providerType, modCfg)
		if loadErr != nil {
			fmt.Fprintf(cleanupStderr, "warning: cleanup: load provider %q (%s): %v\n", m.Name, providerType, loadErr)
			continue
		}
		providers = append(providers, p)
		closers = append(closers, closer)
	}
	return providers, closers, nil
}
