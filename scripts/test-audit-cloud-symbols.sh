#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
TMP_ROOT=$(mktemp -d)
trap 'rm -rf "$TMP_ROOT"' EXIT
TEST_FAILURES=0
K8S_AUDIT_BIN="$TMP_ROOT/kubernetes-boundary-audit"
SYMLINK_HELPER_BIN="$TMP_ROOT/create-symlink"

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

type kubernetesBackend interface{}

type KubernetesBackendFactory func(map[string]any) (kubernetesBackend, error)

type CloudCredentialProvider interface{}

type KubernetesClusterState struct {
	Name string
	Provider string
	Version string
	Status string
}

var kubernetesBackendRegistry = map[string]KubernetesBackendFactory{}

func RegisterKubernetesBackend(clusterType string, factory KubernetesBackendFactory) { kubernetesBackendRegistry[clusterType] = factory }

type PlatformKubernetes struct {
	name string
	config map[string]any
	provider CloudCredentialProvider
	state *KubernetesClusterState
	backend kubernetesBackend
}

type KubernetesBackendBinding struct {
	Name string
	ResourceType string
	Client any
}

func (m *PlatformKubernetes) Init(app any) error {
	accountName, _ := m.config["account"].(string)
	if accountName != "" {
		svc, ok := app.SvcRegistry()[accountName]
		if !ok {
			return fmt.Errorf("account service not found")
		}
		provider, ok := svc.(CloudCredentialProvider)
		if !ok {
			return fmt.Errorf("account service has wrong type")
		}
		m.provider = provider
	}

	clusterType, _ := m.config["type"].(string)
	if clusterType == "" {
		clusterType = "kind"
	}

	if isReservedKubernetesBackendType(clusterType) {
		factory, ok := kubernetesBackendRegistry[clusterType]
		if !ok {
			return fmt.Errorf("platform.kubernetes %q: unsupported type %q", m.name, clusterType)
		}
		backend, err := factory(m.config)
		if err != nil {
			return err
		}
		m.backend = backend
	} else {
		binding, scoped, err := resolveApplicationKubernetesBackend(app, clusterType)
		if err != nil {
			return err
		}
		if !scoped {
			binding, _ = kubernetesBackendClientRegistryInstance.resolve(clusterType)
		}
		if binding.Client != nil {
			m.backend = newGRPCKubernetesBackend(binding.Name, binding.ResourceType, binding.Client)
		} else if factory, ok := kubernetesBackendRegistry[clusterType]; ok {
			backend, createErr := factory(m.config)
			if createErr != nil {
				return createErr
			}
			m.backend = backend
		} else {
			return fmt.Errorf("platform.kubernetes %q: cluster type %q is not built into workflow core "+
				"(in-core types: 'kind', 'k3s'; compatibility fallbacks: 'eks', 'aks'). If %q is a "+
				"plugin-provided backend, install and load the plugin that declares it",
				m.name, clusterType, clusterType)
		}
	}

	version, _ := m.config["version"].(string)
	m.state = &KubernetesClusterState{
		Name: m.name,
		Provider: clusterType,
		Version: version,
		Status: "pending",
	}
	return app.RegisterService(m.name, m)
}
EOF
  cat >"$root/module/platform_kubernetes_plugin_registry.go" <<'EOF'
package module

func isReservedKubernetesBackendType(name string) bool {
	switch name {
	case "kind", "k3s":
		return true
	default:
		return false
	}
}

func normalizeKubernetesBackendRegistration(owner string, bindings []KubernetesBackendBinding) (string, map[string]KubernetesBackendBinding, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return "", nil, fmt.Errorf("kubernetes backend registration: owner must not be empty")
	}
	normalized := make(map[string]KubernetesBackendBinding, len(bindings))
	for _, binding := range bindings {
		name := strings.TrimSpace(binding.Name)
		if name == "" {
			return "", nil, nil
		}
		if isReservedKubernetesBackendType(name) {
			return "", nil, fmt.Errorf("plugin registered reserved kubernetes backend type %q", name)
		}
		normalized[name] = binding
	}
	return owner, normalized, nil
}
EOF
  cat >"$root/module/platform_kubernetes_core.go" <<'EOF'
package module

type kindBackend struct{}
type eksErrorBackend struct{}
type aksBackend struct{}

