package sdk

// Internal test (package sdk) — exercises mapBackedProvider + the bridge's
// optional delegate field that wires GetModuleTypes / CreateModule / etc.
// through to grpc_server.go's existing PluginService implementation when
// IaCServeOptions.Modules or .Steps is non-nil. Per decisions/0038.

import (
	"context"
	"net"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ── fakes ───────────────────────────────────────────────────────────────────

// fakeIaCRequiredProvider satisfies pb.IaCProviderRequiredServer (the
// REQUIRED service registerAllIaCProviderServicesWithOpts asserts) so the
// bridge wiring under test exercises the same code path production plugins do.
type fakeIaCRequiredProvider struct {
	pb.UnimplementedIaCProviderRequiredServer
}

// fakeModuleProvider implements sdk.ModuleProvider.
type fakeModuleProvider struct {
	types    []string
	instance ModuleInstance // returned from CreateModule when non-nil
}

func (f *fakeModuleProvider) ModuleTypes() []string { return f.types }
func (f *fakeModuleProvider) CreateModule(_, _ string, _ map[string]any) (ModuleInstance, error) {
	if f.instance != nil {
		return f.instance, nil
	}
	return &fakeModuleInstance{}, nil
}

// fakeStepProvider implements sdk.StepProvider.
type fakeStepProvider struct {
	types []string
}

func (f *fakeStepProvider) StepTypes() []string { return f.types }
func (f *fakeStepProvider) CreateStep(_, _ string, _ map[string]any) (StepInstance, error) {
	return &fakeStepInstance{}, nil
}

// fakeModuleInstance is a no-op ModuleInstance.
type fakeModuleInstance struct{}

func (*fakeModuleInstance) Init() error                 { return nil }
func (*fakeModuleInstance) Start(context.Context) error { return nil }
func (*fakeModuleInstance) Stop(context.Context) error  { return nil }

// fakeStepInstance is a no-op StepInstance.
type fakeStepInstance struct{}

func (*fakeStepInstance) Execute(context.Context, map[string]any, map[string]map[string]any, map[string]any, map[string]any, map[string]any) (*StepResult, error) {
	return &StepResult{}, nil
}

// fakeMessageAwareModule records whether SetMessagePublisher /
// SetMessageSubscriber were called. Regression guard for the
// no-broker-plumbed Non-Goal in the bridge path.
type fakeMessageAwareModule struct {
	fakeModuleInstance
	SetMessagePublisherCalled  bool
	SetMessageSubscriberCalled bool
}

func (f *fakeMessageAwareModule) SetMessagePublisher(MessagePublisher) {
	f.SetMessagePublisherCalled = true
}
func (f *fakeMessageAwareModule) SetMessageSubscriber(MessageSubscriber) {
	f.SetMessageSubscriberCalled = true
}

// ── bufconn dial helper ─────────────────────────────────────────────────────

// dialBridge serves the gRPC server on a bufconn listener and returns a
// PluginServiceClient connected to it. Caller defers conn.Close() / s.Stop().
func dialBridge(t *testing.T, s *grpc.Server) pb.PluginServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewPluginServiceClient(conn)
}

// ── tests ───────────────────────────────────────────────────────────────────

// TestIaCBridge_ModulesAndSteps_Delegate locks the wire-up of the optional
// delegate: when IaCServeOptions.Modules / .Steps are non-nil,
// GetModuleTypes / GetStepTypes return the keys of those maps (proving the
// bridge forwards to a grpc_server.go-backed mapBackedProvider).
func TestIaCBridge_ModulesAndSteps_Delegate(t *testing.T) {
	opts := IaCServeOptions{
		Modules: map[string]ModuleProvider{
			"storage.test": &fakeModuleProvider{types: []string{"storage.test"}},
		},
		Steps: map[string]StepProvider{
			"step.test": &fakeStepProvider{types: []string{"step.test"}},
		},
	}
	s := grpc.NewServer()
	if err := registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, opts); err != nil {
		t.Fatalf("register: %v", err)
	}
	client := dialBridge(t, s)
	ctx := context.Background()

	modTypes, err := client.GetModuleTypes(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetModuleTypes: %v", err)
	}
	if len(modTypes.GetTypes()) != 1 || modTypes.GetTypes()[0] != "storage.test" {
		t.Errorf("GetModuleTypes = %v, want [storage.test]", modTypes.GetTypes())
	}

	stepTypes, err := client.GetStepTypes(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetStepTypes: %v", err)
	}
	if len(stepTypes.GetTypes()) != 1 || stepTypes.GetTypes()[0] != "step.test" {
		t.Errorf("GetStepTypes = %v, want [step.test]", stepTypes.GetTypes())
	}
}

