# Codebase Audit & Stub Completion Design

## Goal

Complete all stubbed/incomplete code in the workflow engine, implement DockerSandbox for secure container execution, and build realistic test scenarios for plugins with zero coverage.

## Audit Summary

### Stubs Found in Core Engine (3 items)

| File | Step Type | Issue |
|------|-----------|-------|
| `module/pipeline_step_scan_sast.go` | step.scan_sast | Returns `ErrNotImplemented` |
| `module/pipeline_step_scan_container.go` | step.scan_container | Returns `ErrNotImplemented` |
| `module/pipeline_step_scan_deps.go` | step.scan_deps | Returns `ErrNotImplemented` |

**Root cause**: Blocked on `sandbox.DockerSandbox` which doesn't exist. These steps have scanning logic inline in the core engine — wrong architecture. Scanning should be delegated to plugins via a provider interface.

### External Plugins — All Fully Implemented

Deep audits confirmed all plugin code is production-ready:
- **workflow-plugin-supply-chain**: 4 steps + 6 modules (Trivy, Grype, Snyk, ECR, GCP)
- **workflow-plugin-data-protection**: 3 steps + 4 modules (regex, GCP DLP, AWS Macie, Presidio)
- **workflow-plugin-github**: 3 steps + 1 module (41 tests, real GitHub API client)
- All other plugins: bento, authz, payments, waf, security, sandbox — fully implemented

### Missing: DockerSandbox

`workflow-plugin-sandbox` provides WASM execution + goroutine guards but no Docker container isolation. A `sandbox.docker` module is needed for secure container execution (used by CI/CD steps, scan steps, etc.).

### Scenario Coverage Gaps

8 plugins with zero scenario coverage: authz, payments, github, waf, security, sandbox, supply-chain, data-protection.

## Architecture

### Part 1A: Security Scanner Provider Interface

The core engine defines a `SecurityScannerProvider` interface. Scan steps delegate to whichever plugin registers as provider.

```go
// In module/scan_provider.go
type SecurityScannerProvider interface {
    // ScanSAST runs static analysis on source code.
    ScanSAST(ctx context.Context, opts SASTScanOpts) (*ScanResult, error)
    // ScanContainer scans a container image for vulnerabilities.
    ScanContainer(ctx context.Context, opts ContainerScanOpts) (*ScanResult, error)
    // ScanDeps scans dependencies for known vulnerabilities.
    ScanDeps(ctx context.Context, opts DepsScanOpts) (*ScanResult, error)
}

type ScanResult struct {
    Passed       bool
    Findings     []ScanFinding
    Summary      map[string]int  // severity -> count
    OutputFormat string          // sarif, json, table
    RawOutput    string
}

type ScanFinding struct {
    ID          string  // CVE-2024-1234
    Severity    string  // critical, high, medium, low, info
    Title       string
    Description string
    Package     string
    Version     string
    FixVersion  string
    Location    string  // file path or image layer
}
```

The 3 scan steps become thin wrappers:
1. Look up `SecurityScannerProvider` from service registry
2. Call the appropriate method
3. Evaluate severity gate (fail_on_severity)
4. Return structured results

### Part 1B: DockerSandbox Module

Add `sandbox.docker` to the existing `workflow-plugin-sandbox`. Provides secure container execution:

```yaml
modules:
  - name: docker-sandbox
    type: sandbox.docker
    config:
      maxCPU: "1.0"           # CPU limit
      maxMemory: "512m"       # Memory limit
      networkMode: "none"     # No network by default
      readOnlyRootfs: true    # Immutable filesystem
      noPrivileged: true      # Never allow --privileged
      allowedImages:          # Whitelist of allowed images
        - "semgrep/semgrep:*"
        - "aquasec/trivy:*"
        - "anchore/grype:*"
      timeout: "5m"           # Max execution time
```

Interface:
```go
type DockerSandbox interface {
    Run(ctx context.Context, opts DockerRunOpts) (*DockerRunResult, error)
}

type DockerRunOpts struct {
    Image      string
    Command    []string
    Env        map[string]string
    Mounts     []Mount          // Read-only bind mounts
    WorkDir    string
    NetworkMode string          // "none", "bridge" (not "host")
}
```