func init() {
	RegisterKubernetesBackend("kind", func(_ map[string]any) (kubernetesBackend, error) {
		return &kindBackend{}, nil
	})
	RegisterKubernetesBackend("k3s", func(_ map[string]any) (kubernetesBackend, error) {
		return &kindBackend{}, nil
	})
	RegisterKubernetesBackend("eks", func(_ map[string]any) (kubernetesBackend, error) {
		return &eksErrorBackend{}, nil
	})
	RegisterKubernetesBackend("aks", func(_ map[string]any) (kubernetesBackend, error) {
		return &aksBackend{}, nil
	})
}
EOF
}

make_symlink_or_skip() {
  local target=$1
  local link=$2
  local label=$3
  local helper_status
  local error_output
  set +e
  error_output=$("$SYMLINK_HELPER_BIN" "$target" "$link" 2>&1)
  helper_status=$?
  set -e
  case $helper_status in
    0)
      return 0
      ;;
    77)
      echo "test-audit-cloud-symbols: SKIP: $label: $error_output" >&2
      return 1
      ;;
    *)
      fail "$label: create symlink $link -> $target: $error_output"
      ;;
  esac
}

remove_registration() {
  local root=$1
  local name=$2
  local file="$root/module/platform_kubernetes_core.go"
  awk -v name="$name" '
    $0 ~ "RegisterKubernetesBackend\\(\\\"" name "\\\"" { skip=1; next }
    skip && /^\t}\)$/ { skip=0; next }
    !skip { print }
  ' "$file" >"$file.tmp"
  mv "$file.tmp" "$file"
}

replace_kind_factory_with_expression() {
  local root=$1
  local expression=$2
  local file="$root/module/platform_kubernetes_core.go"
  awk -v expression="$expression" '
    /RegisterKubernetesBackend\("kind"/ {
      print "\tRegisterKubernetesBackend(\"kind\", " expression ")"
      skip=1
      next
    }
    skip && /^\t}\)$/ { skip=0; next }
    !skip { print }
  ' "$file" >"$file.tmp"
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

cat >"$TMP_ROOT/create-symlink.go" <<'EOF'
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "create-symlink requires target and link arguments")
		os.Exit(2)
	}
	if err := os.Symlink(os.Args[1], os.Args[2]); err != nil {
		if errors.Is(err, os.ErrPermission) {
			fmt.Fprintf(os.Stderr, "symlink creation not permitted: %v\n", err)
			os.Exit(77)
		}
		fmt.Fprintf(os.Stderr, "create symlink: %v\n", err)
		os.Exit(1)
	}
}
EOF

(cd "$SCRIPT_DIR/.." && GOWORK=off go build -o "$K8S_AUDIT_BIN" ./scripts/kubernetes-boundary-audit) || fail "build Kubernetes boundary audit helper"
GOWORK=off go build -o "$SYMLINK_HELPER_BIN" "$TMP_ROOT/create-symlink.go" || fail "build symlink test helper"

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

func RegisterKubernetesBackend(clusterType string, factory KubernetesBackendFactory) { kubernetesBackendRegistry[clusterType] = factory }
EOF
expect_failure "moved RegisterKubernetesBackend declaration" "$moved_function" "RegisterKubernetesBackend declaration must be in module/platform_kubernetes.go"

moved_registry="$TMP_ROOT/moved-registry"
write_allowed_fixture "$moved_registry"
grep -v '^var kubernetesBackendRegistry' "$moved_registry/module/platform_kubernetes.go" >"$moved_registry/module/platform_kubernetes.go.tmp"
mv "$moved_registry/module/platform_kubernetes.go.tmp" "$moved_registry/module/platform_kubernetes.go"
cat >"$moved_registry/module/provider_backend.go" <<'EOF'
package module

var kubernetesBackendRegistry = map[string]KubernetesBackendFactory{}
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

registry_initializer="$TMP_ROOT/registry-initializer"
write_allowed_fixture "$registry_initializer"
sed 's/map\[string\]KubernetesBackendFactory{}/map[string]KubernetesBackendFactory{"gke": nil}/' "$registry_initializer/module/platform_kubernetes.go" >"$registry_initializer/module/platform_kubernetes.go.tmp"
mv "$registry_initializer/module/platform_kubernetes.go.tmp" "$registry_initializer/module/platform_kubernetes.go"
expect_failure "provider registry initializer entry" "$registry_initializer" "kubernetesBackendRegistry must initialize an empty map literal"