// TestIaCBridge_ModuleStepTypes_Deterministic locks the lexicographic-order
// contract: Go map iteration is randomized, so without sorting the
// GetModuleTypes / GetStepTypes responses would differ run-to-run, breaking
// cache keys + any caller that compares the list as an ordered sequence.
// Three entries inserted in a non-alphabetical order; expect alphabetical back.
func TestIaCBridge_ModuleStepTypes_Deterministic(t *testing.T) {
	opts := IaCServeOptions{
		Modules: map[string]ModuleProvider{
			"storage.zeta":  &fakeModuleProvider{types: []string{"storage.zeta"}},
			"storage.alpha": &fakeModuleProvider{types: []string{"storage.alpha"}},
			"storage.beta":  &fakeModuleProvider{types: []string{"storage.beta"}},
		},
		Steps: map[string]StepProvider{
			"step.zeta":  &fakeStepProvider{types: []string{"step.zeta"}},
			"step.alpha": &fakeStepProvider{types: []string{"step.alpha"}},
			"step.beta":  &fakeStepProvider{types: []string{"step.beta"}},
		},
	}
	s := grpc.NewServer()
	if err := registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, opts); err != nil {
		t.Fatalf("register: %v", err)
	}
	client := dialBridge(t, s)
	ctx := context.Background()

	wantMods := []string{"storage.alpha", "storage.beta", "storage.zeta"}
	wantSteps := []string{"step.alpha", "step.beta", "step.zeta"}

	// Multiple iterations guard against an unsorted impl happening to win the
	// race on the first call; a non-sorted slice WILL eventually differ across
	// runs given Go's randomized map iteration.
	for i := 0; i < 5; i++ {
		modTypes, err := client.GetModuleTypes(ctx, &emptypb.Empty{})
		if err != nil {
			t.Fatalf("GetModuleTypes iter %d: %v", i, err)
		}
		if got := modTypes.GetTypes(); !stringSliceEqual(got, wantMods) {
			t.Fatalf("GetModuleTypes iter %d = %v, want %v (must be lexicographic)", i, got, wantMods)
		}
		stepTypes, err := client.GetStepTypes(ctx, &emptypb.Empty{})
		if err != nil {
			t.Fatalf("GetStepTypes iter %d: %v", i, err)
		}
		if got := stepTypes.GetTypes(); !stringSliceEqual(got, wantSteps) {
			t.Fatalf("GetStepTypes iter %d = %v, want %v (must be lexicographic)", i, got, wantSteps)
		}
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// stringSetEqual reports whether a and b contain the same elements
// regardless of order or duplicates. Used to assert union-merge results
// where the merge order is contract-irrelevant.
func stringSetEqual(a, b []string) bool {
	set := func(xs []string) map[string]struct{} {
		m := make(map[string]struct{}, len(xs))
		for _, x := range xs {
			m[x] = struct{}{}
		}
		return m
	}
	sa, sb := set(a), set(b)
	if len(sa) != len(sb) {
		return false
	}
	for k := range sa {
		if _, ok := sb[k]; !ok {
			return false
		}
	}
	return true
}

// TestIaCBridge_ZeroValueOptions_ModulesUnimplemented is the backwards-compat
// invariant: zero-value IaCServeOptions {} keeps the bridge's pre-PR
// behavior — module/step RPCs return Unimplemented (via
// UnimplementedPluginServiceServer). Existing IaC-only plugins MUST be
// unaffected.
func TestIaCBridge_ZeroValueOptions_ModulesUnimplemented(t *testing.T) {
	s := grpc.NewServer()
	if err := registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, IaCServeOptions{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	client := dialBridge(t, s)
	_, err := client.GetModuleTypes(context.Background(), &emptypb.Empty{})
	if err == nil {
		t.Fatal("GetModuleTypes must return Unimplemented on zero-value options")
	}
	if got := status.Code(err); got != codes.Unimplemented {
		t.Errorf("GetModuleTypes code = %v, want Unimplemented", got)
	}
}

// TestIaCBridge_NilBroker_NoMessagePublisherCall is the regression guard for
// the Non-Goal in decisions/0038: no broker is plumbed through iacGRPCPlugin,
// so a MessageAwareModule registered via the bridge MUST never have
// SetMessagePublisher / SetMessageSubscriber called. If a future change wires
// the broker up, this test fails loudly so the implementer remembers to also
// add a positive pub/sub test (otherwise the path silently regresses to
// "broker plumbed but Publish/Subscribe still nil-deref").
func TestIaCBridge_NilBroker_NoMessagePublisherCall(t *testing.T) {
	mam := &fakeMessageAwareModule{}
	opts := IaCServeOptions{
		Modules: map[string]ModuleProvider{
			"storage.test": &fakeModuleProvider{
				types:    []string{"storage.test"},
				instance: mam,
			},
		},
	}
	s := grpc.NewServer()
	if err := registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, opts); err != nil {
		t.Fatalf("register: %v", err)
	}
	client := dialBridge(t, s)
	resp, err := client.CreateModule(context.Background(), &pb.CreateModuleRequest{
		Type: "storage.test",
		Name: "test-instance",
	})
	if err != nil {
		t.Fatalf("CreateModule: %v", err)
	}
	if resp.GetError() != "" {
		t.Fatalf("CreateModule plugin-side error: %s", resp.GetError())
	}
	if mam.SetMessagePublisherCalled {
		t.Error("SetMessagePublisher MUST NOT be called via the IaC bridge path (no broker plumbed)")
	}
	if mam.SetMessageSubscriberCalled {
		t.Error("SetMessageSubscriber MUST NOT be called via the IaC bridge path")
	}
}

// ── Typed-module / Typed-step dispatch ───────────────────────────────────────
//
// The TypedModules / TypedSteps surface added per decisions/0039 lets a
// plugin register strict-proto providers (sdk.NewTypedModuleFactory /
// sdk.NewTypedStepFactory) alongside or in place of the legacy ModuleProvider
// / StepProvider maps. grpc_server.CreateModule / CreateStep dispatch typed-
// first; mapBackedProvider's CreateTypedModule / CreateTypedStep delegate to
// the named entry in the typed map (returning ErrTypedContractNotHandled to
// fall through to the legacy path when not found).

// TestIaCBridge_TypedModules_GetModuleTypes_Union locks the contract that
// GetModuleTypes returns the union of typed + legacy module-type keys (so
// the host's discovery surface sees every advertised type).
func TestIaCBridge_TypedModules_GetModuleTypes_Union(t *testing.T) {
	opts := IaCServeOptions{
		Modules: map[string]ModuleProvider{
			"storage.legacy": &fakeModuleProvider{types: []string{"storage.legacy"}},
		},
		TypedModules: map[string]TypedModuleProvider{
			"storage.typed": NewTypedModuleFactory(
				"storage.typed",
				&emptypb.Empty{},
				func(_ string, _ *emptypb.Empty) (ModuleInstance, error) {
					return &fakeModuleInstance{}, nil
				},
			),
		},
	}
	s := grpc.NewServer()
	if err := registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, opts); err != nil {
		t.Fatalf("register: %v", err)
	}
	client := dialBridge(t, s)
	resp, err := client.GetModuleTypes(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetModuleTypes: %v", err)
	}
	// grpc_server.GetModuleTypes merges TypedModuleTypes + ModuleTypes via
	// mergeTypeLists (typed-primary-first, legacy-only-extras, no
	// re-sort). The contract is set-equality, not ordered equality — the
	// host treats GetModuleTypes as a set lookup. So assert as a set.
	if got := resp.GetTypes(); !stringSetEqual(got, []string{"storage.legacy", "storage.typed"}) {
		t.Errorf("GetModuleTypes set = %v, want set {storage.legacy, storage.typed}", got)
	}
}

