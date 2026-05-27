package main

import "github.com/GoCodeAlone/workflow/iac/wfctlhelpers"

// writeEnvResolvedConfig is a one-line delegating shim onto
// wfctlhelpers.WriteEnvResolvedConfig. The implementation moved into the
// shared helper package per docs/plans/2026-05-27-infra-admin-dynamic.md
// Task 1 (consolidation follow-up addressing spec-reviewer F2 on commit
// 7a064b824) so the wfctl CLI and the host-side infra.admin module share
// one path and cannot drift. New code should call the wfctlhelpers symbol
// directly; this wrapper exists only to keep existing cmd/wfctl callsites
// untouched.
func writeEnvResolvedConfig(cfgFile, envName string) (tmpPath string, err error) {
	return wfctlhelpers.WriteEnvResolvedConfig(cfgFile, envName)
}
