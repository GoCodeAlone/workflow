package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	pluginexternal "github.com/GoCodeAlone/workflow/plugin/external"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

type candidateEngineLifecycleFixture struct {
	buildErr            error
	startErr            error
	stopErr             error
	startCalls          int
	stopCalls           int
	stopStarted         chan struct{}
	stopRelease         <-chan struct{}
	stopFunc            func()
	startFunc           func(context.Context) error
	requireStopDeadline bool
	missingDeadlineErr  error
}

func (f *candidateEngineLifecycleFixture) BuildFromConfig(*config.WorkflowConfig) error {
	return f.buildErr
}

func (f *candidateEngineLifecycleFixture) Stop(ctx context.Context) error {
	f.stopCalls++
	if f.requireStopDeadline {
		if _, ok := ctx.Deadline(); !ok {
			if f.missingDeadlineErr != nil {
				return f.missingDeadlineErr
			}
			return errors.New("stop context has no deadline")
		}
	}
	if f.stopStarted != nil {
		close(f.stopStarted)
	}
	if f.stopRelease != nil {
		<-f.stopRelease
	}
	if f.stopFunc != nil {
		f.stopFunc()
	}
	return f.stopErr
}

type lifecycleCredentialResolverClient struct {
	accessKey string
}

func (c *lifecycleCredentialResolverClient) DescribeResolvers(context.Context, *pb.CredentialResolverDeclarationsRequest, ...grpc.CallOption) (*pb.CredentialResolverDeclarationsResponse, error) {
	return &pb.CredentialResolverDeclarationsResponse{Resolvers: []*pb.CredentialResolverDeclaration{{
		Provider: "aws", CredentialTypes: []string{"static"},
	}}}, nil
}

func (c *lifecycleCredentialResolverClient) Resolve(_ context.Context, request *pb.CredentialResolveRequest, _ ...grpc.CallOption) (*pb.CredentialResolveResponse, error) {
	return &pb.CredentialResolveResponse{Credentials: &pb.ResolvedCloudCredentials{
		Provider: request.GetProvider(), AccessKey: c.accessKey,
	}}, nil
}

func (f *candidateEngineLifecycleFixture) Start(ctx context.Context) error {
	f.startCalls++
	if f.startFunc != nil {
		return f.startFunc(ctx)
	}
	return f.startErr
}

func (f *candidateEngineLifecycleFixture) RegisteredModuleTypes() []string {
	return []string{"module.fixture"}
}
func (f *candidateEngineLifecycleFixture) RegisteredStepTypes() []string {
	return []string{"step.fixture"}
}
func (f *candidateEngineLifecycleFixture) RegisteredTriggerTypes() []string {
	return []string{"trigger.fixture"}
}

func TestExternalPluginManagerLifecycleExposesStartupManagerAndStopsIt(t *testing.T) {
	manager := pluginexternal.NewExternalPluginManager(t.TempDir(), nil)
	lifecycle := newExternalPluginManagerLifecycleModule(manager)
	app := modular.NewStdApplication(nil, slog.Default())
	if err := lifecycle.Init(app); err != nil {
		t.Fatalf("lifecycle Init: %v", err)
	}
	if err := lifecycle.Init(app); err != nil {
		t.Fatalf("idempotent lifecycle Init: %v", err)
	}
	resolved, err := externalPluginManagerFromApplication(app)
	if err != nil {
		t.Fatalf("externalPluginManagerFromApplication: %v", err)
	}
	if resolved != manager {
		t.Fatal("admin lookup did not return the exact startup plugin manager")
	}
	adminMux, err := newExternalPluginAdminMux(app)
	if err != nil {
		t.Fatalf("newExternalPluginAdminMux: %v", err)
	}
	if adminMux == nil {
		t.Fatal("newExternalPluginAdminMux returned nil")
	}

	stopCalls := 0
	shutdownCtx, cancel := context.WithCancel(context.Background())
	cancel()
	lifecycle.shutdown = func(ctx context.Context) error {
		stopCalls++
		return ctx.Err()
	}
	if err := lifecycle.Stop(shutdownCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("lifecycle Stop: %v", err)
	}
	if stopCalls != 1 {
		t.Fatalf("plugin manager shutdown calls = %d, want 1", stopCalls)
	}
}

