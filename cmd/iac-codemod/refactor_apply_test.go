// Copyright (c) 2026 Jon Langevin
// SPDX-License-Identifier: Apache-2.0

// Tests in this file MUST NOT call t.Parallel(). Same global-state
// constraint as main_test.go / lint_test.go / refactor_plan_test.go.

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

// canonicalApplySrc is a minimal Apply body the codemod will rewrite.
// Loop+switch on action.Action with create/update/delete branches that
// dispatch directly to the driver. Modeled on the simplest pattern
// expected by wfctlhelpers.ApplyPlan.
const canonicalApplySrc = `package p

import (
	"context"
	"fmt"
	"time"
)

type ResourceSpec struct{ Name, Type string }
type ResourceState struct{ Name string; ProviderID string }
type IaCPlan struct{ ID string; CreatedAt time.Time; Actions []PlanAction }
type PlanAction struct{ Action string; Resource ResourceSpec; Current *ResourceState }
type ApplyResult struct{ PlanID string; Errors []ActionError; Resources []ResourceOutput }
type ActionError struct{ Resource, Action, Error string }
type ResourceRef struct{ Name, Type, ProviderID string }
type ResourceOutput struct{ ProviderID string }
type PlanDiagnostic struct{}

type Driver interface {
	Create(ctx context.Context, r ResourceSpec) (*ResourceOutput, error)
	Update(ctx context.Context, ref ResourceRef, r ResourceSpec) (*ResourceOutput, error)
	Delete(ctx context.Context, ref ResourceRef) error
}

type FooProvider struct{}

func (p *FooProvider) ResourceDriver(string) (Driver, error) { return nil, nil }

func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return wfctlhelpers.Plan(ctx, p, desired, current)
}

func (p *FooProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	result := &ApplyResult{PlanID: plan.ID}
	for _, action := range plan.Actions {
		d, err := p.ResourceDriver(action.Resource.Type)
		if err != nil {
			result.Errors = append(result.Errors, ActionError{Resource: action.Resource.Name, Action: action.Action, Error: err.Error()})
			continue
		}
		var out *ResourceOutput
		switch action.Action {
		case "create":
			out, err = d.Create(ctx, action.Resource)
		case "update":
			ref := ResourceRef{Name: action.Resource.Name, Type: action.Resource.Type}
			if action.Current != nil {
				ref.ProviderID = action.Current.ProviderID
			}
			out, err = d.Update(ctx, ref, action.Resource)
		case "delete":
			ref := ResourceRef{Name: action.Resource.Name, Type: action.Resource.Type}
			if action.Current != nil {
				ref.ProviderID = action.Current.ProviderID
			}
			err = d.Delete(ctx, ref)
		default:
			err = fmt.Errorf("unknown action %q", action.Action)
		}
		if err != nil {
			result.Errors = append(result.Errors, ActionError{Resource: action.Resource.Name, Action: action.Action, Error: err.Error()})
			continue
		}
		if out != nil {
			result.Resources = append(result.Resources, *out)
		}
	}
	return result, nil
}

func (p *FooProvider) ValidatePlan(plan *IaCPlan) []PlanDiagnostic { return nil }
`

