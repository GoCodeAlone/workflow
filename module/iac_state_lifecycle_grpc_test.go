package module

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/secrets"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const lifecycleGRPCPrivateMarker = "known-private-cleanup-value"

func lifecycleGRPCFixture(phase interfaces.ResourcePhase) *IaCState {
	return &IaCState{
		ResourceID: "database", ResourceType: "infra.database", Provider: "test-cloud", ProviderRef: "provider-config", ProviderID: "database-id",
		ConfigHash: "config-hash", Status: "destroyed", Outputs: map[string]any{"password": "secret_ref://database_password"},
		Config: map[string]any{"password": "${DATABASE_PASSWORD}"}, Dependencies: []string{"network"},
		CreatedAt: "2026-10-02T00:00:00Z", UpdatedAt: "2026-10-03T00:00:00Z", Error: "cleanup pending",
		Lifecycle: &interfaces.ResourceLifecycle{
			Generation: "generation-1", Phase: phase, RoutingCreated: phase == interfaces.ResourcePhaseSecretRoutingPending,
			Secrets: []interfaces.RoutedSecretReference{
				{Key: "database_password", Store: "named-store", Provider: "file", Scope: "directory", Subject: "/tmp/workflow-secrets"},
				{Key: "named-alias", Provider: "env", Scope: "process", Subject: "WORKFLOW_", VerifiedAbsent: true},
				{Key: "unprefixed-env", Provider: "env", Scope: "process"},
				{Key: "custom-default", Provider: "custom", Scope: "default"},
			},
		},
	}
}

func lifecycleGRPCConnection(t *testing.T, backend IaCStateStore, options ...grpc.ServerOption) func() *grpc.ClientConn {
	t.Helper()
	server := grpc.NewServer(options...)
	t.Cleanup(server.Stop)
	pb.RegisterIaCStateBackendServer(server, &iacStateBackendServer{store: backend})
	listener := bufconn.Listen(4 << 20)
	t.Cleanup(func() { _ = listener.Close() })
	go func() { _ = server.Serve(listener) }()
	return func() *grpc.ClientConn {
		t.Helper()
		conn, err := grpc.NewClient("passthrough:///lifecycle-state-test",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
}

func lifecycleGRPCWireField(t *testing.T, state *pb.IaCState) protoreflect.FieldDescriptor {
	t.Helper()
	field := state.ProtoReflect().Descriptor().Fields().ByName("lifecycle_json")
	if field == nil || field.Number() != 14 || field.Kind() != protoreflect.BytesKind {
		t.Fatal("IaCState lifecycle_json must be bytes at the next free field 14")
	}
	return field
}

func setLifecycleGRPCPayload(t *testing.T, state *pb.IaCState, payload []byte) {
	t.Helper()
	state.ProtoReflect().Set(lifecycleGRPCWireField(t, state), protoreflect.ValueOfBytes(payload))
}

func assertLifecycleGRPCSafeError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("invalid lifecycle metadata was accepted")
	}
	if strings.Contains(err.Error(), lifecycleGRPCPrivateMarker) {
		t.Fatalf("lifecycle validation error echoed private payload: %v", err)
	}
}