func TestExternalPluginAdminMuxCannotLoadAfterEngineStop(t *testing.T) {
	app := modular.NewStdApplication(nil, slog.Default())
	manager := pluginexternal.NewExternalPluginManager(t.TempDir(), nil)
	lifecycle := newExternalPluginManagerLifecycleModule(manager)
	app.RegisterModule(lifecycle)
	if err := app.Init(); err != nil {
		t.Fatalf("application Init: %v", err)
	}
	mux, err := newExternalPluginAdminMux(app)
	if err != nil {
		t.Fatalf("newExternalPluginAdminMux: %v", err)
	}
	activeResponse := httptest.NewRecorder()
	mux.ServeHTTP(activeResponse, httptest.NewRequest(http.MethodGet, "/api/v1/plugins/external/loaded", nil))
	if activeResponse.Code != http.StatusOK {
		t.Fatalf("active manager admin status = %d, want 200", activeResponse.Code)
	}

	engine := workflow.NewStdEngine(app, slog.Default())
	if err := engine.Stop(context.Background()); err != nil {
		t.Fatalf("engine Stop: %v", err)
	}
	staleResponse := httptest.NewRecorder()
	mux.ServeHTTP(staleResponse, httptest.NewRequest(http.MethodPost, "/api/v1/plugins/external/orphan/load", nil))
	if staleResponse.Code != http.StatusInternalServerError || !strings.Contains(staleResponse.Body.String(), "shut down") {
		t.Fatalf("stale admin load response = %d %s, want terminal shutdown error", staleResponse.Code, staleResponse.Body.String())
	}
}

func TestCommitCandidateEnginePromotesOnlyAfterOldRetiresAndCandidateStarts(t *testing.T) {
	oldStopStarted := make(chan struct{})
	releaseOldStop := make(chan struct{})
	oldEngine := &candidateEngineLifecycleFixture{stopStarted: oldStopStarted, stopRelease: releaseOldStop}
	candidate := &candidateEngineLifecycleFixture{}
	activated := make(chan struct{}, 1)
	commitDone := make(chan error, 1)
	go func() {
		commitDone <- commitCandidateEngine(
			context.Background(),
			oldEngine,
			candidate,
			nil,
			func() error { activated <- struct{}{}; return nil },
		)
	}()
	<-oldStopStarted
	if candidate.startCalls != 0 {
		t.Fatalf("candidate started before old retirement: %d", candidate.startCalls)
	}
	select {
	case <-activated:
		t.Fatal("candidate resolvers promoted while old Stop was blocked")
	default:
	}
	close(releaseOldStop)
	if err := <-commitDone; err != nil {
		t.Fatalf("commitCandidateEngine: %v", err)
	}
	if candidate.startCalls != 1 {
		t.Fatalf("candidate start calls = %d, want 1", candidate.startCalls)
	}
	select {
	case <-activated:
	default:
		t.Fatal("candidate resolvers were not promoted after acceptance")
	}
}

func TestServerAppSerializesReloadTransactionAndKeepsFinalGenerationAligned(t *testing.T) {
	applicationA := modular.NewStdApplication(nil, slog.Default())
	applicationB := modular.NewStdApplication(nil, slog.Default())
	engineA := workflow.NewStdEngine(applicationA, slog.Default())
	engineB := workflow.NewStdEngine(applicationB, slog.Default())
	configA := config.NewEmptyWorkflowConfig()
	configB := config.NewEmptyWorkflowConfig()
	registrationA, err := module.PrepareOwnedExternalCredentialResolver(
		context.Background(),
		"server-reload-serialization",
		&lifecycleCredentialResolverClient{accessKey: "generation-a"},
	)
	if err != nil {
		t.Fatalf("prepare generation A: %v", err)
	}
	defer registrationA.Close()
	registrationB, err := module.PrepareOwnedExternalCredentialResolver(
		context.Background(),
		"server-reload-serialization",
		&lifecycleCredentialResolverClient{accessKey: "generation-b"},
	)
	if err != nil {
		t.Fatalf("prepare generation B: %v", err)
	}
	defer registrationB.Close()

	app := &serverApp{}
	aEntered := make(chan struct{})
	releaseA := make(chan struct{})
	aDone := make(chan error, 1)
	go func() {
		aDone <- app.withReloadTransaction(func() error {
			close(aEntered)
			<-releaseA
			if err := registrationA.Activate(); err != nil {
				return err
			}
			app.engine = engineA
			app.currentConfig = configA
			return nil
		})
	}()
	<-aEntered

	bAttempted := make(chan struct{})
	bEntered := make(chan struct{})
	bDone := make(chan error, 1)
	go func() {
		close(bAttempted)
		bDone <- app.withReloadTransaction(func() error {
			close(bEntered)
			drain, err := module.TransitionExternalCredentialResolverRegistration(registrationA, registrationB)
			if err != nil {
				return err
			}
			if err := drain.Wait(context.Background()); err != nil {
				return err
			}
			app.engine = engineB
			app.currentConfig = configB
			return nil
		})
	}()
	<-bAttempted
	select {
	case <-bEntered:
		t.Fatal("reload B entered while reload A still held the transaction")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseA)
	if err := <-aDone; err != nil {
		t.Fatalf("reload A: %v", err)
	}
	if err := <-bDone; err != nil {
		t.Fatalf("reload B: %v", err)
	}
	if app.engine != engineB || app.currentConfig != configB {
		t.Fatalf("final reload state is misaligned: engine=%p config=%p", app.engine, app.currentConfig)
	}
	credentials, err := module.ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{})
	if err != nil || credentials.AccessKey != "generation-b" {
		t.Fatalf("final resolver generation = %+v, %v", credentials, err)
	}
}

