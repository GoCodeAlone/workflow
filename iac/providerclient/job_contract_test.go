package providerclient_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/iac/providerclient"
	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"
)

const jobCancelerService = "workflow.plugin.external.iac.IaCProviderJobCanceler"

// These fixtures implement native job contracts, not protobuf conversions.
type nativeJobProvider struct {
	pb.UnimplementedIaCProviderRequiredServer
	specs chan interfaces.JobSpec
}

func (p *nativeJobProvider) RunJob(_ context.Context, spec interfaces.JobSpec) (*interfaces.JobHandle, error) {
	p.specs <- spec
	return &interfaces.JobHandle{ID: "job-id", Name: spec.Name, Provider: "native", Metadata: map[string]string{"parent_id": "app-id", "deployment_id": "deployment-id"}}, nil
}

func (*nativeJobProvider) JobStatus(_ context.Context, handle interfaces.JobHandle) (*interfaces.JobStatusReply, error) {
	return &interfaces.JobStatusReply{Handle: handle, State: interfaces.JobStateSucceeded, ExitCode: 0, Message: "done"}, nil
}

func (*nativeJobProvider) JobLogs(_ context.Context, _ interfaces.JobHandle, sink interfaces.LogCaptureSink) error {
	if err := sink.WriteLogChunk(interfaces.LogChunk{Data: []byte("job output\n"), Source: "stdout"}); err != nil {
		return err
	}
	return sink.WriteLogChunk(interfaces.LogChunk{EOF: true})
}

type nativeCancelableJobProvider struct {
	*nativeJobProvider
	handles  chan interfaces.JobHandle
	err      error
	canceled chan struct{}
}

func (p *nativeCancelableJobProvider) CancelJob(ctx context.Context, handle interfaces.JobHandle) error {
	p.handles <- handle
	if p.canceled != nil {
		<-ctx.Done()
		close(p.canceled)
		return ctx.Err()
	}
	return p.err
}

type nativeJobCanceler interface {
	CancelJob(context.Context, interfaces.JobHandle) error
}

type nativeCancelOnlyProvider struct {
	pb.UnimplementedIaCProviderRequiredServer
	handles chan interfaces.JobHandle
}

func (p *nativeCancelOnlyProvider) CancelJob(_ context.Context, handle interfaces.JobHandle) error {
	p.handles <- handle
	return nil
}

func discoverJobCanceler(t *testing.T, adapter *providerclient.Adapter) nativeJobCanceler {
	t.Helper()
	accessor := reflect.ValueOf(adapter).MethodByName("JobCanceler")
	if !accessor.IsValid() {
		t.Fatal("providerclient adapter must expose advertisement-gated JobCanceler discovery")
	}
	value := accessor.Call(nil)[0]
	if value.IsNil() {
		return nil
	}
	canceler, ok := value.Interface().(nativeJobCanceler)
	if !ok {
		t.Fatalf("JobCanceler() = %T, want native cancellation contract", value.Interface())
	}
	return canceler
}

