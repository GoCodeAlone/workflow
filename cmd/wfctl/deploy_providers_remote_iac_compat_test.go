package main

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestRemoteIaC_OptionalDriftConfigDetector_FallsBackOnLegacyPlugin pins the
// pipeline's compat story: when a remote plugin does NOT support
// IaCProvider.DetectDriftWithApplied (legacy plugin: returns "method not
// found"), the wfctl caller falls through to legacy IaCProvider.DetectDrift.
//
// This is the wire-format counterpart of the type-assertion fallback in
// runInfraApplyRefreshPhase / runInfraStatusDrift; without this gate, a v0.22
// wfctl talking to a v0.10.3 DO plugin (or an out-of-tree plugin) could trip
// a hard error instead of falling back.
func TestRemoteIaC_OptionalDriftConfigDetector_FallsBackOnLegacyPlugin(t *testing.T) {
	si := &multiMethodStubInvoker{
		// Method not found = canonical signal a legacy plugin emits when
		// it doesn't have a case for this RPC name.
		errByMethod: map[string]error{
			"IaCProvider.DetectDriftWithApplied": errors.New("method not found: IaCProvider.DetectDriftWithApplied"),
		},
		// Legacy DetectDrift returns success.
		respByMethod: map[string]map[string]any{
			"IaCProvider.DetectDrift": {
				"drifts": []any{map[string]any{"name": "x", "type": "infra.test", "drifted": false, "class": "in-sync"}},
			},
		},
	}
	p := &remoteIaCProvider{invoker: si}

	// Caller-side type-assertion: remoteIaCProvider implements
	// DetectDriftWithApplied (it always does, since the wfctl-side wrapper
	// ALWAYS exposes the method). The fallback happens INSIDE the wrapper:
	// if the remote returns method-not-found, the wrapper retries with
	// legacy DetectDrift.
	refs := []interfaces.ResourceRef{{Name: "x", Type: "infra.test"}}
	drifts, err := p.DetectDriftWithApplied(context.Background(), refs, nil)
	if err != nil {
		t.Fatalf("DetectDriftWithApplied should NOT propagate method-not-found; should fall back. err=%v", err)
	}
	if len(drifts) != 1 || drifts[0].Class != interfaces.DriftClassInSync {
		t.Errorf("expected fallback InSync drift; got %+v", drifts)
	}

	// Verify the second call to invoker WAS the legacy method.
	if !si.calledMethods["IaCProvider.DetectDrift"] {
		t.Errorf("expected fallback to call IaCProvider.DetectDrift; called methods: %v", si.calledMethods)
	}
}

// multiMethodStubInvoker is a test double for remoteServiceInvoker that supports
// per-method response and error configuration (unlike the basic stubInvoker
// which records only a single method/resp/err).
type multiMethodStubInvoker struct {
	calledMethods map[string]bool
	respByMethod  map[string]map[string]any
	errByMethod   map[string]error
}

func (s *multiMethodStubInvoker) InvokeService(method string, args map[string]any) (map[string]any, error) {
	if s.calledMethods == nil {
		s.calledMethods = map[string]bool{}
	}
	s.calledMethods[method] = true
	if err, ok := s.errByMethod[method]; ok && err != nil {
		return nil, err
	}
	if resp, ok := s.respByMethod[method]; ok {
		return resp, nil
	}
	return nil, nil
}
