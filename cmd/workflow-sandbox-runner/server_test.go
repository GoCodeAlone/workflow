package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/sandbox"
	"github.com/GoCodeAlone/workflow/secrets"
)

const testBufSize = 1024 * 1024

// fakeRunner is an injectable sandbox.SandboxRunner that returns canned results.
type fakeRunner struct {
	result *sandbox.ExecResult
	err    error
	// capturedCmd is the command passed to Exec.
	capturedCmd []string
}

func (f *fakeRunner) Exec(_ context.Context, cmd []string) (*sandbox.ExecResult, error) {
	f.capturedCmd = cmd
	if f.err != nil {
		return nil, f.err
	}
	if f.result == nil {
		return &sandbox.ExecResult{ExitCode: 0, Stdout: "ok"}, nil
	}
	return f.result, nil
}

func (f *fakeRunner) Close() error { return nil }

// fakeProvider is a secrets.Provider that resolves from a fixed map.
type fakeProvider struct {
	values map[string]string
}

func (p *fakeProvider) Name() string { return "fake" }
func (p *fakeProvider) Get(_ context.Context, key string) (string, error) {
	if v, ok := p.values[key]; ok {
		return v, nil
	}
	return "", secrets.ErrNotFound
}
func (p *fakeProvider) Set(_ context.Context, _, _ string) error { return secrets.ErrUnsupported }
func (p *fakeProvider) Delete(_ context.Context, _ string) error { return secrets.ErrUnsupported }
func (p *fakeProvider) List(_ context.Context) ([]string, error) { return nil, secrets.ErrUnsupported }

// buildTestServer starts an in-process gRPC server using a bufconn listener and
// returns a client connected to it. The provided runnerFactory is injected into
// the server so tests don't require a Docker daemon.
func buildTestServer(t *testing.T, provider secrets.Provider, token string, factory sandboxRunnerFactory) (pb.SandboxExecServiceClient, func()) {
	t.Helper()

	lis := bufconn.Listen(testBufSize)
	srv := grpc.NewServer(grpc.StreamInterceptor(newBearerStreamInterceptor(token)))
	s := newSandboxExecServerWithFactory(provider, slog.Default(), factory)
	pb.RegisterSandboxExecServiceServer(srv, s)

	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}

	cleanup := func() {
		conn.Close() //nolint:errcheck
		srv.GracefulStop()
	}
	return pb.NewSandboxExecServiceClient(conn), cleanup
}

