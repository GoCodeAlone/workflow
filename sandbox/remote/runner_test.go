package remote

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// bufConnSize is the in-memory pipe buffer used by the bufconn listener.
// Matches the fixtureBufSize used in cmd/wfctl/iac_typed_fixture_test.go.
const bufConnSize = 1024 * 1024

// ─── test stub server ────────────────────────────────────────────────────────

// stubExecServer is an in-process SandboxExecServiceServer for unit tests.
// It streams back a configurable sequence of stdout/stderr chunks followed by
// an exit_code, and records every received SandboxExecRequest for assertion.
//
// The mutex makes lastRequest safe to read from a test goroutine while Exec
// runs on a server goroutine (and safe when concurrent Exec RPCs land in the
// -race concurrency test).
type stubExecServer struct {
	pb.UnimplementedSandboxExecServiceServer

	// config
	stdoutData []byte
	stderrData []byte
	exitCode   int32
	// omitExitCode, when true, makes Exec return WITHOUT sending the terminal
	// exit_code chunk — simulating an agent crash / truncated stream so the
	// client's missing-exit-code guard can be exercised.
	omitExitCode bool

	mu sync.Mutex
	// recorded state (set during Exec; read in assertions)
	lastRequest *pb.SandboxExecRequest
}

func (s *stubExecServer) Exec(req *pb.SandboxExecRequest, stream grpc.ServerStreamingServer[pb.SandboxExecChunk]) error {
	s.mu.Lock()
	s.lastRequest = req
	s.mu.Unlock()

	if len(s.stdoutData) > 0 {
		if err := stream.Send(&pb.SandboxExecChunk{Chunk: &pb.SandboxExecChunk_Stdout{Stdout: s.stdoutData}}); err != nil {
			return err
		}
	}
	if len(s.stderrData) > 0 {
		if err := stream.Send(&pb.SandboxExecChunk{Chunk: &pb.SandboxExecChunk_Stderr{Stderr: s.stderrData}}); err != nil {
			return err
		}
	}
	if s.omitExitCode {
		// Return without the exit_code chunk: the stream closes (io.EOF on the
		// client) with no terminal exit code, simulating a crashed agent.
		return nil
	}
	return stream.Send(&pb.SandboxExecChunk{Chunk: &pb.SandboxExecChunk_ExitCode{ExitCode: s.exitCode}})
}

// getLastRequest returns the most recently received request under the mutex.
func (s *stubExecServer) getLastRequest() *pb.SandboxExecRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRequest
}

// ─── test helpers ────────────────────────────────────────────────────────────

// startBufconnServer starts an in-process gRPC server backed by a bufconn
// listener.  It registers the provided SandboxExecServiceServer and returns
// a dialing function suitable for grpc.WithContextDialer.
// All cleanup is registered with t.Cleanup.
//
// mTLS note: the unit test uses insecure transport (no TLS certificates) —
// mTLS is exercised end-to-end in the scenario suite (PR11).  The comment
// here documents the deliberate choice so future readers don't mistake the
// lack of TLS for an oversight.
func startBufconnServer(t *testing.T, srv pb.SandboxExecServiceServer) func(context.Context, string) (net.Conn, error) {
	t.Helper()
	l := bufconn.Listen(bufConnSize)
	t.Cleanup(func() { _ = l.Close() })

	s := grpc.NewServer()
	pb.RegisterSandboxExecServiceServer(s, srv)
	go func() { _ = s.Serve(l) }()
	t.Cleanup(s.Stop)

	return func(ctx context.Context, _ string) (net.Conn, error) {
		return l.DialContext(ctx)
	}
}

