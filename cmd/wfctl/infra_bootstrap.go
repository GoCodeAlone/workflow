package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

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
		// module config (e.g. accessKey: "${SPACES_access_key}").
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

// bootstrapDOSpacesBucketFn is the package-level hook used by bootstrapStateBackend.
// Tests override it to inject fakes without touching the filesystem or S3.
var bootstrapDOSpacesBucketFn = bootstrapDOSpacesBucket

// bootstrapStateBackend checks the iac.state config and creates any required
// backing infrastructure for the configured backend. It dispatches by the
// backend type declared in the iac.state module:
//
//   - spaces     → creates/verifies a DO Spaces bucket via the S3-compatible API
//   - s3         → not yet implemented (returns a descriptive error)
//   - gcs        → not yet implemented (returns a descriptive error)
//   - azure      → not yet implemented (returns a descriptive error)
//   - filesystem → no-op (directory is created on first write)
//   - memory     → no-op (in-process, no external resource required)
//   - postgres   → no-op (connection string validated at connect time)
//
// ${VAR} / $VAR references in the module config are expanded before fields are
// read, so secrets can be injected via the environment.
//
// On a successful bucket bootstrap the resolved name is written back to cfgFile
// (so env-var-referenced values are permanently baked in) and two export lines
// are printed:
//
//	export WFCTL_STATE_BUCKET=<name>    (generic — stable across backends)
//	export <BACKEND>_BUCKET=<name>      (backend-specific, e.g. SPACES_BUCKET)
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

	switch backend {
	case "spaces":
		return bootstrapStateBackendSpaces(ctx, cfgFile, cfg)

	case "s3":
		return fmt.Errorf("s3 state bucket bootstrap not yet implemented; " +
			"create the bucket manually and reference it in iac.state.bucket. " +
			"Contribute a bootstrapStateBackendS3 helper to unblock this")

	case "gcs":
		return fmt.Errorf("gcs state bucket bootstrap not yet implemented; " +
			"create the bucket manually and reference it in iac.state.bucket. " +
			"Contribute a bootstrapStateBackendGCS helper to unblock this")

	case "azure":
		return fmt.Errorf("azure state bucket bootstrap not yet implemented; " +
			"create the container manually and reference it in iac.state.bucket. " +
			"Contribute a bootstrapStateBackendAzure helper to unblock this")

	case "filesystem", "memory", "postgres", "":
		// Self-contained backends — no remote bucket creation required.
		return nil

	default:
		return fmt.Errorf("unknown iac.state backend %q — no bootstrap action taken", backend)
	}
}

// bootstrapStateBackendSpaces handles the DO Spaces bootstrap path. It
// constructs the DO Spaces endpoint URL, creates the bucket if it does not
// exist, exports the resolved bucket name, and writes it back to the config.
func bootstrapStateBackendSpaces(ctx context.Context, cfgFile string, cfg map[string]any) error {
	bucket, _ := cfg["bucket"].(string)
	region, _ := cfg["region"].(string)
	if bucket == "" {
		return fmt.Errorf("iac.state backend=spaces requires 'bucket' in config")
	}
	if region == "" {
		region = "nyc3"
	}

	// Build the DO Spaces S3-compatible endpoint URL here (Spaces-specific knowledge
	// belongs in the Spaces helper, not in lower-level S3 client helpers).
	endpoint := fmt.Sprintf("https://%s.digitaloceanspaces.com", region)

	// Read credentials from the expanded module config.
	// Convention: the YAML uses accessKey/secretKey (camelCase) or access_key/secret_key (snake_case).
	accessKey, _ := cfg["accessKey"].(string)
	if accessKey == "" {
		accessKey, _ = cfg["access_key"].(string)
	}
	secretKey, _ := cfg["secretKey"].(string)
	if secretKey == "" {
		secretKey, _ = cfg["secret_key"].(string)
	}

	if err := bootstrapDOSpacesBucketFn(ctx, bucket, region, endpoint, accessKey, secretKey); err != nil {
		return err
	}

	// Export the resolved bucket name for CI capture:
	//   - WFCTL_STATE_BUCKET is the generic, backend-agnostic variable.
	//   - SPACES_BUCKET is the backend-specific variable (no cloud-provider prefix).
	fmt.Printf("export WFCTL_STATE_BUCKET=%s\n", bucket)
	fmt.Printf("export SPACES_BUCKET=%s\n", bucket)

	// Write the resolved name back to the on-disk config so downstream wfctl
	// commands can load state without the env var being set again.
	if writeErr := writeBucketBackToConfig(cfgFile, bucket); writeErr != nil {
		// Non-fatal: bucket exists; warn and continue.
		fmt.Printf("WARNING: could not write bucket back to config: %v\n", writeErr)
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

// spacesBucketClient is the minimal S3 client interface used by bootstrapDOSpacesBucketWithClient.
// Keeping it narrow makes it easy to inject fakes in tests.
type spacesBucketClient interface {
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
}

// bootstrapDOSpacesBucket creates a DO Spaces bucket if it does not already exist.
// It uses the S3-compatible API authenticated with Spaces access keys — NOT the
// DO Bearer token, which is only valid for the DO REST API, not for Spaces.
// The endpoint URL (e.g. https://nyc3.digitaloceanspaces.com) is constructed by
// the caller (bootstrapStateBackendSpaces) and passed in, keeping DO-specific URL
// knowledge in the Spaces helper rather than here.
func bootstrapDOSpacesBucket(ctx context.Context, bucket, region, endpoint, accessKey, secretKey string) error {
	if accessKey == "" || secretKey == "" {
		return fmt.Errorf("spaces access key and secret key must be set — " +
			"ensure secrets are bootstrapped (step 1) before state backend (step 2); " +
			"set accessKey/secretKey in the iac.state module config (backend=spaces)")
	}

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
	fmt.Printf("  state backend: created spaces state bucket %q in %s\n", bucket, region)
	return nil
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
