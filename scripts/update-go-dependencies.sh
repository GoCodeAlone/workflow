#!/usr/bin/env bash
set -euo pipefail

# Accept a module directory so workflows can keep a repository-relative script
# path for their executable authorization checks.
cd -- "${1:-.}"

# Update direct requirements together, allowing their manifests to select the
# transitive graph. Broad `go get -u ./...` can advance kube-openapi or YAML past
# the APIs required by Kubernetes or actionlint. Preserve explicit replacements.
export GOWORK=off
targets="$(go list -m -f '{{if and (not .Main) (not .Indirect) (not .Replace)}}{{.Path}}@upgrade{{end}}' all)"
if [[ -n "$targets" ]]; then
  modules=()
  while IFS= read -r target; do
    if [[ -n "$target" ]]; then
      modules+=("$target")
    fi
  done <<< "$targets"
  go get "${modules[@]}"
fi
go mod tidy
