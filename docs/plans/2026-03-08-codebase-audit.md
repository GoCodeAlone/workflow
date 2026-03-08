# Codebase Audit Implementation Plan

**Goal:** Fix all stubbed code in the workflow engine, implement DockerSandbox, and build test scenarios for all untested plugins.

**Architecture:** Provider interface for security scanning, Docker sandbox module in existing sandbox plugin, new security scanner plugin, 4 public scenarios, 5 private scenarios.

**Tech Stack:** Go, Docker Engine API, YAML configs, bash test scripts with jq

---

### Task 1: SecurityScannerProvider Interface (Core Engine)

**Files:**
- Create: `module/scan_provider.go`
- Modify: `module/pipeline_step_scan_sast.go`
- Modify: `module/pipeline_step_scan_container.go`
- Modify: `module/pipeline_step_scan_deps.go`

**Step 1: Create the provider interface**

Create `module/scan_provider.go` with:
- `SecurityScannerProvider` interface (ScanSAST, ScanContainer, ScanDeps methods)
- `ScanResult` struct (Passed, Findings, Summary, OutputFormat, RawOutput)
- `ScanFinding` struct (ID, Severity, Title, Description, Package, Version, FixVersion, Location)
- `SASTScanOpts` (Scanner, SourcePath, Rules, FailOnSeverity, OutputFormat)
- `ContainerScanOpts` (Scanner, TargetImage, SeverityThreshold, IgnoreUnfixed, OutputFormat)
- `DepsScanOpts` (Scanner, SourcePath, FailOnSeverity, OutputFormat)
- `severityRank()` helper mapping severity string to int for comparison
- `SeverityAtOrAbove()` exported helper for threshold checks

**Step 2: Rewrite scan_sast to delegate to provider**

Rewrite `ScanSASTStep.Execute()`:
1. Look up `SecurityScannerProvider` from app service registry via `security-scanner` key
2. Build `SASTScanOpts` from step config
3. Call `provider.ScanSAST(ctx, opts)`
4. Evaluate severity gate: if any finding severity >= fail_on_severity, set `response_status: 400`
5. Return structured output: `passed`, `findings`, `summary`, `scanner`
6. If no provider registered, return clear error: "no security scanner provider configured — load a scanner plugin"

**Step 3: Rewrite scan_container to delegate to provider**

Same pattern: look up provider, call `provider.ScanContainer()`, evaluate severity gate.

**Step 4: Rewrite scan_deps to delegate to provider**

Same pattern: look up provider, call `provider.ScanDeps()`, evaluate severity gate.

**Step 5: Add tests**

Create `module/scan_provider_test.go`:
- Test with mock provider implementing the interface
- Test scan_sast delegates correctly, severity gating works
- Test scan_container delegates correctly
- Test scan_deps delegates correctly
- Test error when no provider registered

**Step 6: Commit**

```bash
git add module/scan_provider.go module/pipeline_step_scan_sast.go module/pipeline_step_scan_container.go module/pipeline_step_scan_deps.go module/scan_provider_test.go
git commit -m "feat: SecurityScannerProvider interface — scan steps delegate to plugins"
```

---

### Task 2: DockerSandbox Module (workflow-plugin-sandbox)

**Files:**
- Create: `workflow-plugin-sandbox/internal/module_docker.go`
- Create: `workflow-plugin-sandbox/internal/module_docker_test.go`
- Modify: `workflow-plugin-sandbox/internal/plugin.go`

**Step 1: Define DockerSandbox types**

In `module_docker.go`:
- `DockerSandboxModule` struct with config fields: maxCPU, maxMemory, networkMode, readOnlyRootfs, allowedImages, timeout
- `DockerRunOpts` struct: Image, Command, Env, Mounts, WorkDir, NetworkMode
- `DockerRunResult` struct: ExitCode, Stdout, Stderr, Duration
- `Mount` struct: Source, Target, ReadOnly

**Step 2: Implement module lifecycle**

- `Init()`: validate config, register self as `sandbox-docker:<name>` in service registry
- `Start()`: create Docker client (`client.NewClientWithOpts(client.FromEnv)`)
- `Stop()`: close client
- `Run()`: implementation that:
  1. Validates image against allowedImages whitelist (glob match)
  2. Creates container with security constraints:
     - CPU/memory limits via `container.Resources`
     - NetworkMode from config (never "host")
     - ReadonlyRootfs: true by default
     - No --privileged ever
     - SecurityOpt: ["no-new-privileges"]
  3. Starts container with context timeout
  4. Waits for completion, captures stdout/stderr via attach
  5. Removes container on completion
  6. Returns result

