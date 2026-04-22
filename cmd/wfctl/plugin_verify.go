package main

import (
	"context"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// installVerifyHookFn is a function that handles a single install_verify event.
// Using a function type makes the behaviour testable without a full HookDispatcher.
type installVerifyHookFn func(ctx context.Context, event interfaces.HookEvent, payload interfaces.InstallVerifyPayload) error

// emitInstallVerifyHook emits the install_verify hook when verify is non-nil.
// It is called after tarball download and before extraction.
// Returns nil immediately when verify is nil (feature opt-in).
func emitInstallVerifyHook(ctx context.Context, tarballPath string, verify *config.PluginVerifyConfig, fn installVerifyHookFn) error {
	if verify == nil {
		return nil
	}
	payload := interfaces.InstallVerifyPayload{
		TarballPath: tarballPath,
		VulnPolicy:  verify.VulnPolicy,
	}
	if verify.Signature == "required" {
		// ExpectedSignatureIdentity is intentionally left empty here; the
		// supply-chain hook handler resolves the expected identity from its
		// own cosign policy config. We just surface the policy knob.
		payload.ExpectedSignatureIdentity = ""
	}
	return fn(ctx, interfaces.HookEventInstallVerify, payload)
}

// defaultInstallVerifyHookFn builds a hook function backed by a HookDispatcher.
// This wires the emitInstallVerifyHook call into the real subprocess dispatch
// path used by the production install flow.
func defaultInstallVerifyHookFn(pluginsDir string) installVerifyHookFn {
	disp := NewHookDispatcher(pluginsDir)
	return func(ctx context.Context, event interfaces.HookEvent, payload interfaces.InstallVerifyPayload) error {
		hookPayload := interfaces.HookPayload{
			Event: event,
			Data: map[string]any{
				"tarball_path":                payload.TarballPath,
				"expected_signature_identity": payload.ExpectedSignatureIdentity,
				"expected_sbom_hash":          payload.ExpectedSBOMHash,
				"vuln_policy":                 payload.VulnPolicy,
			},
		}
		return disp.Dispatch(ctx, event, hookPayload)
	}
}