registry_shape="$TMP_ROOT/registry-shape"
write_allowed_fixture "$registry_shape"
sed 's/map\[string\]KubernetesBackendFactory{}/map[string]any{}/' "$registry_shape/module/platform_kubernetes.go" >"$registry_shape/module/platform_kubernetes.go.tmp"
mv "$registry_shape/module/platform_kubernetes.go.tmp" "$registry_shape/module/platform_kubernetes.go"
expect_failure "alternate registry map value type" "$registry_shape" "kubernetesBackendRegistry must initialize an empty map literal"

for assignment_mutation in hard-coded-key substituted-rhs; do
  assignment_root="$TMP_ROOT/assignment-$assignment_mutation"
  write_allowed_fixture "$assignment_root"
  case "$assignment_mutation" in
    hard-coded-key)
      replacement='kubernetesBackendRegistry["gke"] = factory'
      ;;
    substituted-rhs)
      replacement='kubernetesBackendRegistry[clusterType] = providerFactory'
      ;;
  esac
  sed "s/kubernetesBackendRegistry\[clusterType\] = factory/$replacement/" "$assignment_root/module/platform_kubernetes.go" >"$assignment_root/module/platform_kubernetes.go.tmp"
  mv "$assignment_root/module/platform_kubernetes.go.tmp" "$assignment_root/module/platform_kubernetes.go"
  expect_failure "$assignment_mutation canonical registry assignment" "$assignment_root" "RegisterKubernetesBackend must directly assign kubernetesBackendRegistry[clusterType] = factory"
done

canonical_registry_escape="$TMP_ROOT/canonical-registry-escape"
write_allowed_fixture "$canonical_registry_escape"
cat >>"$canonical_registry_escape/module/platform_kubernetes.go" <<'EOF'

func mutateRegistryWithoutRegistration() {
	alias := kubernetesBackendRegistry
	_ = alias
	kubernetesBackendRegistry["gke"]++
}
EOF
expect_failure "canonical registry alias/read/write escape" "$canonical_registry_escape" "kubernetesBackendRegistry reference is only permitted in its declaration and RegisterKubernetesBackend write"

factory_identifier="$TMP_ROOT/factory-identifier"
write_allowed_fixture "$factory_identifier"
replace_kind_factory_with_expression "$factory_identifier" "providerFactory"
expect_failure "provider factory identifier" "$factory_identifier" 'backend "kind" factory must be the canonical kindBackend function literal'

factory_nil="$TMP_ROOT/factory-nil"
write_allowed_fixture "$factory_nil"
replace_kind_factory_with_expression "$factory_nil" "nil"
expect_failure "nil backend factory" "$factory_nil" 'backend "kind" factory must be the canonical kindBackend function literal'

factory_wrapper="$TMP_ROOT/factory-wrapper"
write_allowed_fixture "$factory_wrapper"
awk '
  /RegisterKubernetesBackend\("kind"/ { sub(/, func/, ", (func"); wrapping=1 }
  wrapping && /^\t}\)$/ { print "\t}))"; wrapping=0; next }
  { print }
' "$factory_wrapper/module/platform_kubernetes_core.go" >"$factory_wrapper/module/platform_kubernetes_core.go.tmp"
mv "$factory_wrapper/module/platform_kubernetes_core.go.tmp" "$factory_wrapper/module/platform_kubernetes_core.go"
expect_failure "wrapped backend factory" "$factory_wrapper" 'backend "kind" factory must be the canonical kindBackend function literal'

for factory_shape in parameter result body; do
  factory_shape_root="$TMP_ROOT/factory-shape-$factory_shape"
  write_allowed_fixture "$factory_shape_root"
  awk -v shape="$factory_shape" '
    /RegisterKubernetesBackend\("kind"/ { in_kind=1 }
    in_kind && shape == "parameter" { sub(/map\[string\]any/, "map[string]string"); shape="done" }
    in_kind && shape == "result" { sub(/\(kubernetesBackend, error\)/, "kubernetesBackend"); shape="done" }
    in_kind && shape == "body" && /return &kindBackend{}/ { print "\t\t_ = 1"; shape="done" }
    { print }
    in_kind && /^\t}\)$/ { in_kind=0 }
  ' "$factory_shape_root/module/platform_kubernetes_core.go" >"$factory_shape_root/module/platform_kubernetes_core.go.tmp"
  mv "$factory_shape_root/module/platform_kubernetes_core.go.tmp" "$factory_shape_root/module/platform_kubernetes_core.go"
  expect_failure "$factory_shape backend factory shape" "$factory_shape_root" 'backend "kind" factory must be the canonical kindBackend function literal'