**Step 3: Add mock mode**

When `mock: true` in config:
- `Run()` returns a synthetic result based on image name
- No actual Docker operations — for testing without Docker daemon

**Step 4: Register in plugin**

Add `sandbox.docker` to the plugin's `ModuleTypes()` and `CreateModule()`.

**Step 5: Add tests**

Test mock mode returns expected results. Test config validation (reject host network, require allowed images).

**Step 6: Commit**

---

### Task 3: Security Scanner Plugin (New Public Plugin)

**Files:**
- Create: `workflow-plugin-security-scanner/` (new repo)
- Key files: `cmd/workflow-plugin-security-scanner/main.go`, `internal/plugin.go`, `internal/module_scanner.go`, `internal/scanner_semgrep.go`, `internal/scanner_trivy.go`, `internal/scanner_grype.go`, `internal/scanner_mock.go`

**Step 1: Create repository structure**

```
workflow-plugin-security-scanner/
├── cmd/workflow-plugin-security-scanner/main.go
├── internal/
│   ├── plugin.go          # Plugin provider
│   ├── module_scanner.go  # SecurityScannerModule
│   ├── scanner_semgrep.go # SAST via semgrep CLI
│   ├── scanner_trivy.go   # Container + deps via trivy CLI
│   ├── scanner_grype.go   # Deps via grype CLI
│   ├── scanner_mock.go    # Mock scanner for testing
│   └── scanner_test.go    # Tests
├── go.mod
├── go.sum
└── Makefile
```

**Step 2: Implement SecurityScannerModule**

Module type: `security.scanner`
Config:
```yaml
modules:
  - name: scanner
    type: security.scanner
    config:
      sast_backend: semgrep    # or: mock
      container_backend: trivy # or: mock
      deps_backend: grype      # or: trivy, mock
      docker_sandbox: ""       # optional: name of sandbox.docker module
```

On `Init()`: register as `security-scanner:<name>` service (matching what core scan steps look up).

**Step 3: Implement semgrep backend**

`ScanSAST()`: runs `semgrep scan --json --config=<rules> <source_path>`, parses JSON output into ScanResult.

**Step 4: Implement trivy backend**

`ScanContainer()`: runs `trivy image --format json <target>`, parses findings.
`ScanDeps()`: runs `trivy fs --format json <source>`, parses findings.

**Step 5: Implement grype backend**

`ScanDeps()`: runs `grype <source> -o json`, parses findings.

**Step 6: Implement mock backend**

Returns synthetic findings for testing. Configurable via mock_findings in config.

**Step 7: Add tests with mock backend**

Test all three scan methods with mock backend. Verify output format matches ScanResult.

**Step 8: Commit and tag**

---

### Task 4: Public Scenario 46 — GitHub CI/CD

**Files:**
- Create: `workflow-scenarios/scenarios/46-github-cicd/`
  - `scenario.yaml`, `config/app.yaml`, `k8s/deployment.yaml`, `k8s/service.yaml`, `test/run.sh`

**Step 1: Write scenario config**

```yaml
# scenario.yaml
name: github-cicd
description: GitHub CI/CD integration — webhook events, action triggers, status checks
version: "1.0"
plugins:
  - workflow-plugin-github
```

**Step 2: Write workflow config (app.yaml)**

Pipelines:
- `POST /api/webhooks/github` — receive webhook, validate signature, parse event, store in state
- `POST /api/actions/trigger` — trigger workflow dispatch (mock responds with run ID)
- `GET /api/actions/status/{run_id}` — check workflow run status
- `POST /api/checks/create` — create check run on commit

Use mock GitHub API (step.set to simulate responses since this runs without real GitHub).

**Step 3: Write test script**

Test cases with jq validation:
1. POST webhook with valid HMAC signature → 200, verify event type parsed
2. POST webhook with invalid signature → 401/403
3. POST webhook with filtered event (not in allowed list) → 200 with status:ignored
4. POST trigger action → verify run_id in response
5. GET action status → verify status and conclusion fields
6. POST create check → verify check_run_id in response
7. POST with missing required fields → proper error message

**Step 4: Commit**

---

### Task 5: Public Scenario 47 — Authz RBAC

**Files:**
- Create: `workflow-scenarios/scenarios/47-authz-rbac/`

**Step 1: Write workflow config**

Modules: `authz.casbin` with SQLite adapter (in-memory for tests).