func nativeJobConnection(t *testing.T, provider any) (*grpc.ClientConn, map[string]bool) {
	t.Helper()
	server := grpc.NewServer()
	if err := sdk.RegisterAllIaCProviderServices(server, provider); err != nil {
		t.Fatal(err)
	}
	conn := startFakeServer(t, server)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	registry, err := pb.NewPluginServiceClient(conn).GetContractRegistry(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	advertised := make(map[string]bool)
	for _, contract := range registry.GetContracts() {
		if contract.GetKind() == pb.ContractKind_CONTRACT_KIND_SERVICE {
			advertised[contract.GetServiceName()] = true
		}
	}
	return conn, advertised
}

func providerJobSpec(t *testing.T, target *interfaces.ResourceRef, timeout int) interfaces.JobSpec {
	t.Helper()
	spec := interfaces.JobSpec{
		Name: "migrate", Kind: interfaces.JobKindEphemeral, Image: "registry/app@sha256:fixture",
		RunCommand: "app migrate", EnvVars: map[string]string{"MODE": "migration"},
		EnvVarsSecret: map[string]string{"DB_URI": "secret://migration-db"}, Cron: "",
		Termination:     &interfaces.TerminationSpec{DrainSeconds: 5, GracePeriodSeconds: 10},
		Alerts:          []interfaces.AlertSpec{{Rule: "failed", Operator: ">", Value: 1, Window: "5m", Disabled: true}},
		LogDestinations: []interfaces.LogDestinationSpec{{Name: "logs", Endpoint: "https://logs.invalid", Headers: map[string]string{"source": "test"}, TLS: true}},
	}
	value := reflect.ValueOf(&spec).Elem()
	field := value.FieldByName("Target")
	if !field.IsValid() || field.Type() != reflect.TypeFor[*interfaces.ResourceRef]() {
		t.Fatal("JobSpec must carry a typed Target")
	}
	field.Set(reflect.ValueOf(target))
	field = value.FieldByName("TimeoutSeconds")
	if !field.IsValid() || field.Kind() != reflect.Int {
		t.Fatal("JobSpec must carry TimeoutSeconds")
	}
	field.SetInt(int64(timeout))
	return spec
}

type jobContractLogSink struct{ chunks []interfaces.LogChunk }

func (s *jobContractLogSink) WriteLogChunk(chunk interfaces.LogChunk) error {
	s.chunks = append(s.chunks, chunk)
	return nil
}

func TestProviderJobNativeSDKRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name    string
		target  *interfaces.ResourceRef
		timeout int
	}{
		{"targeted minimum", &interfaces.ResourceRef{Name: "app", Type: "infra.container_service", ProviderID: "app-id"}, 1},
		{"targeted maximum", &interfaces.ResourceRef{Name: "worker", Type: "other.parent", ProviderID: "parent-id"}, 3600},
		{"untargeted legacy zero", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &nativeJobProvider{specs: make(chan interfaces.JobSpec, 1)}
			conn, advertised := nativeJobConnection(t, provider)
			adapter := providerclient.New(conn, advertised)
			runner := adapter.Runner()
			if runner == nil {
				t.Fatal("SDK must register a native IaCProviderRunner implementation")
			}
			spec := providerJobSpec(t, tc.target, tc.timeout)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			handle, err := runner.RunJob(ctx, spec)
			if err != nil {
				t.Fatal(err)
			}
			got := <-provider.specs
			if !reflect.DeepEqual(got, spec) {
				t.Fatalf("native SDK round trip = %+v, want %+v", got, spec)
			}
			if handle == nil || handle.ID != "job-id" || handle.Metadata["deployment_id"] != "deployment-id" {
				t.Fatalf("handle = %+v", handle)
			}
			if tc.target != nil {
				gotTarget := reflect.ValueOf(got).FieldByName("Target").Interface().(*interfaces.ResourceRef)
				gotTarget.ProviderID = "provider-change"
				if tc.target.ProviderID == "provider-change" {
					t.Fatal("target must not alias the caller across the SDK boundary")
				}
			}
			got.EnvVarsSecret["DB_URI"] = "provider-change"
			if spec.EnvVarsSecret["DB_URI"] != "secret://migration-db" {
				t.Fatal("secret references must remain copied, unresolved references")
			}
			reply, err := runner.JobStatus(ctx, *handle)
			if err != nil || reply == nil || !reflect.DeepEqual(reply.Handle, *handle) || reply.State != interfaces.JobStateSucceeded || reply.Message != "done" {
				t.Fatalf("JobStatus = %+v, err = %v", reply, err)
			}
			sink := &jobContractLogSink{}
			if err := runner.JobLogs(ctx, *handle, sink); err != nil {
				t.Fatal(err)
			}
			if len(sink.chunks) != 2 || string(sink.chunks[0].Data) != "job output\n" || sink.chunks[0].Source != "stdout" || !sink.chunks[1].EOF {
				t.Fatalf("native log stream = %+v", sink.chunks)
			}
		})
	}
}

