package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/iac/sensitive"
	"github.com/GoCodeAlone/workflow/secrets"
)

// runInfraAuditStateSecrets is the CLI entry point for
// `wfctl infra audit-state-secrets`.
//
// Walks every entry in IaCStateStore. For each Outputs[k] that is:
//   - a "secret_ref://<name>" placeholder → confirm secrets.Provider has <name>.
//   - a plaintext value matching secrets.DefaultSensitiveKeys() → flag legacy.
//   - a "secret://<key>" string → flag mistaken config-reference in state.
//
// Then walks secrets.Provider.List() (when supported) for any
// "<resource>_<key>" name whose <resource> is NOT in IaCStateStore →
// orphan, candidate for prune.
//
// Exit codes:
//
//	0  no findings
//	1  findings (legacy plaintext, missing routed values, orphan secrets,
//	   mistaken config-references)
//	2  audit error (cannot read state, parse error, etc.)
func runInfraAuditStateSecrets(args []string, w io.Writer) int {
	fs := flag.NewFlagSet("infra audit-state-secrets", flag.ContinueOnError)
	fs.SetOutput(w)
	var configFile string
	fs.StringVar(&configFile, "c", "infra.yaml", "Config file")
	fs.StringVar(&configFile, "config", "infra.yaml", "Config file")
	var prune bool
	fs.BoolVar(&prune, "prune", false, "Delete confirmed orphan secrets from secrets.Provider")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := parseSecretsConfig(configFile)
	if err != nil {
		fmt.Fprintf(w, "audit-state-secrets: parse %q: %v\n", configFile, err)
		return 2
	}
	if cfg == nil {
		fmt.Fprintln(w, "audit-state-secrets: no secrets config; nothing to audit")
		return 0
	}
	prov, err := resolveSecretsProvider(cfg)
	if err != nil {
		fmt.Fprintf(w, "audit-state-secrets: resolve provider: %v\n", err)
		return 2
	}

	store, err := resolveStateStore(configFile, "")
	if err != nil {
		fmt.Fprintf(w, "audit-state-secrets: open state store: %v\n", err)
		return 2
	}
	// noop store contributes no state; allow audit to proceed (provider
	// may still surface orphan findings).

	return runAuditStateSecretsWithPrune(context.Background(), w, store, prov, prune)
}

// runAuditStateSecrets is the testable entry point (no flag parsing).
func runAuditStateSecrets(ctx context.Context, w io.Writer, store infraStateStore, prov secrets.Provider) int {
	return runAuditStateSecretsWithPrune(ctx, w, store, prov, false)
}

// runAuditStateSecretsWithPrune is the testable entry point with --prune
// behaviour parameterised. Returns exit-code per the contract above.
func runAuditStateSecretsWithPrune(ctx context.Context, w io.Writer, store infraStateStore, prov secrets.Provider, prune bool) int {
	states, err := store.ListResources(ctx)
	if err != nil {
		fmt.Fprintf(w, "audit-state-secrets: list state: %v\n", err)
		return 2
	}

	findings := 0
	stateNames := map[string]struct{}{}
	for i := range states {
		stateNames[states[i].Name] = struct{}{}
	}

	defaultSensitive := map[string]struct{}{}
	for _, k := range secrets.DefaultSensitiveKeys() {
		defaultSensitive[k] = struct{}{}
	}

	// Walk state for placeholder/plaintext/config-ref findings.
	// Sort states by name for stable output.
	sort.SliceStable(states, func(i, j int) bool { return states[i].Name < states[j].Name })

	for i := range states {
		st := &states[i]
		keys := make([]string, 0, len(st.Outputs))
		for k := range st.Outputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			v := st.Outputs[k]
			s, isStr := v.(string)
			if !isStr {
				continue
			}
			switch {
			case sensitive.IsPlaceholder(v):
				secretName := strings.TrimPrefix(s, sensitive.PlaceholderPrefix)
				_, getErr := prov.Get(ctx, secretName)
				if getErr == nil {
					continue
				}
				if errors.Is(getErr, secrets.ErrUnsupported) {
					fmt.Fprintf(w, "ADVISORY (Get unsupported): cannot verify routed value for %s/%s -> %q on this provider\n", st.Name, k, secretName)
					continue
				}
				if errors.Is(getErr, secrets.ErrNotFound) {
					fmt.Fprintf(w, "FINDING (missing routed value): %s/%s expects routed secret %q but provider does not have it\n", st.Name, k, secretName)
					findings++
				}
			case strings.HasPrefix(s, secrets.SecretPrefix):
				fmt.Fprintf(w, "FINDING (config-reference in state): %s/%s contains user-config-style %q (expected resolved value or %s placeholder)\n", st.Name, k, s, sensitive.PlaceholderPrefix)
				findings++
			default:
				if _, isSensName := defaultSensitive[k]; isSensName && s != "" {
					fmt.Fprintf(w, "FINDING (legacy plaintext): %s/%s = <plaintext>; rotate via wfctl infra bootstrap --force-rotate or re-apply\n", st.Name, k)
					findings++
				}
			}
		}
	}

	// Walk provider for orphan secrets.
	names, err := prov.List(ctx)
	switch {
	case err == nil:
		sort.Strings(names)
		for _, name := range names {
			res := stripKnownSensitiveSuffix(name)
			if _, ok := stateNames[res]; ok {
				continue
			}
			if prune {
				if delErr := prov.Delete(ctx, name); delErr != nil {
					fmt.Fprintf(w, "PRUNE FAILED: %q: %v\n", name, delErr)
					findings++
				} else {
					fmt.Fprintf(w, "pruned orphan secret %q\n", name)
				}
				continue
			}
			fmt.Fprintf(w, "FINDING (orphan secret): %q has no matching state resource; rerun with --prune to delete\n", name)
			findings++
		}
	case errors.Is(err, secrets.ErrUnsupported):
		fmt.Fprintln(w, "ADVISORY (list unsupported): provider does not support List(); orphan-secret detection skipped on this host")
	default:
		fmt.Fprintf(w, "audit-state-secrets: list provider secrets: %v\n", err)
		return 2
	}

	if findings > 0 {
		fmt.Fprintf(w, "\naudit-state-secrets: %d finding(s)\n", findings)
		return 1
	}
	fmt.Fprintln(w, "audit-state-secrets: no findings")
	return 0
}

// stripKnownSensitiveSuffix returns the resource-name prefix of a
// routed-secret name. Tries DefaultSensitiveKeys suffixes; falls back
// to the original name (which then fails the state-name lookup and is
// flagged as an orphan).
func stripKnownSensitiveSuffix(name string) string {
	for _, k := range secrets.DefaultSensitiveKeys() {
		suf := "_" + k
		if strings.HasSuffix(name, suf) {
			return name[:len(name)-len(suf)]
		}
	}
	return name
}
