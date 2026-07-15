#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
TMP_ROOT=$(mktemp -d)
trap 'rm -rf "$TMP_ROOT"' EXIT
TEST_FAILURES=0
K8S_AUDIT_BIN="$TMP_ROOT/kubernetes-boundary-audit"

fail() {
  echo "test-audit-cloud-symbols: FAIL: $*" >&2
  exit 1
}

record_failure() {
  echo "test-audit-cloud-symbols: FAIL: $*" >&2
  TEST_FAILURES=$((TEST_FAILURES + 1))
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
  cat >"$root/module/platform_kubernetes.go" <<'EOF'
package module

var kubernetesBackendRegistry = map[string]any{}

func RegisterKubernetesBackend(name string, factory any) { kubernetesBackendRegistry[name] = factory }
EOF
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

remove_registration() {
  local root=$1
  local name=$2
  local file="$root/module/platform_kubernetes_core.go"
  grep -v "RegisterKubernetesBackend(\"$name\"" "$file" >"$file.tmp"
  mv "$file.tmp" "$file"
}

run_audit() {
  local root=$1
  set +e
  AUDIT_OUTPUT=$("$K8S_AUDIT_BIN" --fixture-root "$root" 2>&1)
  AUDIT_STATUS=$?
  set -e
}

run_production_audit() {
  local root=$1
  set +e
  AUDIT_OUTPUT=$("$K8S_AUDIT_BIN" --root "$root" 2>&1)
  AUDIT_STATUS=$?
  set -e
}

expect_failure() {
  local label=$1
  local root=$2
  local expected=$3
  run_audit "$root"
  if [[ $AUDIT_STATUS -eq 0 ]]; then
    record_failure "$label: expected non-zero exit, got 0:\n$AUDIT_OUTPUT"
    return
  fi
  [[ "$AUDIT_OUTPUT" == *"$expected"* ]] || record_failure "$label: expected output containing '$expected', got:\n$AUDIT_OUTPUT"
  [[ "$AUDIT_OUTPUT" == *"kubernetes-boundary-audit: FAIL"* ]] || record_failure "$label: expected audit failure footer, got:\n$AUDIT_OUTPUT"
}

expect_success() {
  local label=$1
  local root=$2
  run_audit "$root"
  if [[ $AUDIT_STATUS -ne 0 ]]; then
    record_failure "$label: expected exit 0, got $AUDIT_STATUS:\n$AUDIT_OUTPUT"
    return
  fi
  [[ "$AUDIT_OUTPUT" == *"kubernetes-boundary-audit: OK"* ]] || record_failure "$label: expected audit success footer, got:\n$AUDIT_OUTPUT"
}

expect_production_failure() {
  local label=$1
  local root=$2
  local expected=$3
  run_production_audit "$root"
  if [[ $AUDIT_STATUS -eq 0 ]]; then
    record_failure "$label: expected non-zero exit, got 0:\n$AUDIT_OUTPUT"
    return
  fi
  [[ "$AUDIT_OUTPUT" == *"$expected"* ]] || record_failure "$label: expected output containing '$expected', got:\n$AUDIT_OUTPUT"
  [[ "$AUDIT_OUTPUT" == *"kubernetes-boundary-audit: FAIL"* ]] || record_failure "$label: expected audit failure footer, got:\n$AUDIT_OUTPUT"
}

(cd "$SCRIPT_DIR/.." && GOWORK=off go build -o "$K8S_AUDIT_BIN" ./scripts/kubernetes-boundary-audit) || fail "build Kubernetes boundary audit helper"

for bypass in comment-separated parenthesized alias; do
  bypass_root="$TMP_ROOT/bypass-$bypass"
  write_allowed_fixture "$bypass_root"
  case "$bypass" in
    comment-separated)
      hidden_call='RegisterKubernetesBackend /* hidden */ ("gke", nil)'
      ;;
    parenthesized)
      hidden_call='(RegisterKubernetesBackend)("gke", nil)'
      ;;
    alias)
      hidden_call=$'register := RegisterKubernetesBackend\n\tregister("gke", nil)'
      ;;
  esac
  {
    printf '\nfunc registerHiddenBackend() {\n\t%s\n}\n' "$hidden_call"
  } >>"$bypass_root/module/platform_kubernetes_core.go"
  expect_failure "$bypass RegisterKubernetesBackend reference" "$bypass_root" "RegisterKubernetesBackend"
done

moved_function="$TMP_ROOT/moved-function"
write_allowed_fixture "$moved_function"
grep -v '^func RegisterKubernetesBackend' "$moved_function/module/platform_kubernetes.go" >"$moved_function/module/platform_kubernetes.go.tmp"
mv "$moved_function/module/platform_kubernetes.go.tmp" "$moved_function/module/platform_kubernetes.go"
cat >"$moved_function/module/provider_backend.go" <<'EOF'
package module

func RegisterKubernetesBackend(name string, factory any) { kubernetesBackendRegistry[name] = factory }
EOF
expect_failure "moved RegisterKubernetesBackend declaration" "$moved_function" "RegisterKubernetesBackend declaration must be in module/platform_kubernetes.go"