// doUpsertApplySrc replicates the DigitalOcean upsert-on-create-conflict
// pattern. The "create" case branches on errors.Is(err,
// ErrResourceAlreadyExists) and routes through Read+Update to recover.
// The codemod must DETECT this and refuse to rewrite, emitting a
// suggested upsertSupporter hook patch.
const doUpsertApplySrc = `package p

import (
	"context"
	"errors"
	"fmt"
)

type ResourceSpec struct{ Name, Type string }
type ResourceState struct{ Name, ProviderID string }
type IaCPlan struct{ Actions []PlanAction }
type PlanAction struct{ Action string; Resource ResourceSpec; Current *ResourceState }
type ApplyResult struct{ Errors []ActionError; Resources []ResourceOutput }
type ActionError struct{ Resource, Action, Error string }
type ResourceRef struct{ Name, Type, ProviderID string }
type ResourceOutput struct{ ProviderID string }
type PlanDiagnostic struct{}

var ErrResourceAlreadyExists = errors.New("already exists")

type upsertSupporter interface{ SupportsUpsert() bool }

type Driver interface {
	Create(ctx context.Context, r ResourceSpec) (*ResourceOutput, error)
	Update(ctx context.Context, ref ResourceRef, r ResourceSpec) (*ResourceOutput, error)
	Read(ctx context.Context, ref ResourceRef) (*ResourceOutput, error)
	Delete(ctx context.Context, ref ResourceRef) error
}

type DOProvider struct{}

func (p *DOProvider) ResourceDriver(string) (Driver, error) { return nil, nil }

func (p *DOProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return wfctlhelpers.Plan(ctx, p, desired, current)
}

func (p *DOProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	result := &ApplyResult{}
	for _, action := range plan.Actions {
		d, err := p.ResourceDriver(action.Resource.Type)
		if err != nil {
			result.Errors = append(result.Errors, ActionError{Resource: action.Resource.Name, Action: action.Action, Error: err.Error()})
			continue
		}
		var out *ResourceOutput
		switch action.Action {
		case "create":
			out, err = d.Create(ctx, action.Resource)
			if errors.Is(err, ErrResourceAlreadyExists) {
				us, ok := d.(upsertSupporter)
				if !ok || !us.SupportsUpsert() {
					break
				}
				createErr := err
				ref := ResourceRef{Name: action.Resource.Name, Type: action.Resource.Type}
				existing, readErr := d.Read(ctx, ref)
				if readErr != nil {
					err = fmt.Errorf("upsert: read after conflict: %w", errors.Join(createErr, readErr))
					break
				}
				ref.ProviderID = existing.ProviderID
				out, err = d.Update(ctx, ref, action.Resource)
			}
		case "update":
			ref := ResourceRef{Name: action.Resource.Name, Type: action.Resource.Type, ProviderID: action.Current.ProviderID}
			out, err = d.Update(ctx, ref, action.Resource)
		default:
			err = fmt.Errorf("unknown action %q", action.Action)
		}
		if err != nil {
			result.Errors = append(result.Errors, ActionError{Resource: action.Resource.Name, Action: action.Action, Error: err.Error()})
			continue
		}
		if out != nil {
			result.Resources = append(result.Resources, *out)
		}
	}
	return result, nil
}

func (p *DOProvider) ValidatePlan(plan *IaCPlan) []PlanDiagnostic { return nil }
`

// awsUpdateReplaceCollapseSrc replicates AWSProvider.Apply: the
// "update" and "replace" actions share a single case clause. The
// codemod must DETECT this and emit "manual port required" with line
// numbers because wfctlhelpers' doReplace path is meaningfully
// different from doUpdate (delete+create vs in-place modify) and
// silent collapse would lose semantic distinction.
const awsUpdateReplaceCollapseSrc = `package p

import (
	"context"
	"fmt"
)

type ResourceSpec struct{ Name, Type string }
type ResourceState struct{ Name, ProviderID string }
type IaCPlan struct{ ID string; Actions []PlanAction }
type PlanAction struct{ Action string; Resource ResourceSpec; Current *ResourceState }
type ApplyResult struct{ PlanID string; Errors []ActionError; Resources []ResourceOutput }
type ActionError struct{ Resource, Action, Error string }
type ResourceRef struct{ Name, Type, ProviderID string }
type ResourceOutput struct{ ProviderID string }
type PlanDiagnostic struct{}

type Driver interface {
	Create(ctx context.Context, r ResourceSpec) (*ResourceOutput, error)
	Update(ctx context.Context, ref ResourceRef, r ResourceSpec) (*ResourceOutput, error)
	Delete(ctx context.Context, ref ResourceRef) error
}

type AWSProvider struct{}

func (p *AWSProvider) resourceDriver(string) (Driver, error) { return nil, nil }

func (p *AWSProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return wfctlhelpers.Plan(ctx, p, desired, current)
}

func (p *AWSProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	result := &ApplyResult{PlanID: plan.ID}
	for _, action := range plan.Actions {
		drv, err := p.resourceDriver(action.Resource.Type)
		if err != nil {
			result.Errors = append(result.Errors, ActionError{Resource: action.Resource.Name, Action: action.Action, Error: err.Error()})
			continue
		}
		var out *ResourceOutput
		switch action.Action {
		case "create":
			out, err = drv.Create(ctx, action.Resource)
		case "update", "replace":
			ref := ResourceRef{Name: action.Resource.Name, Type: action.Resource.Type}
			if action.Current != nil {
				ref.ProviderID = action.Current.ProviderID
			}
			out, err = drv.Update(ctx, ref, action.Resource)
		case "delete":
			ref := ResourceRef{Name: action.Resource.Name, Type: action.Resource.Type}
			if action.Current != nil {
				ref.ProviderID = action.Current.ProviderID
			}
			err = drv.Delete(ctx, ref)
		}
		if err != nil {
			result.Errors = append(result.Errors, ActionError{Resource: action.Resource.Name, Action: action.Action, Error: err.Error()})
			continue
		}
		if out != nil {
			result.Resources = append(result.Resources, *out)
		}
	}
	_ = fmt.Sprintf("anchor")
	return result, nil
}

func (p *AWSProvider) ValidatePlan(plan *IaCPlan) []PlanDiagnostic { return nil }
`

