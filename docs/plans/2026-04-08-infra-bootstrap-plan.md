---
status: implemented
area: wfctl
owner: workflow
implementation_refs:
  - repo: workflow
    commit: 4ec75b7
  - repo: workflow
    commit: ad72f2f
  - repo: workflow
    commit: 89ce08a
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - 'rg -n "runInfraBootstrap|bootstrapStateBackend|bootstrapSecrets|auto_bootstrap|infra_output|infra outputs" cmd/wfctl config docs -S'
    - 'git log --oneline --all -- cmd/wfctl/infra_bootstrap.go cmd/wfctl/infra_output_secrets.go cmd/wfctl/infra_outputs.go config/infra_config.go'
  result: pass
supersedes: []
superseded_by: []
---

# wfctl Infra Bootstrap Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `wfctl infra bootstrap`, cross-resource output wiring (`{{ outputs.* }}`), pluggable secrets provider, and sensitive value masking to the workflow engine.

**Architecture:** Bootstrap creates state backend + generates secrets. Output template resolution runs after each resource applies, feeding outputs to dependent resources. Secrets Provider interface (existing) extended with GitHub provider and generators. Sensitive values masked in state, logs, and plan output.

**Tech Stack:** Go 1.26, `secrets.Provider` interface, GitHub API, `crypto/rand`

**Design Doc:** `docs/plans/2026-04-08-infra-bootstrap-design.md`

---

## Task 1: Add Sensitive field to ResourceOutput + masking utilities

**Files:**
- Modify: `/Users/jon/workspace/workflow/interfaces/iac_resource_driver.go`
- Create: `/Users/jon/workspace/workflow/secrets/masking.go`
- Create: `/Users/jon/workspace/workflow/secrets/masking_test.go`

**Step 1: Add Sensitive field**

In `iac_resource_driver.go`, add to `ResourceOutput`:
```go
type ResourceOutput struct {
    Name       string         `json:"name"`
    Type       string         `json:"type"`
    ProviderID string         `json:"provider_id"`
    Outputs    map[string]any `json:"outputs"`
    Status     string         `json:"status"`
    Sensitive  map[string]bool `json:"sensitive,omitempty"` // keys that are sensitive (uri, password, etc.)
}
```

**Step 2: Create masking.go**

```go
package secrets

// MaskSensitiveOutputs replaces sensitive values with "(sensitive)" for display.
func MaskSensitiveOutputs(outputs map[string]any, sensitive map[string]bool) map[string]any {
    masked := make(map[string]any, len(outputs))
    for k, v := range outputs {
        if sensitive[k] {
            masked[k] = "(sensitive)"
        } else {
            masked[k] = v
        }
    }
    return masked
}

// DefaultSensitiveKeys returns keys that are always considered sensitive.
func DefaultSensitiveKeys() map[string]bool {
    return map[string]bool{
        "uri": true, "password": true, "secret": true, "token": true,
        "connection_string": true, "dsn": true, "secret_key": true,
        "access_key": true, "private_key": true, "api_key": true,
    }
}

// MergeSensitiveKeys merges driver-declared sensitive keys with defaults.
func MergeSensitiveKeys(driverKeys, defaults map[string]bool) map[string]bool {
    merged := make(map[string]bool, len(defaults)+len(driverKeys))
    for k, v := range defaults { merged[k] = v }
    for k, v := range driverKeys { merged[k] = v }
    return merged
}
```

**Step 3: Tests, verify, commit**

```bash
go test ./secrets/ -run TestMask -v
git add interfaces/iac_resource_driver.go secrets/masking.go secrets/masking_test.go
git commit -m "feat: add Sensitive field to ResourceOutput + masking utilities"
```

---

## Task 2: Create GitHubSecretsProvider

**Files:**
- Create: `/Users/jon/workspace/workflow/secrets/github_provider.go`
- Create: `/Users/jon/workspace/workflow/secrets/github_provider_test.go`

**Step 1: Implement GitHubSecretsProvider**

