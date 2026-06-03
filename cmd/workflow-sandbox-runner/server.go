package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/sandbox"
	"github.com/GoCodeAlone/workflow/secrets"
)

// allowedProfiles is the set of accepted security profiles that the agent will honour.
// Any profile NOT in this set (e.g. "permissive", or unknown values) is clamped to "standard".
var allowedProfiles = map[string]bool{
	"strict":   true,
	"standard": true,
}

// clampProfile enforces the agent-side profile allow-set (ADR 0019).
// "permissive" and any unknown profile are clamped to "standard".
// "strict" and "standard" are accepted unchanged.
func clampProfile(requested string) string {
	if allowedProfiles[requested] {
		return requested
	}
	return "standard"
}

// sandboxRunnerFactory is a function that creates a SandboxRunner from a SandboxConfig.
// The default is sandbox.NewLocalDockerRunner; tests inject a fake.
type sandboxRunnerFactory func(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error)

// sandboxExecServer implements pb.SandboxExecServiceServer.
type sandboxExecServer struct {
	pb.UnimplementedSandboxExecServiceServer
	provider      secrets.Provider
	log           *slog.Logger
	runnerFactory sandboxRunnerFactory
}

// newSandboxExecServer creates a new sandboxExecServer using the default
// (local Docker) runner factory.
func newSandboxExecServer(provider secrets.Provider, log *slog.Logger) *sandboxExecServer {
	return &sandboxExecServer{
		provider:      provider,
		log:           log,
		runnerFactory: sandbox.NewLocalDockerRunner,
	}
}

// newSandboxExecServerWithFactory creates a sandboxExecServer with an injected
// runner factory — used by tests to avoid requiring a Docker daemon.
func newSandboxExecServerWithFactory(provider secrets.Provider, log *slog.Logger, factory sandboxRunnerFactory) *sandboxExecServer {
	return &sandboxExecServer{
		provider:      provider,
		log:           log,
		runnerFactory: factory,
	}
}

// Exec implements SandboxExecServiceServer. It:
//  1. Clamps the requested security profile to the allowed set.
//  2. Resolves any secret:// references in req.Env server-side.
//  3. Runs the command via a local Docker runner.
//  4. Streams stdout/stderr chunks and terminates with an exit_code chunk.
func (s *sandboxExecServer) Exec(req *pb.SandboxExecRequest, stream grpc.ServerStreamingServer[pb.SandboxExecChunk]) error {
	ctx := stream.Context()

	// 0. Up-front request validation. These are caller errors, not server
	// failures — surface them as InvalidArgument rather than letting the Docker
	// runner reject them as a generic Internal error later.
	if len(req.GetCommand()) == 0 {
		return status.Error(codes.InvalidArgument, "command must not be empty")
	}
	if req.GetImage() == "" {
		return status.Error(codes.InvalidArgument, "image is required")
	}

	// 1. Profile clamping.
	clampedProfile := clampProfile(req.GetProfile())
	if clampedProfile != req.GetProfile() {
		s.log.Warn("sandbox-runner: clamped requested profile", "requested", req.GetProfile(), "effective", clampedProfile)
	}

	// 2. Agent-side secret resolution.
	resolvedEnv, err := s.resolveEnv(ctx, req.GetEnv())
	if err != nil {
		// Non-leaky error: don't include the key value or the secret ref in the
		// gRPC status (it would be visible to the caller and could leak context).
		return status.Errorf(codes.InvalidArgument, "failed to resolve one or more secret references in env")
	}

	// 3. Build sandbox config and create runner.
	sandboxCfg := sandbox.BuildSandboxConfig(clampedProfile, req.GetImage())
	if len(resolvedEnv) > 0 {
		sandboxCfg.Env = resolvedEnv
	}
	if req.GetWorkdir() != "" {
		sandboxCfg.WorkDir = req.GetWorkdir()
	}

	runner, err := s.runnerFactory(sandboxCfg)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to create sandbox runner")
	}
	defer runner.Close() //nolint:errcheck

	// 4. Execute the command and collect results.
	result, err := runner.Exec(ctx, req.GetCommand())
	if err != nil {
		return status.Errorf(codes.Internal, "sandbox execution failed")
	}

	// Stream stdout if non-empty.
	if result.Stdout != "" {
		if sendErr := stream.Send(&pb.SandboxExecChunk{
			Chunk: &pb.SandboxExecChunk_Stdout{Stdout: []byte(result.Stdout)},
		}); sendErr != nil {
			return sendErr
		}
	}

	// Stream stderr if non-empty.
	if result.Stderr != "" {
		if sendErr := stream.Send(&pb.SandboxExecChunk{
			Chunk: &pb.SandboxExecChunk_Stderr{Stderr: []byte(result.Stderr)},
		}); sendErr != nil {
			return sendErr
		}
	}

	// Terminal exit_code chunk.
	// ExitCode is an OS process exit code (0-255); int32 can hold the full range safely. //nolint:gosec // G115: safe cast, OS exit codes fit in int32
	return stream.Send(&pb.SandboxExecChunk{
		Chunk: &pb.SandboxExecChunk_ExitCode{ExitCode: int32(result.ExitCode)}, //nolint:gosec // G115
	})
}

// resolveEnv resolves all secret:// references in the env map.
// Returns a new map with resolved values. Non-secret values are passed through.
// If any reference cannot be resolved, an error is returned.
// NEVER log the resolved value — do not leak secret material to logs.
func (s *sandboxExecServer) resolveEnv(ctx context.Context, env map[string]string) (map[string]string, error) {
	if len(env) == 0 {
		return env, nil
	}
	resolver := secrets.NewResolver(s.provider)
	resolved := make(map[string]string, len(env))
	for k, v := range env {
		r, err := resolver.Resolve(ctx, v)
		if err != nil {
			// Log the key only (never the value or the resolved secret).
			s.log.Error("sandbox-runner: failed to resolve secret ref", "env_key", k)
			return nil, fmt.Errorf("resolve env key %q: %w", k, err)
		}
		resolved[k] = r
	}
	return resolved, nil
}

// newBearerStreamInterceptor returns a gRPC StreamServerInterceptor that checks
// the "authorization: Bearer <token>" metadata on every incoming streaming RPC.
// If token is empty, auth is disabled (useful for local development without a token).
func newBearerStreamInterceptor(token string) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if token == "" {
			// No auth configured — permit all.
			return handler(srv, ss)
		}
		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "missing metadata")
		}
		authValues := md.Get("authorization")
		if len(authValues) == 0 {
			return status.Error(codes.Unauthenticated, "missing authorization header")
		}
		const prefix = "Bearer "
		authHeader := authValues[0]
		if !strings.HasPrefix(authHeader, prefix) {
			return status.Error(codes.Unauthenticated, "authorization header must use Bearer scheme")
		}
		// Compare fixed-length SHA-256 digests so the comparison is constant-time
		// AND length-independent: subtle.ConstantTimeCompare is only constant-time
		// for equal-length inputs, so comparing the raw tokens would leak the
		// expected token's length via a different (early-return) code path. Digests
		// are always 32 bytes, removing that side channel.
		presented := authHeader[len(prefix):]
		got := sha256.Sum256([]byte(presented))
		want := sha256.Sum256([]byte(token))
		if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			return status.Error(codes.Unauthenticated, "invalid bearer token")
		}
		return handler(srv, ss)
	}
}