Pipelines:
- `POST /api/policies` — add Casbin policy (step.authz_add_policy)
- `DELETE /api/policies` — remove policy (step.authz_remove_policy)
- `POST /api/roles/assign` — assign role to user (step.authz_role_assign)
- `POST /api/check` — check authorization (step.authz_check_casbin)
- `GET /api/protected/admin` — admin-only endpoint (step.authz_check_casbin → 403 if not admin)
- `GET /api/protected/viewer` — viewer endpoint (any role)

**Step 2: Write test script**

1. Check access before any policies → denied (403)
2. Add admin policy for user alice → 200
3. Assign admin role to alice → 200
4. Check alice admin access → allowed
5. Check bob admin access → denied (403)
6. Add viewer policy for bob → 200
7. Check bob viewer access → allowed
8. Remove alice admin policy → 200
9. Re-check alice admin access → denied (403)

**Step 3: Commit**

---

### Task 6: Public Scenario 48 — Payment Processing

**Files:**
- Create: `workflow-scenarios/scenarios/48-payment-processing/`

**Step 1: Write workflow config**

Modules: `payments.provider` with mock provider.

Pipelines:
- `POST /api/customers` — ensure customer (step.payment_customer_ensure)
- `POST /api/charges` — create charge (step.payment_charge)
- `POST /api/captures/{charge_id}` — capture charge (step.payment_capture)
- `POST /api/refunds/{charge_id}` — refund charge (step.payment_refund)
- `POST /api/subscriptions` — create subscription (step.payment_subscription_create)
- `DELETE /api/subscriptions/{sub_id}` — cancel subscription (step.payment_subscription_cancel)
- `POST /api/checkout` — create checkout session (step.payment_checkout_create)

**Step 2: Write test script**

1. Ensure customer → verify customer_id returned
2. Create charge (amount: 5000, currency: usd) → verify charge_id, status=pending
3. Capture charge → verify status=captured
4. Refund charge → verify status=refunded, refund_id returned
5. Create subscription → verify subscription_id, status=active
6. Cancel subscription → verify status=canceled
7. Create checkout → verify checkout_url returned
8. Charge with invalid amount → proper error

**Step 3: Commit**

---

### Task 7: Public Scenario 49 — Security Scanning

**Files:**
- Create: `workflow-scenarios/scenarios/49-security-scanning/`

**Step 1: Write workflow config**

Modules: `security.scanner` with mock backend.

Pipelines:
- `POST /api/scan/sast` — run SAST scan (step.scan_sast)
- `POST /api/scan/container` — run container scan (step.scan_container)
- `POST /api/scan/deps` — run dependency scan (step.scan_deps)

**Step 2: Write test script**

1. SAST scan with clean source → passed=true, findings=[]
2. SAST scan with vulnerable source → passed=false, findings array not empty
3. Container scan → verify summary has severity counts
4. Deps scan with fail_on_severity=critical → passes when no criticals
5. Deps scan with fail_on_severity=low → fails when low+ findings exist

**Step 3: Commit**

---

### Task 8: Create Private Scenarios Repository

**Files:**
- Create: `workflow-scenarios-private/` (new repo)

**Step 1: Create repo structure**

Mirror workflow-scenarios structure:
```
workflow-scenarios-private/
├── scenarios/
│   ├── 01-waf-protection/
│   ├── 02-mfa-encryption/
│   ├── 03-wasm-sandbox/
│   ├── 04-data-protection/
│   └── 05-supply-chain/
├── scripts/
│   ├── deploy.sh
│   └── test.sh
├── Makefile
├── scenarios.json
└── README.md
```

**Step 2: Create base infrastructure**

Copy deploy.sh, test.sh, Makefile from workflow-scenarios (adapted for private plugin images).

**Step 3: Commit**

---

### Task 9: Private Scenario 01 — WAF Protection

**Files:**
- Create: `workflow-scenarios-private/scenarios/01-waf-protection/`

**Step 1: Write workflow config**

Modules: `security.waf` (Coraza local mode).

Pipelines:
- `POST /api/sanitize` — input sanitization (step.input_sanitize)
- `POST /api/check-ip` — IP reputation check (step.ip_check)
- `POST /api/evaluate` — full WAF evaluation (step.waf_evaluate)
- `POST /api/submit` — form submission protected by WAF
- `GET /api/data` — data endpoint protected by WAF

**Step 2: Write test script**