Uses GitHub REST API to manage repository secrets:
- `GET /repos/{owner}/{repo}/actions/secrets/{name}` — check existence
- `PUT /repos/{owner}/{repo}/actions/secrets/{name}` — create/update (requires libsodium-sealed-box encryption)
- `DELETE /repos/{owner}/{repo}/actions/secrets/{name}` — delete
- `GET /repos/{owner}/{repo}/actions/secrets` — list

GitHub secrets are write-only (can't read values back). So `Get()` returns `ErrUnsupported` for reading values, but `Set()` works. For bootstrap, we only need `Set()`.

```go
type GitHubSecretsProvider struct {
    owner string
    repo  string
    token string
    client *http.Client
}

func NewGitHubSecretsProvider(repo, tokenEnvVar string) (*GitHubSecretsProvider, error) {
    // Parse owner/repo from "GoCodeAlone/workflow-dnd"
    // Read token from env var
}
```

For encrypting secrets before PUT: use `golang.org/x/crypto/nacl/box` (libsodium sealed box). GitHub provides a public key via `GET /repos/{owner}/{repo}/actions/secrets/public-key`.

**Step 2: Tests (mock HTTP), verify, commit**

---

## Task 3: Create secret generators

**Files:**
- Create: `/Users/jon/workspace/workflow/secrets/generators.go`
- Create: `/Users/jon/workspace/workflow/secrets/generators_test.go`

**Generators:**
```go
// GenerateSecret creates a secret value based on the generator config.
func GenerateSecret(genType string, config map[string]any) (string, error) {
    switch genType {
    case "random_hex":
        length, _ := config["length"].(int)
        if length <= 0 { length = 32 }
        b := make([]byte, length)
        crypto_rand.Read(b)
        return hex.EncodeToString(b), nil
    case "random_base64":
        length, _ := config["length"].(int)
        if length <= 0 { length = 32 }
        b := make([]byte, length)
        crypto_rand.Read(b)
        return base64.StdEncoding.EncodeToString(b), nil
    case "random_alphanumeric":
        // ...
    default:
        return "", fmt.Errorf("unknown generator type: %s", genType)
    }
}
```

**Tests, verify, commit.**

---

## Task 4: Add secrets config parsing to wfctl infra

**Files:**
- Modify: `/Users/jon/workspace/workflow/cmd/wfctl/infra.go`

**Step 1: Parse `secrets` section from infra YAML**

Add struct and parsing:
```go
type SecretsConfig struct {
    Provider string         `yaml:"provider"`
    Config   map[string]any `yaml:"config"`
    Generate []SecretGen    `yaml:"generate"`
}

type SecretGen struct {
    Key    string         `yaml:"key"`
    Type   string         `yaml:"type"`
    Length int            `yaml:"length,omitempty"`
    Source string         `yaml:"source,omitempty"`
}

type InfraConfig struct {
    AutoBootstrap *bool          `yaml:"auto_bootstrap,omitempty"` // default true
    Secrets       *SecretsConfig `yaml:"secrets,omitempty"`
}
```

Add `parseSecretsConfig(cfgFile string) (*SecretsConfig, error)` that reads the `secrets:` key from the YAML.

Add `resolveSecretsProvider(cfg *SecretsConfig) (secrets.Provider, error)` that instantiates the configured provider (github, vault, aws, env).

**Step 2: Tests, verify, commit**

---

## Task 5: Implement `wfctl infra bootstrap` command

**Files:**
- Modify: `/Users/jon/workspace/workflow/cmd/wfctl/infra.go`

**Step 1: Add bootstrap to command dispatch**

In `runInfra()`, add `"bootstrap"` case that calls `runInfraBootstrap(args)`.

**Step 2: Implement `runInfraBootstrap`**

```go
func runInfraBootstrap(args []string) error {
    fs := flag.NewFlagSet("infra bootstrap", flag.ExitOnError)
    var configFile string
    fs.StringVar(&configFile, "config", "", "Config file")
    fs.StringVar(&configFile, "c", "", "Config file (short)")
    fs.Parse(args)
    configFile = resolveInfraConfig(configFile, fs)

    // 1. Parse secrets config
    secretsCfg, _ := parseSecretsConfig(configFile)

    // 2. Create state backend if needed
    //    Read iac.state module config, check if backend exists
    //    For "spaces": check if bucket exists via provider API, create if not
    bootstrapStateBackend(configFile)

    // 3. Generate and store secrets
    if secretsCfg != nil {
        provider, _ := resolveSecretsProvider(secretsCfg)
        for _, gen := range secretsCfg.Generate {
            // Check if already set
            existing, err := provider.Get(ctx, gen.Key)
            if err == nil && existing != "" {
                fmt.Printf("  ✓ %s already set\n", gen.Key)
                continue
            }
            // Generate
            value, _ := secrets.GenerateSecret(gen.Type, map[string]any{"length": gen.Length})
            // Store
            provider.Set(ctx, gen.Key, value)
            fmt.Printf("  + %s generated and stored\n", gen.Key)
        }
    }

    fmt.Println("Bootstrap complete.")
    return nil
}
```

**Step 3: Tests, verify, commit**

---

## Task 6: Implement output template resolution

**Files:**
- Modify: `/Users/jon/workspace/workflow/cmd/wfctl/infra.go`

**Step 1: Add `resolveOutputTemplates` function**

```go
// resolveOutputTemplates scans config values for {{ outputs.resource.key }}
// and {{ secrets.key }} patterns and resolves them.
func resolveOutputTemplates(
    spec *interfaces.ResourceSpec,
    outputs map[string]map[string]any,  // resource_name → outputs
    secretsProvider secrets.Provider,
) error {
    for key, val := range spec.Config {
        strVal, ok := val.(string)
        if !ok { continue }
        resolved := templatePattern.ReplaceAllStringFunc(strVal, func(match string) string {
            // {{ outputs.staging-db.uri }}
            if strings.HasPrefix(match, "{{ outputs.") {
                parts := strings.Split(strings.TrimSpace(match[11:len(match)-3]), ".")
                if len(parts) == 2 {
                    if resOutputs, ok := outputs[parts[0]]; ok {
                        if v, ok := resOutputs[parts[1]]; ok {
                            return fmt.Sprintf("%v", v)
                        }
                    }
                }
            }
            // {{ secrets.jwt_secret }}
            if strings.HasPrefix(match, "{{ secrets.") {
                key := strings.TrimSpace(match[11:len(match)-3])
                if secretsProvider != nil {
                    v, err := secretsProvider.Get(context.Background(), key)
                    if err == nil { return v }
                }
            }
            return match // unresolved — leave as-is
        })
        spec.Config[key] = resolved
    }
    return nil
}

var templatePattern = regexp.MustCompile(`\{\{\s*(outputs\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_]+|secrets\.[a-zA-Z0-9_]+)\s*\}\}`)
```

