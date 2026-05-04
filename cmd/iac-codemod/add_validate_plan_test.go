// Copyright (c) 2026 Jon Langevin
// SPDX-License-Identifier: Apache-2.0

// Tests in this file MUST NOT call t.Parallel(). Same global-state
// constraint as main_test.go / lint_test.go / refactor_*_test.go.

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// ============================================================
// Source fixtures
// ============================================================

// avpProviderMissingValidatePlanSrc is a provider with both Plan and Apply
// but no ValidatePlan method. The codemod must insert a no-op stub.
const avpProviderMissingValidatePlanSrc = `package p

import "context"

type ResourceSpec struct{}
type ResourceState struct{}
type IaCPlan struct{}
type ApplyResult struct{}
type PlanDiagnostic struct{}

type FooProvider struct{}

func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return &IaCPlan{}, nil
}

func (p *FooProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	return &ApplyResult{}, nil
}
`

// avpProviderWithValidatePlanSrc is the no-op idempotent case: ValidatePlan
// already exists; the codemod must NOT add another stub.
const avpProviderWithValidatePlanSrc = `package p

import "context"

type ResourceSpec struct{}
type ResourceState struct{}
type IaCPlan struct{}
type ApplyResult struct{}
type PlanDiagnostic struct{}

type FooProvider struct{}

func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return &IaCPlan{}, nil
}

func (p *FooProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	return &ApplyResult{}, nil
}

func (p *FooProvider) ValidatePlan(plan *IaCPlan) []PlanDiagnostic { return nil }
`

// avpProviderSkippedValidatePlanSrc carries the marker on the type decl —
// the codemod must NOT inject ValidatePlan and must list the site as
// skipped. (Plan rev2 line 2400: marker honored at type-doc level.)
const avpProviderSkippedValidatePlanSrc = `package p

import "context"

type ResourceSpec struct{}
type ResourceState struct{}
type IaCPlan struct{}
type ApplyResult struct{}
type PlanDiagnostic struct{}

// FooProvider is intentionally without ValidatePlan; the constraint
// surface lives in a sibling type.
//
// wfctl:skip-iac-codemod sibling-validator pattern, see ADR-042
type FooProvider struct{}

func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return &IaCPlan{}, nil
}

func (p *FooProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	return &ApplyResult{}, nil
}
`

// avpNonProviderSrc — has methods named Plan/Apply but on a non-provider
// type (insufficient signature shape). Must NOT be touched.
const avpNonProviderSrc = `package p

import "context"

type Settings struct{}

func (s Settings) Plan(x int) error  { return nil }
func (s Settings) Apply(y int) error { return nil }
`

// ============================================================
// Detection (dry-run)
// ============================================================

func TestAddValidatePlan_DryRun_DetectsMissing(t *testing.T) {
	path := writeFixture(t, "provider.go", avpProviderMissingValidatePlanSrc)
	var stdout, stderr bytes.Buffer
	code := runAddValidatePlan([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "FooProvider") {
		t.Errorf("report should name FooProvider; got:\n%s", out)
	}
	if !strings.Contains(out, "missing-validate-plan") {
		t.Errorf("report should classify as missing-validate-plan; got:\n%s", out)
	}
	got, _ := os.ReadFile(path)
	if string(got) != avpProviderMissingValidatePlanSrc {
		t.Errorf("dry-run modified the file; expected no mutation")
	}
}

func TestAddValidatePlan_DryRun_AlreadyImplemented(t *testing.T) {
	path := writeFixture(t, "provider.go", avpProviderWithValidatePlanSrc)
	var stdout, stderr bytes.Buffer
	code := runAddValidatePlan([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "already-implemented") {
		t.Errorf("report should classify provider as already-implemented; got:\n%s", out)
	}
}

func TestAddValidatePlan_DryRun_HonorsSkipMarker(t *testing.T) {
	path := writeFixture(t, "provider.go", avpProviderSkippedValidatePlanSrc)
	var stdout, stderr bytes.Buffer
	code := runAddValidatePlan([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Skipped") {
		t.Errorf("report should have a Skipped section; got:\n%s", out)
	}
	if !strings.Contains(out, "FooProvider") {
		t.Errorf("Skipped section should list FooProvider; got:\n%s", out)
	}
}

func TestAddValidatePlan_DryRun_IgnoresNonProviders(t *testing.T) {
	path := writeFixture(t, "settings.go", avpNonProviderSrc)
	var stdout, stderr bytes.Buffer
	code := runAddValidatePlan([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, "Settings") {
		t.Errorf("non-provider type Settings should NOT be reported; got:\n%s", out)
	}
}

// ============================================================
// Mutation (-fix)
// ============================================================

func TestAddValidatePlan_Fix_InsertsStub(t *testing.T) {
	path := writeFixture(t, "provider.go", avpProviderMissingValidatePlanSrc)
	var stdout, stderr bytes.Buffer
	code := runAddValidatePlan([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	gotStr := string(got)
	if !strings.Contains(gotStr, "ValidatePlan(plan *IaCPlan) []PlanDiagnostic") {
		t.Errorf("inserted stub must be `ValidatePlan(plan *IaCPlan) []PlanDiagnostic`; got:\n%s", gotStr)
	}
	// Stub returns nil (no-op).
	if !strings.Contains(gotStr, "return nil") {
		t.Errorf("inserted stub must return nil; got:\n%s", gotStr)
	}
}

func TestAddValidatePlan_Fix_IdempotentOnImplemented(t *testing.T) {
	path := writeFixture(t, "provider.go", avpProviderWithValidatePlanSrc)
	var stdout, stderr bytes.Buffer
	if code := runAddValidatePlan([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != avpProviderWithValidatePlanSrc {
		t.Errorf("provider with ValidatePlan must be byte-identical after fix (idempotent); got:\n%s", string(got))
	}
}

func TestAddValidatePlan_Fix_HonorsSkipMarker(t *testing.T) {
	path := writeFixture(t, "provider.go", avpProviderSkippedValidatePlanSrc)
	var stdout, stderr bytes.Buffer
	code := runAddValidatePlan([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != avpProviderSkippedValidatePlanSrc {
		t.Errorf("skip-marker'd provider must NOT receive ValidatePlan stub; file changed:\n%s", string(got))
	}
}

func TestAddValidatePlan_Fix_DoesNotTouchNonProvider(t *testing.T) {
	path := writeFixture(t, "settings.go", avpNonProviderSrc)
	var stdout, stderr bytes.Buffer
	if code := runAddValidatePlan([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != avpNonProviderSrc {
		t.Errorf("non-provider file must NOT be modified")
	}
}

// ============================================================
// Mutation-gate negative test
// ============================================================

func TestAddValidatePlan_DryRunFalseWithoutFix_DoesNotMutate(t *testing.T) {
	path := writeFixture(t, "provider.go", avpProviderMissingValidatePlanSrc)
	stat0, _ := os.Stat(path)
	mtime0 := stat0.ModTime()
	time.Sleep(10 * time.Millisecond)

	var stdout, stderr bytes.Buffer
	code := run([]string{"add-validate-plan", "-dry-run=false", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != avpProviderMissingValidatePlanSrc {
		t.Errorf("file must NOT be mutated when -dry-run=false alone; content changed")
	}
	stat1, _ := os.Stat(path)
	if !stat1.ModTime().Equal(mtime0) {
		t.Errorf("file mtime should be unchanged; before=%v after=%v", mtime0, stat1.ModTime())
	}
}
