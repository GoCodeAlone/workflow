package secrets

import (
	"bufio"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

// DevVaultConfig holds configuration for a managed Vault dev server.
type DevVaultConfig struct {
	// RootToken is the root token for the dev server. Default: "dev-root-token".
	RootToken string
	// ListenAddr is the address to listen on. Default: "127.0.0.1:0" (random port).
	ListenAddr string
	// MountPath is the KV v2 mount path. Default: "secret".
	MountPath string
}

// DevVaultProvider manages a Vault dev server subprocess and provides
// a real VaultProvider connected to it. This is useful for local development
// and integration testing without requiring an external Vault server.
type DevVaultProvider struct {
	*VaultProvider
	cmd  *exec.Cmd
	addr string
}

// NewDevVaultProvider starts a Vault dev server and returns a provider connected to it.
// It finds the vault binary on PATH, starts it with -dev mode, waits for readiness,
// and returns a fully functional VaultProvider.
//
// The caller must call Close() to stop the subprocess.
//
// Returns an error if the vault binary is not found or the server fails to start.
func NewDevVaultProvider(cfg DevVaultConfig) (*DevVaultProvider, error) {
	// Find vault binary
	vaultBin, err := exec.LookPath("vault")
	if err != nil {
		return nil, fmt.Errorf("%w: vault binary not found on PATH (install from https://developer.hashicorp.com/vault/install): %v", ErrProviderInit, err)
	}

	// Apply defaults
	if cfg.RootToken == "" {
		cfg.RootToken = "dev-root-token"
	}
	if cfg.ListenAddr == "" {
		// Pick a random free port
		port, err := getFreePort()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to find free port: %v", ErrProviderInit, err)
		}
		cfg.ListenAddr = fmt.Sprintf("127.0.0.1:%d", port)
	}
	if cfg.MountPath == "" {
		cfg.MountPath = "secret"
	}

	// Start vault dev server
	cmd := exec.Command(vaultBin, "server", "-dev", //nolint:gosec // vault binary path comes from exec.LookPath
		"-dev-root-token-id="+cfg.RootToken,
		"-dev-listen-address="+cfg.ListenAddr,
		"-dev-no-store-token",
	)

	// Capture stderr for readiness detection
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to create stderr pipe: %v", ErrProviderInit, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: failed to start vault dev server: %v", ErrProviderInit, err)
	}

	// Wait for readiness by scanning stderr for the ready message
	ready := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "Development mode should NOT be used in production") ||
				strings.Contains(line, "Vault server started") ||
				strings.Contains(line, "Api Address") {
				close(ready)
				return
			}
		}
	}()

	// Wait for ready or timeout
	select {
	case <-ready:
		// Give a brief moment for the server to finish initializing
		time.Sleep(100 * time.Millisecond)
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("%w: vault dev server failed to start within 10 seconds", ErrProviderInit)
	}

	// Create the real VaultProvider pointing at the dev server
	vaultAddr := "http://" + cfg.ListenAddr
	provider, err := NewVaultProvider(VaultConfig{
		Address:   vaultAddr,
		Token:     cfg.RootToken,
		MountPath: cfg.MountPath,
	})
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("%w: failed to create vault provider for dev server: %v", ErrProviderInit, err)
	}

	return &DevVaultProvider{
		VaultProvider: provider,
		cmd:           cmd,
		addr:          cfg.ListenAddr,
	}, nil
}

// Addr returns the listen address of the dev server.
func (p *DevVaultProvider) Addr() string {
	return p.addr
}

// Close stops the Vault dev server subprocess and cleans up.
func (p *DevVaultProvider) Close() error {
	if p.cmd != nil && p.cmd.Process != nil {
		if err := p.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("secrets: failed to kill vault dev server: %w", err)
		}
		// Wait to avoid zombies
		_ = p.cmd.Wait()
	}
	return nil
}

// getFreePort asks the OS for a free TCP port.
func getFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