// drainStream collects all chunks from a streaming exec RPC and returns them.
// It is used by success-path tests, so any non-EOF stream error is a test failure
// (t.Error) rather than being silently swallowed. Error-path tests use their own
// inline Recv loops and do not call drainStream.
func drainStream(t *testing.T, stream pb.SandboxExecService_ExecClient) []*pb.SandboxExecChunk {
	t.Helper()
	var chunks []*pb.SandboxExecChunk
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Errorf("drainStream: unexpected stream error: %v", err)
			return chunks
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

// --- clampProfile unit tests ---

func TestClampProfile_AllowedProfiles(t *testing.T) {
	for _, p := range []string{"strict", "standard"} {
		got := clampProfile(p)
		if got != p {
			t.Errorf("clampProfile(%q) = %q; want %q (should be unchanged)", p, got, p)
		}
	}
}

func TestClampProfile_PermissiveClamped(t *testing.T) {
	got := clampProfile("permissive")
	if got != "standard" {
		t.Errorf("clampProfile(permissive) = %q; want %q", got, "standard")
	}
}

func TestClampProfile_UnknownClamped(t *testing.T) {
	for _, p := range []string{"", "root", "unsafe", "anything-else"} {
		got := clampProfile(p)
		if got != "standard" {
			t.Errorf("clampProfile(%q) = %q; want standard", p, got)
		}
	}
}

// --- Profile-clamping integration test ---

// TestExec_ProfileClamp verifies that a "permissive" request is clamped to
// "standard" on the server side (the runner sees the standard SandboxConfig,
// not a permissive one).
func TestExec_ProfileClamp(t *testing.T) {
	var capturedConfig sandbox.SandboxConfig
	factory := func(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
		capturedConfig = cfg
		return &fakeRunner{}, nil
	}

	client, cleanup := buildTestServer(t, &fakeProvider{values: map[string]string{}}, "", factory)
	defer cleanup()

	stream, err := client.Exec(context.Background(), &pb.SandboxExecRequest{
		Profile: "permissive",
		Image:   "alpine:3.19",
		Command: []string{"echo", "hi"},
	})
	if err != nil {
		t.Fatalf("Exec RPC: %v", err)
	}
	_ = drainStream(t, stream)

	// The server should have built a "standard" SandboxConfig, not "permissive".
	// Standard profile sets MemoryLimit to 256MB (see sandbox.BuildSandboxConfig).
	expectedStdCfg := sandbox.BuildSandboxConfig("standard", "alpine:3.19")
	if capturedConfig.MemoryLimit != expectedStdCfg.MemoryLimit {
		t.Errorf("profile clamp: got MemoryLimit=%d (permissive=0), want %d (standard)", capturedConfig.MemoryLimit, expectedStdCfg.MemoryLimit)
	}
	if capturedConfig.NetworkMode != expectedStdCfg.NetworkMode {
		t.Errorf("profile clamp: got NetworkMode=%q, want %q", capturedConfig.NetworkMode, expectedStdCfg.NetworkMode)
	}
}

// --- Secret resolution tests ---

func TestExec_SecretResolution(t *testing.T) {
	provider := &fakeProvider{
		values: map[string]string{
			"db/password": "s3cr3t!",
		},
	}

	var capturedConfig sandbox.SandboxConfig
	factory := func(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
		capturedConfig = cfg
		return &fakeRunner{}, nil
	}

	client, cleanup := buildTestServer(t, provider, "", factory)
	defer cleanup()

	stream, err := client.Exec(context.Background(), &pb.SandboxExecRequest{
		Profile: "standard",
		Image:   "alpine:3.19",
		Command: []string{"env"},
		Env: map[string]string{
			"DB_PASS": "secret://db/password",
			"PLAIN":   "no-secret-here",
		},
	})
	if err != nil {
		t.Fatalf("Exec RPC: %v", err)
	}
	_ = drainStream(t, stream)

	// The env the runner receives must have the resolved value.
	if capturedConfig.Env["DB_PASS"] != "s3cr3t!" {
		t.Errorf("DB_PASS not resolved: got %q, want %q", capturedConfig.Env["DB_PASS"], "s3cr3t!")
	}
	// Non-secret values must be passed through unchanged.
	if capturedConfig.Env["PLAIN"] != "no-secret-here" {
		t.Errorf("PLAIN: got %q, want %q", capturedConfig.Env["PLAIN"], "no-secret-here")
	}
}

func TestExec_SecretResolution_MissingRef_Error(t *testing.T) {
	// Provider has no entry for the requested key.
	provider := &fakeProvider{values: map[string]string{}}

	factory := func(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
		return &fakeRunner{}, nil
	}

	client, cleanup := buildTestServer(t, provider, "", factory)
	defer cleanup()

	stream, err := client.Exec(context.Background(), &pb.SandboxExecRequest{
		Profile: "standard",
		Image:   "alpine:3.19",
		Command: []string{"env"},
		Env: map[string]string{
			"MISSING": "secret://does-not-exist",
		},
	})
	if err != nil {
		// Some gRPC versions return the error directly from the client call.
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.InvalidArgument {
			t.Fatalf("expected InvalidArgument, got: %v", err)
		}
		return
	}
	// If the error comes from the stream, drainStream will capture it.
	for {
		_, recvErr := stream.Recv()
		if recvErr == io.EOF {
			t.Fatal("expected error from stream, got EOF")
		}
		if recvErr != nil {
			st, ok := status.FromError(recvErr)
			if !ok {
				t.Fatalf("non-status error: %v", recvErr)
			}
			if st.Code() != codes.InvalidArgument {
				t.Errorf("expected InvalidArgument, got %v: %v", st.Code(), recvErr)
			}
			return
		}
	}
}

// --- Auth interceptor tests ---

// TestAuth_NoToken_Configured_PermitsAll covers the interceptor behavior when the
// agent is started WITHOUT a token. NOTE: as of the security review fix, the agent
// only reaches this state when the operator passed --allow-unauthenticated (see
// checkAuthRequirement / TestCheckAuthRequirement in main_test.go) — otherwise main
// refuses to start. The interceptor itself permits all when no token is configured.
func TestAuth_NoToken_Configured_PermitsAll(t *testing.T) {
	client, cleanup := buildTestServer(t, &fakeProvider{}, "" /* empty = no auth; requires --allow-unauthenticated to reach */, func(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
		return &fakeRunner{}, nil
	})
	defer cleanup()

	// No auth header — should be allowed when no token is configured.
	stream, err := client.Exec(context.Background(), &pb.SandboxExecRequest{
		Profile: "standard", Image: "alpine", Command: []string{"true"},
	})
	if err != nil {
		t.Fatalf("expected success without auth configured, got: %v", err)
	}
	_ = drainStream(t, stream)
}

func TestAuth_CorrectToken_Accepted(t *testing.T) {
	const token = "secret-token-abc"
	client, cleanup := buildTestServer(t, &fakeProvider{}, token, func(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
		return &fakeRunner{}, nil
	})
	defer cleanup()

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	stream, err := client.Exec(ctx, &pb.SandboxExecRequest{
		Profile: "standard", Image: "alpine", Command: []string{"true"},
	})
	if err != nil {
		t.Fatalf("Exec with correct token: %v", err)
	}
	_ = drainStream(t, stream)
}

func TestAuth_WrongToken_Unauthenticated(t *testing.T) {
	const token = "secret-token-abc"
	client, cleanup := buildTestServer(t, &fakeProvider{}, token, func(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
		return &fakeRunner{}, nil
	})
	defer cleanup()

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer wrong-token"))
	stream, err := client.Exec(ctx, &pb.SandboxExecRequest{
		Profile: "standard", Image: "alpine", Command: []string{"true"},
	})
	if err != nil {
		st, _ := status.FromError(err)
		if st.Code() != codes.Unauthenticated {
			t.Errorf("expected Unauthenticated, got %v", st.Code())
		}
		return
	}
	// Error may arrive on first Recv.
	_, recvErr := stream.Recv()
	if recvErr == nil || recvErr == io.EOF {
		t.Fatal("expected Unauthenticated error")
	}
	st, _ := status.FromError(recvErr)
	if st.Code() != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v: %v", st.Code(), recvErr)
	}
}

