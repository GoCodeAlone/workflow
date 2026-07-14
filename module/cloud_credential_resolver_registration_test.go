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
	accessKey       string
	credentialTypes []string
	resolveStarted  chan struct{}
	resolveRelease  <-chan struct{}
	startOnce       sync.Once
}

type preparedCredentialResolverProvider struct {
	registrations []*ExternalCredentialResolverRegistration
}

func (p preparedCredentialResolverProvider) CredentialResolverRegistrations() []*ExternalCredentialResolverRegistration {
	return p.registrations
}

func (c *preparedCredentialResolverClient) DescribeResolvers(context.Context, *pb.CredentialResolverDeclarationsRequest, ...grpc.CallOption) (*pb.CredentialResolverDeclarationsResponse, error) {
	credentialTypes := c.credentialTypes
	if len(credentialTypes) == 0 {
		credentialTypes = []string{"static"}
	}
	return &pb.CredentialResolverDeclarationsResponse{Resolvers: []*pb.CredentialResolverDeclaration{{
		Provider: "aws", CredentialTypes: credentialTypes,
	}}}, nil
}

func (c *preparedCredentialResolverClient) Resolve(_ context.Context, request *pb.CredentialResolveRequest, _ ...grpc.CallOption) (*pb.CredentialResolveResponse, error) {
	if c.resolveStarted != nil {
		c.startOnce.Do(func() { close(c.resolveStarted) })
	}
	if c.resolveRelease != nil {
		<-c.resolveRelease
	}
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

func TestOwnedExternalCredentialResolverLatestGenerationShadowsDroppedType(t *testing.T) {
	oldRegistration, err := PrepareOwnedExternalCredentialResolver(
		context.Background(),
		"plugin-owner:dropped-type",
		&preparedCredentialResolverClient{accessKey: "old", credentialTypes: []string{"static", "env"}},
	)
	if err != nil {
		t.Fatalf("prepare old: %v", err)
	}
	if err := oldRegistration.Activate(); err != nil {
		t.Fatalf("activate old: %v", err)
	}
	t.Cleanup(oldRegistration.Close)

	newRegistration, err := PrepareOwnedExternalCredentialResolver(
		context.Background(),
		"plugin-owner:dropped-type",
		&preparedCredentialResolverClient{accessKey: "new", credentialTypes: []string{"static"}},
	)
	if err != nil {
		t.Fatalf("prepare new: %v", err)
	}
	if err := newRegistration.Activate(); err != nil {
		t.Fatalf("activate new: %v", err)
	}
	t.Cleanup(newRegistration.Close)

	if _, err := ResolveExternalCloudCredentials(context.Background(), "aws", "env", map[string]any{}); err == nil || !strings.Contains(err.Error(), "no external credential resolver") {
		t.Fatalf("dropped type resolved through an old generation: %v", err)
	}
	newRegistration.Close()
	credentials, err := ResolveExternalCloudCredentials(context.Background(), "aws", "env", map[string]any{})
	if err != nil || credentials.AccessKey != "old" {
		t.Fatalf("restored old generation = %+v, %v", credentials, err)
	}
}

func TestOwnedExternalCredentialResolverLatestGenerationShadowsDroppedResolver(t *testing.T) {
	oldRegistration, err := PrepareOwnedExternalCredentialResolver(
		context.Background(),
		"plugin-owner:dropped-resolver",
		&preparedCredentialResolverClient{accessKey: "old"},
	)
	if err != nil {
		t.Fatalf("prepare old: %v", err)
	}
	if err := oldRegistration.Activate(); err != nil {
		t.Fatalf("activate old: %v", err)
	}
	t.Cleanup(oldRegistration.Close)
	tombstone, err := PrepareExternalCredentialResolverOwner("plugin-owner:dropped-resolver")
	if err != nil {
		t.Fatalf("prepare owner tombstone: %v", err)
	}
	if err := tombstone.Activate(); err != nil {
		t.Fatalf("activate owner tombstone: %v", err)
	}
	t.Cleanup(tombstone.Close)
	if _, err := ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{}); err == nil || !strings.Contains(err.Error(), "no external credential resolver") {
		t.Fatalf("dropped resolver fell back to old generation: %v", err)
	}
	tombstone.Close()
	credentials, err := ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{})
	if err != nil || credentials.AccessKey != "old" {
		t.Fatalf("restored old resolver = %+v, %v", credentials, err)
	}
}