// newTestRunner creates a RemoteRunner wired to the given bufconn dialer.
// It uses insecure transport (matching the test server) and no bearer token
// so unit tests are self-contained without certificates.
func newTestRunner(t *testing.T, dialer func(context.Context, string) (net.Conn, error), cfg RemoteRunnerConfig) *RemoteRunner {
	t.Helper()
	// Override address to a placeholder; we replace the dialer via DialOption.
	cfg.Address = "passthrough:///bufnet"
	cfg.TLS = nil // ensure insecure path in connect()

	r := &RemoteRunner{cfg: cfg}

	conn, err := grpc.NewClient(cfg.Address,
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("newTestRunner: grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	r.conn = conn
	return r
}

// ─── tests ───────────────────────────────────────────────────────────────────

// TestRemoteRunner_ExecReturnsExpectedResult verifies that RemoteRunner.Exec
// accumulates stdout/stderr chunks and the exit_code from the streaming
// SandboxExecService and returns a correctly populated sandbox.ExecResult.
func TestRemoteRunner_ExecReturnsExpectedResult(t *testing.T) {
	stub := &stubExecServer{
		stdoutData: []byte("hello stdout"),
		stderrData: []byte("hello stderr"),
		exitCode:   42,
	}
	dialer := startBufconnServer(t, stub)

	runner := newTestRunner(t, dialer, RemoteRunnerConfig{
		Profile: "default",
		Image:   "alpine:3.19",
	})

	result, err := runner.Exec(context.Background(), []string{"echo", "hello"})
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}

	if result.Stdout != "hello stdout" {
		t.Errorf("stdout: want %q, got %q", "hello stdout", result.Stdout)
	}
	if result.Stderr != "hello stderr" {
		t.Errorf("stderr: want %q, got %q", "hello stderr", result.Stderr)
	}
	if result.ExitCode != 42 {
		t.Errorf("exit_code: want 42, got %d", result.ExitCode)
	}
}

// TestRemoteRunner_ExecPassesCommandToServer verifies that the command slice
// supplied to Exec is forwarded verbatim in the SandboxExecRequest.
func TestRemoteRunner_ExecPassesCommandToServer(t *testing.T) {
	stub := &stubExecServer{exitCode: 0}
	dialer := startBufconnServer(t, stub)

	runner := newTestRunner(t, dialer, RemoteRunnerConfig{
		Profile: "strict",
		Image:   "busybox:1.36",
	})

	cmd := []string{"sh", "-c", "echo hi"}
	result, err := runner.Exec(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	// Assert the zero exit code is reported faithfully (not just the 42 case).
	if result.ExitCode != 0 {
		t.Errorf("exit_code: want 0, got %d", result.ExitCode)
	}

	req := stub.getLastRequest()
	if req == nil {
		t.Fatal("stub: no request received")
	}
	if len(req.Command) != len(cmd) {
		t.Fatalf("command len: want %d, got %d", len(cmd), len(req.Command))
	}
	for i, arg := range cmd {
		if req.Command[i] != arg {
			t.Errorf("command[%d]: want %q, got %q", i, arg, req.Command[i])
		}
	}
	if req.Profile != "strict" {
		t.Errorf("profile: want %q, got %q", "strict", req.Profile)
	}
	if req.Image != "busybox:1.36" {
		t.Errorf("image: want %q, got %q", "busybox:1.36", req.Image)
	}
}

// TestRemoteRunner_ExecPassesSecretRefUnresolved asserts the ADR 0017 invariant:
// env values that contain secret:// references MUST arrive at the agent
// unmodified — RemoteRunner MUST NOT resolve them.
//
// The stub captures the received SandboxExecRequest and the test asserts that
// the raw "secret://vault/x" string is present in env, proving the client
// passed it verbatim.
func TestRemoteRunner_ExecPassesSecretRefUnresolved(t *testing.T) {
	const secretRef = "secret://vault/my-key"

	stub := &stubExecServer{exitCode: 0}
	dialer := startBufconnServer(t, stub)

	runner := newTestRunner(t, dialer, RemoteRunnerConfig{
		Image: "alpine:3.19",
		Env: map[string]string{
			"DB_PASSWORD": secretRef,
			"PORT":        "8080",
		},
	})

	_, err := runner.Exec(context.Background(), []string{"env"})
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}

	req := stub.getLastRequest()
	if req == nil {
		t.Fatal("stub: no request received")
	}
	got, ok := req.Env["DB_PASSWORD"]
	if !ok {
		t.Fatal("env: DB_PASSWORD key missing in received request")
	}
	if got != secretRef {
		t.Errorf("env[DB_PASSWORD]: want unresolved %q, got %q (client must NOT resolve secret:// refs)", secretRef, got)
	}
}

// TestRemoteRunner_Close verifies that Close does not panic and can be called
// multiple times safely.
func TestRemoteRunner_Close(t *testing.T) {
	stub := &stubExecServer{exitCode: 0}
	dialer := startBufconnServer(t, stub)

	runner := newTestRunner(t, dialer, RemoteRunnerConfig{Image: "alpine:3.19"})

	if err := runner.Close(); err != nil {
		t.Errorf("first Close: unexpected error: %v", err)
	}
	// Second Close must be a no-op (conn is nil after first Close).
	if err := runner.Close(); err != nil {
		t.Errorf("second Close: unexpected error: %v", err)
	}
}

// TestNewRemoteRunner_RequiresAddress verifies that NewRemoteRunner returns an
// error when Address is empty.
func TestNewRemoteRunner_RequiresAddress(t *testing.T) {
	_, err := NewRemoteRunner(RemoteRunnerConfig{Image: "alpine:3.19"})
	if err == nil {
		t.Fatal("expected error for empty address, got nil")
	}
}

// TestRemoteRunner_ExecMissingExitCodeIsError asserts that a stream which ends
// (io.EOF) WITHOUT delivering an exit_code chunk — an agent crash or truncated
// stream — surfaces as an error rather than a silent ExecResult{ExitCode: 0}
// false success.
func TestRemoteRunner_ExecMissingExitCodeIsError(t *testing.T) {
	stub := &stubExecServer{
		stdoutData:   []byte("partial output before crash"),
		omitExitCode: true,
	}
	dialer := startBufconnServer(t, stub)

	runner := newTestRunner(t, dialer, RemoteRunnerConfig{Image: "alpine:3.19"})

	result, err := runner.Exec(context.Background(), []string{"do-something"})
	if err == nil {
		t.Fatalf("expected error when stream closes without exit_code, got result=%+v", result)
	}
	if result != nil {
		t.Errorf("expected nil result on missing exit_code, got %+v", result)
	}
	if !strings.Contains(err.Error(), "exit_code") {
		t.Errorf("error should mention missing exit_code; got %q", err.Error())
	}
}

// TestRemoteRunner_ConcurrentExecAndClose drives Exec from many goroutines
// while Close races against them. Under -race this proves connect() returning
// the *grpc.ClientConn (rather than Exec re-reading r.conn unlocked) closes the
// data race / nil-deref window: a concurrent Close() setting r.conn = nil
// cannot nil the connection out from under an in-flight Exec.
//
// Exec calls after Close may legitimately fail (the conn is closed / re-dialed
// or the RPC errors); the test only asserts no panic and no data race — it
// tolerates per-call errors.
func TestRemoteRunner_ConcurrentExecAndClose(t *testing.T) {
	stub := &stubExecServer{stdoutData: []byte("ok"), exitCode: 0}
	dialer := startBufconnServer(t, stub)

	runner := newTestRunner(t, dialer, RemoteRunnerConfig{Image: "alpine:3.19"})

	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			// Errors are tolerated; the point is no race / no nil panic.
			_, _ = runner.Exec(context.Background(), []string{"echo", "hi"})
		}()
	}

	// Race a Close against the in-flight Exec calls.
	_ = runner.Close()
	wg.Wait()
}