func TestGRPCIaCStateLifecycleRoundTripAndReload(t *testing.T) {
	for _, phase := range []interfaces.ResourcePhase{
		interfaces.ResourcePhaseActive, interfaces.ResourcePhaseSecretRoutingPending,
		interfaces.ResourcePhaseCloudDeletePending, interfaces.ResourcePhaseCloudDeletedSecretCleanupPending,
	} {
		t.Run(string(phase), func(t *testing.T) {
			backend := NewMemoryIaCStateStore()
			connect := lifecycleGRPCConnection(t, backend)
			conn := connect()
			store := NewGRPCIaCStateStore(pb.NewIaCStateBackendClient(conn))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			want := lifecycleGRPCFixture(phase)
			if err := store.SaveState(ctx, want); err != nil {
				t.Fatal(err)
			}
			stored, err := backend.GetState(ctx, want.ResourceID)
			if err != nil || !reflect.DeepEqual(stored, want) {
				t.Fatalf("native backend lost wire metadata: state = %+v, err = %v", stored, err)
			}
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
			store = NewGRPCIaCStateStore(pb.NewIaCStateBackendClient(connect()))
			got, err := store.GetState(ctx, want.ResourceID)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("reloaded client read = %+v, err = %v; want %+v", got, err, want)
			}
			got.Lifecycle.Generation = "caller-change"
			got.Lifecycle.Secrets[1].VerifiedAbsent = false
			listed, err := store.ListStates(ctx, map[string]string{"provider": want.Provider})
			if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0], want) {
				t.Fatalf("list round trip = %+v, err = %v", listed, err)
			}
			listed[0].Lifecycle.Secrets[0].Subject = "list-caller-change"
			got, err = store.GetState(ctx, want.ResourceID)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("caller mutation changed stored evidence: %+v, %v", got, err)
			}
			wire, err := iacStateToProto(want)
			if err != nil {
				t.Fatal(err)
			}
			payload := wire.ProtoReflect().Get(lifecycleGRPCWireField(t, wire)).Bytes()
			var lifecycle interfaces.ResourceLifecycle
			if err := json.Unmarshal(payload, &lifecycle); err != nil || !reflect.DeepEqual(&lifecycle, want.Lifecycle) {
				t.Fatalf("wire lifecycle differs: lifecycle = %+v, err = %v", lifecycle, err)
			}
			if strings.Contains(string(payload), lifecycleGRPCPrivateMarker) {
				t.Fatal("wire lifecycle contains private material")
			}
		})
	}
}

func TestGRPCIaCStateLifecycleDescribeTargetCompatibility(t *testing.T) {
	// Exposing only Provider hides the optional TargetDescriber method.
	withoutDescriber := struct{ secrets.Provider }{secrets.NewFileProvider(t.TempDir())}
	if _, ok := any(withoutDescriber).(secrets.TargetDescriber); ok {
		t.Fatal("fallback provider unexpectedly implements TargetDescriber")
	}
	for _, tc := range []struct {
		name     string
		provider secrets.Provider
		scope    string
	}{
		{"provider without TargetDescriber", withoutDescriber, "default"},
		{"empty environment prefix", secrets.NewEnvProvider(""), "process"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := secrets.DescribeTarget(tc.provider)
			if target.Provider != tc.provider.Name() || target.Scope != tc.scope || target.Subject != "" {
				t.Fatalf("unexpected producer namespace: %+v", target)
			}
			backend := NewMemoryIaCStateStore()
			connect := lifecycleGRPCConnection(t, backend)
			conn := connect()
			store := NewGRPCIaCStateStore(pb.NewIaCStateBackendClient(conn))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			want := lifecycleGRPCFixture(interfaces.ResourcePhaseSecretRoutingPending)
			want.Lifecycle.Secrets = []interfaces.RoutedSecretReference{{
				Key: "routed-key", Provider: target.Provider, Scope: target.Scope, Subject: target.Subject,
			}}
			if err := store.SaveState(ctx, want); err != nil {
				t.Fatalf("actual producer namespace rejected: %v", err)
			}
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
			store = NewGRPCIaCStateStore(pb.NewIaCStateBackendClient(connect()))
			got, err := store.GetState(ctx, want.ResourceID)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("producer namespace reload = %+v, err = %v", got, err)
			}
			listed, err := store.ListStates(ctx, nil)
			if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0], want) {
				t.Fatalf("producer namespace listing = %+v, err = %v", listed, err)
			}
		})
	}
}

func TestGRPCIaCStateLifecycleLegacyAbsence(t *testing.T) {
	backend := NewMemoryIaCStateStore()
	conn := lifecycleGRPCConnection(t, backend)()
	store := NewGRPCIaCStateStore(pb.NewIaCStateBackendClient(conn))
	want := lifecycleGRPCFixture(interfaces.ResourcePhaseActive)
	want.Lifecycle = nil
	if err := store.SaveState(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetState(t.Context(), want.ResourceID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy read = %+v, err = %v", got, err)
	}
	listed, err := store.ListStates(t.Context(), nil)
	if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0], want) {
		t.Fatalf("legacy listing = %+v, err = %v", listed, err)
	}
	wire, err := iacStateToProto(want)
	if err != nil || len(wire.ProtoReflect().Get(lifecycleGRPCWireField(t, wire)).Bytes()) != 0 {
		t.Fatalf("legacy state should omit lifecycle_json: %v", err)
	}
}