func TestExternalCredentialResolverReplacementDrainsInFlightResolution(t *testing.T) {
	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-releaseOld:
		default:
			close(releaseOld)
		}
	})
	oldRegistration, err := PrepareOwnedExternalCredentialResolver(
		context.Background(),
		"plugin-owner:drain",
		&preparedCredentialResolverClient{accessKey: "old", resolveStarted: oldStarted, resolveRelease: releaseOld},
	)
	if err != nil {
		t.Fatalf("prepare old: %v", err)
	}
	if err := oldRegistration.Activate(); err != nil {
		t.Fatalf("activate old: %v", err)
	}
	t.Cleanup(oldRegistration.Close)
	newRegistration, err := PrepareOwnedExternalCredentialResolver(
		context.Background(),
		"plugin-owner:drain",
		&preparedCredentialResolverClient{accessKey: "new"},
	)
	if err != nil {
		t.Fatalf("prepare new: %v", err)
	}
	t.Cleanup(newRegistration.Close)

	oldResult := make(chan string, 1)
	go func() {
		credentials, resolveErr := ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{})
		if resolveErr != nil {
			oldResult <- "error:" + resolveErr.Error()
			return
		}
		oldResult <- credentials.AccessKey
	}()
	<-oldStarted

	swapLocked := make(chan struct{})
	releaseSwap := make(chan struct{})
	swapDone := make(chan error, 1)
	go func() {
		swapDone <- replaceExternalCredentialResolverRegistration(oldRegistration, newRegistration, func() {
			close(swapLocked)
			<-releaseSwap
		})
	}()
	<-swapLocked
	newResult := make(chan string, 1)
	go func() {
		credentials, resolveErr := ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{})
		if resolveErr != nil {
			newResult <- "error:" + resolveErr.Error()
			return
		}
		newResult <- credentials.AccessKey
	}()
	close(releaseSwap)
	if result := <-newResult; result != "new" {
		t.Fatalf("post-swap resolution = %q, want new", result)
	}
	select {
	case err := <-swapDone:
		t.Fatalf("replacement returned before old resolution drained: %v", err)
	default:
	}
	close(releaseOld)
	if result := <-oldResult; result != "old" {
		t.Fatalf("in-flight old resolution = %q, want old", result)
	}
	if err := <-swapDone; err != nil {
		t.Fatalf("replacement: %v", err)
	}
}

func TestCloudAccountScopedCandidateResolverDoesNotDisplaceLiveResolution(t *testing.T) {
	liveRegistration, err := PrepareOwnedExternalCredentialResolver(
		context.Background(),
		"plugin-owner:candidate-isolation",
		&preparedCredentialResolverClient{accessKey: "live"},
	)
	if err != nil {
		t.Fatalf("prepare live: %v", err)
	}
	if err := liveRegistration.Activate(); err != nil {
		t.Fatalf("activate live: %v", err)
	}
	t.Cleanup(liveRegistration.Close)

	candidateStarted := make(chan struct{})
	releaseCandidate := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-releaseCandidate:
		default:
			close(releaseCandidate)
		}
	})
	candidateRegistration, err := PrepareOwnedExternalCredentialResolver(
		context.Background(),
		"plugin-owner:candidate-isolation",
		&preparedCredentialResolverClient{accessKey: "candidate", resolveStarted: candidateStarted, resolveRelease: releaseCandidate},
	)
	if err != nil {
		t.Fatalf("prepare candidate: %v", err)
	}
	t.Cleanup(candidateRegistration.Close)

	app := NewMockApplication()
	if err := app.RegisterService(ExternalCredentialResolverRegistrationProviderServiceName, preparedCredentialResolverProvider{
		registrations: []*ExternalCredentialResolverRegistration{candidateRegistration},
	}); err != nil {
		t.Fatalf("register candidate resolver provider: %v", err)
	}
	candidateAccount := NewCloudAccount("candidate-account", map[string]any{
		"provider":    "aws",
		"credentials": map[string]any{"type": "static"},
	})
	candidateDone := make(chan error, 1)
	go func() { candidateDone <- candidateAccount.Init(app) }()
	<-candidateStarted

	const liveCallers = 32
	results := make(chan string, liveCallers)
	var wait sync.WaitGroup
	for range liveCallers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			credentials, resolveErr := ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{})
			if resolveErr != nil {
				results <- "error:" + resolveErr.Error()
				return
			}
			results <- credentials.AccessKey
		}()
	}
	wait.Wait()
	close(results)
	for result := range results {
		if result != "live" {
			t.Fatalf("live resolution routed through candidate: %q", result)
		}
	}
	close(releaseCandidate)
	if err := <-candidateDone; err != nil {
		t.Fatalf("candidate CloudAccount.Init: %v", err)
	}
	candidateCredentials, err := candidateAccount.GetCredentials(context.Background())
	if err != nil || candidateCredentials.AccessKey != "candidate" {
		t.Fatalf("candidate credentials = %+v, %v", candidateCredentials, err)
	}
	candidateRegistration.Close()
	credentials, err := ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{})
	if err != nil || credentials.AccessKey != "live" {
		t.Fatalf("live credentials after candidate cleanup = %+v, %v", credentials, err)
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
