package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"slices"

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
	token := os.Getenv("DIGITALOCEAN_TOKEN")
	if token == "" {
		return fmt.Errorf("DIGITALOCEAN_TOKEN not set")
	}
	if region == "" {
		region = "nyc3"
	}

	// Check if bucket exists.
	checkURL := fmt.Sprintf("https://api.digitalocean.com/v2/spaces/%s", bucket)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
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

	// Create bucket.
	payload := map[string]string{"name": bucket, "region": region}
	body, _ := json.Marshal(payload)
	createReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.digitalocean.com/v2/spaces", bytes.NewReader(body))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		return fmt.Errorf("create bucket %q: %w", bucket, err)
	}
	createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated && createResp.StatusCode != http.StatusOK {
		return fmt.Errorf("create bucket %q: HTTP %d", bucket, createResp.StatusCode)
	}
	fmt.Printf("  state backend: created DO Spaces bucket %q in %s\n", bucket, region)
	return nil
}

// bootstrapSecrets generates and stores secrets that don't already exist.
func bootstrapSecrets(ctx context.Context, provider secrets.Provider, cfg *SecretsConfig) error {
	// Cache List() results so a repeated bootstrap run with many secrets only
	// hits the provider once. Resolved lazily on first write-only Get.
	var listCache []string
	var listErr error
	var listDone bool
	for _, gen := range cfg.Generate {
		// Build generator config from SecretGen fields.
		genConfig := map[string]any{}
		if gen.Length > 0 {
			genConfig["length"] = gen.Length
		}
		if gen.Source != "" {
			genConfig["source"] = gen.Source
		}

		// Determine the probe key to check for existence. provider_credential
		// generators (e.g. digitalocean.spaces) expand to multiple sub-keys
		// (<key>_access_key, <key>_secret_key), so probe the access_key suffix.
		probeKey := gen.Key
		if gen.Type == "provider_credential" {
			probeKey = gen.Key + "_access_key"
		}

		// Check if already set. GitHub Actions secrets are write-only, so Get
		// returns ErrUnsupported — fall back to List() and scan for the name.
		// Without this fallback, every run regenerates the secret, and for
		// provider_credential that creates orphaned upstream credentials
		// (e.g. duplicate DO Spaces access keys) on every run.
		exists := false
		_, err := provider.Get(ctx, probeKey)
		switch {
		case err == nil:
			exists = true
		case errors.Is(err, secrets.ErrNotFound):
			// Confirmed absent.
		case errors.Is(err, secrets.ErrUnsupported):
			if !listDone {
				listCache, listErr = provider.List(ctx)
				listDone = true
			}
			if listErr != nil && !errors.Is(listErr, secrets.ErrUnsupported) {
				return fmt.Errorf("list secrets to check %q: %w", probeKey, listErr)
			}
			exists = slices.Contains(listCache, probeKey)
		default:
			return fmt.Errorf("check secret %q: %w", probeKey, err)
		}
		if exists {
			fmt.Printf("  secret %q: already exists — skipped\n", gen.Key)
			continue
		}

		// Generate the secret value.
		value, err := secrets.GenerateSecret(ctx, gen.Type, genConfig)
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