func TestGRPCIaCStateLifecycleRejectsInvalidNativeMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*interfaces.ResourceLifecycle)
	}{
		{"empty generation", func(l *interfaces.ResourceLifecycle) { l.Generation = "" }},
		{"blank generation", func(l *interfaces.ResourceLifecycle) { l.Generation = " \t" }},
		{"invalid UTF-8 generation", func(l *interfaces.ResourceLifecycle) { l.Generation = "generation-\xff" }},
		{"unknown phase", func(l *interfaces.ResourceLifecycle) { l.Phase = interfaces.ResourcePhase(lifecycleGRPCPrivateMarker) }},
		{"empty phase", func(l *interfaces.ResourceLifecycle) { l.Phase = "" }},
		{"empty key", func(l *interfaces.ResourceLifecycle) { l.Secrets[0].Key = "" }},
		{"empty provider", func(l *interfaces.ResourceLifecycle) { l.Secrets[0].Provider = "" }},
		{"empty scope", func(l *interfaces.ResourceLifecycle) { l.Secrets[0].Scope = "" }},
		{"missing scoped subject", func(l *interfaces.ResourceLifecycle) { l.Secrets[0].Subject = "" }},
		{"blank named store", func(l *interfaces.ResourceLifecycle) { l.Secrets[0].Store = " " }},
		{"invalid UTF-8 namespace", func(l *interfaces.ResourceLifecycle) { l.Secrets[0].Subject = "directory-\xff" }},
		{"control in key", func(l *interfaces.ResourceLifecycle) { l.Secrets[0].Key = "key\n" + lifecycleGRPCPrivateMarker }},
		{"control in namespace", func(l *interfaces.ResourceLifecycle) {
			l.Secrets[0].Subject = "directory\x00" + lifecycleGRPCPrivateMarker
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := NewMemoryIaCStateStore()
			conn := lifecycleGRPCConnection(t, backend)()
			store := NewGRPCIaCStateStore(pb.NewIaCStateBackendClient(conn))
			state := lifecycleGRPCFixture(interfaces.ResourcePhaseActive)
			tc.mutate(state.Lifecycle)
			wire, err := iacStateToProto(state)
			assertLifecycleGRPCSafeError(t, err)
			if wire != nil || !errors.Is(err, interfaces.ErrValidation) {
				t.Fatalf("invalid native lifecycle conversion = %+v, %v", wire, err)
			}
			err = store.SaveState(t.Context(), state)
			assertLifecycleGRPCSafeError(t, err)
			if !errors.Is(err, interfaces.ErrValidation) {
				t.Fatalf("SaveState error must preserve ErrValidation: %v", err)
			}
			got, err := backend.GetState(t.Context(), state.ResourceID)
			if err != nil || got != nil {
				t.Fatalf("invalid native lifecycle reached backend: %+v, %v", got, err)
			}
		})
	}
}