func TestBuildEngineFromConfigStopsCandidateOnBuildFailure(t *testing.T) {
	buildErr := errors.New("candidate build failed")
	stopErr := errors.New("candidate cleanup failed")
	engine := &candidateEngineLifecycleFixture{buildErr: buildErr, stopErr: stopErr, requireStopDeadline: true}
	cleanupCalls := 0
	err := buildEngineFromConfig(context.Background(), engine, config.NewEmptyWorkflowConfig(), func() { cleanupCalls++ })
	if !errors.Is(err, buildErr) || !errors.Is(err, stopErr) {
		t.Fatalf("buildEngineFromConfig error = %v", err)
	}
	if engine.stopCalls != 1 {
		t.Fatalf("candidate stop calls = %d, want 1", engine.stopCalls)
	}
	if cleanupCalls != 1 {
		t.Fatalf("external plugin cleanup calls = %d, want 1", cleanupCalls)
	}
}

func TestDetachedCleanupContextReplacesExpiredDeadline(t *testing.T) {
	parent, cancelParent := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelParent()
	cleanupCtx, cancelCleanup := detachedCleanupContext(parent)
	defer cancelCleanup()
	deadline, ok := cleanupCtx.Deadline()
	if !ok {
		t.Fatal("cleanup context has no deadline")
	}
	if cleanupCtx.Err() != nil || time.Until(deadline) <= 0 {
		t.Fatalf("cleanup context inherited expired state: deadline=%v error=%v", deadline, cleanupCtx.Err())
	}
}

func TestInspectCandidateEngineStopsAfterCollectingTypes(t *testing.T) {
	engine := &candidateEngineLifecycleFixture{requireStopDeadline: true}
	result, err := inspectAndStopCandidateEngine(context.Background(), engine)
	if err != nil {
		t.Fatalf("inspectAndStopCandidateEngine: %v", err)
	}
	if engine.stopCalls != 1 {
		t.Fatalf("candidate stop calls = %d, want 1", engine.stopCalls)
	}
	if result.Status != "build_ok" || len(result.ModuleTypes) != 1 || result.ModuleTypes[0] != "module.fixture" {
		t.Fatalf("candidate result = %+v", result)
	}
}

func TestTryActivateInspectionDoesNotPromoteStagedResolver(t *testing.T) {
	live, err := module.PrepareOwnedExternalCredentialResolver(
		context.Background(),
		"server-try-activate-owner",
		&lifecycleCredentialResolverClient{accessKey: "live"},
	)
	if err != nil {
		t.Fatalf("prepare live resolver: %v", err)
	}
	if err := live.Activate(); err != nil {
		t.Fatalf("activate live resolver: %v", err)
	}
	defer live.Close()
	candidate, err := module.PrepareOwnedExternalCredentialResolver(
		context.Background(),
		"server-try-activate-owner",
		&lifecycleCredentialResolverClient{accessKey: "candidate"},
	)
	if err != nil {
		t.Fatalf("prepare candidate resolver: %v", err)
	}
	engine := &candidateEngineLifecycleFixture{stopFunc: candidate.Close, requireStopDeadline: true}
	if _, err := inspectAndStopCandidateEngine(context.Background(), engine); err != nil {
		t.Fatalf("inspect candidate: %v", err)
	}
	credentials, err := module.ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{})
	if err != nil || credentials.AccessKey != "live" {
		t.Fatalf("try-activate inspection displaced live resolver: %+v, %v", credentials, err)
	}
}

func TestCommitCandidateEngineActivationFailureStopsCandidate(t *testing.T) {
	activationErr := errors.New("activation failed")
	stopErr := errors.New("candidate stop failed")
	old := &candidateEngineLifecycleFixture{requireStopDeadline: true}
	candidate := &candidateEngineLifecycleFixture{stopErr: stopErr, requireStopDeadline: true}
	err := commitCandidateEngine(context.Background(), old, candidate, nil, func() error { return activationErr })
	if !errors.Is(err, activationErr) || !errors.Is(err, stopErr) {
		t.Fatalf("commit activation error = %v", err)
	}
	if candidate.startCalls != 1 || candidate.stopCalls != 1 {
		t.Fatalf("candidate lifecycle calls = start %d stop %d; want 1 each", candidate.startCalls, candidate.stopCalls)
	}
	if old.stopCalls != 1 {
		t.Fatalf("old engine stop calls = %d, want 1", old.stopCalls)
	}
}