**Step 2: Add auto-dependency inference**

```go
// inferDependencies scans config values for {{ outputs.resource.key }}
// references and adds implicit DependsOn entries.
func inferDependencies(specs []interfaces.ResourceSpec) {
    for i := range specs {
        for _, val := range specs[i].Config {
            strVal, ok := val.(string)
            if !ok { continue }
            matches := templatePattern.FindAllString(strVal, -1)
            for _, m := range matches {
                if strings.HasPrefix(m, "{{ outputs.") {
                    parts := strings.Split(strings.TrimSpace(m[11:len(m)-3]), ".")
                    if len(parts) >= 1 {
                        dep := parts[0]
                        if !slices.Contains(specs[i].DependsOn, dep) {
                            specs[i].DependsOn = append(specs[i].DependsOn, dep)
                        }
                    }
                }
            }
        }
    }
}
```

**Step 3: Wire into apply flow**

In `runInfraApply` (or the pipeline step), after each resource applies:
- Store outputs: `allOutputs[resource.Name] = result.Outputs`
- Before applying next resource: `resolveOutputTemplates(&spec, allOutputs, secretsProvider)`

**Step 4: Tests, verify, commit**

---

## Task 7: Implement auto-bootstrap in apply

**Files:**
- Modify: `/Users/jon/workspace/workflow/cmd/wfctl/infra.go`

**Step 1: Parse `infra.auto_bootstrap` config**

```go
func parseAutoBootstrap(cfgFile string) bool {
    // Read top-level "infra" key from YAML
    // Default: true
    // If "auto_bootstrap: false" → return false
}
```

**Step 2: Wire into `runInfraApply`**

