# Self-Improvement Tutorial

This tutorial walks through building a self-improving workflow application from
scratch. You will start with a basic task API, add an AI agent, configure
guardrails, run the first improvement cycle, and then review what changed.

## Prerequisites

- Docker (with Compose)
- Ollama running with Gemma 4: `ollama pull gemma4`
- `wfctl` CLI installed
- **workflow-plugin-agent v0.8.0+** — The `agent.provider`, `agent.guardrails`,
  and all `step.self_improve_*` / `step.blackboard_*` types are provided by this
  plugin, not the workflow core. Install it before running the examples:
  ```bash
  wfctl plugin install workflow-plugin-agent
  ```

## Step 1: Build the Base Application

Create a basic task CRUD API as the starting point for self-improvement.

**`config/app.yaml`:**
```yaml
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"

  - name: router
    type: http.router
    dependsOn: [server]

  - name: db
    type: storage.sqlite
    config:
      dbPath: /data/tasks.db
      walMode: true

pipelines:
  health_check:
    trigger:
      type: http
      config:
        path: /healthz
        method: GET
    steps:
      - name: respond
        type: step.json_response
        config:
          status: 200
          body: {status: healthy}

  create_task:
    trigger:
      type: http
      config:
        path: /tasks
        method: POST
    steps:
      - name: insert
        type: step.db_exec
        config:
          database: db
          query: "INSERT INTO tasks (title, status) VALUES (?, 'pending')"
          params: ["{{ .body.title }}"]
      - name: respond
        type: step.json_response
        config:
          status: 201
          body: {status: created}

  list_tasks:
    trigger:
      type: http
      config:
        path: /tasks
        method: GET
    steps:
      - name: query
        type: step.db_query
        config:
          database: db
          mode: list
          query: "SELECT id, title, status, created_at FROM tasks"
      - name: respond
        type: step.json_response
        config:
          status: 200
          body_from: "steps.query.rows"

workflows:
  http:
    router: router
    server: server
```

Validate the config:
```bash
wfctl validate config/app.yaml
# PASS config/app.yaml (3 modules, 1 workflows, 0 triggers)
```

## Step 2: Add the Agent Provider Module

Add the AI agent module to your config:

```yaml
modules:
  # ... existing modules ...

  - name: ai
    type: agent.provider
    config:
      provider: ollama
      model: gemma4
      base_url: http://localhost:11434   # or http://ollama:11434 in Docker
      max_tokens: 8192
```

The agent provider connects to Ollama and makes Gemma 4 available to pipeline
steps that need LLM reasoning.

## Step 3: Configure Guardrails

Add guardrails to control what the agent is allowed to do:

```yaml
modules:
  # ... existing modules ...

  - name: guardrails
    type: agent.guardrails
    config:
      defaults:
        enable_self_improvement: true
        enable_iac_modification: false    # Don't allow infrastructure changes
        require_diff_review: true         # Always generate a diff
        max_iterations_per_cycle: 3       # Start conservatively
        deploy_strategy: hot_reload
        allowed_tools:
          - "mcp:wfctl:validate_config"   # Agent can validate
          - "mcp:wfctl:inspect_config"    # Agent can inspect current config
          - "mcp:wfctl:get_module_schema" # Agent can look up schemas
          - "mcp:wfctl:list_step_types"   # Agent can discover step types
          - "mcp:lsp:diagnose"            # Agent can check YAML syntax
        command_policy:
          mode: allowlist
          allowed_commands: ["wfctl", "curl"]
          block_pipe_to_shell: true
          block_script_execution: true
          enable_static_analysis: true
      immutable_sections:
        - path: "modules.guardrails"      # Protect guardrails from modification
          override: challenge_token
      override:
        mechanism: challenge_token
        admin_secret_env: "WFCTL_ADMIN_SECRET"
```

> **Security note:** Always include `modules.guardrails` in `immutable_sections`.
> Without this, an agent could potentially disable its own guardrails.

## Step 4: Define the Self-Improvement Pipeline

Add the improvement loop pipeline:

```yaml
pipelines:
  # ... existing pipelines ...

  improve:
    trigger:
      type: http
      config:
        path: /improve
        method: POST
    steps:
      - name: load_config
        type: step.read_file
        config:
          path: /data/config/app.yaml

      - name: designer
        type: step.agent_execute
        config:
          provider: ai
          system_prompt: |
            You are a workflow configuration designer. Your goal is to improve
            this workflow application's config to add the features described
            in the user's request. Follow these rules:
            1. Always call validate_config before submitting your proposal.
            2. Use inspect_config to understand the current structure first.
            3. Use get_module_schema to check required fields for new modules.
            4. Propose one focused improvement per iteration.
          tools:
            - "mcp:wfctl:validate_config"
            - "mcp:wfctl:inspect_config"
            - "mcp:wfctl:get_module_schema"
            - "mcp:wfctl:list_step_types"
            - "mcp:lsp:diagnose"
          max_iterations: 10

      - name: post_design
        type: step.blackboard_post
        config:
          phase: design
          artifact_type: config_proposal

      - name: validate
        type: step.self_improve_validate
        config:
          validation_level: strict
          require_zero_errors: true

      - name: diff
        type: step.self_improve_diff
        config: {}

      - name: deploy
        type: step.self_improve_deploy
        config:
          strategy: hot_reload
          config_path: /data/config/app.yaml
```