// TestRemoteRunner_ConcurrentExec drives Exec from many goroutines with no
// Close to prove the happy-path connect()/Exec read of the connection is also
// race-free under -race.
func TestRemoteRunner_ConcurrentExec(t *testing.T) {
	stub := &stubExecServer{stdoutData: []byte("ok"), exitCode: 0}
	dialer := startBufconnServer(t, stub)

	runner := newTestRunner(t, dialer, RemoteRunnerConfig{Image: "alpine:3.19"})

	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := runner.Exec(context.Background(), []string{"echo", "hi"}); err != nil {
				t.Errorf("concurrent Exec: unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
}

// TestNewRemoteRunner_TokenWithoutTLSRejected asserts the credential-leak guard:
// a non-empty Token with no TLS and no AllowInsecure must be rejected so the
// bearer token is never sent in cleartext.
func TestNewRemoteRunner_TokenWithoutTLSRejected(t *testing.T) {
	_, err := NewRemoteRunner(RemoteRunnerConfig{
		Address: "127.0.0.1:1234",
		Token:   "secret-token",
		// TLS nil, AllowInsecure false
	})
	if err == nil {
		t.Fatal("expected error for token over insecure connection, got nil")
	}
	if !strings.Contains(err.Error(), "insecure") {
		t.Errorf("error should mention insecure connection; got %q", err.Error())
	}
}

// TestNewRemoteRunner_TokenWithoutTLSAllowedWithOptIn verifies the explicit
// AllowInsecure escape hatch lets a token-over-insecure runner construct (for
// local-dev / tests).
func TestNewRemoteRunner_TokenWithoutTLSAllowedWithOptIn(t *testing.T) {
	r, err := NewRemoteRunner(RemoteRunnerConfig{
		Address:       "127.0.0.1:1234",
		Token:         "secret-token",
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatalf("AllowInsecure should permit token over insecure: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil runner")
	}
	t.Cleanup(func() { _ = r.Close() })
}
