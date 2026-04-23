package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/secrets"
)

func runInfraBootstrap(args []string) error {
	fs := flag.NewFlagSet("infra bootstrap", flag.ContinueOnError)
	var configFile string
	fs.StringVar(&configFile, "config", "", "Config file")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	var envName string
	fs.StringVar(&envName, "env", "", "Environment name (resolves per-module environments: overrides)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile, err := resolveInfraConfig(fs, configFile)
	if err != nil {
		return err
	}

	// originalCfgFile is used for parseSecretsConfig so that the secrets.generate[]
	// block is always read from the original YAML. writeEnvResolvedConfig marshals
	// via config.WorkflowConfig which has no Generate field, so the generate[] section
	// would be silently dropped from the env-resolved temp file.
	originalCfgFile := cfgFile

	// If --env is set, resolve the config for that environment before bootstrapping.
	// cfgFile is reassigned to the temp path; originalCfgFile retains the original.
	if envName != "" {
		tmp, resErr := writeEnvResolvedConfig(cfgFile, envName)
		if resErr != nil {
			return resErr
		}
		defer os.Remove(tmp)
		cfgFile = tmp
	}

	ctx := context.Background()

	// 1. Generate and store secrets FIRST so that state-backend access keys are
	//    available in the current process environment before bootstrapStateBackend
	//    runs. (Remote bucket creation requires API auth with the provider key pair,
	//    which is generated here via the provider_credential source.)
	// Use originalCfgFile — not cfgFile — so the generate[] block is not lost
	// in the env-resolved temp file's round-trip through config.WorkflowConfig.
	secretsCfg, err := parseSecretsConfig(originalCfgFile)
	if err != nil {
		return fmt.Errorf("parse secrets config: %w", err)
	}

	if secretsCfg != nil && len(secretsCfg.Generate) > 0 {
		provider, err := resolveSecretsProvider(secretsCfg)
		if err != nil {
			return fmt.Errorf("resolve secrets provider: %w", err)
		}
		generated, err := bootstrapSecrets(ctx, provider, secretsCfg)
		if err != nil {
			return err
		}
		// Export freshly generated secrets to the current process environment so
		// that bootstrapStateBackend can read them via ${VAR} expansion in the
		// module config (e.g. accessKey: "${PROVIDER_access_key}").
		for k, v := range generated {
			os.Setenv(k, v) //nolint:errcheck
		}
	} else {
		fmt.Println("No secrets to generate.")
	}

	// 2. Bootstrap state backend (uses provider keys now in env from step 1).
	if err := bootstrapStateBackend(ctx, cfgFile); err != nil {
		return fmt.Errorf("bootstrap state backend: %w", err)
	}

	return nil
}

// bootstrapStateBackend checks the iac.state config and creates any required
// backing infrastructure for the configured backend. It dispatches by the
// backend type declared in the iac.state module:
//
//   - filesystem / memory / postgres / "" → no-op (no remote bucket required)
//   - any other backend → dispatched through the IaCProvider plugin via
//     provider.BootstrapStateBackend(ctx, iacStateConfig)
//
// ${VAR} / $VAR references in the module config are expanded before the config
// is passed to the provider, so secrets can be injected via the environment.
//
// On success, each entry in result.EnvVars is printed as `export KEY=VALUE`
// for CI capture, and result.Bucket is written back to the on-disk config so
// downstream commands can load state without the env var being set again.
func bootstrapStateBackend(ctx context.Context, cfgFile string) error {
	iacStates, _, _, err := discoverInfraModules(cfgFile)
	if err != nil {
		return fmt.Errorf("discover infra modules: %w", err)
	}
	if len(iacStates) == 0 {
		return nil
	}
	m := iacStates[0]
	// Expand ${VAR} / $VAR references before reading individual fields.
	cfg := config.ExpandEnvInMap(m.Config)
	backend, _ := cfg["backend"].(string)

	// Self-contained backends don't require a remote bucket to be created.
	switch backend {
	case "filesystem", "memory", "postgres", "":
		return nil
	}

	// For remote backends, dispatch through the IaCProvider plugin interface.
	// Find the first iac.provider module declared in the config.
	rawCfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	var provType string
	var provCfg map[string]any
	for i := range rawCfg.Modules {
		mod := &rawCfg.Modules[i]
		if mod.Type != "iac.provider" {
			continue
		}
		modCfg := config.ExpandEnvInMap(mod.Config)
		if pt, ok := modCfg["provider"].(string); ok && pt != "" {
			provType = pt
			provCfg = modCfg
			break
		}
	}
	if provType == "" {
		return fmt.Errorf("no iac.provider module found in config — add an iac.provider module to bootstrap remote state backends (backend=%q)", backend)
	}

	provider, closer, err := resolveIaCProvider(ctx, provType, provCfg)
	if err != nil {
		return fmt.Errorf("load provider %q for state backend bootstrap: %w", provType, err)
	}
	if closer != nil {
		defer closer.Close() //nolint:errcheck
	}

	result, err := provider.BootstrapStateBackend(ctx, cfg)
	if err != nil {
		return fmt.Errorf("bootstrap state backend (backend=%q, provider=%q): %w", backend, provType, err)
	}
	if result == nil {
		return nil
	}

	// Ensure WFCTL_STATE_BUCKET is always present when a bucket was returned.
	if result.Bucket != "" {
		if result.EnvVars == nil {
			result.EnvVars = make(map[string]string)
		}
		if result.EnvVars["WFCTL_STATE_BUCKET"] == "" {
			result.EnvVars["WFCTL_STATE_BUCKET"] = result.Bucket
		}
	}

	// Print export lines in stable order for CI capture (e.g. $GITHUB_ENV).
	keys := make([]string, 0, len(result.EnvVars))
	for k := range result.EnvVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("export %s=%s\n", k, result.EnvVars[k])
	}

	// Write the resolved bucket name back to the on-disk config so downstream
	// wfctl commands can load state without the env var being set again.
	if result.Bucket != "" {
		if writeErr := writeBucketBackToConfig(cfgFile, result.Bucket); writeErr != nil {
			// Non-fatal: bucket exists; warn and continue.
			fmt.Printf("WARNING: could not write bucket back to config: %v\n", writeErr)
		}
	}
	return nil
}