1. Submit clean input → 200, sanitized output matches input
2. Submit XSS payload (`<script>alert(1)</script>`) → blocked (403)
3. Submit SQL injection (`' OR 1=1 --`) → blocked (403)
4. Check known-bad IP → blocked
5. Check clean IP → allowed
6. WAF evaluate with clean request → passed=true
7. WAF evaluate with malicious headers → blocked with rule ID

**Step 3: Commit**

---

### Task 10: Private Scenario 02 — MFA & Encryption

**Files:**
- Create: `workflow-scenarios-private/scenarios/02-mfa-encryption/`

**Step 1: Write workflow config**

Modules: `security.mfa` (TOTP), `security.encryption` (local AES-256-GCM).

Pipelines:
- `POST /api/mfa/enroll` — generate TOTP secret (step.mfa_enroll)
- `POST /api/mfa/verify` — verify TOTP code (step.mfa_verify)
- `POST /api/encrypt` — encrypt a value (step.encrypt_value)
- `POST /api/decrypt` — decrypt a value (step.decrypt_value)

**Step 2: Write test script**

1. Enroll MFA → verify secret_url and secret returned (otpauth:// format)
2. Verify with invalid code → rejected
3. Encrypt plaintext → ciphertext differs from plaintext, not empty
4. Decrypt ciphertext → matches original plaintext
5. Decrypt with wrong key/invalid ciphertext → error

**Step 3: Commit**

---

### Task 11: Private Scenario 03 — WASM Sandbox

**Files:**
- Create: `workflow-scenarios-private/scenarios/03-wasm-sandbox/`

**Step 1: Write workflow config**

Modules: `sandbox.wasm`.

Pipelines:
- `POST /api/exec/wasm` — execute WASM module (step.wasm_exec)
- `POST /api/exec/guarded` — guarded goroutine execution (step.goroutine_guard)

**Step 2: Write test script**

1. Execute simple WASM → verify output matches expected
2. Execute with resource limits → completes within limits
3. Goroutine guard with safe function → succeeds
4. Goroutine guard with timeout → properly terminated

**Step 3: Commit**

---

### Task 12: Private Scenario 04 — Data Protection

**Files:**
- Create: `workflow-scenarios-private/scenarios/04-data-protection/`

**Step 1: Write workflow config**

Modules: `data.pii` (local regex mode).

Pipelines:
- `POST /api/detect` — PII detection (step.pii_detect)
- `POST /api/mask` — data masking (step.data_mask)
- `POST /api/classify` — data classification (step.data_classify)

**Step 2: Write test script**

1. Detect PII in email → finds email type, count=1
2. Detect PII in SSN → finds ssn type
3. Detect PII in credit card → finds credit_card type, validates Luhn
4. Detect clean data → count=0
5. Mask with redact strategy → field shows [REDACTED]
6. Mask with partial strategy → shows last 4 chars
7. Mask with hash strategy → SHA-256 hash returned
8. Classify → fields with PII marked "restricted", clean fields "internal"

**Step 3: Commit**

---

### Task 13: Private Scenario 05 — Supply Chain

**Files:**
- Create: `workflow-scenarios-private/scenarios/05-supply-chain/`

**Step 1: Write workflow config**

Modules: `security.plugin-verifier`, scanner with mock mode.

Pipelines:
- `POST /api/verify` — verify plugin signature (step.verify_signature)
- `POST /api/scan` — vulnerability scan (step.vuln_scan)
- `POST /api/sbom/generate` — SBOM generation (step.sbom_generate)
- `POST /api/sbom/check` — SBOM policy check (step.sbom_check)

**Step 2: Write test script**

Since these need real CLI tools (trivy, grype, cosign), tests use mock mode:
1. Verify valid signature → verified=true
2. Verify tampered file → verified=false in enforce mode → pipeline stops
3. Vuln scan with mock findings → passed=false, findings count matches
4. Vuln scan clean → passed=true
5. SBOM check with denied package → violations found

**Step 3: Commit**

---

### Task 14: Wire Everything Together

**Step 1: Update workflow engine to register SecurityScannerProvider lookup**

In `engine.go`, ensure scan steps can find the provider from the service registry.

**Step 2: Update workflow-scenarios Makefile and scenarios.json**

Add entries for scenarios 46-49.

**Step 3: Run all tests**

```bash
# Core engine (workflow repo)
go test ./...

# Sandbox plugin (workflow-plugin-sandbox repo)
go test ./...

# Security scanner plugin (workflow-plugin-security-scanner repo)
go test ./...
```

**Step 4: Final commit and push all repos**
