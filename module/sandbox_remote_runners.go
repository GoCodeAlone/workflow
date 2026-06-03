package module

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/sandbox/remote"
)

// SandboxRemoteRunnerServiceName is the service name under which the
// RemoteRunnerRegistry is registered in the modular service registry.
const SandboxRemoteRunnerServiceName = "sandbox.remote_runners"

// reservedRunnerNames are exec_env values the engine handles itself — a remote
// runner registered under one of these names would be silently unreachable
// (resolveSandboxRunner short-circuits them before consulting the registry).
var reservedRunnerNames = map[string]bool{
	"":             true,
	"local-docker": true,
	"ephemeral":    true,
}

// RemoteRunnerSpec holds the config for a single named remote sandbox runner.
// It maps directly to one element of the sandbox.remote_runners[*] config block.
type RemoteRunnerSpec struct {
	// Name is the runner identifier — matched against exec_env in step.sandbox_exec.
	Name string `yaml:"name"`
	// Address is the gRPC target of the remote sandbox agent (host:port).
	Address string `yaml:"address"`
	// Token is the bearer token sent on every RPC. May be a secret:// reference,
	// which is resolved at runner-build time via the module's secrets provider.
	Token string `yaml:"token"`
	// TLS holds optional mTLS settings for the connection to the agent.
	TLS *RemoteRunnerTLSSpec `yaml:"tls,omitempty"`
	// AllowInsecure is an explicit local/dev-only opt-in that relaxes two
	// otherwise-rejected insecure configurations:
	//   - sending a non-empty Token over a non-TLS connection, and
	//   - registering a runner with NEITHER a token NOR TLS.
	// Production must use TLS (and/or a token); never set this in production.
	AllowInsecure bool `yaml:"allow_insecure"`
}

// RemoteRunnerTLSSpec carries the per-runner TLS certificate paths.
type RemoteRunnerTLSSpec struct {
	// Cert is the path to the client certificate file (PEM).
	Cert string `yaml:"cert"`
	// Key is the path to the client private key file (PEM).
	Key string `yaml:"key"`
	// CA is the path to the CA certificate used to verify the server (PEM).
	CA string `yaml:"ca"`
}

// hasTLS reports whether the spec configures any TLS material.
func (s RemoteRunnerSpec) hasTLS() bool {
	return s.TLS != nil && (s.TLS.Cert != "" || s.TLS.Key != "" || s.TLS.CA != "")
}

// RemoteRunnerRegistry exposes named RemoteRunnerSpecs to other modules.
// It is registered as a service under SandboxRemoteRunnerServiceName and
// consumed by resolveSandboxRunner in execenv_factory.go.
type RemoteRunnerRegistry struct {
	runners map[string]RemoteRunnerSpec
	// secretsProvider is the name of the secrets module used to resolve any
	// secret:// references in runner tokens. Empty means no provider configured.
	secretsProvider string
}

// Get returns the spec for the named runner, or (zero, false) if not found.
func (r *RemoteRunnerRegistry) Get(name string) (RemoteRunnerSpec, bool) {
	if r == nil {
		return RemoteRunnerSpec{}, false
	}
	spec, ok := r.runners[name]
	return spec, ok
}

// SecretsProvider returns the configured secrets provider name (may be empty).
func (r *RemoteRunnerRegistry) SecretsProvider() string {
	if r == nil {
		return ""
	}
	return r.secretsProvider
}