// customErrorWrapApplySrc replicates a custom-error-wrapping idiom:
// errors returned from the driver are wrapped with bespoke domain text
// before being recorded. wfctlhelpers' default error path doesn't
// preserve this wrapping, so the codemod must DETECT and emit an
// extension-point hook + sample patch (post-hook on ApplyResult.Errors).
const customErrorWrapApplySrc = `package p

import (
	"context"
	"fmt"
)

type ResourceSpec struct{ Name, Type string }
type ResourceState struct{ Name, ProviderID string }
type IaCPlan struct{ Actions []PlanAction }
type PlanAction struct{ Action string; Resource ResourceSpec }
type ApplyResult struct{ Errors []ActionError; Resources []ResourceOutput }
type ActionError struct{ Resource, Action, Error string }
type ResourceRef struct{ Name, Type string }
type ResourceOutput struct{}
type PlanDiagnostic struct{}

type Driver interface {
	Create(ctx context.Context, r ResourceSpec) (*ResourceOutput, error)
	Update(ctx context.Context, ref ResourceRef, r ResourceSpec) (*ResourceOutput, error)
	Delete(ctx context.Context, ref ResourceRef) error
}

type WrapProvider struct{}

func (p *WrapProvider) resourceDriver(string) (Driver, error) { return nil, nil }

func (p *WrapProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return wfctlhelpers.Plan(ctx, p, desired, current)
}

func (p *WrapProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	result := &ApplyResult{}
	for _, action := range plan.Actions {
		d, err := p.resourceDriver(action.Resource.Type)
		if err != nil {
			result.Errors = append(result.Errors, ActionError{Resource: action.Resource.Name, Action: action.Action, Error: err.Error()})
			continue
		}
		var out *ResourceOutput
		switch action.Action {
		case "create":
			out, err = d.Create(ctx, action.Resource)
			if err != nil {
				err = fmt.Errorf("wrap: %s create %s failed: %w", "wrap-provider", action.Resource.Name, err)
			}
		case "update":
			out, err = d.Update(ctx, ResourceRef{Name: action.Resource.Name}, action.Resource)
			if err != nil {
				err = fmt.Errorf("wrap: %s update %s failed: %w", "wrap-provider", action.Resource.Name, err)
			}
		}
		if err != nil {
			result.Errors = append(result.Errors, ActionError{Resource: action.Resource.Name, Action: action.Action, Error: err.Error()})
			continue
		}
		_ = out
	}
	return result, nil
}

func (p *WrapProvider) ValidatePlan(plan *IaCPlan) []PlanDiagnostic { return nil }
`

// skippedApplySrc carries the canonical marker on the function doc.
// Apply must not be rewritten regardless of body shape; site listed in
// the report.
const skippedApplySrc = `package p

import "context"

type ResourceSpec struct{}
type ResourceState struct{}
type IaCPlan struct{}
type ApplyResult struct{}
type FooProvider struct{}
type PlanDiagnostic struct{}

func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) { return wfctlhelpers.Plan(ctx, p, desired, current) }

// wfctl:skip-iac-codemod custom orchestration, see ADR-042
func (p *FooProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	return &ApplyResult{}, nil
}

func (p *FooProvider) ValidatePlan(plan *IaCPlan) []PlanDiagnostic { return nil }
`