func TestRollbackCommitGetsFreshBoundedContextAfterCandidateDeadline(t *testing.T) {
	type rollbackContextKey struct{}
	parentWithValue := context.WithValue(context.Background(), rollbackContextKey{}, "preserved")
	parent, cancelParent := context.WithDeadline(parentWithValue, time.Now().Add(-time.Second))
	defer cancelParent()
	candidate := &candidateEngineLifecycleFixture{
		requireStopDeadline: true,
		startFunc: func(ctx context.Context) error {
			return ctx.Err()
		},
	}
	if err := commitCandidateEngine(parent, nil, candidate, nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("candidate commit error = %v, want deadline exceeded", err)
	}

	rollbackCtx, cancelRollback := detachedOperationContext(parent, engineOperationTimeout)
	defer cancelRollback()
	rollback := &candidateEngineLifecycleFixture{
		startFunc: func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) <= 0 {
				return errors.New("rollback start context has no live deadline")
			}
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("rollback start context is canceled: %w", err)
			}
			if value := ctx.Value(rollbackContextKey{}); value != "preserved" {
				return fmt.Errorf("rollback context value = %v", value)
			}
			return nil
		},
	}
	if err := commitCandidateEngine(rollbackCtx, nil, rollback, nil, nil); err != nil {
		t.Fatalf("rollback commit with fresh bounded context: %v", err)
	}
}

func TestRegisterPostStartServicesReplacesExternalPluginAdminMuxForNewEngine(t *testing.T) {
	newEngine := func() (*workflow.StdEngine, *externalPluginManagerLifecycleModule) {
		application := modular.NewStdApplication(nil, slog.Default())
		engine := workflow.NewStdEngine(application, slog.Default())
		manager := pluginexternal.NewExternalPluginManager(t.TempDir(), nil)
		lifecycle := newExternalPluginManagerLifecycleModule(manager)
		if err := lifecycle.Init(application); err != nil {
			t.Fatalf("lifecycle Init: %v", err)
		}
		return engine, lifecycle
	}

	firstEngine, firstLifecycle := newEngine()
	app := &serverApp{engine: firstEngine}
	if err := app.registerPostStartServices(slog.Default()); err != nil {
		t.Fatalf("register first post-start services: %v", err)
	}
	firstMux := app.services.externalPluginMux
	if firstMux == nil {
		t.Fatal("first external plugin admin mux is nil")
	}
	if err := firstLifecycle.Stop(context.Background()); err != nil {
		t.Fatalf("stop first lifecycle: %v", err)
	}

	secondEngine, secondLifecycle := newEngine()
	t.Cleanup(func() { _ = secondLifecycle.Stop(context.Background()) })
	app.engine = secondEngine
	if err := app.registerPostStartServices(slog.Default()); err != nil {
		t.Fatalf("register second post-start services: %v", err)
	}
	if app.services.externalPluginMux == nil || app.services.externalPluginMux == firstMux {
		t.Fatal("engine replacement retained the stopped manager's external plugin admin mux")
	}
}

func TestStartEngineWithCleanupJoinsStartAndStopFailures(t *testing.T) {
	startErr := errors.New("start failed")
	stopErr := errors.New("stop failed")
	engine := &candidateEngineLifecycleFixture{startErr: startErr, stopErr: stopErr, requireStopDeadline: true}
	err := startEngineWithCleanup(context.Background(), engine, "start fixture")
	if !errors.Is(err, startErr) || !errors.Is(err, stopErr) {
		t.Fatalf("startEngineWithCleanup error = %v", err)
	}
	if engine.startCalls != 1 || engine.stopCalls != 1 {
		t.Fatalf("lifecycle calls = start %d, stop %d; want 1 each", engine.startCalls, engine.stopCalls)
	}
}

func TestRunPostStartHooksWithCleanupStopsEngine(t *testing.T) {
	hookErr := errors.New("hook failed")
	stopErr := errors.New("stop failed")
	engine := &candidateEngineLifecycleFixture{stopErr: stopErr, requireStopDeadline: true}
	err := runPostStartHooksWithCleanup(context.Background(), engine, []func() error{
		func() error { return hookErr },
	})
	if !errors.Is(err, hookErr) || !errors.Is(err, stopErr) {
		t.Fatalf("runPostStartHooksWithCleanup error = %v", err)
	}
	if engine.stopCalls != 1 {
		t.Fatalf("stop calls = %d, want 1", engine.stopCalls)
	}
}
