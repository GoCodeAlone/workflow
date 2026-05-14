#!/usr/bin/env bash
# audit-cloud-symbols.sh — authoritative cloud-SDK ownership map for the
# WHOLE workflow-core repo (not just module/).
#
# Drafted as a verification aid for the cloud-SDK-extraction design
# (docs/plans/2026-05-14-cloud-sdk-extraction-design.md); formalized and
# extended in that design's Phase 0. It exists because review cycles kept
# finding hand-maintained inventory claims wrong — first grep matching SDK
# names inside doc comments (cycle 9), then a survey scoped to module/ that
# missed aws-sdk importers in provider/, plugin/rbac/, iam/, artifact/
# (cycle 10). This script is comment-immune AND whole-repo by construction.
#
# This script answers, mechanically:
#   1. Which *.go files (repo-wide, *_test.go excluded) have a REAL import
#      of each in-scope SDK tree (parsed from the `import (...)` block —
#      never from comments), split into module/ vs. elsewhere.
#   2. Which files name an SDK only in comments (false positives to ignore).
#   3. Whether cloud_account_aws_creds.go still imports aws-sdk-go-v2
#      (Phase B rewrite invariant: must be zero post-rewrite).
#   4. platform_kubernetes_kind.go backend-split readiness (advisory).
#
# Exit non-zero if invoked with --check and an invariant is violated.

set -euo pipefail

cd "$(dirname "$0")/.."

SDK_TREES=(
  'aws-sdk-go-v2'
  'azure-sdk-for-go'
  'cloud.google.com/go'
  'google.golang.org/api'
)

# Extract just the Go import block of a file (handles single `import (...)`).
import_block() {
  awk '/^import \(/{f=1} f{print} /^\)/{if(f)exit}' "$1"
}

real_import() {  # file, sdk → 0 if sdk appears in a real import (block OR single-line)
  # `|| true` on the inner grep: a no-match exit 1 must not poison the pipe
  # under `set -o pipefail`.
  { import_block "$1"; grep -E '^import "' "$1" 2>/dev/null || true; } | grep -q "$2"
}

CHECK=0
[[ "${1:-}" == "--check" ]] && CHECK=1
FAIL=0

echo "== Cloud-SDK real-import map (WHOLE REPO, *_test.go excluded) =="
echo "   module/ = this design's IaC-state/platform/standalone scope"
echo "   elsewhere = out-of-scope surface (see design Non-Goals): provider/,"
echo "   plugin/rbac/, iam/, artifact/ — the #653 'RBAC/secrets/artifact stay'"
echo "   surface, parallel to godo."
for sdk in "${SDK_TREES[@]}"; do
  echo
  echo "### $sdk"
  # Every file that names the SDK anywhere (import or comment):
  while IFS= read -r f; do
    [[ -z "$f" ]] && continue
    loc="module/  "
    [[ "$f" != ./module/* && "$f" != module/* ]] && loc="elsewhere"
    if real_import "$f" "$sdk"; then
      echo "  REAL  [$loc] $f"
    else
      echo "  comment-only  $f   (false positive — ignore)"
    fi
  done < <(grep -rl "$sdk" . --include='*.go' | grep -v '_test\.go' | sort)
done

echo
echo "== Invariant: cloud_account_aws_creds.go imports of aws-sdk-go-v2 =="
CREDS=module/cloud_account_aws_creds.go
if [[ -f "$CREDS" ]]; then
  n=$(import_block "$CREDS" | grep -c 'aws-sdk-go-v2' || true)
  echo "  aws-sdk-go-v2 import lines in $CREDS: $n"
  echo "  (pre-extraction: nonzero is expected; Phase B rewrite invariant: MUST be 0)"
  # Only enforced once the design's Phase B marker file exists.
  if [[ $CHECK -eq 1 && -f .phase-b-complete && $n -ne 0 ]]; then
    echo "  INVARIANT VIOLATED: cloud_account_aws_creds.go still imports aws-sdk-go-v2 post-Phase-B"
    FAIL=1
  fi
fi

echo
echo "== Advisory: platform_kubernetes_kind.go backend split readiness =="
KIND=module/platform_kubernetes_kind.go
if [[ -f module/platform_kubernetes_kind.go ]]; then
  echo "  backend types: $(grep -cE '^type .*[Bb]ackend struct' "$KIND") (expect kind/eksError/gke/aks pre-Phase-0)"
  echo "  shared init(): $(grep -c '^func init()' "$KIND") (expect 1 pre-Phase-0; 0 here post-split — each _provider.go gets its own)"
  echo "  real SDK imports here:"
  for sdk in "${SDK_TREES[@]}"; do
    real_import "$KIND" "$sdk" && echo "    REAL: $sdk"
  done
fi

echo
echo "== Invariant: no init() mixes core-staying + plugin-bound k8s backends =="
# Post-Phase-0, platform_kubernetes_core.go must register ONLY kind/k3s/eks/aks
# and platform_kubernetes_gke.go must register ONLY gke. A file registering a
# name from the other set is a partition violation.
CORE_K8S=module/platform_kubernetes_core.go
GKE_K8S=module/platform_kubernetes_gke.go
if [[ -f "$CORE_K8S" && -f "$GKE_K8S" ]]; then
  if grep -qE 'RegisterKubernetesBackend\("gke"' "$CORE_K8S"; then
    echo "  VIOLATION: $CORE_K8S registers the plugin-bound 'gke' backend"; FAIL=1
  fi
  for n in kind k3s eks aks; do
    if grep -qE "RegisterKubernetesBackend\\(\"$n\"" "$GKE_K8S"; then
      echo "  VIOLATION: $GKE_K8S registers the core-staying '$n' backend"; FAIL=1
    fi
  done
  [[ $FAIL -eq 0 ]] && echo "  OK — init() partition clean"
fi

echo
if [[ $CHECK -eq 1 ]]; then
  [[ $FAIL -eq 0 ]] && echo "audit-cloud-symbols: OK" || { echo "audit-cloud-symbols: FAIL"; exit 1; }
else
  echo "audit-cloud-symbols: report-only (pass --check to enforce invariants)"
fi