done

factory_swap="$TMP_ROOT/factory-swap"
write_allowed_fixture "$factory_swap"
awk '
  !swapped && /return &kindBackend{}/ { sub(/kindBackend/, "eksErrorBackend"); swapped=1 }
  { print }
' "$factory_swap/module/platform_kubernetes_core.go" >"$factory_swap/module/platform_kubernetes_core.go.tmp"
mv "$factory_swap/module/platform_kubernetes_core.go.tmp" "$factory_swap/module/platform_kubernetes_core.go"
expect_failure "allowed-name backend factory swap" "$factory_swap" 'backend "kind" factory must be the canonical kindBackend function literal'

dummy_reads="$TMP_ROOT/dummy-reads"
write_allowed_fixture "$dummy_reads"
awk '
  /^func \(m \*PlatformKubernetes\) Init/ {
    print "func (m *PlatformKubernetes) Init(clusterType string) error {"
    print "\t_, _ = kubernetesBackendRegistry[clusterType]"
    print "\t_, _ = kubernetesBackendRegistry[clusterType]"
    print "\treturn nil"
    print "}"
    exit
  }
  { print }
' "$dummy_reads/module/platform_kubernetes.go" >"$dummy_reads/module/platform_kubernetes.go.tmp"
mv "$dummy_reads/module/platform_kubernetes.go.tmp" "$dummy_reads/module/platform_kubernetes.go"
expect_failure "dummy registry reads" "$dummy_reads" "must preserve the core-local Kubernetes backend lookup and initialization branch"

routing_replacement="$TMP_ROOT/routing-replacement"
write_allowed_fixture "$routing_replacement"
sed 's/isReservedKubernetesBackendType(clusterType)/isProviderKubernetesBackendType(clusterType)/' "$routing_replacement/module/platform_kubernetes.go" >"$routing_replacement/module/platform_kubernetes.go.tmp"
mv "$routing_replacement/module/platform_kubernetes.go.tmp" "$routing_replacement/module/platform_kubernetes.go"
expect_failure "core-local routing replacement" "$routing_replacement" "must preserve the core-local Kubernetes backend lookup and initialization branch"

extra_registry_read="$TMP_ROOT/extra-registry-read"
write_allowed_fixture "$extra_registry_read"
awk '
  /^\tif isReservedKubernetesBackendType/ { print "\t_, _ = kubernetesBackendRegistry[clusterType]" }
  { print }
' "$extra_registry_read/module/platform_kubernetes.go" >"$extra_registry_read/module/platform_kubernetes.go.tmp"
mv "$extra_registry_read/module/platform_kubernetes.go.tmp" "$extra_registry_read/module/platform_kubernetes.go"
expect_failure "extra registry read" "$extra_registry_read" "expected exactly two kubernetesBackendRegistry lookups in (*PlatformKubernetes).Init, found 3"

moved_fallback="$TMP_ROOT/moved-fallback"
write_allowed_fixture "$moved_fallback"
awk '
  /} else if factory, ok := kubernetesBackendRegistry\[clusterType\]; ok {/ {
    print "\t\t}"
    sub(/} else if/, "if")
  }
  { print }
' "$moved_fallback/module/platform_kubernetes.go" >"$moved_fallback/module/platform_kubernetes.go.tmp"
mv "$moved_fallback/module/platform_kubernetes.go.tmp" "$moved_fallback/module/platform_kubernetes.go"
expect_failure "moved compatibility registry lookup" "$moved_fallback" "must preserve the provider-first compatibility fallback lookup and initialization branch"

earlier_provider_route="$TMP_ROOT/earlier-provider-route"
write_allowed_fixture "$earlier_provider_route"
sed 's/^\tif isReservedKubernetesBackendType/\tif providerRoute { return nil }\
\tif isReservedKubernetesBackendType/' "$earlier_provider_route/module/platform_kubernetes.go" >"$earlier_provider_route/module/platform_kubernetes.go.tmp"
mv "$earlier_provider_route/module/platform_kubernetes.go.tmp" "$earlier_provider_route/module/platform_kubernetes.go"
expect_failure "earlier provider route with preserved canonical branch" "$earlier_provider_route" "must remain the anchored routing decision"

