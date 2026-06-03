// Package remote provides a RemoteRunner that implements sandbox.SandboxRunner
// by dialing a remote sandbox agent over gRPC (mTLS + bearer token auth).
//
// The remote agent binary and config wiring land in PR8.  This package ships
// the client only (ADR 0019).
//
// Secret-ref invariant (ADR 0017): env values may carry unresolved secret://
// references. RemoteRunner passes them verbatim to the agent — it MUST NOT
// attempt to resolve them. The agent resolves secret:// refs server-side.
package remote

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/GoCodeAlone/workflow/sandbox"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// RemoteRunnerConfig carries the dial-time and per-exec identity of a remote
// sandbox agent. Profile, Image, Env, and WorkDir are sent on every
// SandboxExecRequest; command-specific overrides are applied by Exec.
type RemoteRunnerConfig struct {
	// Address is the gRPC target of the remote sandbox agent (host:port).
	Address string

	// Token is the bearer token sent in the "authorization" metadata header on
	// every RPC. Empty string means no bearer token is sent.
	Token string

	// TLS is the TLS configuration for mTLS dial. nil means insecure (useful
	// for unit tests; production always supplies a tls.Config with client certs).
	TLS *tls.Config

	// AllowInsecure permits a non-empty Token to be sent over an insecure
	// (non-TLS) connection. This is an explicit opt-in for tests and local
	// development ONLY. Without it, NewRemoteRunner rejects Token != "" &&
	// TLS == nil to prevent leaking the bearer token in cleartext.
	AllowInsecure bool

	// Profile is the requested sandbox security profile (e.g. "default",
	// "strict"). The agent clamps the effective profile to its configured
	// maximum-allowed value (PR8).
	Profile string

	// Image is the OCI image reference to use for command execution.
	Image string

	// Env is the base process environment sent to the agent. Values may be
	// unresolved secret:// references — the agent resolves them (ADR 0017).
	// RemoteRunner passes them verbatim; it MUST NOT resolve them.
	Env map[string]string

	// WorkDir is the working directory inside the container. Empty = image default.
	WorkDir string
}

// RemoteRunner implements sandbox.SandboxRunner by streaming commands to a
// remote sandbox agent over gRPC.
type RemoteRunner struct {
	cfg  RemoteRunnerConfig
	mu   sync.Mutex
	conn *grpc.ClientConn
}

// Compile-time assertion: *RemoteRunner satisfies sandbox.SandboxRunner.
var _ sandbox.SandboxRunner = (*RemoteRunner)(nil)

// NewRemoteRunner dials the remote sandbox agent and returns a RemoteRunner.
// The connection is lazy-cached; subsequent Exec calls reuse it.
//
// If a bearer Token is supplied without TLS, NewRemoteRunner returns an error
// unless AllowInsecure is set — sending a token over a cleartext connection
// would leak the credential (gRPC does not reject it because the runner's
// PerRPCCredentials.RequireTransportSecurity is intentionally false to allow
// the explicit local-dev/test opt-in).
func NewRemoteRunner(cfg RemoteRunnerConfig) (*RemoteRunner, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("remote: address is required")
	}
	if cfg.Token != "" && cfg.TLS == nil && !cfg.AllowInsecure {
		return nil, fmt.Errorf("remote: refusing to send bearer token over insecure connection: set TLS, or set AllowInsecure for local/dev only")
	}
	r := &RemoteRunner{cfg: cfg}
	if _, err := r.connect(); err != nil {
		return nil, err
	}
	return r, nil
}

// connect opens (or reuses) the gRPC connection to the remote agent and
// returns it. It returns the connection rather than relying on the caller to
// re-read r.conn, so a concurrent Close() (which sets r.conn = nil under r.mu)
// cannot nil the connection out from under an in-flight Exec.
// Caller must NOT hold r.mu.
func (r *RemoteRunner) connect() (*grpc.ClientConn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn != nil {
		return r.conn, nil
	}

	var dialOpts []grpc.DialOption

	if r.cfg.TLS != nil {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(r.cfg.TLS)))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	if r.cfg.Token != "" {
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(&bearerToken{token: r.cfg.Token}))
	}

	conn, err := grpc.NewClient(r.cfg.Address, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("remote: dial %s: %w", r.cfg.Address, err)
	}
	r.conn = conn
	return conn, nil
}

// Exec runs cmd inside the remote sandbox and returns the combined result.
// env VALUES that contain secret:// references are passed verbatim to the
// agent — RemoteRunner does NOT resolve them (ADR 0017).
func (r *RemoteRunner) Exec(ctx context.Context, cmd []string) (*sandbox.ExecResult, error) {
	// Reject an empty argv, matching the local DockerSandbox runner — an empty
	// command is a caller/config error, not a valid remote exec.
	if len(cmd) == 0 {
		return nil, fmt.Errorf("remote sandbox: empty command")
	}
	conn, err := r.connect()
	if err != nil {
		return nil, err
	}

	client := pb.NewSandboxExecServiceClient(conn)

	req := &pb.SandboxExecRequest{
		Profile: r.cfg.Profile,
		Image:   r.cfg.Image,
		Command: cmd,
		Env:     r.cfg.Env,
		Workdir: r.cfg.WorkDir,
	}

	stream, err := client.Exec(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("remote: Exec RPC: %w", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := 0
	exitCodeSeen := false

	for {
		chunk, recvErr := stream.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("remote: stream recv: %w", recvErr)
		}

		switch v := chunk.Chunk.(type) {
		case *pb.SandboxExecChunk_Stdout:
			stdout.Write(v.Stdout)
		case *pb.SandboxExecChunk_Stderr:
			stderr.Write(v.Stderr)
		case *pb.SandboxExecChunk_ExitCode:
			exitCode = int(v.ExitCode)
			exitCodeSeen = true
		}
		// exit_code is the terminal chunk per the proto contract. Stop reading as
		// soon as it arrives rather than waiting for io.EOF — a server that sends
		// exit_code but forgets to close (or sends trailing chunks) must not hang
		// the client.
		if exitCodeSeen {
			break
		}
	}

	// A stream that ends (io.EOF) without ever delivering an exit_code chunk is
	// a truncated stream (agent crash or protocol violation). Returning
	// ExecResult{ExitCode: 0} here would be a silent false success — a caller
	// checking ExitCode != 0 would treat a crashed remote command as passing.
	if !exitCodeSeen {
		return nil, fmt.Errorf("remote sandbox: stream closed without exit_code (agent crash or protocol violation)")
	}

	return &sandbox.ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

// Close releases the underlying gRPC connection held by the runner.
func (r *RemoteRunner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn == nil {
		return nil
	}
	err := r.conn.Close()
	r.conn = nil
	return err
}

// bearerToken implements grpc.PerRPCCredentials for bearer token auth.
// It attaches "authorization: Bearer <token>" to every RPC call.
type bearerToken struct {
	token string
}

func (b *bearerToken) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{
		"authorization": "Bearer " + b.token,
	}, nil
}

// RequireTransportSecurity returns false so the token path can be exercised
// over insecure transport (bufconn tests, AllowInsecure local-dev). The
// cleartext-leak guard lives in NewRemoteRunner (Token+no-TLS is rejected
// unless AllowInsecure is set), so this flag stays false to permit the
// explicit opt-in. In production, cfg.TLS is non-nil and the connection
// itself provides transport security.
func (b *bearerToken) RequireTransportSecurity() bool {
	return false
}
