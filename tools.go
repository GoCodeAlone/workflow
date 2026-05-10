//go:build tools
// +build tools

// This file pins build-time tool dependencies that are not imported by
// runtime code. Listed here so `go mod tidy` keeps them in go.mod/go.sum and
// so the release pipeline can resolve their exact versions for the
// grpc-versions.txt artifact (cross-repo dep sync foundation per
// 2026-05-10-strict-contracts-force-cutover Task 2).
//
// Guarded by the `tools` build tag so it is excluded from normal builds
// (avoids package-name conflict with the rest of the workflow root).
package workflow

import (
	_ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
