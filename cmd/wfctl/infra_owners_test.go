package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/iactest"
	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestRunInfraOwnersListsOwnedResources(t *testing.T) {
	var stdout, stderr bytes.Buffer
	prevOut, prevErr, prevLoader := ownersStdout, ownersStderr, ownersLoadProviders
	ownersStdout, ownersStderr = &stdout, &stderr
	ownersLoadProviders = func(context.Context, *flag.FlagSet, string, string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{&ownersCmdProvider{}}, nil, nil
	}
	t.Cleanup(func() {
		ownersStdout, ownersStderr, ownersLoadProviders = prevOut, prevErr, prevLoader
	})

	if err := runInfraOwners([]string{"--owner", "team-a", "--type", "infra.container_service"}); err != nil {
		t.Fatalf("runInfraOwners: %v\nstderr=%s", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"PROVIDER\tTYPE\tNAME\tPROVIDER_ID\tOWNER\tSOURCE",
		"owners-stub\tinfra.container_service\tapp\tapp-1\tteam-a\ttag:managed-by",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
}

func TestRunInfraOwnersSkipsUnsupportedProviders(t *testing.T) {
	var stdout bytes.Buffer
	prevOut, prevErr, prevLoader := ownersStdout, ownersStderr, ownersLoadProviders
	ownersStdout, ownersStderr = &stdout, io.Discard
	ownersLoadProviders = func(context.Context, *flag.FlagSet, string, string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{&iactest.NoopProvider{ProviderName: "plain"}}, nil, nil
	}
	t.Cleanup(func() {
		ownersStdout, ownersStderr, ownersLoadProviders = prevOut, prevErr, prevLoader
	})

	if err := runInfraOwners([]string{"--owner", "team-a"}); err != nil {
		t.Fatalf("runInfraOwners: %v", err)
	}
	if !strings.Contains(stdout.String(), "skipped plain: provider does not implement OwnershipProvider") {
		t.Fatalf("stdout did not show skip:\n%s", stdout.String())
	}
}

func TestRunInfraOwnersSkipsTypedAdapterUnsupportedSentinel(t *testing.T) {
	var stdout bytes.Buffer
	prevOut, prevErr, prevLoader := ownersStdout, ownersStderr, ownersLoadProviders
	ownersStdout, ownersStderr = &stdout, io.Discard
	ownersLoadProviders = func(context.Context, *flag.FlagSet, string, string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{&ownersUnsupportedProvider{}}, nil, nil
	}
	t.Cleanup(func() {
		ownersStdout, ownersStderr, ownersLoadProviders = prevOut, prevErr, prevLoader
	})

	if err := runInfraOwners([]string{"--owner", "team-a"}); err != nil {
		t.Fatalf("runInfraOwners: %v", err)
	}
	if !strings.Contains(stdout.String(), "skipped owners-unsupported: provider does not implement OwnershipProvider") {
		t.Fatalf("stdout did not show skip:\n%s", stdout.String())
	}
}

type ownersCmdProvider struct {
	iactest.NoopProvider
}

func (p *ownersCmdProvider) Name() string { return "owners-stub" }

func (p *ownersCmdProvider) GetOwner(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOwner, error) {
	return &interfaces.ResourceOwner{Ref: ref, Owner: "team-a", Source: "tag:managed-by"}, nil
}

func (p *ownersCmdProvider) SetOwner(context.Context, interfaces.ResourceRef, string) error {
	return nil
}

func (p *ownersCmdProvider) ListOwners(_ context.Context, filter interfaces.OwnerFilter) ([]interfaces.ResourceOwner, error) {
	return []interfaces.ResourceOwner{{
		Ref:    interfaces.ResourceRef{Name: "app", Type: filter.ResourceType, ProviderID: "app-1"},
		Owner:  filter.Owner,
		Source: "tag:managed-by",
	}}, nil
}

type ownersUnsupportedProvider struct {
	iactest.NoopProvider
}

func (p *ownersUnsupportedProvider) Name() string { return "owners-unsupported" }

func (p *ownersUnsupportedProvider) GetOwner(context.Context, interfaces.ResourceRef) (*interfaces.ResourceOwner, error) {
	return nil, interfaces.ErrProviderMethodUnimplemented
}

func (p *ownersUnsupportedProvider) SetOwner(context.Context, interfaces.ResourceRef, string) error {
	return interfaces.ErrProviderMethodUnimplemented
}

func (p *ownersUnsupportedProvider) ListOwners(context.Context, interfaces.OwnerFilter) ([]interfaces.ResourceOwner, error) {
	return nil, interfaces.ErrProviderMethodUnimplemented
}
