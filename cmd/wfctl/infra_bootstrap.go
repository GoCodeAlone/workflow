package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

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

	// If --env is set, resolve the config for that environment before bootstrapping.
	if envName != "" {
		tmp, resErr := writeEnvResolvedConfig(cfgFile, envName)
		if resErr != nil {
			return resErr
		}
		defer os.Remove(tmp)
		cfgFile = tmp
	}

	ctx := context.Background()

	// 1. Bootstrap state backend.
	if err := bootstrapStateBackend(ctx, cfgFile); err != nil {
		return fmt.Errorf("bootstrap state backend: %w", err)
	}

	// 2. Generate and store secrets.
	secretsCfg, err := parseSecretsConfig(cfgFile)
	if err != nil {
		return fmt.Errorf("parse secrets config: %w", err)
	}
	if secretsCfg == nil || len(secretsCfg.Generate) == 0 {
		fmt.Println("No secrets to generate.")
		return nil
	}

	provider, err := resolveSecretsProvider(secretsCfg)
	if err != nil {
		return fmt.Errorf("resolve secrets provider: %w", err)
	}

	return bootstrapSecrets(ctx, provider, secretsCfg)
}

// bootstrapStateBackend checks the iac.state config and creates any required
// backing infrastructure (e.g. a DO Spaces bucket) if it does not already exist.
func bootstrapStateBackend(ctx context.Context, cfgFile string) error {
	iacStates, _, _, err := discoverInfraModules(cfgFile)
	if err != nil {
		return fmt.Errorf("discover infra modules: %w", err)
	}
	if len(iacStates) == 0 {
		return nil
	}
	m := iacStates[0]
	backend, _ := m.Config["backend"].(string)
	if backend != "spaces" {
		// Only DO Spaces requires bootstrap; filesystem/memory are self-contained.
		return nil
	}

	bucket, _ := m.Config["bucket"].(string)
	region, _ := m.Config["region"].(string)
	if bucket == "" {
		return fmt.Errorf("iac.state backend=spaces requires 'bucket' in config")
	}
	return bootstrapDOSpacesBucket(ctx, bucket, region)
}

// bootstrapDOSpacesBucket creates a DO Spaces bucket if it does not already exist.
func bootstrapDOSpacesBucket(ctx context.Context, bucket, region string) error {
	return bootstrapDOSpacesBucketAt(ctx, bucket, region, "https://api.digitalocean.com")
}

// bootstrapDOSpacesBucketAt is the testable core of bootstrapDOSpacesBucket.
// apiBase is the DO API base URL (injectable for tests).
func bootstrapDOSpacesBucketAt(ctx context.Context, bucket, region, apiBase string) error {
	token := os.Getenv("DIGITALOCEAN_TOKEN")
	if token == "" {
		return fmt.Errorf("DIGITALOCEAN_TOKEN not set")
	}
	if region == "" {
		region = "nyc3"
	}

	// Check if bucket exists using the Spaces Buckets REST API.
	checkURL := fmt.Sprintf("%s/v2/spaces/buckets/%s", apiBase, bucket)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return fmt.Errorf("check bucket %q: %w", bucket, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("check bucket %q: %w", bucket, err)
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Printf("  state backend: bucket %q already exists — skipped\n", bucket)
		return nil
	}

	// Create bucket via POST /v2/spaces/buckets.
	payload := map[string]string{"name": bucket, "region": region}
	body, _ := json.Marshal(payload)
	createReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/v2/spaces/buckets", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create bucket %q: %w", bucket, err)
	}
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		return fmt.Errorf("create bucket %q: %w", bucket, err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated && createResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(createResp.Body)
		return fmt.Errorf("create bucket %q: HTTP %d: %s", bucket, createResp.StatusCode, respBody)
	}
	fmt.Printf("  state backend: created DO Spaces bucket %q in %s\n", bucket, region)
	return nil
}

// providerCredentialSubKeys lists the sub-key names produced by each
// provider_credential source. Existence checks must verify ALL of them so a
// partial prior write (e.g. one sub-key manually deleted) triggers a full
// regeneration rather than an incorrect skip.
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
func bootstrapSecrets(ctx context.Context, provider secrets.Provider, cfg *SecretsConfig) error {
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
				return err
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
			return fmt.Errorf("generate secret %q: %w", gen.Key, err)
		}

		// For provider_credential results (JSON map), store each sub-key.
		if gen.Type == "provider_credential" {
			var subKeys map[string]string
			if jsonErr := json.Unmarshal([]byte(value), &subKeys); jsonErr == nil {
				for subKey, subVal := range subKeys {
					fullKey := gen.Key + "_" + subKey
					if setErr := provider.Set(ctx, fullKey, subVal); setErr != nil {
						return fmt.Errorf("store secret %q: %w", fullKey, setErr)
					}
					fmt.Printf("  secret %q: created\n", fullKey)
				}
				continue
			}
		}

		if err := provider.Set(ctx, gen.Key, value); err != nil {
			return fmt.Errorf("store secret %q: %w", gen.Key, err)
		}
		fmt.Printf("  secret %q: created\n", gen.Key)
	}
	return nil
}
