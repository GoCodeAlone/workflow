package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
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

	decls := make([]setupDecl, 0, len(cfg.Secrets.Entries))
	for _, e := range cfg.Secrets.Entries {
		decls = append(decls, setupDecl{
			name:        e.Name,
			sensitive:   isSecretSensitive(e.Name),
			description: e.Description,
		})
	}

	// 1. Resolve the store (prompt when unresolved + interactive).
	storeName, err := resolveSetupStoreInteractive(a.storeName, cfg)
	if err != nil {
		return err
	}

	// 2. Build the provider + print an access line.
	provider, err := getProviderForStore(storeName, cfg)
	if err != nil {
		return fmt.Errorf("resolve store %q: %w", storeName, err)
	}
	printStoreAccessLine(ctx, out, storeName, provider)

	// 3. Query per-entry status and let the user MultiSelect.
	statuses := queryDeclStatuses(ctx, decls, provider)
	items := buildMultiSelectItems(decls, statuses)

	selectedIdx, err := prompt.MultiSelect(
		fmt.Sprintf("Select secrets to set in %q (space to toggle, enter to confirm)", storeName),
		items,
	)
	if err != nil {
		if errors.Is(err, prompt.ErrNotInteractive) {
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
		fmt.Sprintf("Set %d secret(s) to store %q?", len(selectedIdx), storeName),
		true,
	)
	if err != nil {
		if errors.Is(err, prompt.ErrNotInteractive) {
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
	selectedSet := make(map[string]bool, len(selectedIdx))
	for _, i := range selectedIdx {
		selectedSet[decls[i].name] = true
	}
	selector := func(ds []setupDecl, _ []SecretStatus) ([]setupDecl, error) {
		var keep []setupDecl
		for _, d := range ds {
			if selectedSet[d.name] {
				keep = append(keep, d)
			}
		}
		return keep, nil
	}

	var promptErr error
	valuer := func(d setupDecl) (string, bool, error) {
		label := d.name
		if d.description != "" {
			label = d.name + " — " + d.description
		}
		v, err := prompt.Input(label, d.sensitive)
		if err != nil {
			if errors.Is(err, prompt.ErrNotInteractive) {
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

	auditFn := func(name, _ string) {
		_ = writeSecretsAuditRecord(name, storeName) //nolint:errcheck // best-effort audit
	}

	report, err := runSetupEngine(ctx, decls,
		func(d setupDecl) string { return d.name },
		provider, selector, valuer, auditFn, false)
	if err != nil {
		// If a prompt.Input hit a non-TTY mid-flow, surface ErrNotInteractive.
		if promptErr != nil {
			return promptErr
		}
		return err
	}

	printSetupReport(out, report)
	if len(report.Failed) > 0 {
		return fmt.Errorf("%d secret(s) failed to set", len(report.Failed))
	}
	return nil
}

// resolveSetupStoreInteractive resolves the store, prompting the user with
// prompt.Select when the resolver returns unresolved ("" + nil error).
func resolveSetupStoreInteractive(storeFlag string, cfg *config.WorkflowConfig) (string, error) {
	defaultStore := ""
	if cfg.Secrets != nil {
		defaultStore = cfg.Secrets.DefaultStore
		if defaultStore == "" {
			defaultStore = cfg.Secrets.Provider
		}
	}
	name, err := resolveSetupStoreName(storeFlag, defaultStore, cfg.SecretStores, true)
	if err != nil {
		return "", err
	}
	if name != "" {
		return name, nil
	}
	// Unresolved + interactive → prompt over store keys + builtin provider types.
	opts := storePickOptions(cfg.SecretStores)
	idx, err := prompt.Select("Pick a secret store", opts)
	if err != nil {
		return "", err
	}
	return opts[idx], nil
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
		items = append(items, prompt.Item{
			Label:       formatStatusLabel(d.name, st),
			Preselected: !st.IsSet, // preselect unset
		})
	}
	return items
}

// formatStatusLabel renders a MultiSelect row label for one secret.
//
//	NAME   ✓ set · updated 3d ago
//	NAME   ✗ unset
func formatStatusLabel(name string, st SecretStatus) string {
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