// writeBucketBackToConfig rewrites the iac.state module's `bucket:` field in
// cfgFile with the resolved bucket name. It is backend-neutral — the same
// field name is used by all remote backends (spaces, s3, gcs, azure). It uses
// a line-level text replacement to preserve YAML formatting, comments, and
// indentation.
func writeBucketBackToConfig(cfgFile, bucket string) error {
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	inIACState := false
	bucketReplaced := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track when we enter an iac.state module block.
		if strings.Contains(trimmed, "type: iac.state") {
			inIACState = true
		}
		// Leave the block when we encounter the next top-level module entry.
		if inIACState && strings.HasPrefix(trimmed, "- name:") {
			inIACState = false
		}

		if inIACState && strings.HasPrefix(trimmed, "bucket:") {
			// Preserve leading whitespace from the original line.
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "bucket: " + bucket
			bucketReplaced = true
		}
	}

	if !bucketReplaced {
		// Nothing to replace — bucket field not present or already correct.
		return nil
	}

	return os.WriteFile(cfgFile, []byte(strings.Join(lines, "\n")), 0o600)
}

// providerCredentialSubKeys lists the sub-key names produced by each
// provider_credential source. Existence checks must verify ALL of them so a
// partial prior write (e.g. one sub-key manually deleted) triggers a full
// regeneration rather than an incorrect skip.
//
// TODO(follow-up #29): this map is hard-coded for DO Spaces. It should be
// registered by provider plugins via a hook (e.g. CredentialSubKeys() method
// on EnginePlugin) so that third-party providers can declare their own sub-keys
// without modifying wfctl core.
var providerCredentialSubKeys = map[string][]string{
	"digitalocean.spaces": {"access_key", "secret_key"},
}

// generateSecret is the package-level hook used by bootstrapSecrets. Tests
// override it to exercise provider_credential code paths without reaching
// out to cloud APIs.
var generateSecret = secrets.GenerateSecret