// alreadyDelegatedApplySrc has Apply already calling wfctlhelpers.ApplyPlan.
// The mode must NOT report it as non-canonical and must NOT mutate it.
const alreadyDelegatedApplySrc = `package p

import "context"

type ResourceSpec struct{}
type ResourceState struct{}
type IaCPlan struct{}
type ApplyResult struct{}
type FooProvider struct{}
type PlanDiagnostic struct{}

func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) { return wfctlhelpers.Plan(ctx, p, desired, current) }
func (p *FooProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	return wfctlhelpers.ApplyPlan(ctx, p, plan)
}
func (p *FooProvider) ValidatePlan(plan *IaCPlan) []PlanDiagnostic { return nil }
`

// ============================================================
// Detection (dry-run)
// ============================================================

func TestRefactorApply_DryRun_DetectsCanonical(t *testing.T) {
	path := writeFixture(t, "provider.go", canonicalApplySrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorApply([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "FooProvider.Apply") {
		t.Errorf("report should name FooProvider.Apply; got:\n%s", out)
	}
	if !strings.Contains(out, "canonical") {
		t.Errorf("report should classify as canonical; got:\n%s", out)
	}
	got, _ := os.ReadFile(path)
	if string(got) != canonicalApplySrc {
		t.Errorf("dry-run modified the file; expected no mutation")
	}
}

func TestRefactorApply_DryRun_DetectsDOUpsertRecovery(t *testing.T) {
	path := writeFixture(t, "provider.go", doUpsertApplySrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorApply([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "DOProvider.Apply") {
		t.Errorf("report should name DOProvider.Apply; got:\n%s", out)
	}
	if !strings.Contains(out, "upsert-recovery") {
		t.Errorf("report should classify as upsert-recovery; got:\n%s", out)
	}
	if !strings.Contains(out, "upsertSupporter") {
		t.Errorf("report should suggest upsertSupporter hook patch; got:\n%s", out)
	}
}

func TestRefactorApply_DryRun_DetectsUpdateReplaceCollapse(t *testing.T) {
	path := writeFixture(t, "provider.go", awsUpdateReplaceCollapseSrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorApply([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "AWSProvider.Apply") {
		t.Errorf("report should name AWSProvider.Apply; got:\n%s", out)
	}
	if !strings.Contains(out, "update+replace-collapse") {
		t.Errorf("report should classify as update+replace-collapse; got:\n%s", out)
	}
	if !strings.Contains(out, "manual port required") {
		t.Errorf("report should advise manual port; got:\n%s", out)
	}
	// Must include line numbers for the offending case clause.
	if !strings.Contains(out, ":") {
		t.Errorf("report should include path:line for the offending case; got:\n%s", out)
	}
}

func TestRefactorApply_DryRun_DetectsCustomErrorWrapping(t *testing.T) {
	path := writeFixture(t, "provider.go", customErrorWrapApplySrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorApply([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "WrapProvider.Apply") {
		t.Errorf("report should name WrapProvider.Apply; got:\n%s", out)
	}
	if !strings.Contains(out, "custom-error-wrapping") {
		t.Errorf("report should classify as custom-error-wrapping; got:\n%s", out)
	}
	if !strings.Contains(out, "extension-point") {
		t.Errorf("report should mention extension-point hook; got:\n%s", out)
	}
}

func TestRefactorApply_DryRun_HonorsSkipMarker(t *testing.T) {
	path := writeFixture(t, "provider.go", skippedApplySrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorApply([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Skipped") {
		t.Errorf("report should have a Skipped section; got:\n%s", out)
	}
	if !strings.Contains(out, "FooProvider.Apply") {
		t.Errorf("Skipped section should list FooProvider.Apply; got:\n%s", out)
	}
}

func TestRefactorApply_DryRun_AlreadyDelegated(t *testing.T) {
	path := writeFixture(t, "provider.go", alreadyDelegatedApplySrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorApply([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "already-delegated") {
		t.Errorf("already-delegated Apply should be classified explicitly; got:\n%s", out)
	}
}

// ============================================================
// Mutation (-fix)
// ============================================================

func TestRefactorApply_Fix_RewritesCanonical(t *testing.T) {
	path := writeFixture(t, "provider.go", canonicalApplySrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorApply([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	gotStr := string(got)
	if !strings.Contains(gotStr, "return wfctlhelpers.ApplyPlan(ctx, p, plan)") {
		t.Errorf("rewritten Apply must call wfctlhelpers.ApplyPlan; got:\n%s", gotStr)
	}
	if strings.Contains(gotStr, "switch action.Action {") {
		t.Errorf("canonical switch should be removed by rewrite; got:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, `"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"`) {
		t.Errorf("rewrite should add wfctlhelpers import; got:\n%s", gotStr)
	}
}

func TestRefactorApply_Fix_DoesNotRewriteUpsertRecovery(t *testing.T) {
	path := writeFixture(t, "provider.go", doUpsertApplySrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorApply([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != doUpsertApplySrc {
		t.Errorf("upsert-recovery must NOT be rewritten; file changed:\n%s", string(got))
	}
}

func TestRefactorApply_Fix_DoesNotRewriteUpdateReplaceCollapse(t *testing.T) {
	path := writeFixture(t, "provider.go", awsUpdateReplaceCollapseSrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorApply([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != awsUpdateReplaceCollapseSrc {
		t.Errorf("update+replace-collapse must NOT be rewritten; file changed:\n%s", string(got))
	}
}

func TestRefactorApply_Fix_DoesNotRewriteCustomErrorWrapping(t *testing.T) {
	path := writeFixture(t, "provider.go", customErrorWrapApplySrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorApply([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != customErrorWrapApplySrc {
		t.Errorf("custom-error-wrapping must NOT be rewritten; file changed:\n%s", string(got))
	}
}

func TestRefactorApply_Fix_HonorsSkipMarker(t *testing.T) {
	path := writeFixture(t, "provider.go", skippedApplySrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorApply([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != skippedApplySrc {
		t.Errorf("skip-marker'd Apply must NOT be rewritten; file changed:\n%s", string(got))
	}
}

func TestRefactorApply_Fix_IdempotentOnAlreadyDelegated(t *testing.T) {
	path := writeFixture(t, "provider.go", alreadyDelegatedApplySrc)
	var stdout, stderr bytes.Buffer
	if code := runRefactorApply([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != alreadyDelegatedApplySrc {
		t.Errorf("already-delegated source must be byte-identical after fix (idempotent)")
	}
}

// ============================================================
// codemod-report.md output (per spec line 2388)
// ============================================================

func TestRefactorApply_DryRun_WritesReportFile(t *testing.T) {
	// Per plan §T8.4 line 2388: "Output `codemod-report.md` with per-file
	// findings + suggested handling." When -report-file is supplied the
	// mode writes the report there as well as stdout. Default report
	// filename matches the spec literally.
	dir := t.TempDir()
	reportPath := dir + "/codemod-report.md"
	path := writeFixture(t, "provider.go", doUpsertApplySrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorApply([]string{"-report-file", reportPath, path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	body, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("report file not written: %v", err)
	}
	if !strings.Contains(string(body), "upsert-recovery") {
		t.Errorf("report file must include classification; got:\n%s", string(body))
	}
}

// ============================================================
// Mutation-gate negative tests
// ============================================================

func TestRefactorApply_DryRunFalseWithoutFix_DoesNotMutate(t *testing.T) {
	path := writeFixture(t, "provider.go", canonicalApplySrc)
	stat0, _ := os.Stat(path)
	mtime0 := stat0.ModTime()
	time.Sleep(10 * time.Millisecond)

	var stdout, stderr bytes.Buffer
	code := run([]string{"refactor-apply", "-dry-run=false", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != canonicalApplySrc {
		t.Errorf("file must NOT be mutated; content changed")
	}
	stat1, _ := os.Stat(path)
	if !stat1.ModTime().Equal(mtime0) {
		t.Errorf("file mtime should be unchanged; before=%v after=%v", mtime0, stat1.ModTime())
	}
}