func TestProviderJobValidationBeforeClientAndNativeDispatch(t *testing.T) {
	provider := &nativeJobProvider{specs: make(chan interfaces.JobSpec, 1)}
	conn, advertised := nativeJobConnection(t, provider)
	runner := providerclient.New(conn, advertised).Runner()
	if runner == nil {
		t.Fatal("SDK must register native runner")
	}
	target := &interfaces.ResourceRef{Name: "app", Type: "infra.container_service", ProviderID: "app-id"}
	for _, timeout := range []int{-1, 0, 3601, int(^uint(0) >> 1)} {
		spec := providerJobSpec(t, target, timeout)
		handle, err := runner.RunJob(t.Context(), spec)
		if handle != nil || !errors.Is(err, interfaces.ErrValidation) {
			t.Fatalf("timeout %d: handle = %+v, err = %v; want client-side ErrValidation", timeout, handle, err)
		}
	}
	for _, invalid := range []*interfaces.ResourceRef{
		{Type: target.Type, ProviderID: target.ProviderID},
		{Name: target.Name, ProviderID: target.ProviderID},
		{Name: target.Name, Type: target.Type},
	} {
		handle, err := runner.RunJob(t.Context(), providerJobSpec(t, invalid, 600))
		if handle != nil || !errors.Is(err, interfaces.ErrValidation) {
			t.Fatalf("incomplete target: handle = %+v, err = %v", handle, err)
		}
	}
	// A direct generated client bypasses host validation; the real SDK must
	// still reject invalid wire requests before the native provider runs.
	for _, timeout := range []int64{-1, 0, 3601, 1<<31 - 1} {
		request := &pb.JobSpec{Name: "migrate"}
		message := request.ProtoReflect()
		field := message.Descriptor().Fields().ByName("target")
		if field == nil {
			t.Fatal("protobuf target field missing")
		}
		message.Set(field, protoreflect.ValueOfMessage((&pb.ResourceRef{Name: target.Name, Type: target.Type, ProviderId: target.ProviderID}).ProtoReflect()))
		field = message.Descriptor().Fields().ByName("timeout_seconds")
		if field == nil {
			t.Fatal("protobuf timeout field missing")
		}
		message.Set(field, protoreflect.ValueOfInt32(int32(timeout)))
		_, err := pb.NewIaCProviderRunnerClient(conn).RunJob(t.Context(), request)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("wire timeout %d: %v, want InvalidArgument", timeout, err)
		}
	}
	if len(provider.specs) != 0 {
		t.Fatal("invalid request reached native provider")
	}
}

func TestProviderJobIndependentCancellationCapability(t *testing.T) {
	provider := &nativeCancelOnlyProvider{handles: make(chan interfaces.JobHandle, 1)}
	conn, advertised := nativeJobConnection(t, provider)
	adapter := providerclient.New(conn, advertised)
	if adapter.Runner() != nil || advertised[providerclient.IaCServiceRunner] {
		t.Fatal("cancellation must not imply the runner capability")
	}
	canceler := discoverJobCanceler(t, adapter)
	if canceler == nil {
		t.Fatal("standalone native cancellation capability was not advertised")
	}
	handle := interfaces.JobHandle{ID: "job-id", Metadata: map[string]string{"component": "owned-job"}}
	if err := canceler.CancelJob(t.Context(), handle); err != nil {
		t.Fatal(err)
	}
	if got := <-provider.handles; !reflect.DeepEqual(got, handle) {
		t.Fatalf("standalone cancellation handle = %+v, want %+v", got, handle)
	}
}

func TestProviderJobCancelerDiscoveryAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name              string
		legacy            bool
		hidden            bool
		phantom           bool
		providerErr       error
		wantCode          codes.Code
		wantAbsent        bool
		wantUnimplemented bool
	}{
		{name: "legacy absence", legacy: true, wantAbsent: true},
		{name: "registered but unadvertised", hidden: true, wantAbsent: true},
		{name: "success"},
		{name: "native unimplemented sentinel", providerErr: interfaces.ErrProviderMethodUnimplemented, wantCode: codes.Unimplemented, wantUnimplemented: true},
		{name: "native unimplemented status", providerErr: status.Error(codes.Unimplemented, "unsupported"), wantCode: codes.Unimplemented, wantUnimplemented: true},
		{name: "advertised missing service", legacy: true, phantom: true, wantCode: codes.Unimplemented, wantUnimplemented: true},
		{name: "denied", providerErr: status.Error(codes.PermissionDenied, "denied"), wantCode: codes.PermissionDenied},
		{name: "unavailable", providerErr: status.Error(codes.Unavailable, "unavailable"), wantCode: codes.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			legacy := &nativeJobProvider{specs: make(chan interfaces.JobSpec, 1)}
			provider := &nativeCancelableJobProvider{nativeJobProvider: legacy, handles: make(chan interfaces.JobHandle, 1), err: tc.providerErr}
			var registered any = provider
			if tc.legacy {
				registered = legacy
			}
			conn, advertised := nativeJobConnection(t, registered)
			if advertised[jobCancelerService] == tc.legacy {
				t.Fatalf("SDK cancellation advertisement = %v, legacy = %v", advertised[jobCancelerService], tc.legacy)
			}
			if tc.hidden {
				delete(advertised, jobCancelerService)
			}
			if tc.phantom {
				advertised[jobCancelerService] = true
			}
			adapter := providerclient.New(conn, advertised)
			if _, unconditional := any(adapter).(nativeJobCanceler); unconditional {
				t.Fatal("adapter must not unconditionally implement cancellation")
			}
			canceler := discoverJobCanceler(t, adapter)
			if (canceler == nil) != tc.wantAbsent {
				t.Fatalf("canceler = %T, wantAbsent = %v", canceler, tc.wantAbsent)
			}
			if canceler == nil {
				if len(provider.handles) != 0 {
					t.Fatal("unadvertised canceler received a call")
				}
				return
			}
			handle := interfaces.JobHandle{ID: "job-id", Name: "migrate", Provider: "native", Metadata: map[string]string{"parent_id": "app-id", "component": "owned-job", "prior_spec_digest": "digest"}}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err := canceler.CancelJob(ctx, handle)
			if status.Code(err) != tc.wantCode || errors.Is(err, interfaces.ErrProviderMethodUnimplemented) != tc.wantUnimplemented {
				t.Fatalf("CancelJob() = %v (%v), want code %v, unimplemented %v", err, status.Code(err), tc.wantCode, tc.wantUnimplemented)
			}
			if tc.providerErr != nil && !tc.wantUnimplemented && status.Convert(err).Message() != status.Convert(tc.providerErr).Message() {
				t.Fatalf("provider error changed: %v", err)
			}
			if !tc.legacy {
				got := <-provider.handles
				if !reflect.DeepEqual(got, handle) {
					t.Fatalf("native cancel handle = %+v, want %+v", got, handle)
				}
				got.Metadata["component"] = "provider-change"
				if handle.Metadata["component"] != "owned-job" {
					t.Fatal("cancellation metadata aliases caller")
				}
			}
		})
	}
}

func TestProviderJobCancelerCallerCancellation(t *testing.T) {
	provider := &nativeCancelableJobProvider{
		nativeJobProvider: &nativeJobProvider{specs: make(chan interfaces.JobSpec, 1)},
		handles:           make(chan interfaces.JobHandle, 1), canceled: make(chan struct{}),
	}
	conn, advertised := nativeJobConnection(t, provider)
	canceler := discoverJobCanceler(t, providerclient.New(conn, advertised))
	if canceler == nil {
		t.Fatal("SDK must register native cancellation")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	results := make(chan error, 1)
	go func() { results <- canceler.CancelJob(ctx, interfaces.JobHandle{ID: "job-id"}) }()
	select {
	case <-provider.handles:
	case <-time.After(5 * time.Second):
		t.Fatal("native cancellation call did not start")
	}
	cancel()
	select {
	case err := <-results:
		if status.Code(err) != codes.Canceled || errors.Is(err, interfaces.ErrProviderMethodUnimplemented) {
			t.Fatalf("caller cancellation = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("caller cancellation did not stop client RPC")
	}
	select {
	case <-provider.canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("caller cancellation did not reach native provider")
	}
}