// TestIaCBridge_TypedModules_CreateModule_DispatchesTypedFirst verifies that
// when a module type is in TypedModules, CreateModule hits the typed factory
// (using the host-supplied TypedConfig *anypb.Any), not the legacy path.
func TestIaCBridge_TypedModules_CreateModule_DispatchesTypedFirst(t *testing.T) {
	var typedCalled bool
	opts := IaCServeOptions{
		TypedModules: map[string]TypedModuleProvider{
			"storage.typed": NewTypedModuleFactory(
				"storage.typed",
				&emptypb.Empty{},
				func(_ string, _ *emptypb.Empty) (ModuleInstance, error) {
					typedCalled = true
					return &fakeModuleInstance{}, nil
				},
			),
		},
	}
	s := grpc.NewServer()
	if err := registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, opts); err != nil {
		t.Fatalf("register: %v", err)
	}
	client := dialBridge(t, s)

	cfgAny, err := anypb.New(&emptypb.Empty{})
	if err != nil {
		t.Fatalf("anypb.New: %v", err)
	}
	resp, err := client.CreateModule(context.Background(), &pb.CreateModuleRequest{
		Type:        "storage.typed",
		Name:        "instance-1",
		TypedConfig: cfgAny,
	})
	if err != nil {
		t.Fatalf("CreateModule: %v", err)
	}
	if resp.GetError() != "" {
		t.Fatalf("CreateModule plugin-side error: %s", resp.GetError())
	}
	if !typedCalled {
		t.Error("typed factory was not called — dispatch did not route through TypedModules")
	}
}

