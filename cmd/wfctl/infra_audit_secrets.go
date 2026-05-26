package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// runInfraAuditSecrets reads infra.yaml's secrets.generate block and reports
// known anti-patterns. Exit non-zero on any finding so CI blocks misconfiguration
// before plan/apply.
//
// Detected anti-patterns:
//  1. Two-entry provider_credential: keys ending in `_access_key` or `_secret_key`
//     with type=provider_credential AND a known source. The canonical shape uses
//     a single entry whose key is the credential bundle name (e.g. SPACES); the
//     bootstrap layer auto-derives the sub-keys (SPACES_access_key /
//     SPACES_secret_key) from providerCredentialSubKeys.
//  2. Same `name` across multiple provider_credential entries (the doubled-create
//     symptom — each entry creates a separate cloud resource).
//  3. provider_credential with source whose subkeys are not in
//     providerCredentialSubKeys (lurking misconfiguration; the bootstrap layer
//     wouldn't know how to derive sub-keys).
func runInfraAuditSecrets(args []string, w io.Writer) int {
	fs := flag.NewFlagSet("infra audit-secrets", flag.ContinueOnError)
	fs.SetOutput(w)
	var configFile string
	fs.StringVar(&configFile, "c", "infra.yaml", "Config file")
	fs.StringVar(&configFile, "config", "infra.yaml", "Config file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := parseSecretsConfig(configFile)
	if err != nil {
		fmt.Fprintf(w, "audit-secrets: parse %q: %v\n", configFile, err)
		return 2
	}
	if cfg == nil {
		fmt.Fprintln(w, "audit-secrets: no findings")
		return 0
	}

	findings := 0
	seenNames := map[string]string{} // name -> first key seen with that name

	for _, gen := range cfg.Generate {
		if gen.Type != "provider_credential" {
			continue
		}
		// Anti-pattern 1: key ending in _access_key/_secret_key
		if hasSubKeySuffix(gen.Key) {
			fmt.Fprintf(w, "FINDING (two-entry provider_credential): key %q must be canonical (e.g. %q), not the auto-derived sub-key\n",
				gen.Key, stripSubKeySuffix(gen.Key))
			findings++
		}
		// Anti-pattern 2: same name across multiple entries
		if gen.Name != "" {
			if prior, ok := seenNames[gen.Name]; ok {
				fmt.Fprintf(w, "FINDING (duplicate provider_credential name): name %q used by both %q and %q (each entry creates a separate cloud resource)\n",
					gen.Name, prior, gen.Key)
				findings++
			}
			seenNames[gen.Name] = gen.Key
		}
		// Anti-pattern 3: unknown source
		if _, ok := providerCredentialSubKeys[gen.Source]; !ok {
			fmt.Fprintf(w, "FINDING (unknown provider_credential source): %q has no subkey mapping; check workflow version supports source\n", gen.Source)
			findings++
		}
	}

	if findings > 0 {
		fmt.Fprintf(w, "\naudit-secrets: %d finding(s) — see https://docs.gocodealone.com/workflow/wfctl/infra-audit-secrets\n", findings)
		return 1
	}
	fmt.Fprintln(w, "audit-secrets: no findings")
	return 0
}

func hasSubKeySuffix(key string) bool {
	return strings.HasSuffix(key, "_access_key") || strings.HasSuffix(key, "_secret_key")
}

func stripSubKeySuffix(key string) string {
	if strings.HasSuffix(key, "_access_key") {
		return key[:len(key)-len("_access_key")]
	}
	if strings.HasSuffix(key, "_secret_key") {
		return key[:len(key)-len("_secret_key")]
	}
	return key
}