// SandboxRemoteRunnersModule is a modular.Module that parses the
// sandbox.remote_runners config section and registers a RemoteRunnerRegistry
// as a service. Other modules (e.g. execenv_factory) retrieve the registry
// via app.GetService(SandboxRemoteRunnerServiceName, ...).
//
// Config structure (YAML excerpt):
//
//	modules:
//	  - name: my-runners
//	    type: sandbox.remote_runners
//	    secrets_provider: my-vault   # optional; resolves secret:// tokens
//	    remote_runners:
//	      - name: prod-runner
//	        address: "agent.example.com:50051"
//	        token: "secret://runner/token"
//	        tls:
//	          cert: "/etc/certs/client.crt"
//	          key: "/etc/certs/client.key"
//	          ca: "/etc/certs/ca.crt"
type SandboxRemoteRunnersModule struct {
	name            string
	secretsProvider string
	specs           []RemoteRunnerSpec
	registry        *RemoteRunnerRegistry
}

// NewSandboxRemoteRunnersModule creates a new module from the provided name and
// config map. Config fields:
//   - remote_runners: []map with name/address/token/tls/allow_insecure fields.
//   - secrets_provider: optional name of a secrets module for secret:// token refs.
func NewSandboxRemoteRunnersModule(name string, cfg map[string]any) *SandboxRemoteRunnersModule {
	m := &SandboxRemoteRunnersModule{name: name}

	if sp, ok := cfg["secrets_provider"].(string); ok {
		m.secretsProvider = sp
	}

	runnersRaw, ok := cfg["remote_runners"]
	if !ok {
		return m
	}

	switch v := runnersRaw.(type) {
	case []any:
		for _, item := range v {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			m.specs = append(m.specs, parseRemoteRunnerSpec(itemMap))
		}
	case []map[string]any:
		for _, itemMap := range v {
			m.specs = append(m.specs, parseRemoteRunnerSpec(itemMap))
		}
	}
	return m
}

func parseRemoteRunnerSpec(m map[string]any) RemoteRunnerSpec {
	spec := RemoteRunnerSpec{}
	if n, ok := m["name"].(string); ok {
		spec.Name = n
	}
	if a, ok := m["address"].(string); ok {
		spec.Address = a
	}
	if t, ok := m["token"].(string); ok {
		spec.Token = t
	}
	if ai, ok := m["allow_insecure"].(bool); ok {
		spec.AllowInsecure = ai
	}
	if tlsRaw, ok := m["tls"].(map[string]any); ok {
		spec.TLS = &RemoteRunnerTLSSpec{}
		if c, ok := tlsRaw["cert"].(string); ok {
			spec.TLS.Cert = c
		}
		if k, ok := tlsRaw["key"].(string); ok {
			spec.TLS.Key = k
		}
		if ca, ok := tlsRaw["ca"].(string); ok {
			spec.TLS.CA = ca
		}
	}
	return spec
}

// Name satisfies modular.Module.
func (m *SandboxRemoteRunnersModule) Name() string { return m.name }

// Init validates the parsed specs and builds the registry. It rejects:
//   - a runner whose name is empty or a reserved value (would be unreachable),
//   - duplicate runner names (a second silently overwriting the first),
//   - a missing address,
//   - a no-auth-no-TLS runner unless it opts in via allow_insecure.
func (m *SandboxRemoteRunnersModule) Init(_ modular.Application) error {
	runners := make(map[string]RemoteRunnerSpec, len(m.specs))
	for _, spec := range m.specs {
		if reservedRunnerNames[spec.Name] {
			return fmt.Errorf("sandbox.remote_runners: runner name %q is empty or reserved (reserved: local-docker, ephemeral); choose a distinct name", spec.Name)
		}
		if _, dup := runners[spec.Name]; dup {
			return fmt.Errorf("sandbox.remote_runners: duplicate runner name %q", spec.Name)
		}
		if spec.Address == "" {
			return fmt.Errorf("sandbox.remote_runners: runner %q: 'address' is required", spec.Name)
		}
		// A runner with no bearer token AND no TLS is unauthenticated and
		// unencrypted — refuse unless the operator explicitly opts in.
		if spec.Token == "" && !spec.hasTLS() && !spec.AllowInsecure {
			return fmt.Errorf("sandbox.remote_runners: runner %q has neither a token nor TLS; set token, set tls, or set allow_insecure: true for local/dev only", spec.Name)
		}
		// A runner with a token but no TLS would leak the bearer token in
		// cleartext. The client-side NewRemoteRunner enforces this too, but
		// catch it here at Init so the misconfiguration fails fast at boot
		// rather than at first Execute.
		if spec.Token != "" && !spec.hasTLS() && !spec.AllowInsecure {
			return fmt.Errorf("sandbox.remote_runners: runner %q sets a token but no TLS; the token would be sent in cleartext — set tls, or set allow_insecure: true for local/dev only", spec.Name)
		}
		// Client cert/key must be set together — fail fast at boot rather than at
		// first Execute (buildTLSConfig also enforces this).
		if spec.TLS != nil && (spec.TLS.Cert == "") != (spec.TLS.Key == "") {
			return fmt.Errorf("sandbox.remote_runners: runner %q: tls.cert and tls.key must be set together (both-or-neither)", spec.Name)
		}
		runners[spec.Name] = spec
	}
	m.registry = &RemoteRunnerRegistry{
		runners:         runners,
		secretsProvider: m.secretsProvider,
	}
	return nil
}

