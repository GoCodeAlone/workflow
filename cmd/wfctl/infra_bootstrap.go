package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

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

	// 1. Generate and store secrets FIRST so that Spaces access keys are available
	//    in the current process environment before bootstrapStateBackend runs.
	//    (DO Spaces bucket creation requires S3 API auth with the Spaces key pair,
	//    which is generated here via the provider_credential source.)
	secretsCfg, err := parseSecretsConfig(cfgFile)
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
		// module config (e.g. accessKey: "${SPACES_access_key}").
		for k, v := range generated {
			os.Setenv(k, v) //nolint:errcheck
		}
	} else {
		fmt.Println("No secrets to generate.")
	}

	// 2. Bootstrap state backend (uses Spaces keys now in env from step 1).
	if err := bootstrapStateBackend(ctx, cfgFile); err != nil {
		return fmt.Errorf("bootstrap state backend: %w", err)
	}

	return nil
}

// bootstrapDOSpacesBucketFn is the package-level hook used by bootstrapStateBackend.
// Tests override it to inject fakes without touching the filesystem or S3.
var bootstrapDOSpacesBucketFn = bootstrapDOSpacesBucket

// bootstrapStateBackend checks the iac.state config and creates any required
// backing infrastructure (e.g. a DO Spaces bucket) if it does not already exist.
// ${VAR} / $VAR references in the module config are expanded via os.ExpandEnv
// before the config fields are read, so secrets can be injected through the
// environment (e.g. SPACES_access_key=xxx in CI).
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
	if backend != "spaces" {
		// Only DO Spaces requires bootstrap; filesystem/memory are self-contained.
		return nil
	}

	bucket, _ := cfg["bucket"].(string)
	region, _ := cfg["region"].(string)
	if bucket == "" {
		return fmt.Errorf("iac.state backend=spaces requires 'bucket' in config")
	}

	// Read Spaces credentials from the expanded module config.
	// Convention: the YAML uses accessKey/secretKey (camelCase) or access_key/secret_key (snake_case).
	accessKey, _ := cfg["accessKey"].(string)
	if accessKey == "" {
		accessKey, _ = cfg["access_key"].(string)
	}
	secretKey, _ := cfg["secretKey"].(string)
	if secretKey == "" {
		secretKey, _ = cfg["secret_key"].(string)
	}

	return bootstrapDOSpacesBucketFn(ctx, bucket, region, accessKey, secretKey)
}

// spacesBucketClient is the minimal S3 client interface used by bootstrapDOSpacesBucketWithClient.
// Keeping it narrow makes it easy to inject fakes in tests.
type spacesBucketClient interface {
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
}

// bootstrapDOSpacesBucket creates a DO Spaces bucket if it does not already exist.
// It uses the S3-compatible Spaces API authenticated with Spaces access keys —
// NOT the DO Bearer token, which is only valid for the DO REST API, not for Spaces.
func bootstrapDOSpacesBucket(ctx context.Context, bucket, region, accessKey, secretKey string) error {
	if region == "" {
		region = "nyc3"
	}
	if accessKey == "" || secretKey == "" {
		return fmt.Errorf("spaces access key and secret key must be set — " +
			"ensure secrets are bootstrapped (step 1) before state backend (step 2); " +
			"set accessKey/secretKey in the iac.state module config referencing ${SPACES_access_key} and ${SPACES_secret_key}")
	}
	endpoint := fmt.Sprintf("https://%s.digitaloceanspaces.com", region)

	cfg, cfgErr := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if cfgErr != nil {
		return fmt.Errorf("spaces bootstrap: build S3 config: %w", cfgErr)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = &endpoint
		o.UsePathStyle = true
	})
	return bootstrapDOSpacesBucketWithClient(ctx, bucket, region, client)
}

// bootstrapDOSpacesBucketWithClient is the testable core of bootstrapDOSpacesBucket.
// It uses HeadBucket to check existence and CreateBucket to create if absent.
func bootstrapDOSpacesBucketWithClient(ctx context.Context, bucket, region string, client spacesBucketClient) error {
	// Check whether the bucket already exists.
	_, headErr := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &bucket})
	if headErr == nil {
		fmt.Printf("  state backend: bucket %q already exists — skipped\n", bucket)
		return nil
	}

	// HeadBucket returns types.NotFound (HTTP 404) when the bucket does not exist.
	// Any other error (e.g. 403 Forbidden — bucket owned by another account) is fatal.
	var notFound *s3types.NotFound
	if !errors.As(headErr, &notFound) {
		return fmt.Errorf("check bucket %q: %w", bucket, headErr)
	}

	// Create the bucket.
	_, createErr := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: &bucket,
		CreateBucketConfiguration: &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(region),
		},
	})
	if createErr != nil {
		// BucketAlreadyOwnedByYou: a concurrent create won the race — still OK.
		var alreadyOwned *s3types.BucketAlreadyOwnedByYou
		if errors.As(createErr, &alreadyOwned) {
			fmt.Printf("  state backend: bucket %q already owned — skipped\n", bucket)
			return nil
		}
		return fmt.Errorf("create bucket %q: %w", bucket, createErr)
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
