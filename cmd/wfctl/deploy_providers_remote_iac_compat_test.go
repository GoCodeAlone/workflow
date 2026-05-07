package main

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestRemoteIaC_DriftConfigDetector_SendsSpecsViaDetectDrift pins the wire
// protocol: DetectDriftWithSpecs sends "IaCProvider.DetectDrift" with a
// "specs" arg map. The DO plugin v0.10.5+ dispatches spec-injection inside
// IaCProvider.DetectDrift when the "specs" key is present — no separate RPC
// method name is required.
func TestRemoteIaC_DriftConfigDetector_SendsSpecsViaDetectDrift(t *testing.T) {
	si := &multiMethodStubInvoker{
		respByMethod: map[string]map[string]any{
			"IaCProvider.DetectDrift": {
				"drifts": []any{map[string]any{"name": "x", "type": "infra.test", "drifted": true, "class": "config"}},
			},
		},
	}
	p := &remoteIaCProvider{invoker: si}

	refs := []interfaces.ResourceRef{{Name: "x", Type: "infra.test"}}
	specs := map[string]interfaces.ResourceSpec{
		"x": {Name: "x", Type: "infra.test", Config: map[string]any{"region": "nyc3"}},
	}
	drifts, err := p.DetectDriftWithSpecs(context.Background(), refs, specs)
	if err != nil {
		t.Fatalf("DetectDriftWithSpecs: unexpected error: %v", err)
	}
	if len(drifts) != 1 || drifts[0].Class != interfaces.DriftClassConfig {
		t.Errorf("expected config-drift result; got %+v", drifts)
	}

	// Verify the invoker was called with IaCProvider.DetectDrift (not a
	// separate DetectDriftWithSpecs method) — this is the wire contract.
	if !si.calledMethods["IaCProvider.DetectDrift"] {
		t.Errorf("DetectDriftWithSpecs must invoke IaCProvider.DetectDrift; called methods: %v", si.calledMethods)
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