// ProvidesServices exposes the RemoteRunnerRegistry under SandboxRemoteRunnerServiceName.
func (m *SandboxRemoteRunnersModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        SandboxRemoteRunnerServiceName,
			Description: "Registry of named remote sandbox runner connections",
			Instance:    m.registry,
		},
	}
}

// RequiresServices returns nil — this module has no service dependencies.
func (m *SandboxRemoteRunnersModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Start is a no-op.
func (m *SandboxRemoteRunnersModule) Start(_ context.Context) error { return nil }

// Stop is a no-op.
func (m *SandboxRemoteRunnersModule) Stop(_ context.Context) error { return nil }

// buildTLSConfig constructs a *tls.Config for mTLS from PEM file paths.
// certFile and keyFile are the client certificate and key (may be empty for server-only TLS).
// caFile is the CA certificate for verifying the server (required when non-empty).
func buildTLSConfig(certFile, keyFile, caFile string) (*tls.Config, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13}

	// Client cert and key must be supplied together (both-or-neither). Silently
	// ignoring a lone cert/key would drop client authentication the operator
	// intended to configure.
	if (certFile == "") != (keyFile == "") {
		return nil, fmt.Errorf("tls: 'cert' and 'key' must be set together (both-or-neither)")
	}

	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load client cert/key: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	if caFile != "" {
		caPEM, err := os.ReadFile(caFile) //nolint:gosec // G304: operator-configured path
		if err != nil {
			return nil, fmt.Errorf("read CA cert %q: %w", caFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse CA cert from %q", caFile)
		}
		tlsCfg.RootCAs = pool
	}

	return tlsCfg, nil
}

// buildRemoteRunnerFromSpec constructs a remote.RemoteRunner from a RemoteRunnerSpec,
// merging in the per-exec SandboxConfig (profile, image, env, workdir).
//
// resolvedToken is the bearer token AFTER any secret:// reference has been resolved
// (the caller resolves it via the module's secrets provider). It MUST already be a
// literal token value — never a secret:// reference.
func buildRemoteRunnerFromSpec(spec RemoteRunnerSpec, resolvedToken string, cfg remote.RemoteRunnerConfig) (*remote.RemoteRunner, error) {
	cfg.Address = spec.Address
	cfg.Token = resolvedToken
	cfg.AllowInsecure = spec.AllowInsecure

	if spec.hasTLS() {
		tlsCfg, err := buildTLSConfig(spec.TLS.Cert, spec.TLS.Key, spec.TLS.CA)
		if err != nil {
			return nil, fmt.Errorf("sandbox remote runner %q: build TLS config: %w", spec.Name, err)
		}
		cfg.TLS = tlsCfg
	}

	return remote.NewRemoteRunner(cfg)
}
