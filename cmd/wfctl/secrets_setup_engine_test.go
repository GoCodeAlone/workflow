package main

import (
	"context"
	"errors"
	"testing"
)

// engineTestProvider is an in-memory SecretsProvider for engine tests.
// Distinct from the existing fakeSecretsProvider in infra_bootstrap_env_test.go.
type engineTestProvider struct {
	data     map[string]string
	setCnt   map[string]int
	checkCnt map[string]int
}

func newEngineTestProvider(initial map[string]string) *engineTestProvider {
	p := &engineTestProvider{
		data:     make(map[string]string),
		setCnt:   make(map[string]int),
		checkCnt: make(map[string]int),
	}
	for k, v := range initial {
		p.data[k] = v
	}
	return p
}

func (f *engineTestProvider) Get(_ context.Context, name string) (string, error) {
	return f.data[name], nil
}
func (f *engineTestProvider) Set(_ context.Context, name, value string) error {
	if value == "__fail__" {
		return errors.New("injected set failure")
	}
	f.data[name] = value
	f.setCnt[name]++
	return nil
}
func (f *engineTestProvider) Check(_ context.Context, name string) (SecretState, error) {
	f.checkCnt[name]++
	if _, ok := f.data[name]; ok {
		return SecretSet, nil
	}
	return SecretNotSet, nil
}
func (f *engineTestProvider) List(_ context.Context) ([]SecretStatus, error) {
	var out []SecretStatus
	for k := range f.data {
		out = append(out, SecretStatus{Name: k, State: SecretSet, IsSet: true})
	}
	return out, nil
}
func (f *engineTestProvider) Delete(_ context.Context, name string) error {
	delete(f.data, name)
	return nil
}

// engineDecl is the declared-secret type used in engine tests.
type engineDecl struct {
	name      string
	sensitive bool
}

// TestSetupEngine_SkipExisting: store has A set; selector picks only B;
// engine should Set(B) once and report A as skipped.
func TestSetupEngine_SkipExisting(t *testing.T) {
	provider := newEngineTestProvider(map[string]string{"A": "existing-value"})

	decls := []engineDecl{
		{name: "A", sensitive: true},
		{name: "B", sensitive: false},
	}

	selector := func(decls []engineDecl, statuses []SecretStatus) ([]engineDecl, error) {
		setMap := make(map[string]bool)
		for _, s := range statuses {
			if s.IsSet {
				setMap[s.Name] = true
			}
		}
		var out []engineDecl
		for _, d := range decls {
			if !setMap[d.name] {
				out = append(out, d)
			}
		}
		return out, nil
	}

	valuer := func(d engineDecl) (string, bool, error) {
		switch d.name {
		case "B":
			return "b-value", true, nil
		default:
			return "", false, nil
		}
	}

	var audited []string
	audit := func(name, store string) {
		audited = append(audited, name)
	}

	report, err := runSetupEngine(context.Background(), decls,
		func(d engineDecl) string { return d.name },
		provider, selector, valuer, audit, false)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if len(report.Set) != 1 || report.Set[0] != "B" {
		t.Errorf("Set = %v, want [B]", report.Set)
	}
	if len(report.Skipped) != 1 || report.Skipped[0] != "A" {
		t.Errorf("Skipped = %v, want [A]", report.Skipped)
	}
	if len(report.Failed) != 0 {
		t.Errorf("Failed = %v, want []", report.Failed)
	}
	if provider.setCnt["B"] != 1 {
		t.Errorf("provider Set(B) called %d times, want 1", provider.setCnt["B"])
	}
	if provider.data["A"] != "existing-value" {
		t.Error("A should not have been modified")
	}
	if len(audited) != 1 || audited[0] != "B" {
		t.Errorf("audited = %v, want [B]", audited)
	}
}

// TestSetupEngine_SetError: Set failure goes to Failed, not fatal.
func TestSetupEngine_SetError(t *testing.T) {
	provider := newEngineTestProvider(map[string]string{"A": "existing-value"})

	decls := []engineDecl{{name: "B"}}
	selector := func(decls []engineDecl, _ []SecretStatus) ([]engineDecl, error) { return decls, nil }
	valuer := func(d engineDecl) (string, bool, error) {
		return "__fail__", true, nil // trigger fake failure
	}
	var audited []string
	audit := func(name, store string) { audited = append(audited, name) }

	report, err := runSetupEngine(context.Background(), decls,
		func(d engineDecl) string { return d.name },
		provider, selector, valuer, audit, false)
	if err != nil {
		t.Fatalf("engine fatal: %v", err)
	}
	if len(report.Failed) != 1 || report.Failed[0] != "B" {
		t.Errorf("Failed = %v, want [B]", report.Failed)
	}
	if len(report.Set) != 0 {
		t.Errorf("Set = %v, want []", report.Set)
	}
	if len(audited) != 0 {
		t.Errorf("audited = %v, want [] (no audit on failure)", audited)
	}
}

// TestSetupEngine_ValuerNotProvided: valuer returns provided=false → skip.
func TestSetupEngine_ValuerNotProvided(t *testing.T) {
	provider := newEngineTestProvider(nil)
	decls := []engineDecl{{name: "C"}}
	selector := func(decls []engineDecl, _ []SecretStatus) ([]engineDecl, error) { return decls, nil }
	valuer := func(d engineDecl) (string, bool, error) { return "", false, nil }
	var audited []string
	audit := func(name, store string) { audited = append(audited, name) }

	report, err := runSetupEngine(context.Background(), decls,
		func(d engineDecl) string { return d.name },
		provider, selector, valuer, audit, false)
	if err != nil {
		t.Fatalf("engine fatal: %v", err)
	}
	if len(report.Set) != 0 {
		t.Errorf("Set = %v, want []", report.Set)
	}
	if len(report.Skipped) != 1 || report.Skipped[0] != "C" {
		t.Errorf("Skipped = %v, want [C]", report.Skipped)
	}
	if len(audited) != 0 {
		t.Errorf("audited = %v, want []", audited)
	}
}

// TestSetupEngine_StopOnError: with stopOnError=true, first failure aborts.
func TestSetupEngine_StopOnError(t *testing.T) {
	provider := newEngineTestProvider(nil)
	decls := []engineDecl{{name: "B"}, {name: "C"}}
	selector := func(decls []engineDecl, _ []SecretStatus) ([]engineDecl, error) { return decls, nil }
	valuer := func(d engineDecl) (string, bool, error) {
		return "__fail__", true, nil
	}
	var audited []string
	audit := func(name, store string) { audited = append(audited, name) }

	report, err := runSetupEngine(context.Background(), decls,
		func(d engineDecl) string { return d.name },
		provider, selector, valuer, audit, true)
	if err == nil {
		t.Fatal("expected non-nil error with stopOnError=true")
	}
	// Only first secret should appear in Failed.
	if len(report.Failed) != 1 {
		t.Errorf("Failed = %v, want [B] (stopped after first)", report.Failed)
	}
}