## Step 5: Set Up Docker Compose

**`docker-compose.yaml`:**
```yaml
services:
  ollama:
    image: ollama/ollama:latest
    ports:
      - "11434:11434"
    volumes:
      - ollama-data:/root/.ollama
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:11434/api/tags"]
      interval: 10s
      timeout: 5s
      retries: 30

  app:
    image: ghcr.io/gocodealone/workflow:latest
    ports:
      - "8080:8080"
    volumes:
      - app-data:/data
      - ./config:/data/config
    environment:
      - WFCTL_ADMIN_SECRET=my-admin-secret
    command: ["-config", "/data/config/app.yaml", "-data-dir", "/data"]
    depends_on:
      ollama:
        condition: service_healthy

volumes:
  ollama-data:
  app-data:
```

Start the stack:
```bash
docker compose up -d
```

## Step 6: Run the First Improvement Cycle

First, verify the base app is healthy:
```bash
curl http://localhost:8080/healthz
# {"status":"healthy"}
```

Create a task:
```bash
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{"title": "Test task"}'
# {"status":"created"}
```

Now trigger the improvement loop:
```bash
curl -X POST http://localhost:8080/improve \
  -H "Content-Type: application/json" \
  -d '{"goal": "Add a description field to tasks and support filtering by status"}'
```

The agent will:
1. Load the current `app.yaml`
2. Inspect its structure with `mcp:wfctl:inspect_config`
3. Look up schemas for relevant module/step types
4. Propose changes to add a `description` field and `status` query parameter
5. Validate the proposal with `mcp:wfctl:validate_config`
6. Post the proposal to the blackboard
7. Generate a diff
8. Hot-reload the updated config

## Step 7: Review Blackboard Artifacts

Query the blackboard to see what the agent produced:

```bash
# Read the design phase artifact
curl http://localhost:8080/blackboard/design/config_proposal
```

The artifact includes:
- The proposed config YAML
- The agent's reasoning
- MCP tool calls made during design
- Iteration count

## Step 8: Use Challenge Tokens for Overrides

If you need to modify a guardrails-protected section, generate a challenge token:

```bash
# Compute the SHA256 hash of the proposed config change
HASH=$(echo "$PROPOSED_YAML" | sha256sum | cut -d' ' -f1)

# Generate an override token (reads WFCTL_ADMIN_SECRET from env)
TOKEN=$(wfctl override generate "sha256:$HASH")

echo "Override token: $TOKEN"
```

Provide the token to the agent in the improvement request:

```bash
curl -X POST http://localhost:8080/improve \
  -H "Content-Type: application/json" \
  -d "{
    \"goal\": \"Update the guardrails to allow go test commands\",
    \"challenge_token\": \"$TOKEN\"
  }"
```

## Step 9: Deploy via Git PR Strategy

For production scenarios where you want human review before changes go live,
switch to the `git_pr` deploy strategy:

```yaml
- name: deploy
  type: step.self_improve_deploy
  config:
    strategy: git_pr
    config_path: /data/config/app.yaml
    git:
      repo: /data/repo
      branch_prefix: "agent/improve-"
      commit_message: "agent: add ${improvement_name}"
      pr_title: "Agent improvement: ${improvement_name}"
```

The agent will:
1. Create a new branch (`agent/improve-<timestamp>`)
2. Commit the improved config
3. Open a PR for human review
4. The PR description includes the agent's reasoning and the diff

## Step 10: Canary Deployment

For gradual rollout of changes, use the `canary` strategy:

```yaml
- name: deploy
  type: step.self_improve_deploy
  config:
    strategy: canary
    config_path: /data/config/app.yaml
    canary:
      initial_weight: 10      # Start with 10% traffic
      step_weight: 10         # Increase by 10% each step
      step_interval: "5m"     # Check every 5 minutes
      success_threshold: 0.99 # Require 99% success rate
      error_budget: 0.01      # Allow 1% errors before rollback
      rollback_on_failure: true
```

The engine will:
1. Route 10% of traffic to the new config
2. Monitor error rates for 5 minutes
3. If error rate is below 1%, increase to 20%
4. Continue until 100% or automatic rollback

## What's Next

- Explore [Scenario 85](../scenarios/85-self-improving-api/) for a complete working example
- Read the [Guardrails Guide](guardrails-guide.md) for advanced safety configuration
- See the [MCP Tools Reference](mcp-tools-reference.md) for all available tools
- Try [Scenario 86](../scenarios/86-self-extending-mcp/) to see agents creating new MCP tools
- Try [Scenario 87](../scenarios/87-autonomous-agile-agent/) for fully autonomous improvement