prefix_provider_route="$TMP_ROOT/prefix-provider-route"
write_allowed_fixture "$prefix_provider_route"
awk '
  /^\tclusterType, _ :=/ { print "\tif providerRoute { return initializeProvider(m) }"; print "" }
  { print }
' "$prefix_provider_route/module/platform_kubernetes.go" >"$prefix_provider_route/module/platform_kubernetes.go.tmp"
mv "$prefix_provider_route/module/platform_kubernetes.go.tmp" "$prefix_provider_route/module/platform_kubernetes.go"
expect_failure "provider route before cluster extraction" "$prefix_provider_route" "must remain the anchored routing decision"

provider_binding_source="$TMP_ROOT/provider-binding-source"
write_allowed_fixture "$provider_binding_source"
sed 's/resolveApplicationKubernetesBackend(app, clusterType)/resolveProviderKubernetesBackend(app, clusterType)/' "$provider_binding_source/module/platform_kubernetes.go" >"$provider_binding_source/module/platform_kubernetes.go.tmp"
mv "$provider_binding_source/module/platform_kubernetes.go.tmp" "$provider_binding_source/module/platform_kubernetes.go"
expect_failure "provider-specific binding source" "$provider_binding_source" "must preserve the provider-first compatibility fallback lookup and initialization branch"

swallowed_factory_error="$TMP_ROOT/swallowed-factory-error"
write_allowed_fixture "$swallowed_factory_error"
awk '
  !swallowed && /return err/ { sub(/return err/, "return nil"); swallowed=1 }
  { print }
' "$swallowed_factory_error/module/platform_kubernetes.go" >"$swallowed_factory_error/module/platform_kubernetes.go.tmp"
mv "$swallowed_factory_error/module/platform_kubernetes.go.tmp" "$swallowed_factory_error/module/platform_kubernetes.go"
expect_failure "swallowed core factory error" "$swallowed_factory_error" "must preserve the core-local Kubernetes backend lookup and initialization branch"

swallowed_fallback_error="$TMP_ROOT/swallowed-fallback-error"
write_allowed_fixture "$swallowed_fallback_error"
sed 's/return createErr/return nil/' "$swallowed_fallback_error/module/platform_kubernetes.go" >"$swallowed_fallback_error/module/platform_kubernetes.go.tmp"
mv "$swallowed_fallback_error/module/platform_kubernetes.go.tmp" "$swallowed_fallback_error/module/platform_kubernetes.go"
expect_failure "swallowed compatibility factory error" "$swallowed_fallback_error" "must preserve the provider-first compatibility fallback lookup and initialization branch"

wrapped_factory_error="$TMP_ROOT/wrapped-factory-error"
write_allowed_fixture "$wrapped_factory_error"
awk '
  !wrapped && /return err/ { sub(/return err/, "return func(error) error { return nil }(err)"); wrapped=1 }
  { print }
' "$wrapped_factory_error/module/platform_kubernetes.go" >"$wrapped_factory_error/module/platform_kubernetes.go.tmp"
mv "$wrapped_factory_error/module/platform_kubernetes.go.tmp" "$wrapped_factory_error/module/platform_kubernetes.go"
expect_failure "wrapped swallowed core factory error" "$wrapped_factory_error" "must preserve the core-local Kubernetes backend lookup and initialization branch"

shadowed_binding="$TMP_ROOT/shadowed-binding"
write_allowed_fixture "$shadowed_binding"
sed 's/if binding.Client != nil/if binding := providerBinding; binding.Client != nil/' "$shadowed_binding/module/platform_kubernetes.go" >"$shadowed_binding/module/platform_kubernetes.go.tmp"
mv "$shadowed_binding/module/platform_kubernetes.go.tmp" "$shadowed_binding/module/platform_kubernetes.go"
expect_failure "provider branch shadows binding" "$shadowed_binding" "must preserve the provider-first compatibility fallback lookup and initialization branch"

