#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
AUDIT_SCRIPT="$SCRIPT_DIR/audit-cloud-symbols.sh"
TMP_ROOT=$(mktemp -d)
trap 'rm -rf "$TMP_ROOT"' EXIT

fail() {
  echo "test-audit-cloud-symbols: FAIL: $*" >&2
  exit 1
}

assert_contains() {
  local output=$1
  local expected=$2
  local label=$3
  [[ "$output" == *"$expected"* ]] || fail "$label: expected output containing '$expected', got:\n$output"
}

write_allowed_fixture() {
  local root=$1
  mkdir -p "$root/module"
  cat >"$root/module/platform_kubernetes_core.go" <<'EOF'
package module

func init() {
	RegisterKubernetesBackend("kind", nil)
	RegisterKubernetesBackend("k3s", nil)
	RegisterKubernetesBackend("eks", nil)
	RegisterKubernetesBackend("aks", nil)
}
EOF
}

run_audit() {
  local root=$1
  set +e
  AUDIT_OUTPUT=$(WORKFLOW_ROOT="$root" "$AUDIT_SCRIPT" --check 2>&1)
  AUDIT_STATUS=$?
  set -e
}

expect_failure() {
  local label=$1
  local root=$2
  local expected=$3
  run_audit "$root"
  [[ $AUDIT_STATUS -ne 0 ]] || fail "$label: expected non-zero exit, got 0:\n$AUDIT_OUTPUT"
  assert_contains "$AUDIT_OUTPUT" "$expected" "$label"
  assert_contains "$AUDIT_OUTPUT" "audit-cloud-symbols: FAIL" "$label"
}

gke="$TMP_ROOT/gke"
write_allowed_fixture "$gke"
cat >"$gke/module/provider_backend.go" <<'EOF'
package module

func registerProviderBackend() {
	RegisterKubernetesBackend("gke", nil)
}
EOF
expect_failure "provider-specific registration" "$gke" 'backend "gke" is not framework-owned'

arbitrary="$TMP_ROOT/arbitrary"
write_allowed_fixture "$arbitrary"
cat >"$arbitrary/module/provider_backend.go" <<'EOF'
package module

func registerProviderBackend() {
	RegisterKubernetesBackend("managed-cloud", nil)
}
EOF
expect_failure "arbitrary registration" "$arbitrary" 'backend "managed-cloud" is not framework-owned'

dynamic="$TMP_ROOT/dynamic"
write_allowed_fixture "$dynamic"
cat >"$dynamic/module/provider_backend.go" <<'EOF'
package module

func registerProviderBackend(name string) {
	RegisterKubernetesBackend(name, nil)
}
EOF
expect_failure "dynamic registration" "$dynamic" "registration must use an explicit string literal"

resurrected="$TMP_ROOT/resurrected"
write_allowed_fixture "$resurrected"
cat >"$resurrected/module/platform_kubernetes_gke.go" <<'EOF'
package module
EOF
expect_failure "deleted GKE file resurrection" "$resurrected" "deleted module/platform_kubernetes_gke.go exists"

allowed="$TMP_ROOT/allowed"
write_allowed_fixture "$allowed"
run_audit "$allowed"
[[ $AUDIT_STATUS -eq 0 ]] || fail "allowed registrations: expected exit 0, got $AUDIT_STATUS:\n$AUDIT_OUTPUT"
assert_contains "$AUDIT_OUTPUT" "Kubernetes backend boundary" "allowed registrations"
assert_contains "$AUDIT_OUTPUT" "registrations: kind k3s eks aks" "allowed registrations"
assert_contains "$AUDIT_OUTPUT" "audit-cloud-symbols: OK" "allowed registrations"

echo "test-audit-cloud-symbols: OK"