moved_registry="$TMP_ROOT/moved-registry"
write_allowed_fixture "$moved_registry"
grep -v '^var kubernetesBackendRegistry' "$moved_registry/module/platform_kubernetes.go" >"$moved_registry/module/platform_kubernetes.go.tmp"
mv "$moved_registry/module/platform_kubernetes.go.tmp" "$moved_registry/module/platform_kubernetes.go"
cat >"$moved_registry/module/provider_backend.go" <<'EOF'
package module

var kubernetesBackendRegistry = map[string]any{}
EOF
expect_failure "moved kubernetesBackendRegistry declaration" "$moved_registry" "kubernetesBackendRegistry declaration must be in module/platform_kubernetes.go"

registry_write="$TMP_ROOT/registry-write"
write_allowed_fixture "$registry_write"
cat >"$registry_write/module/provider_backend.go" <<'EOF'
package module

func registerProviderBackendDirectly() {
	kubernetesBackendRegistry["gke"] = nil
}
EOF
expect_failure "noncanonical registry write" "$registry_write" "kubernetesBackendRegistry write must remain in RegisterKubernetesBackend"

lexical_noise="$TMP_ROOT/lexical-noise"
write_allowed_fixture "$lexical_noise"
cat >"$lexical_noise/module/lexical_noise.go" <<'EOF'
package module

// RegisterKubernetesBackend("gke", nil)
/* RegisterKubernetesBackend("managed-cloud", nil) */
const quotedRegistration = "RegisterKubernetesBackend(\"gke\", nil)"
const rawRegistration = `RegisterKubernetesBackend("managed-cloud", nil)`
EOF
expect_success "comments and strings are not registrations" "$lexical_noise"

lookalike="$TMP_ROOT/lookalike"
write_allowed_fixture "$lookalike"
cat >"$lookalike/go.mod" <<'EOF'
module example.com/workflow-lookalike

go 1.26.5
EOF
touch "$lookalike/.phase-b-complete" "$lookalike/.phase-c-complete"
expect_production_failure "lookalike production root" "$lookalike" "module identity must be github.com/GoCodeAlone/workflow"

for missing_marker in .phase-b-complete .phase-c-complete; do
  missing_marker_root="$TMP_ROOT/missing-${missing_marker#.}"
  write_allowed_fixture "$missing_marker_root"
  cat >"$missing_marker_root/go.mod" <<'EOF'
module github.com/GoCodeAlone/workflow

go 1.26.5
EOF
  touch "$missing_marker_root/.phase-b-complete" "$missing_marker_root/.phase-c-complete"
  rm "$missing_marker_root/$missing_marker"
  expect_production_failure "missing production marker $missing_marker" "$missing_marker_root" "missing committed phase marker $missing_marker"
done

wrong_root="$TMP_ROOT/wrong-root"
mkdir -p "$wrong_root"
expect_failure "wrong Workflow root" "$wrong_root" "missing canonical Kubernetes registration file module/platform_kubernetes.go"

for missing_file in platform_kubernetes.go platform_kubernetes_core.go; do
  missing_file_root="$TMP_ROOT/missing-${missing_file%.go}"
  write_allowed_fixture "$missing_file_root"
  rm "$missing_file_root/module/$missing_file"
  expect_failure "missing canonical $missing_file" "$missing_file_root" "missing canonical Kubernetes registration file module/$missing_file"
done

for missing_name in kind k3s eks aks; do
  missing_name_root="$TMP_ROOT/missing-$missing_name"
  write_allowed_fixture "$missing_name_root"
  remove_registration "$missing_name_root" "$missing_name"
  expect_failure "missing $missing_name registration" "$missing_name_root" "missing required Kubernetes backend registration \"$missing_name\""
done

mislocated="$TMP_ROOT/mislocated"
write_allowed_fixture "$mislocated"
remove_registration "$mislocated" eks
cat >"$mislocated/module/provider_backend.go" <<'EOF'
package module

func registerProviderBackend() {
	RegisterKubernetesBackend("eks", nil)
}
EOF
expect_failure "mislocated allowed registration" "$mislocated" 'RegisterKubernetesBackend call must be in module/platform_kubernetes_core.go'

duplicate="$TMP_ROOT/duplicate"
write_allowed_fixture "$duplicate"
cat >>"$duplicate/module/platform_kubernetes_core.go" <<'EOF'

func registerDuplicateBackend() {
	RegisterKubernetesBackend("kind", nil)
}
EOF
expect_failure "duplicate registration" "$duplicate" 'duplicate Kubernetes backend registration "kind"'

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
expect_failure "dynamic registration" "$dynamic" "RegisterKubernetesBackend first argument must be an explicit string literal"

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
assert_contains "$AUDIT_OUTPUT" "kubernetes-boundary-audit: OK" "allowed registrations"

[[ $TEST_FAILURES -eq 0 ]] || exit 1
echo "test-audit-cloud-symbols: OK"