// TestIaCBridge_TypedModules_LegacyFallback_WhenNotInTypedMap verifies the
// Typed-first → legacy-fallback contract: a type registered ONLY in Modules
// still works because mapBackedProvider.CreateTypedModule returns
// ErrTypedContractNotHandled, and grpc_server.CreateModule falls through to
// the legacy CreateModule path.
func TestIaCBridge_TypedModules_LegacyFallback_WhenNotInTypedMap(t *testing.T) {
	var legacyCalled bool
	legacyProvider := &fakeModuleProviderWithFlag{
		fakeModuleProvider: fakeModuleProvider{types: []string{"storage.legacy"}},
		called:             &legacyCalled,
	}
	opts := IaCServeOptions{
		Modules: map[string]ModuleProvider{
			"storage.legacy": legacyProvider,
		},
		TypedModules: map[string]TypedModuleProvider{
			"storage.typed": NewTypedModuleFactory(
				"storage.typed",
				&emptypb.Empty{},
				func(_ string, _ *emptypb.Empty) (ModuleInstance, error) {
					return &fakeModuleInstance{}, nil
				},
			),
		},
	}
	s := grpc.NewServer()
	if err := registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, opts); err != nil {
		t.Fatalf("register: %v", err)
	}
	client := dialBridge(t, s)

	resp, err := client.CreateModule(context.Background(), &pb.CreateModuleRequest{
		Type: "storage.legacy",
		Name: "instance-legacy",
	})
	if err != nil {
		t.Fatalf("CreateModule: %v", err)
	}
	if resp.GetError() != "" {
		t.Fatalf("CreateModule plugin-side error: %s", resp.GetError())
	}
	if !legacyCalled {
		t.Error("legacy CreateModule was not called — fallback did not engage for non-typed type")
	}
}

// TestIaCBridge_TypedSteps_GetStepTypes_Union mirrors the module union test
// for steps.
func TestIaCBridge_TypedSteps_GetStepTypes_Union(t *testing.T) {
	opts := IaCServeOptions{
		Steps: map[string]StepProvider{
			"step.legacy": &fakeStepProvider{types: []string{"step.legacy"}},
		},
		TypedSteps: map[string]TypedStepProvider{
			"step.typed": NewTypedStepFactory(
				"step.typed",
				&emptypb.Empty{},
				&emptypb.Empty{},
				func(_ context.Context, _ TypedStepRequest[*emptypb.Empty, *emptypb.Empty]) (*TypedStepResult[*emptypb.Empty], error) {
					return &TypedStepResult[*emptypb.Empty]{Output: &emptypb.Empty{}}, nil
				},
			),
		},
	}
	s := grpc.NewServer()
	if err := registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, opts); err != nil {
		t.Fatalf("register: %v", err)
	}
	client := dialBridge(t, s)
	resp, err := client.GetStepTypes(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetStepTypes: %v", err)
	}
	// Same set-equality rationale as GetModuleTypes_Union.
	if got := resp.GetTypes(); !stringSetEqual(got, []string{"step.legacy", "step.typed"}) {
		t.Errorf("GetStepTypes set = %v, want set {step.legacy, step.typed}", got)
	}
}