func TestGRPCIaCStateLifecycleRejectsInvalidWireMetadata(t *testing.T) {
	valid := `{"generation":"generation-1","phase":"active"}`
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{"malformed", `{"generation":"` + lifecycleGRPCPrivateMarker},
		{"invalid UTF-8", "{\"generation\":\"generation-\xff\",\"phase\":\"active\"}"},
		{"root string", `"` + lifecycleGRPCPrivateMarker + `"`},
		{"root array", `[]`},
		{"root null", `null`},
		{"blank payload", ` `},
		{"empty object", `{}`},
		{"unknown field", `{"generation":"generation-1","phase":"active","credential_value":"` + lifecycleGRPCPrivateMarker + `"}`},
		{"unknown private field name", `{"generation":"generation-1","phase":"active","` + lifecycleGRPCPrivateMarker + `":true}`},
		{"nested value field", `{"generation":"generation-1","phase":"active","secrets":[{"key":"key","provider":"env","scope":"process","value":"` + lifecycleGRPCPrivateMarker + `"}]}`},
		{"wrong generation type", `{"generation":123,"phase":"active"}`},
		{"wrong phase type", `{"generation":"generation-1","phase":[]}`},
		{"unknown phase", `{"generation":"generation-1","phase":"` + lifecycleGRPCPrivateMarker + `"}`},
		{"missing generation", `{"phase":"active"}`},
		{"missing namespace", `{"generation":"generation-1","phase":"active","secrets":[{"key":"key"}]}`},
		{"wrong absent type", `{"generation":"generation-1","phase":"active","secrets":[{"key":"key","provider":"env","scope":"process","verified_absent":"` + lifecycleGRPCPrivateMarker + `"}]}`},
		{"null absent type", `{"generation":"generation-1","phase":"active","secrets":[{"key":"key","provider":"env","scope":"process","verified_absent":null}]}`},
		{"null store type", `{"generation":"generation-1","phase":"active","secrets":[{"key":"key","provider":"env","scope":"process","store":null}]}`},
		{"null secrets type", `{"generation":"generation-1","phase":"active","secrets":null}`},
		{"trailing object", valid + ` {"credential_value":"` + lifecycleGRPCPrivateMarker + `"}`},
		{"trailing garbage", valid + lifecycleGRPCPrivateMarker},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := NewMemoryIaCStateStore()
			want := lifecycleGRPCFixture(interfaces.ResourcePhaseActive)
			if err := backend.SaveState(t.Context(), want); err != nil {
				t.Fatal(err)
			}
			conn := lifecycleGRPCConnection(t, backend)()
			wire, err := iacStateToProto(want)
			if err != nil {
				t.Fatal(err)
			}
			setLifecycleGRPCPayload(t, wire, []byte(tc.payload))
			state, err := iacStateFromProto(wire)
			assertLifecycleGRPCSafeError(t, err)
			if state != nil || !errors.Is(err, interfaces.ErrValidation) {
				t.Fatalf("invalid wire conversion = %+v, %v", state, err)
			}
			_, err = pb.NewIaCStateBackendClient(conn).SaveState(t.Context(), &pb.SaveStateRequest{State: wire})
			assertLifecycleGRPCSafeError(t, err)
			got, err := backend.GetState(t.Context(), want.ResourceID)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("invalid wire payload replaced backend state: %+v, %v", got, err)
			}
		})
	}
}

func TestGRPCIaCStateLifecycleConsumerRejectsCorruptReadAndList(t *testing.T) {
	lifecycleGRPCWireField(t, &pb.IaCState{})
	backend := NewMemoryIaCStateStore()
	want := lifecycleGRPCFixture(interfaces.ResourcePhaseCloudDeletedSecretCleanupPending)
	if err := backend.SaveState(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	interceptor := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		reply, err := handler(ctx, req)
		if err == nil {
			payload := []byte(`{"generation":"generation-1","phase":"active","credential_value":"` + lifecycleGRPCPrivateMarker + `"}`)
			switch r := reply.(type) {
			case *pb.GetStateResponse:
				setLifecycleGRPCPayload(t, r.State, payload)
			case *pb.ListStatesResponse:
				for _, state := range r.States {
					setLifecycleGRPCPayload(t, state, payload)
				}
			}
		}
		return reply, err
	}
	conn := lifecycleGRPCConnection(t, backend, grpc.UnaryInterceptor(interceptor))()
	store := NewGRPCIaCStateStore(pb.NewIaCStateBackendClient(conn))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	state, err := store.GetState(ctx, want.ResourceID)
	assertLifecycleGRPCSafeError(t, err)
	if state != nil || !errors.Is(err, interfaces.ErrValidation) {
		t.Fatalf("consumer returned corrupt state: %+v, %v", state, err)
	}
	states, err := store.ListStates(ctx, nil)
	assertLifecycleGRPCSafeError(t, err)
	if states != nil || !errors.Is(err, interfaces.ErrValidation) {
		t.Fatalf("consumer returned corrupt listing: %+v, %v", states, err)
	}
}
