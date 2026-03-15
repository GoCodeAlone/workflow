# Plugin Authoring Guide

This guide covers creating, testing, publishing, and registering workflow plugins.

## Quick Start

```bash
# Scaffold a new plugin
wfctl plugin init my-plugin -author MyOrg -description "My custom plugin"

# Build and test
cd my-plugin
go mod tidy
make build
make test

# Install locally for development
make install-local
```

## Project Structure

`wfctl plugin init` generates a complete project:

```
my-plugin/
├── cmd/workflow-plugin-my-plugin/main.go   # gRPC entrypoint
├── internal/
│   ├── provider.go                         # Plugin provider (registers steps/modules)
│   └── steps.go                            # Step implementations
├── plugin.json                             # Plugin manifest
├── go.mod
├── .goreleaser.yml                         # Cross-platform release builds
├── .github/workflows/
│   ├── ci.yml                              # Test + lint on PR
│   └── release.yml                         # GoReleaser + registry notification
├── Makefile
└── README.md
```

## Implementing Steps

Step types are the primary extension point. Each step implements the `sdk.StepInstance` interface from `github.com/GoCodeAlone/workflow/plugin/external/sdk`:

```go
// MyStep implements sdk.StepInstance.
type MyStep struct {
    config map[string]any
}

func (s *MyStep) Execute(
    ctx context.Context,
    triggerData map[string]any,
    stepOutputs map[string]map[string]any,
    current map[string]any,
    metadata map[string]any,
    config map[string]any,
) (*sdk.StepResult, error) {
    // Access step config: config["key"] or s.config["key"]
    // Access pipeline context: current["key"]
    // Access previous step output: stepOutputs["step-name"]["key"]
    return &sdk.StepResult{
        Output: map[string]any{"result": "value"},
    }, nil
}
```

Register in `internal/provider.go` by implementing `sdk.StepProvider`:

```go
// StepTypes implements sdk.StepProvider.
func (p *Provider) StepTypes() []string {
    return []string{"step.my_action"}
}

// CreateStep implements sdk.StepProvider.
func (p *Provider) CreateStep(typeName, name string, config map[string]any) (sdk.StepInstance, error) {
    switch typeName {
    case "step.my_action":
        return &MyStep{config: config}, nil
    }
    return nil, fmt.Errorf("unknown step type: %s", typeName)
}
```

## Implementing Modules

Modules provide runtime services (database connections, API clients, etc.) by implementing `sdk.ModuleProvider`:

```go
// ModuleTypes implements sdk.ModuleProvider.
func (p *Provider) ModuleTypes() []string {
    return []string{"my.provider"}
}

// CreateModule implements sdk.ModuleProvider.
func (p *Provider) CreateModule(typeName, name string, config map[string]any) (sdk.ModuleInstance, error) {
    return &MyModule{config: config}, nil
}
```

## Plugin Manifest

The `plugin.json` declares what your plugin provides. The name should match what you passed to `wfctl plugin init`:

```json
{
    "name": "my-plugin",
    "version": "0.1.0",
    "description": "My custom plugin",
    "author": "MyOrg",
    "license": "MIT",
    "type": "external",
    "tier": "community",
    "minEngineVersion": "0.3.30",
    "capabilities": {
        "moduleTypes": ["my.provider"],
        "stepTypes": ["step.my_action", "step.my_query"],
        "triggerTypes": []
    }
}
```

## Testing

```bash
# Unit tests
make test

# Install to local engine
make install-local

# Validate manifest format (from registry by name)
wfctl plugin validate my-plugin

# Validate a local manifest file
wfctl plugin validate --file plugin.json

# Full lifecycle test (start/stop/execute)
wfctl plugin test .
```

## Publishing a Release

1. Tag your version:
   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```

2. GoReleaser builds cross-platform binaries and creates a GitHub Release automatically.

3. If `REGISTRY_PAT` secret is configured, the registry is notified of the new version.

## Registering in the Public Registry

1. Fork [GoCodeAlone/workflow-registry](https://github.com/GoCodeAlone/workflow-registry)
2. Create `plugins/<your-plugin>/manifest.json` conforming to the [schema](https://github.com/GoCodeAlone/workflow-registry/blob/main/schema/registry-schema.json)
3. Open a PR — CI validates your manifest automatically
4. After maintainer review and merge, your plugin appears in `wfctl plugin search`

### Manifest Example

```json
{
    "name": "workflow-plugin-my-plugin",
    "version": "0.1.0",
    "description": "My custom plugin",
    "author": "MyOrg",
    "type": "external",
    "tier": "community",
    "license": "MIT",
    "repository": "https://github.com/MyOrg/workflow-plugin-my-plugin",
    "keywords": ["example"],
    "capabilities": {
        "moduleTypes": [],
        "stepTypes": ["step.my_action"],
        "triggerTypes": []
    },
    "downloads": [
        {"os": "linux", "arch": "amd64", "url": "https://github.com/MyOrg/workflow-plugin-my-plugin/releases/download/v0.1.0/workflow-plugin-my-plugin-linux-amd64.tar.gz"},
        {"os": "linux", "arch": "arm64", "url": "https://github.com/MyOrg/workflow-plugin-my-plugin/releases/download/v0.1.0/workflow-plugin-my-plugin-linux-arm64.tar.gz"},
        {"os": "darwin", "arch": "amd64", "url": "https://github.com/MyOrg/workflow-plugin-my-plugin/releases/download/v0.1.0/workflow-plugin-my-plugin-darwin-amd64.tar.gz"},
        {"os": "darwin", "arch": "arm64", "url": "https://github.com/MyOrg/workflow-plugin-my-plugin/releases/download/v0.1.0/workflow-plugin-my-plugin-darwin-arm64.tar.gz"}
    ]
}
```

## Private Plugins

No registry needed — install directly:

```bash
# From a GitHub Release URL
wfctl plugin install --url https://github.com/MyOrg/my-plugin/releases/download/v0.1.0/my-plugin-darwin-arm64.tar.gz

# From a local build
wfctl plugin install --local ./path/to/build/

# The lockfile (.wfctl.yaml) is updated automatically
```

## Engine Auto-Fetch

Declare plugins in your workflow config for automatic download on engine startup:

```yaml
plugins:
  external:
    - name: my-plugin
      autoFetch: true
      version: ">=0.1.0"
```

The engine calls `wfctl plugin install` if the plugin isn't found locally.

## Trust Tiers

| Tier | Requirements |
|------|-------------|
| **community** | Valid manifest, PR reviewed, SHA-256 checksums via GoReleaser |
| **verified** | + cosign-signed releases, public key in manifest |
| **official** | GoCodeAlone-maintained, signed with org key |

## Registry Notification

Add the [notify-registry Action template](https://github.com/GoCodeAlone/workflow-registry/blob/main/templates/notify-registry.yml) to your release workflow for automatic version tracking.