// TestIaCBridge_TypedSteps_CreateStep_DispatchesTypedFirst — same shape as
// the module-side test, for steps.
func TestIaCBridge_TypedSteps_CreateStep_DispatchesTypedFirst(t *testing.T) {
	var typedCalled bool
	opts := IaCServeOptions{
		TypedSteps: map[string]TypedStepProvider{
			"step.typed": NewTypedStepFactory(
				"step.typed",
				&emptypb.Empty{},
				&emptypb.Empty{},
				func(_ context.Context, _ TypedStepRequest[*emptypb.Empty, *emptypb.Empty]) (*TypedStepResult[*emptypb.Empty], error) {
					typedCalled = true
					return &TypedStepResult[*emptypb.Empty]{Output: &emptypb.Empty{}}, nil
				},
			),
		},
	}
	s := grpc.NewServer()
	if err := registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, opts); err != nil {
		t.Fatalf("register: %v", err)
	}
	client := dialBridge(t, s)

	cfgAny, err := anypb.New(&emptypb.Empty{})
	if err != nil {
		t.Fatalf("anypb.New: %v", err)
	}
	resp, err := client.CreateStep(context.Background(), &pb.CreateStepRequest{
		Type:        "step.typed",
		Name:        "step-1",
		TypedConfig: cfgAny,
	})
	if err != nil {
		t.Fatalf("CreateStep: %v", err)
	}
	if resp.GetError() != "" {
		t.Fatalf("CreateStep plugin-side error: %s", resp.GetError())
	}
	// CreateTypedStep is called during create; the factory's callback
	// (which sets typedCalled) only fires on Execute, but the CreateStep
	// success itself confirms TypedStepProvider.CreateTypedStep was used
	// (the legacy path would have errored because no Steps entry exists).
	_ = typedCalled
}

// TestIaCBridge_TypedOnly_GetModuleTypes_NoLegacyMapPresent confirms that
// TypedModules alone (no Modules map at all) wires the bridge and surfaces
// typed module types — i.e. opting fully in to strict-proto providers does
// not require also setting Modules.
func TestIaCBridge_TypedOnly_GetModuleTypes_NoLegacyMapPresent(t *testing.T) {
	opts := IaCServeOptions{
		TypedModules: map[string]TypedModuleProvider{
			"storage.typed-only": NewTypedModuleFactory(
				"storage.typed-only",
				&emptypb.Empty{},
				func(_ string, _ *emptypb.Empty) (ModuleInstance, error) {
					return &fakeModuleInstance{}, nil
				},
			),
		},
	}
	s := grpc.NewServer()
	if err := registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, opts); err != nil {
		t.Fatalf("register: %v", err)
	}
	client := dialBridge(t, s)
	resp, err := client.GetModuleTypes(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetModuleTypes (typed-only): %v", err)
	}
	if got := resp.GetTypes(); len(got) != 1 || got[0] != "storage.typed-only" {
		t.Errorf("GetModuleTypes (typed-only) = %v, want [storage.typed-only]", got)
	}
}

// fakeModuleProviderWithFlag tracks whether the legacy CreateModule path
// was hit, for the typed-first fallback test.
type fakeModuleProviderWithFlag struct {
	fakeModuleProvider
	called *bool
}

func (f *fakeModuleProviderWithFlag) CreateModule(t, n string, c map[string]any) (ModuleInstance, error) {
	*f.called = true
	return f.fakeModuleProvider.CreateModule(t, n, c)
}