typed_nil_lookup="$TMP_ROOT/typed-nil-lookup"
write_allowed_fixture "$typed_nil_lookup"
sed 's/return fmt.Errorf("platform.kubernetes %q: unsupported type %q", m.name, clusterType)/return unsupportedError/' "$typed_nil_lookup/module/platform_kubernetes.go" >"$typed_nil_lookup/module/platform_kubernetes.go.tmp"
mv "$typed_nil_lookup/module/platform_kubernetes.go.tmp" "$typed_nil_lookup/module/platform_kubernetes.go"
expect_failure "typed nil core lookup rejection" "$typed_nil_lookup" "must preserve the core-local Kubernetes backend lookup and initialization branch"

provider_final_fallback="$TMP_ROOT/provider-final-fallback"
write_allowed_fixture "$provider_final_fallback"
awk '
  /else if factory, ok := kubernetesBackendRegistry/ { saw_fallback=1 }
  saw_fallback && /^\t\t} else \{$/ {
    print
    print "\t\t\tm.backend = providerBackend"
    print "\t\t\treturn nil"
    replacing=1
    next
  }
  replacing && /^\t\t}$/ { replacing=0; print; next }
  replacing { next }
  { print }
' "$provider_final_fallback/module/platform_kubernetes.go" >"$provider_final_fallback/module/platform_kubernetes.go.tmp"
mv "$provider_final_fallback/module/platform_kubernetes.go.tmp" "$provider_final_fallback/module/platform_kubernetes.go"
expect_failure "provider backend in final fallback" "$provider_final_fallback" "must preserve the provider-first compatibility fallback lookup and initialization branch"

reserved_map_resurrection="$TMP_ROOT/reserved-map-resurrection"
write_allowed_fixture "$reserved_map_resurrection"
cat >"$reserved_map_resurrection/module/hidden_reserved.go" <<'EOF'
package module

var reservedKubernetesBackendTypes = map[string]struct{}{"digitalocean": {}}
EOF
expect_failure "removed reserved map resurrection" "$reserved_map_resurrection" "removed reservedKubernetesBackendTypes map must not exist"

provider_predicate_case="$TMP_ROOT/provider-predicate-case"
write_allowed_fixture "$provider_predicate_case"
sed 's/case "kind", "k3s":/case "kind", "k3s", "digitalocean":/' "$provider_predicate_case/module/platform_kubernetes_plugin_registry.go" >"$provider_predicate_case/module/platform_kubernetes_plugin_registry.go.tmp"
mv "$provider_predicate_case/module/platform_kubernetes_plugin_registry.go.tmp" "$provider_predicate_case/module/platform_kubernetes_plugin_registry.go"
expect_failure "provider added to reserved predicate" "$provider_predicate_case" "isReservedKubernetesBackendType must be the canonical exact predicate"

default_true_predicate="$TMP_ROOT/default-true-predicate"
write_allowed_fixture "$default_true_predicate"
sed 's/^\t\treturn false$/\t\treturn true/' "$default_true_predicate/module/platform_kubernetes_plugin_registry.go" >"$default_true_predicate/module/platform_kubernetes_plugin_registry.go.tmp"
mv "$default_true_predicate/module/platform_kubernetes_plugin_registry.go.tmp" "$default_true_predicate/module/platform_kubernetes_plugin_registry.go"
expect_failure "reserved predicate default true" "$default_true_predicate" "isReservedKubernetesBackendType must be the canonical exact predicate"

missing_normalization_guard="$TMP_ROOT/missing-normalization-guard"
write_allowed_fixture "$missing_normalization_guard"
awk '
  /^\t\tif isReservedKubernetesBackendType\(name\)/ { skipping=1; next }
  skipping && /^\t\t}$/ { skipping=0; next }
  !skipping { print }
' "$missing_normalization_guard/module/platform_kubernetes_plugin_registry.go" >"$missing_normalization_guard/module/platform_kubernetes_plugin_registry.go.tmp"
mv "$missing_normalization_guard/module/platform_kubernetes_plugin_registry.go.tmp" "$missing_normalization_guard/module/platform_kubernetes_plugin_registry.go"
expect_failure "missing reserved normalization guard" "$missing_normalization_guard" "normalizeKubernetesBackendRegistration must directly guard normalized name with isReservedKubernetesBackendType"

overwritten_normalized_name="$TMP_ROOT/overwritten-normalized-name"
write_allowed_fixture "$overwritten_normalized_name"
awk '
  /^\t\tif name == ""/ { print "\t\tname = \"digitalocean\""; skipping=1; next }
  skipping && /^\t\t}$/ { skipping=0; next }
  !skipping { print }
