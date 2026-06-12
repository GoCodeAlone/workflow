package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/cmd/wfctl/internal/prompt"
	"github.com/GoCodeAlone/workflow/config"
)

// interactiveSetupArgs carries the parsed flags for the interactive
// secrets setup path.
type interactiveSetupArgs struct {
	configFile string
	storeName  string // --store flag override
	envName    string // --env, used for per-secret store resolution display
}

// setupDecl is the declared-secret type used by both setup front-ends.
type setupDecl struct {
	name        string
	sensitive   bool
	description string
	store       string
}

// builtinProviderTypes are the provider names a user can pick when no named
// store resolves and they must choose one interactively.
var builtinProviderTypes = []string{"env", "github", "vault", "aws", "keychain", "file"}

// runSecretsSetupInteractive drives the interactive wizard:
//  1. Resolve the store (prompting with prompt.Select when unresolved).
//  2. Build the provider + print a store-access line (✓ / ✗ redacted).
//  3. Query per-entry status, prompt.MultiSelect which to set.
//  4. prompt.Input (masked when sensitive) for each selected value.
//  5. prompt.Confirm the summary, then run the shared engine.
//
// If any prompt returns prompt.ErrNotInteractive (stdin not a TTY despite the
// caller routing here), the function returns that error so the caller can fall
// back to the non-interactive path — it never hangs.
func runSecretsSetupInteractive(ctx context.Context, a *interactiveSetupArgs, out io.Writer) error {
	cfg, err := config.LoadFromFile(a.configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.Secrets == nil || len(cfg.Secrets.Entries) == 0 {
		fmt.Fprintln(out, "No secrets declared in config.")
		return nil
	}

	decls := buildSetupDecls(cfg, a.envName, a.storeName)

	// 3. Query per-entry status and let the user MultiSelect.
	for _, storeName := range setupDeclStores(decls) {
		provider, err := getProviderForStore(storeName, cfg)
		if err != nil {
			return fmt.Errorf("resolve store %q: %w", storeName, err)
		}
		printStoreAccessLine(ctx, out, storeName, provider)
	}
	statuses := querySetupDeclStatuses(ctx, decls, cfg)
	items := buildMultiSelectItems(decls, statuses)

	selectedIdx, err := prompt.MultiSelect(
		"Select secrets to set (space to toggle, enter to confirm)",
		items,
	)
	if err != nil {
		if errors.Is(err, prompt.ErrNotInteractive) {
			return err
		}
		if errors.Is(err, prompt.ErrCancelled) {
			return err
		}
		return fmt.Errorf("select secrets: %w", err)
	}
	if len(selectedIdx) == 0 {
		fmt.Fprintln(out, "No secrets selected; nothing to do.")
		return nil
	}

	// 4. Confirm before writing.
	ok, err := prompt.Confirm(
		fmt.Sprintf("Set %d secret(s) to their resolved store(s)?", len(selectedIdx)),
		true,
	)
	if err != nil {
		if errors.Is(err, prompt.ErrNotInteractive) {
			return err
		}
		if errors.Is(err, prompt.ErrCancelled) {
			return err
		}
		return fmt.Errorf("confirm: %w", err)
	}
	if !ok {
		fmt.Fprintln(out, "Aborted.")
		return nil
	}

	// 5. Build the engine selector (the user's MultiSelect choice) + valuer
	//    (masked prompt.Input). Reuse the SAME engine + audit as non-interactive.
	selectedSet := make(map[int]bool, len(selectedIdx))
	for _, i := range selectedIdx {
		selectedSet[i] = true
	}
	selectedDecls := make([]setupDecl, 0, len(selectedIdx))
	for i, decl := range decls {
		if selectedSet[i] {
			selectedDecls = append(selectedDecls, decl)
		}
	}

	var promptErr error
	valuer := func(d setupDecl) (string, bool, error) {
		label := d.name
		if d.description != "" {
			label = d.name + " — " + d.description
		}
		v, err := prompt.Input(label, d.sensitive)
		if err != nil {
			if errors.Is(err, prompt.ErrNotInteractive) || errors.Is(err, prompt.ErrCancelled) {
				promptErr = err
			}
			return "", false, err
		}
		if v == "" {
			// Empty input → skip this secret rather than write an empty value.
			return "", false, nil
		}
		return v, true, nil
	}

	auditFn := func(name, store string) {
		_ = writeSecretsAuditRecord(name, store) //nolint:errcheck // best-effort audit
	}

	report, err := runSetupDeclsByStore(ctx, cfg, selectedDecls, valuer, auditFn, false)
	// If a prompt.Input hit a non-TTY mid-flow, surface ErrNotInteractive so the
	// caller's fallback triggers. The engine runs with stopOnErr=false, so it
	// returns (report, nil) even when the valuer reported ErrNotInteractive —
	// hence this check must run regardless of err.
	if promptErr != nil {
		return promptErr
	}
	if err != nil {
		return err
	}

	printSetupReport(out, report)
	if len(report.Failed) > 0 {
		return fmt.Errorf("%d secret(s) failed to set", len(report.Failed))
	}
	return nil
}

func buildSetupDecls(cfg *config.WorkflowConfig, envName, storeOverride string) []setupDecl {
	if cfg == nil || cfg.Secrets == nil {
		return nil
	}
	decls := make([]setupDecl, 0, len(cfg.Secrets.Entries))
	for _, e := range cfg.Secrets.Entries {
		storeName := strings.TrimSpace(storeOverride)
		if storeName == "" {
			storeName = ResolveSecretStore(e.Name, envName, cfg)
		}
		decls = append(decls, setupDecl{
			name:        e.Name,
			sensitive:   isSecretSensitive(e.Name),
			description: e.Description,
			store:       storeName,
		})
	}
	return decls
}

func setupDeclStores(decls []setupDecl) []string {
	seen := map[string]bool{}
	var stores []string
	for _, decl := range decls {
		store := strings.TrimSpace(decl.store)
		if store == "" || seen[store] {
			continue
		}
		seen[store] = true
		stores = append(stores, store)
	}
	sort.Strings(stores)
	return stores
}

// storePickOptions returns the ordered list of store names a user can pick:
// configured secretStores keys (sorted) first, then builtin provider types
// that aren't already a store name.
func storePickOptions(stores map[string]*config.SecretStoreConfig) []string {
	var keys []string
	seen := make(map[string]bool)
	for k := range stores {
		keys = append(keys, k)
		seen[k] = true
	}
	sort.Strings(keys)
	for _, p := range builtinProviderTypes {
		if !seen[p] {
			keys = append(keys, p)
		}
	}
	return keys
}

// queryDeclStatuses queries the provider for each declared secret's status.
func queryDeclStatuses(ctx context.Context, decls []setupDecl, provider SecretsProvider) []SecretStatus {
	statuses := make([]SecretStatus, 0, len(decls))
	for _, d := range decls {
		state, _ := provider.Check(ctx, d.name)
		statuses = append(statuses, SecretStatus{
			Name:  d.name,
			State: state,
			IsSet: state == SecretSet,
		})
	}
	return statuses
}

func querySetupDeclStatuses(ctx context.Context, decls []setupDecl, cfg *config.WorkflowConfig) []SecretStatus {
	statuses := make([]SecretStatus, 0, len(decls))
	providers := map[string]SecretsProvider{}
	for _, d := range decls {
		provider, ok := providers[d.store]
		if !ok {
			var err error
			provider, err = getProviderForStore(d.store, cfg)
			if err != nil {
				statuses = append(statuses, SecretStatus{
					Name:  d.name,
					Store: d.store,
					State: SecretUnconfigured,
					Error: err.Error(),
				})
				continue
			}
			providers[d.store] = provider
		}
		state, err := provider.Check(ctx, d.name)
		status := SecretStatus{
			Name:  d.name,
			Store: d.store,
			State: state,
			IsSet: state == SecretSet,
		}
		if err != nil {
			status.Error = err.Error()
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// buildMultiSelectItems builds the prompt.MultiSelect rows. Unset secrets are
// preselected; set secrets show their last-updated age when known.
func buildMultiSelectItems(decls []setupDecl, statuses []SecretStatus) []prompt.Item {
	statusByName := make(map[string]SecretStatus, len(statuses))
	for _, s := range statuses {
		statusByName[s.Name] = s
	}
	items := make([]prompt.Item, 0, len(decls))
	for _, d := range decls {
		st := statusByName[d.name]
		label := formatStatusLabel(d.name, st)
		if d.store != "" {
			label += " [" + d.store + "]"
		}
		items = append(items, prompt.Item{
			Label:       label,
			Preselected: !st.IsSet, // preselect unset
		})
	}
	return items
}

func runSetupDeclsByStore(ctx context.Context, cfg *config.WorkflowConfig, decls []setupDecl, valuer func(setupDecl) (string, bool, error), audit func(string, string), stopOnError bool) (setupReport, error) {
	var report setupReport
	providers := map[string]SecretsProvider{}
	for _, decl := range decls {
		provider, ok := providers[decl.store]
		if !ok {
			var err error
			provider, err = getProviderForStore(decl.store, cfg)
			if err != nil {
				report.Failed = append(report.Failed, decl.name)
				if stopOnError {
					return report, err
				}
				continue
			}
			providers[decl.store] = provider
		}
		value, provided, err := valuer(decl)
		if err != nil {
			report.Failed = append(report.Failed, decl.name)
			if errors.Is(err, prompt.ErrCancelled) || errors.Is(err, prompt.ErrNotInteractive) {
				return report, err
			}
			if stopOnError {
				return report, err
			}
			continue
		}
		if !provided {
			report.Skipped = append(report.Skipped, decl.name)
			continue
		}
		if err := provider.Set(ctx, decl.name, value); err != nil {
			report.Failed = append(report.Failed, decl.name)
			if stopOnError {
				return report, err
			}
			continue
		}
		report.Set = append(report.Set, decl.name)
		if audit != nil {
			audit(decl.name, decl.store)
		}
	}
	return report, nil
}

// formatStatusLabel renders a MultiSelect row label for one secret.
//
//	NAME   ✓ set · updated 3d ago
//	NAME   ✗ unset
func formatStatusLabel(name string, st SecretStatus) string {
	switch st.State {
	case SecretNoAccess:
		return fmt.Sprintf("%-24s ! no access", name)
	case SecretFetchError:
		return fmt.Sprintf("%-24s ! check failed", name)
	case SecretUnconfigured:
		return fmt.Sprintf("%-24s ! unconfigured", name)
	}
	if st.IsSet {
		age := formatRotatedAge(st.LastRotated)
		if age != "" {
			return fmt.Sprintf("%-24s ✓ set · updated %s", name, age)
		}
		return fmt.Sprintf("%-24s ✓ set", name)
	}
	return fmt.Sprintf("%-24s ✗ unset", name)
}

// formatRotatedAge renders a coarse "Nd ago" / "Nh ago" string, or "" when
// the timestamp is zero (store doesn't expose it).
func formatRotatedAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins < 1 {
			return "just now"
		}
		return fmt.Sprintf("%dm ago", mins)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// printStoreAccessLine type-asserts to the concrete adapter and prints a
// store-access status line. Errors are redacted to avoid leaking credentials.
func printStoreAccessLine(ctx context.Context, out io.Writer, storeName string, provider SecretsProvider) {
	adapter, ok := provider.(secretsProviderAdapter)
	if !ok {
		fmt.Fprintf(out, "Store %q access: (unknown)\n", storeName)
		return
	}
	if err := adapter.checkAccess(ctx); err != nil {
		fmt.Fprintf(out, "Store %q access: ✗ %s\n", storeName, redactAccessError(err))
		return
	}
	fmt.Fprintf(out, "Store %q access: ✓\n", storeName)
}

// redactAccessError returns a non-leaky description of an access-check error.
// We deliberately surface only the error class, never the message body, since
// provider errors can echo back credentials or tokens.
func redactAccessError(err error) string {
	if err == nil {
		return ""
	}
	return "not accessible (credentials or permissions; details redacted)"
}

// printSetupReport prints the engine result summary (never values).
func printSetupReport(out io.Writer, report setupReport) {
	for _, n := range report.Set {
		fmt.Fprintf(out, "  %-24s  [set]\n", n)
	}
	for _, n := range report.Skipped {
		fmt.Fprintf(out, "  %-24s  [skipped]\n", n)
	}
	for _, n := range report.Failed {
		fmt.Fprintf(out, "  %-24s  [failed]\n", n)
	}
	fmt.Fprintf(out, "\nDone: %d set, %d skipped, %d failed.\n",
		len(report.Set), len(report.Skipped), len(report.Failed))
}