func TestAuth_MissingToken_Unauthenticated(t *testing.T) {
	const token = "secret-token-abc"
	client, cleanup := buildTestServer(t, &fakeProvider{}, token, func(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
		return &fakeRunner{}, nil
	})
	defer cleanup()

	// No auth header at all.
	stream, err := client.Exec(context.Background(), &pb.SandboxExecRequest{
		Profile: "standard", Image: "alpine", Command: []string{"true"},
	})
	if err != nil {
		st, _ := status.FromError(err)
		if st.Code() != codes.Unauthenticated {
			t.Errorf("expected Unauthenticated, got %v", st.Code())
		}
		return
	}
	_, recvErr := stream.Recv()
	if recvErr == nil || recvErr == io.EOF {
		t.Fatal("expected Unauthenticated error")
	}
	st, _ := status.FromError(recvErr)
	if st.Code() != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v: %v", st.Code(), recvErr)
	}
}

// --- Stream output test ---

func TestExec_OutputChunks(t *testing.T) {
	factory := func(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
		return &fakeRunner{
			result: &sandbox.ExecResult{
				ExitCode: 0,
				Stdout:   "hello stdout",
				Stderr:   "hello stderr",
			},
		}, nil
	}

	client, cleanup := buildTestServer(t, &fakeProvider{}, "", factory)
	defer cleanup()

	stream, err := client.Exec(context.Background(), &pb.SandboxExecRequest{
		Profile: "standard", Image: "alpine", Command: []string{"echo", "hi"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	chunks := drainStream(t, stream)
	var gotStdout, gotStderr bool
	var gotExitCode bool
	for _, chunk := range chunks {
		switch v := chunk.Chunk.(type) {
		case *pb.SandboxExecChunk_Stdout:
			gotStdout = true
			if string(v.Stdout) != "hello stdout" {
				t.Errorf("stdout: got %q, want %q", v.Stdout, "hello stdout")
			}
		case *pb.SandboxExecChunk_Stderr:
			gotStderr = true
			if string(v.Stderr) != "hello stderr" {
				t.Errorf("stderr: got %q, want %q", v.Stderr, "hello stderr")
			}
		case *pb.SandboxExecChunk_ExitCode:
			gotExitCode = true
			if v.ExitCode != 0 {
				t.Errorf("exit_code: got %d, want 0", v.ExitCode)
			}
		}
	}
	if !gotStdout {
		t.Error("expected stdout chunk, got none")
	}
	if !gotStderr {
		t.Error("expected stderr chunk, got none")
	}
	if !gotExitCode {
		t.Error("expected exit_code chunk, got none")
	}
}

// TestExec_NonZeroExitCode verifies a non-zero command exit code is faithfully
// streamed as the terminal exit_code chunk (a failing command must not be
// reported as exit 0).
func TestExec_NonZeroExitCode(t *testing.T) {
	factory := func(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
		return &fakeRunner{
			result: &sandbox.ExecResult{ExitCode: 7, Stderr: "boom"},
		}, nil
	}

	client, cleanup := buildTestServer(t, &fakeProvider{}, "", factory)
	defer cleanup()

	stream, err := client.Exec(context.Background(), &pb.SandboxExecRequest{
		Profile: "standard", Image: "alpine", Command: []string{"false"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	chunks := drainStream(t, stream)
	var exitCode int32 = -1
	var gotExit bool
	for _, chunk := range chunks {
		if v, ok := chunk.Chunk.(*pb.SandboxExecChunk_ExitCode); ok {
			gotExit = true
			exitCode = v.ExitCode
		}
	}
	if !gotExit {
		t.Fatal("expected terminal exit_code chunk, got none")
	}
	if exitCode != 7 {
		t.Errorf("exit_code: got %d, want 7", exitCode)
	}
}

// expectStreamCode opens the stream's first Recv (or the call error) and asserts
// the returned gRPC status code. Used by error-path tests.
func expectStreamCode(t *testing.T, callErr error, stream pb.SandboxExecService_ExecClient, want codes.Code) {
	t.Helper()
	if callErr != nil {
		st, _ := status.FromError(callErr)
		if st.Code() != want {
			t.Errorf("expected %v, got %v: %v", want, st.Code(), callErr)
		}
		return
	}
	_, recvErr := stream.Recv()
	if recvErr == nil || recvErr == io.EOF {
		t.Fatalf("expected %v error, got: %v", want, recvErr)
	}
	st, _ := status.FromError(recvErr)
	if st.Code() != want {
		t.Errorf("expected %v, got %v: %v", want, st.Code(), recvErr)
	}
}

// TestAuth_WrongLengthToken_Unauthenticated verifies a token of a DIFFERENT
// LENGTH than the configured token is rejected. This guards the length-independent
// (SHA-256 digest) comparison path: a raw ConstantTimeCompare would early-return on
// length mismatch and leak the expected token's length via timing.
func TestAuth_WrongLengthToken_Unauthenticated(t *testing.T) {
	const token = "secret-token-abc" // 16 bytes
	client, cleanup := buildTestServer(t, &fakeProvider{}, token, func(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
		return &fakeRunner{}, nil
	})
	defer cleanup()

	// A much shorter token (different length) must still be rejected as Unauthenticated.
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer x"))
	stream, err := client.Exec(ctx, &pb.SandboxExecRequest{
		Profile: "standard", Image: "alpine", Command: []string{"true"},
	})
	expectStreamCode(t, err, stream, codes.Unauthenticated)
}

// TestExec_EmptyCommand_InvalidArgument verifies an empty command is a caller
// error (InvalidArgument), not a server failure (Internal).
func TestExec_EmptyCommand_InvalidArgument(t *testing.T) {
	client, cleanup := buildTestServer(t, &fakeProvider{}, "", func(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
		t.Error("runner factory must not be called for an empty command")
		return &fakeRunner{}, nil
	})
	defer cleanup()

	stream, err := client.Exec(context.Background(), &pb.SandboxExecRequest{
		Profile: "standard", Image: "alpine", Command: nil, // empty
	})
	expectStreamCode(t, err, stream, codes.InvalidArgument)
}

// TestExec_MissingImage_InvalidArgument verifies a missing image is a caller
// error (InvalidArgument), not a server failure (Internal).
func TestExec_MissingImage_InvalidArgument(t *testing.T) {
	client, cleanup := buildTestServer(t, &fakeProvider{}, "", func(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
		t.Error("runner factory must not be called for a missing image")
		return &fakeRunner{}, nil
	})
	defer cleanup()

	stream, err := client.Exec(context.Background(), &pb.SandboxExecRequest{
		Profile: "standard", Image: "", Command: []string{"echo", "hi"},
	})
	expectStreamCode(t, err, stream, codes.InvalidArgument)
}