' "$overwritten_normalized_name/module/platform_kubernetes_plugin_registry.go" >"$overwritten_normalized_name/module/platform_kubernetes_plugin_registry.go.tmp"
mv "$overwritten_normalized_name/module/platform_kubernetes_plugin_registry.go.tmp" "$overwritten_normalized_name/module/platform_kubernetes_plugin_registry.go"
expect_failure "normalized name overwritten before reserved guard" "$overwritten_normalized_name" "normalizeKubernetesBackendRegistration must directly guard normalized name with isReservedKubernetesBackendType"

early_reserved_return="$TMP_ROOT/early-reserved-return"
write_allowed_fixture "$early_reserved_return"
awk '
  { print }
  /^\tnormalized := make/ {
    print "\tif len(bindings) == 1 {"
    print "\t\tnormalized[\"kind\"] = bindings[0]"
    print "\t\treturn owner, normalized, nil"
    print "\t}"
  }
' "$early_reserved_return/module/platform_kubernetes_plugin_registry.go" >"$early_reserved_return/module/platform_kubernetes_plugin_registry.go.tmp"
mv "$early_reserved_return/module/platform_kubernetes_plugin_registry.go.tmp" "$early_reserved_return/module/platform_kubernetes_plugin_registry.go"
expect_failure "reserved registration returned before guarded loop" "$early_reserved_return" "normalizeKubernetesBackendRegistration must directly guard normalized name with isReservedKubernetesBackendType"

replaced_normalization_guard="$TMP_ROOT/replaced-normalization-guard"
write_allowed_fixture "$replaced_normalization_guard"
sed 's/isReservedKubernetesBackendType(name)/isProviderKubernetesBackendType(name)/' "$replaced_normalization_guard/module/platform_kubernetes_plugin_registry.go" >"$replaced_normalization_guard/module/platform_kubernetes_plugin_registry.go.tmp"
mv "$replaced_normalization_guard/module/platform_kubernetes_plugin_registry.go.tmp" "$replaced_normalization_guard/module/platform_kubernetes_plugin_registry.go"
expect_failure "replaced reserved normalization guard" "$replaced_normalization_guard" "normalizeKubernetesBackendRegistration must directly guard normalized name with isReservedKubernetesBackendType"

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

for linkname_symbol in RegisterKubernetesBackend kubernetesBackendRegistry reservedKubernetesBackendTypes; do
  linkname_root="$TMP_ROOT/linkname-$linkname_symbol"
  write_allowed_fixture "$linkname_root"
  if [[ "$linkname_symbol" == "RegisterKubernetesBackend" ]]; then
    cat >"$linkname_root/module/linkname_alias.go" <<'EOF'
package module

import _ "unsafe"

//go:linkname hiddenRegister github.com/GoCodeAlone/workflow/module.RegisterKubernetesBackend
func hiddenRegister(string, KubernetesBackendFactory)

func init() {
	hiddenRegister("digitalocean", nil)
}
EOF
  elif [[ "$linkname_symbol" == "kubernetesBackendRegistry" ]]; then
    cat >"$linkname_root/module/linkname_alias.go" <<'EOF'
package module

import _ "unsafe"

//go:linkname	hiddenRegistry	github.com/GoCodeAlone/workflow/module.kubernetesBackendRegistry
var hiddenRegistry map[string]KubernetesBackendFactory

func init() {
	hiddenRegistry["digitalocean"] = nil
}
EOF
  else
    cat >"$linkname_root/module/linkname_alias.go" <<'EOF'
package module

import _ "unsafe"

//go:linkname hiddenReserved github.com/GoCodeAlone/workflow/module.reservedKubernetesBackendTypes
var hiddenReserved map[string]struct{}
EOF
  fi
  expect_failure "go:linkname alias for $linkname_symbol" "$linkname_root" "go:linkname must not reference Kubernetes backend boundary symbol"
done

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

symlink_root_target="$TMP_ROOT/symlink-root-target"
write_allowed_fixture "$symlink_root_target"
symlink_root="$TMP_ROOT/symlink-root"
if make_symlink_or_skip "$symlink_root_target" "$symlink_root" "symlinked root"; then
  expect_failure "symlinked fixture root" "$symlink_root" "Workflow root must not be a symlink"
fi

symlink_gomod="$TMP_ROOT/symlink-gomod"
write_allowed_fixture "$symlink_gomod"
cat >"$symlink_gomod/go.mod.target" <<'EOF'
module github.com/GoCodeAlone/workflow