At the start of `runInfraApply`, before running the pipeline:
```go
if parseAutoBootstrap(configFile) {
    if err := runInfraBootstrap([]string{"--config", configFile}); err != nil {
        return fmt.Errorf("auto-bootstrap failed: %w", err)
    }
}
```

**Step 3: Tests, verify, commit**

---

## Task 8: Apply sensitive masking to plan/apply output

**Files:**
- Modify: `/Users/jon/workspace/workflow/cmd/wfctl/infra.go`

**Step 1: Mask sensitive values in formatPlanTable and formatPlanMarkdown**

In `resourceSummaryKeys()`, check if a key is in `DefaultSensitiveKeys()`. If so, display `(sensitive)` instead of the value.

**Step 2: Mask in apply progress output**

When logging apply results, mask outputs where `sensitive[key]` is true.

**Step 3: Add `--show-sensitive` flag**

```go
var showSensitive bool
fs.BoolVar(&showSensitive, "show-sensitive", false, "Show sensitive values in output")
```

When `showSensitive` is false (default), mask. When true, show plaintext (for debugging).

**Step 4: Tests, verify, commit**

---

## Task 9: Mark DO database driver outputs as sensitive

**Files:**
- Modify: `/Users/jon/workspace/workflow-plugin-digitalocean/internal/drivers/database.go`

**Step 1: Add sensitive markers to database driver outputs**

In the `dbOutput()` function, after building the outputs map, add:
```go
output.Sensitive = map[string]bool{
    "uri": true,
    "password": true,
    "user": true,
}
```

**Step 2: Commit in the DO plugin repo, tag**

```bash
cd /Users/jon/workspace/workflow-plugin-digitalocean
git add internal/drivers/database.go
git commit -m "feat: mark database URI/password outputs as sensitive"
git tag v0.2.1
git push origin main --tags
```

---

## Task 10: Update workflow-dnd deploy.yml + infra YAML + docs

**Files:**
- Modify: `/Users/jon/workspace/workflow-dnd/.github/workflows/deploy.yml`
- Modify: `/Users/jon/workspace/workflow-dnd/infra/staging.yaml`
- Modify: `/Users/jon/workspace/workflow-dnd/docs/DEPLOYMENT.md`
- Modify: `/Users/jon/workspace/workflow-dnd/infra/README.md`

**Step 1: Simplify deploy.yml**

Replace the entire bootstrap job + shell scripts with:
```yaml
- run: wfctl infra bootstrap -c infra/staging.yaml
  env:
    DIGITALOCEAN_TOKEN: ${{ secrets.DIGITALOCEAN_TOKEN }}
    GH_MANAGEMENT_TOKEN: ${{ secrets.GH_MANAGEMENT_TOKEN }}
- run: wfctl infra apply -c infra/staging.yaml -y
  env:
    DIGITALOCEAN_TOKEN: ${{ secrets.DIGITALOCEAN_TOKEN }}
```

Remove the bootstrap job entirely. Remove DATABASE_URL, JWT_SECRET, SPACES_* from secrets references.

**Step 2: Update infra/staging.yaml**

Add `secrets:` section and `{{ outputs.staging-db.uri }}` references per the design doc.

**Step 3: Update docs/DEPLOYMENT.md**

Update prerequisites (only 2-3 secrets needed), update pipeline flow description, remove manual Spaces/DATABASE_URL steps.

**Step 4: Commit, push, tag**

```bash
git add .github/workflows/deploy.yml infra/staging.yaml docs/DEPLOYMENT.md infra/README.md
git commit -m "feat: use wfctl infra bootstrap — reduce required secrets from 6 to 2"
git push origin master
git tag v1.6.0
git push origin v1.6.0
```

---

## Task 11: Tag workflow engine release

**Step 1: Run all tests**

```bash
cd /Users/jon/workspace/workflow
go test ./... -count=1 -timeout 300s
```

**Step 2: Tag and push**

```bash
git tag v0.10.0
git push origin main --tags
```

**Step 3: Verify wfctl release artifacts are built by GoReleaser**

Check that the GitHub release has `wfctl-linux-amd64`, `wfctl-darwin-arm64`, etc.
