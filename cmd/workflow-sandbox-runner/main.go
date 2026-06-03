// Package main is the entrypoint for the workflow-sandbox-runner agent.
//
// The agent serves the SandboxExecService gRPC interface over mTLS + bearer-token auth.
// It resolves secret:// references in env values server-side before launching commands,
// and clamps requested security profiles to a safe maximum (permissive → standard).
//
// Design: docs/decisions/0019-remote-sandbox-agent.md (ADR 0019)
package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime/debug"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/secrets"
)

// version is set at build time via -ldflags "-X main.version=<version>".
// When built without ldflags (e.g. go run), buildVersion() reads the module
// version from the embedded build info, falling back to "dev".
var version = buildVersion()

func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func main() {
	showVersion := flag.Bool("version", false, "Print version and exit")
	listenAddr := flag.String("addr", ":50051", "gRPC listen address (host:port)")
	certFile := flag.String("tls-cert", "", "TLS certificate file (PEM)")
	keyFile := flag.String("tls-key", "", "TLS private key file (PEM)")
	caFile := flag.String("tls-ca", "", "CA certificate for mTLS client auth (PEM; enables mTLS when set). REQUIRED (or --token) unless --allow-unauthenticated.")
	bearerToken := flag.String("token", "", "Expected bearer token for RPC auth (from SANDBOX_RUNNER_TOKEN env if unset). REQUIRED (or --tls-ca) unless --allow-unauthenticated.")
	allowUnauth := flag.Bool("allow-unauthenticated", false, "Permit startup with NO auth (no token AND no mTLS). DANGEROUS — local/dev only, never production.")
	secretsBackend := flag.String("secrets-backend", "env", "Secrets backend for secret:// resolution: env, file")
	secretsDir := flag.String("secrets-dir", "", "Directory for 'file' secrets backend")
	secretsEnvPrefix := flag.String("secrets-env-prefix", "", "Env-var prefix for 'env' secrets backend")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	// Allow token to come from environment for 12-factor deployment.
	token := *bearerToken
	if token == "" {
		token = os.Getenv("SANDBOX_RUNNER_TOKEN")
	}

	// Refuse to start as an unauthenticated remote code executor unless the
	// operator explicitly opts in. "Authenticated" means a bearer token OR mTLS
	// (a CA configured for client-cert verification).
	if err := checkAuthRequirement(token, *caFile, *allowUnauth); err != nil {
		slog.Error("sandbox-runner: refusing to start", "err", err)
		os.Exit(1)
	}
	if *allowUnauth && token == "" && *caFile == "" {
		slog.Warn("sandbox-runner: WARNING — running with NO authentication (no token, no mTLS); do NOT use in production")
	}

	// Build secrets provider for server-side secret:// resolution.
	var provider secrets.Provider
	switch *secretsBackend {
	case "file":
		if *secretsDir == "" {
			slog.Error("sandbox-runner: --secrets-dir is required for file backend")
			os.Exit(1)
		}
		provider = secrets.NewFileProvider(*secretsDir)
	default: // "env"
		provider = secrets.NewEnvProvider(*secretsEnvPrefix)
	}

	// Build gRPC server options.
	serverOpts, err := buildServerOptions(*certFile, *keyFile, *caFile)
	if err != nil {
		slog.Error("sandbox-runner: failed to build TLS options", "err", err)
		os.Exit(1)
	}

	// Stream interceptor for bearer-token auth.
	serverOpts = append(serverOpts, grpc.StreamInterceptor(newBearerStreamInterceptor(token)))

	grpcServer := grpc.NewServer(serverOpts...)
	srv := newSandboxExecServer(provider, slog.Default())
	pb.RegisterSandboxExecServiceServer(grpcServer, srv)

	lis, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		slog.Error("sandbox-runner: listen failed", "addr", *listenAddr, "err", err)
		os.Exit(1)
	}

	slog.Info("sandbox-runner: starting", "addr", *listenAddr, "version", version)
	if err := grpcServer.Serve(lis); err != nil {
		slog.Error("sandbox-runner: serve error", "err", err)
		os.Exit(1)
	}
}

// checkAuthRequirement enforces that the agent — a remote command executor —
// has SOME authentication configured. Auth is satisfied by either a non-empty
// bearer token or mTLS (a CA file for client-cert verification). If neither is
// present, startup is refused unless allowUnauth is explicitly set.
func checkAuthRequirement(token, caFile string, allowUnauth bool) error {
	if token != "" || caFile != "" {
		return nil
	}
	if allowUnauth {
		return nil
	}
	return fmt.Errorf("no authentication configured: set --token (bearer auth) or --tls-ca (mTLS), " +
		"or pass --allow-unauthenticated for local/dev only (never production)")
}

// buildServerOptions constructs gRPC server credentials from the supplied TLS files.
// If certFile and keyFile are empty, the server starts without TLS (test/dev only).
// If caFile is also set, mTLS client authentication is enabled.
func buildServerOptions(certFile, keyFile, caFile string) ([]grpc.ServerOption, error) {
	// mTLS requires the server to present its own certificate; a CA alone only
	// configures client-cert verification. Refuse a --tls-ca that lacks the
	// server cert/key, otherwise the agent would start INSECURE while the
	// operator believes mTLS is on (checkAuthRequirement treats --tls-ca as auth).
	if caFile != "" && (certFile == "" || keyFile == "") {
		return nil, fmt.Errorf("--tls-ca requires both --tls-cert and --tls-key (mTLS needs the server's own certificate)")
	}
	if certFile == "" && keyFile == "" {
		// No TLS — insecure mode for local development / testing.
		return []grpc.ServerOption{grpc.Creds(insecure.NewCredentials())}, nil
	}
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("both --tls-cert and --tls-key must be set together")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load server cert/key: %w", err)
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}
	if caFile != "" {
		caPEM, err := os.ReadFile(caFile) //nolint:gosec // G304: path is operator-supplied via flag
		if err != nil {
			return nil, fmt.Errorf("read CA cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse CA cert from %q", caFile)
		}
		tlsCfg.ClientCAs = pool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return []grpc.ServerOption{grpc.Creds(credentials.NewTLS(tlsCfg))}, nil
}