Uses Docker Engine API client (`github.com/docker/docker/client`), NOT `os/exec` with `docker run` (which can be shell-injected).

### Part 1C: Security Scanner Plugin

Create `workflow-plugin-security-scanner` (public, Apache-2.0) that:
1. Implements `SecurityScannerProvider` interface
2. Provides `security.scanner` module type
3. Supports backends: semgrep (SAST), trivy (container + deps), grype (deps)
4. Optionally uses DockerSandbox for isolated execution when available
5. Falls back to direct CLI execution when DockerSandbox isn't configured
6. Includes `mock` mode for testing without real tools

### Part 2: Public Scenarios (workflow-scenarios)

| # | Scenario | Plugin | Tests | Verification |
|---|----------|--------|-------|-------------|
| 46 | github-cicd | workflow-plugin-github | Webhook HMAC validation, action trigger/status, check runs | Verify payload parsing, signature validation, event filtering |
| 47 | authz-rbac | workflow-plugin-authz | Casbin policy CRUD, role enforcement, deny access | Verify policy creates, role assigns, access denied returns 403 |
| 48 | payment-processing | workflow-plugin-payments | Charge, capture, refund, subscription lifecycle | Verify amounts, status transitions, webhook handling |
| 49 | security-scanning | Core + scanner plugin | SAST, container scan, dependency scan | Verify findings count, severity filtering, pass/fail gate |

### Part 3: Private Scenarios (workflow-scenarios-private)

| # | Scenario | Plugin | Tests | Verification |
|---|----------|--------|-------|-------------|
| 01 | waf-protection | workflow-plugin-waf | Input sanitization, IP check, WAF evaluate | Verify blocked requests return 403, clean requests pass |
| 02 | mfa-encryption | workflow-plugin-security | TOTP enroll/verify, AES encrypt/decrypt | Verify TOTP codes validate, encrypted != plaintext, decrypt == original |
| 03 | wasm-sandbox | workflow-plugin-sandbox | WASM exec, goroutine guards | Verify WASM output, resource limits enforced |
| 04 | data-protection | workflow-plugin-data-protection | PII detect, data mask, classify | Verify PII found in test data, masked values differ, classifications correct |
| 05 | supply-chain | workflow-plugin-supply-chain | Signature verify, vuln scan, SBOM | Verify signature validation, finding counts, SBOM component counts |

### Scenario Design Principles

Every test script uses `jq` for JSON validation:
```bash
# Good: verify specific field values
RESULT=$(curl -s "$BASE_URL/api/scan" -d '{"target":"test-image:v1"}')
PASSED=$(echo "$RESULT" | jq -r '.passed')
SEVERITY=$(echo "$RESULT" | jq -r '.summary.critical')
[ "$PASSED" = "false" ] && [ "$SEVERITY" -gt 0 ] && echo "PASS: scan detected critical vulns" || echo "FAIL: expected critical findings"

# Bad: just check HTTP status
curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/api/scan" | grep -q "200" && echo "PASS"
```

Tests must verify:
1. **Data transforms** — output values match expected transformations
2. **State changes** — persistence confirmed by reading back after writing
3. **Enforcement** — denied/blocked requests fail with proper error codes (403, 400)
4. **Error paths** — invalid inputs return descriptive error messages

## Implementation Order

1. **Part 1A**: SecurityScannerProvider interface in core engine
2. **Part 1B**: DockerSandbox module in workflow-plugin-sandbox
3. **Part 1C**: Security scanner plugin implementing the provider
4. **Part 1 wiring**: Update core scan steps to delegate to provider
5. **Part 2**: Public scenarios 46-49
6. **Part 3**: Private scenarios repo + scenarios 01-05

## Out of Scope

- Real cloud API integration tests (need credentials)
- `cache.modular` interface gap (needs modular framework changes)
- Phase 5 architecture refactoring (separate effort)
- Documentation updates (separate effort)
