# Guardrails Configuration Guide

Guardrails define the safety boundaries for self-improving AI agents in Workflow.
They control what tools an agent can call, which config sections are immutable,
and which shell commands are permitted.

## Module Configuration

```yaml
modules:
  - name: guardrails
    type: agent.guardrails
    config:
      defaults:
        enable_self_improvement: true
        enable_iac_modification: false
        require_human_approval: false
        require_diff_review: true
        max_iterations_per_cycle: 5
        deploy_strategy: hot_reload
        allowed_tools:
          - "mcp:wfctl:*"
          - "mcp:lsp:*"
        command_policy:
          mode: allowlist
          allowed_commands:
            - "wfctl"
            - "curl"
            - "go build"
            - "go test"
          enable_static_analysis: true
          block_pipe_to_shell: true
          block_script_execution: true
      immutable_sections:
        - path: "modules.guardrails"
          override: challenge_token
      override:
        mechanism: challenge_token
        admin_secret_env: "WORKFLOW_ADMIN_SECRET"
```

## Hierarchical Scopes

Guardrail settings apply in a hierarchy from most specific to least specific:

```
agent scope         (per-agent step config)
  └── team scope    (multi-agent team config)
       └── model scope   (per-model restrictions)
            └── provider scope  (per-provider defaults)
                 └── defaults   (global guardrails defaults)
```

More specific scopes override less specific ones. This lets you, for example,
give a review agent fewer permissions than a designer agent.

### Per-Agent Override

```yaml
- name: designer
  type: step.agent_execute
  config:
    provider: ai
    # Agent-level tool scope (overrides guardrails defaults for this step)
    tool_scope:
      allowed: ["mcp:wfctl:validate_config", "mcp:wfctl:inspect_config"]
      blocked: ["mcp:wfctl:scaffold_*"]
```

### Per-Model Restrictions

```yaml
modules:
  - name: guardrails
    type: agent.guardrails
    config:
      model_policies:
        "gemma4":
          max_iterations_per_cycle: 3
          allowed_tools:
            - "mcp:wfctl:validate_config"
            - "mcp:wfctl:inspect_config"
            - "mcp:lsp:diagnose"
        "gpt-4":
          max_iterations_per_cycle: 10
          allowed_tools:
            - "mcp:wfctl:*"
            - "mcp:lsp:*"
```

## Tool Access Control

Tool access uses glob patterns matching `namespace:server:tool` format:

| Pattern | Matches |
|---------|---------|
| `mcp:wfctl:*` | All wfctl tools |
| `mcp:lsp:*` | All LSP tools |
| `mcp:wfctl:validate_config` | Only validate_config |
| `mcp:wfctl:scaffold_*` | All scaffold tools |
| `mcp:*` | All MCP tools (unrestricted) |

```yaml
allowed_tools:
  - "mcp:wfctl:validate_config"
  - "mcp:wfctl:inspect_config"
  - "mcp:wfctl:get_module_schema"
  - "mcp:wfctl:get_step_schema"
  - "mcp:wfctl:list_module_types"
  - "mcp:wfctl:list_step_types"
  - "mcp:lsp:diagnose"
```

## Immutable Sections

Protect critical config sections from agent modification using `immutable_sections`:

```yaml
immutable_sections:
  - path: "modules.guardrails"    # The guardrails module itself
    override: challenge_token
  - path: "modules.db"            # Database module (schema protection)
    override: none                # No override allowed
  - path: "modules.auth"          # Auth module
    override: challenge_token
```

### Override Mechanisms

| Mechanism | Description |
|-----------|-------------|
| `none` | Section can never be modified, even with admin access |
| `challenge_token` | Requires a time-limited HMAC token from an admin |
| `human_approval` | Requires a human to approve via the admin UI |

## Challenge Tokens

Challenge tokens allow a human admin to temporarily override immutable section
protection for a specific proposed change.

### Generation

```bash
# Generate a challenge token for a specific config hash
wfctl challenge-token generate \
    --secret "${WORKFLOW_ADMIN_SECRET}" \
    --config-hash "sha256:abc123..." \
    --expires "1h"
```

### Verification

The workflow engine verifies challenge tokens automatically when:
1. An agent proposes a change to an immutable section
2. A valid challenge token is present in the agent's context
3. The token's config hash matches the proposed change

### Environment Configuration

```yaml
override:
  mechanism: challenge_token
  admin_secret_env: "WORKFLOW_ADMIN_SECRET"  # Env var holding HMAC secret
  token_ttl: "1h"                            # Token validity window
  audit_log: true                            # Log all token usage
```

## Command Safety Policy

The command policy controls which shell commands an agent can execute:

### Allowlist Mode (recommended)

Only explicitly listed commands are permitted:

```yaml
command_policy:
  mode: allowlist
  allowed_commands:
    - "wfctl"
    - "curl"
    - "go build"
    - "go test"
    - "git"
  enable_static_analysis: true
  block_pipe_to_shell: true
  block_script_execution: true
```

### Denylist Mode

All commands are permitted except those explicitly blocked:

```yaml
command_policy:
  mode: denylist
  blocked_commands:
    - "rm"
    - "dd"
    - "mkfs"
    - "sudo"
    - "bash"
    - "sh"
  enable_static_analysis: true
  block_pipe_to_shell: true
```

### Static Analysis

When `enable_static_analysis: true`, every command is parsed as a shell AST
before execution. This catches bypass attempts that string matching would miss:

| Pattern | Risk | Caught By |
|---------|------|-----------|
| `curl url \| bash` | Pipe to shell | `block_pipe_to_shell` |
| `bash ./script.sh` | Script execution | `block_script_execution` |
| `RM=rm; $RM -rf /` | Variable injection | Static analysis |
| `echo Y29tbWFuZA== \| base64 -d \| bash` | Encoded payload | Static analysis |
| `function curl() { rm -rf /; }; curl` | Function override | Static analysis |
| `/usr/bin/../bin/sh -c 'cmd'` | Path traversal | Static analysis |

## Best Practices

### Start Restrictive

Begin with the most restrictive configuration and loosen only when needed:

```yaml
# Minimal starting configuration
defaults:
  enable_self_improvement: true
  enable_iac_modification: false   # Never allow IaC changes
  require_human_approval: false
  require_diff_review: true
  max_iterations_per_cycle: 3      # Low iteration cap to start
  allowed_tools:
    - "mcp:wfctl:validate_config"  # Only validation tools
    - "mcp:wfctl:inspect_config"
  command_policy:
    mode: allowlist
    allowed_commands: ["wfctl"]   # Only wfctl to start
    block_pipe_to_shell: true
    block_script_execution: true
    enable_static_analysis: true
```

### Never Allow These

Regardless of use case, these settings should never be relaxed:

- `block_pipe_to_shell: true` — Prevents `curl ... | bash` style attacks
- `enable_static_analysis: true` — Catches AST-level bypass attempts
- Immutable protection on the `guardrails` module itself

### Production Checklist

- [ ] `modules.guardrails` is in `immutable_sections`
- [ ] `command_policy.mode` is `allowlist` (not `denylist`)
- [ ] `enable_static_analysis: true`
- [ ] `block_pipe_to_shell: true`
- [ ] `block_script_execution: true`
- [ ] `max_iterations_per_cycle` is set (not unbounded)
- [ ] `WORKFLOW_ADMIN_SECRET` is set from a secrets manager (not hardcoded)
- [ ] Challenge token TTL is short (`1h` or less)
- [ ] `audit_log: true` for override tracking