go 1.26.5
EOF
touch "$symlink_gomod/.phase-b-complete" "$symlink_gomod/.phase-c-complete"
if make_symlink_or_skip "go.mod.target" "$symlink_gomod/go.mod" "symlinked go.mod"; then
  expect_production_failure "symlinked go.mod" "$symlink_gomod" "symlink is not permitted"
fi

symlink_marker="$TMP_ROOT/symlink-marker"
write_allowed_fixture "$symlink_marker"
cat >"$symlink_marker/go.mod" <<'EOF'
module github.com/GoCodeAlone/workflow

go 1.26.5
EOF
touch "$symlink_marker/.phase-b-complete.target" "$symlink_marker/.phase-c-complete"
if make_symlink_or_skip ".phase-b-complete.target" "$symlink_marker/.phase-b-complete" "symlinked phase marker"; then
  expect_production_failure "symlinked phase marker" "$symlink_marker" "symlink is not permitted"
fi

symlink_canonical="$TMP_ROOT/symlink-canonical"
write_allowed_fixture "$symlink_canonical"
mv "$symlink_canonical/module/platform_kubernetes.go" "$symlink_canonical/module/platform_kubernetes.go.target"
if make_symlink_or_skip "platform_kubernetes.go.target" "$symlink_canonical/module/platform_kubernetes.go" "symlinked canonical Go file"; then
  expect_failure "symlinked canonical Go file" "$symlink_canonical" "symlink is not permitted"
fi

symlink_candidate="$TMP_ROOT/symlink-candidate"
write_allowed_fixture "$symlink_candidate"
cat >"$symlink_candidate/module/provider_backend.go.target" <<'EOF'
package module
EOF
if make_symlink_or_skip "provider_backend.go.target" "$symlink_candidate/module/provider_backend.go" "symlinked scanned Go candidate"; then
  expect_failure "symlinked scanned Go candidate" "$symlink_candidate" "production Go file"
fi

wrong_root="$TMP_ROOT/wrong-root"
mkdir -p "$wrong_root"
expect_failure "wrong Workflow root" "$wrong_root" "missing canonical Kubernetes registration file module/platform_kubernetes.go"

for missing_file in platform_kubernetes.go platform_kubernetes_core.go platform_kubernetes_plugin_registry.go; do
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

func init() {
	RegisterKubernetesBackend("kind", func(_ map[string]any) (kubernetesBackend, error) {
		return &kindBackend{}, nil
	})
	RegisterKubernetesBackend("k3s", func(_ map[string]any) (kubernetesBackend, error) {
		return &kindBackend{}, nil
	})
	RegisterKubernetesBackend("eks", func(_ map[string]any) (kubernetesBackend, error) {
		return &eksErrorBackend{}, nil
	})
	RegisterKubernetesBackend("aks", func(_ map[string]any) (kubernetesBackend, error) {
		return &aksBackend{}, nil
	})
}
EOF
expect_failure "duplicate registration" "$duplicate" 'duplicate Kubernetes backend registration "kind"'

dead_registration_init="$TMP_ROOT/dead-registration-init"
write_allowed_fixture "$dead_registration_init"
sed 's/^func init()/func registerCoreBackends()/' "$dead_registration_init/module/platform_kubernetes_core.go" >"$dead_registration_init/module/platform_kubernetes_core.go.tmp"
mv "$dead_registration_init/module/platform_kubernetes_core.go.tmp" "$dead_registration_init/module/platform_kubernetes_core.go"
expect_failure "registrations moved to dead helper" "$dead_registration_init" "calls must be direct statements of one top-level func init()"

conditional_registration_init="$TMP_ROOT/conditional-registration-init"
write_allowed_fixture "$conditional_registration_init"
awk '
  /^func init\(\) \{$/ { print; print "\tif disableCoreRegistration { return }"; next }
  { print }
' "$conditional_registration_init/module/platform_kubernetes_core.go" >"$conditional_registration_init/module/platform_kubernetes_core.go.tmp"
mv "$conditional_registration_init/module/platform_kubernetes_core.go.tmp" "$conditional_registration_init/module/platform_kubernetes_core.go"
expect_failure "conditionally skipped registrations" "$conditional_registration_init" "calls must be direct statements of one top-level func init()"

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