// expectedStoredKeys returns every key name that a completed generation of
// gen would have stored in the provider. For provider_credential with a
// known source, that is "<key>_<subkey>" for each subkey; for simple
// generators it is just gen.Key.
func expectedStoredKeys(gen SecretGen) []string {
	if gen.Type == "provider_credential" {
		if subs, ok := providerCredentialSubKeys[gen.Source]; ok {
			out := make([]string, len(subs))
			for i, s := range subs {
				out[i] = gen.Key + "_" + s
			}
			return out
		}
		// Unknown source — best-effort single probe.
		return []string{gen.Key + "_access_key"}
	}
	return []string{gen.Key}
}

// bootstrapSecrets generates and stores secrets that don't already exist.
// It returns a map of every key-value pair that was newly generated in this
// run (skipped/pre-existing keys are NOT included). The caller can export
// these to the process environment for downstream steps.
func bootstrapSecrets(ctx context.Context, provider secrets.Provider, cfg *SecretsConfig) (map[string]string, error) {
	generated := map[string]string{}

	// Cache List() as a set so repeated probes in a bootstrap run only hit
	// the provider once and subsequent lookups are O(1). Resolved lazily on
	// the first write-only Get.
	var listSet map[string]struct{}
	var listErr error
	var listDone bool
	lookupViaList := func(key string) (bool, error) {
		if !listDone {
			names, err := provider.List(ctx)
			listErr = err
			if err == nil {
				listSet = make(map[string]struct{}, len(names))
				for _, n := range names {
					listSet[n] = struct{}{}
				}
			}
			listDone = true
		}
		if listErr != nil && !errors.Is(listErr, secrets.ErrUnsupported) {
			return false, fmt.Errorf("list secrets to check %q: %w", key, listErr)
		}
		_, ok := listSet[key]
		return ok, nil
	}
	secretExists := func(key string) (bool, error) {
		_, err := provider.Get(ctx, key)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, secrets.ErrNotFound):
			return false, nil
		case errors.Is(err, secrets.ErrUnsupported):
			return lookupViaList(key)
		default:
			return false, fmt.Errorf("check secret %q: %w", key, err)
		}
	}

	for _, gen := range cfg.Generate {
		// infra_output secrets depend on apply-time state; skip during bootstrap.
		if gen.Type == "infra_output" {
			continue
		}

		// Build generator config from SecretGen fields.
		genConfig := map[string]any{}
		if gen.Length > 0 {
			genConfig["length"] = gen.Length
		}
		if gen.Source != "" {
			genConfig["source"] = gen.Source
		}

		// Check that EVERY expected stored key is already present before
		// skipping. provider_credential writes multiple sub-keys; if a prior
		// write was partial or one sub-key was manually removed, we must
		// regenerate to produce a usable credential. Without this loop,
		// every run regenerates for write-only providers (GH Actions), and
		// provider_credential regeneration orphans upstream credentials.
		expected := expectedStoredKeys(gen)
		allExist := true
		for _, key := range expected {
			present, err := secretExists(key)
			if err != nil {
				return generated, err
			}
			if !present {
				allExist = false
				break
			}
		}
		if allExist {
			fmt.Printf("  secret %q: already exists — skipped\n", gen.Key)
			continue
		}

		// Generate the secret value.
		value, err := generateSecret(ctx, gen.Type, genConfig)
		if err != nil {
			return generated, fmt.Errorf("generate secret %q: %w", gen.Key, err)
		}

		// For provider_credential results (JSON map), store each sub-key.
		if gen.Type == "provider_credential" {
			var subKeys map[string]string
			if jsonErr := json.Unmarshal([]byte(value), &subKeys); jsonErr == nil {
				for subKey, subVal := range subKeys {
					fullKey := gen.Key + "_" + subKey
					if setErr := provider.Set(ctx, fullKey, subVal); setErr != nil {
						return generated, fmt.Errorf("store secret %q: %w", fullKey, setErr)
					}
					generated[fullKey] = subVal
					fmt.Printf("  secret %q: created\n", fullKey)
				}
				continue
			}
		}

		if err := provider.Set(ctx, gen.Key, value); err != nil {
			return generated, fmt.Errorf("store secret %q: %w", gen.Key, err)
		}
		generated[gen.Key] = value
		fmt.Printf("  secret %q: created\n", gen.Key)
	}
	return generated, nil
}
