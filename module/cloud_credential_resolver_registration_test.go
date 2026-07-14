package module

import (
	"context"
	"strings"
	"sync"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

type preparedCredentialResolverClient struct {
	accessKey string
}

func (c *preparedCredentialResolverClient) DescribeResolvers(context.Context, *pb.CredentialResolverDeclarationsRequest, ...grpc.CallOption) (*pb.CredentialResolverDeclarationsResponse, error) {
	return &pb.CredentialResolverDeclarationsResponse{Resolvers: []*pb.CredentialResolverDeclaration{{
		Provider: "aws", CredentialTypes: []string{"static"},
	}}}, nil
}

func (c *preparedCredentialResolverClient) Resolve(_ context.Context, request *pb.CredentialResolveRequest, _ ...grpc.CallOption) (*pb.CredentialResolveResponse, error) {
	return &pb.CredentialResolveResponse{Credentials: &pb.ResolvedCloudCredentials{
		Provider: request.GetProvider(), AccessKey: c.accessKey,
	}}, nil
}

func TestPreparedExternalCredentialResolverActivationAndClose(t *testing.T) {
	registration, err := PrepareExternalCredentialResolver(context.Background(), &preparedCredentialResolverClient{accessKey: "prepared"})
	if err != nil {
		t.Fatalf("PrepareExternalCredentialResolver: %v", err)
	}
	assertNoPreparedCredentialResolver(t)
	if err := registration.Activate(); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	credentials, err := ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{})
	if err != nil || credentials.AccessKey != "prepared" {
		t.Fatalf("active resolution = %+v, %v", credentials, err)
	}
	registration.Close()
	registration.Close()
	assertNoPreparedCredentialResolver(t)
}

func TestExternalCredentialResolverReplacementIsAtomic(t *testing.T) {
	oldRegistration, err := PrepareExternalCredentialResolver(context.Background(), &preparedCredentialResolverClient{accessKey: "old"})
	if err != nil {
		t.Fatalf("prepare old: %v", err)
	}
	if err := oldRegistration.Activate(); err != nil {
		t.Fatalf("activate old: %v", err)
	}
	t.Cleanup(oldRegistration.Close)
	newRegistration, err := PrepareExternalCredentialResolver(context.Background(), &preparedCredentialResolverClient{accessKey: "new"})
	if err != nil {
		t.Fatalf("prepare new: %v", err)
	}
	t.Cleanup(newRegistration.Close)

	swapEntered := make(chan struct{})
	releaseSwap := make(chan struct{})
	writeLockHeld := make(chan bool, 1)
	swapDone := make(chan error, 1)
	go func() {
		swapDone <- replaceExternalCredentialResolverRegistration(oldRegistration, newRegistration, func() {
			acquiredReadLock := credentialResolvers.TryRLock()
			if acquiredReadLock {
				credentialResolvers.RUnlock()
			}
			writeLockHeld <- !acquiredReadLock
			close(swapEntered)
			<-releaseSwap
		})
	}()
	<-swapEntered
	if !<-writeLockHeld {
		close(releaseSwap)
		t.Fatal("replacement hook did not run under the registry write lock")
	}

	const callers = 32
	started := make(chan struct{}, callers)
	results := make(chan string, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			started <- struct{}{}
			credentials, resolveErr := ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{})
			if resolveErr != nil {
				results <- "error:" + resolveErr.Error()
				return
			}
			results <- credentials.AccessKey
		}()
	}
	for range callers {
		<-started
	}
	select {
	case result := <-results:
		t.Fatalf("resolution escaped an in-progress atomic swap: %q", result)
	default:
	}
	close(releaseSwap)
	if err := <-swapDone; err != nil {
		t.Fatalf("replace registration: %v", err)
	}
	wait.Wait()
	close(results)
	for result := range results {
		if result != "new" {
			t.Fatalf("resolution observed non-atomic replacement result %q", result)
		}
	}
	assertNoResolverErrorContains(t, "multiple external credential resolvers")
}

func TestOwnedExternalCredentialResolverSelectsLatestAndRestoresPrevious(t *testing.T) {
	prepare := func(accessKey string) *ExternalCredentialResolverRegistration {
		t.Helper()
		registration, err := PrepareOwnedExternalCredentialResolver(
			context.Background(),
			"plugin-owner:resolver-fixture",
			&preparedCredentialResolverClient{accessKey: accessKey},
		)
		if err != nil {
			t.Fatalf("PrepareOwnedExternalCredentialResolver(%q): %v", accessKey, err)
		}
		if err := registration.Activate(); err != nil {
			t.Fatalf("Activate(%q): %v", accessKey, err)
		}
		t.Cleanup(registration.Close)
		return registration
	}

	oldRegistration := prepare("old")
	nonLatestRegistration := prepare("middle")
	latestRegistration := prepare("latest")
	assertPreparedCredentialResolverAccessKey(t, "latest")

	nonLatestRegistration.Close()
	assertPreparedCredentialResolverAccessKey(t, "latest")

	latestRegistration.Close()
	assertPreparedCredentialResolverAccessKey(t, "old")

	oldRegistration.Close()
	assertNoPreparedCredentialResolver(t)
}

func TestOwnedExternalCredentialResolverRejectsAmbiguousOwner(t *testing.T) {
	for _, owner := range []string{"", " ", " owner", "owner ", "owner\x00other"} {
		t.Run(owner, func(t *testing.T) {
			registration, err := PrepareOwnedExternalCredentialResolver(
				context.Background(),
				owner,
				&preparedCredentialResolverClient{accessKey: "unused"},
			)
			if err == nil || registration != nil {
				t.Fatalf("PrepareOwnedExternalCredentialResolver(%q) = %+v, %v; want rejection", owner, registration, err)
			}
		})
	}
}

func assertNoPreparedCredentialResolver(t *testing.T) {
	t.Helper()
	_, err := ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "install a plugin") {
		t.Fatalf("expected no active external resolver, got %v", err)
	}
}

func assertPreparedCredentialResolverAccessKey(t *testing.T, want string) {
	t.Helper()
	credentials, err := ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{})
	if err != nil || credentials.AccessKey != want {
		t.Fatalf("resolved access key = %q, %v; want %q", credentials.AccessKey, err, want)
	}
}

func assertNoResolverErrorContains(t *testing.T, forbidden string) {
	t.Helper()
	credentials, err := ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{})
	if err != nil && strings.Contains(err.Error(), forbidden) {
		t.Fatalf("resolver registry retained a duplicate: %v", err)
	}
	if err != nil || credentials.AccessKey != "new" {
		t.Fatalf("post-swap resolution = %+v, %v", credentials, err)
	}
}
