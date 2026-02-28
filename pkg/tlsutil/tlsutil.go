package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// TLSConfig is the common TLS configuration used across all transports.
type TLSConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	CertFile   string `yaml:"cert_file" json:"cert_file"`
	KeyFile    string `yaml:"key_file" json:"key_file"`
	CAFile     string `yaml:"ca_file" json:"ca_file"`
	ClientAuth string `yaml:"client_auth" json:"client_auth"` // require | request | none
	SkipVerify bool   `yaml:"skip_verify" json:"skip_verify"` // for dev only
}

// AutocertConfig holds Let's Encrypt autocert configuration.
type AutocertConfig struct {
	Domains  []string `yaml:"domains" json:"domains"`
	CacheDir string   `yaml:"cache_dir" json:"cache_dir"`
	Email    string   `yaml:"email" json:"email"`
}

// LoadTLSConfig builds a *tls.Config from the YAML-friendly struct.
func LoadTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.SkipVerify, //nolint:gosec // G402: intentional dev-only option
	}

	// Load server/client certificate keypair if provided
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("tlsutil: load key pair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	// Load CA certificate for client verification or custom root CA
	if cfg.CAFile != "" {
		caPEM, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("tlsutil: read CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("tlsutil: no valid certificates found in %s", cfg.CAFile)
		}
		tlsCfg.RootCAs = pool
		tlsCfg.ClientCAs = pool
	}

	// Configure client authentication policy
	switch cfg.ClientAuth {
	case "require":
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	case "request":
		tlsCfg.ClientAuth = tls.RequestClientCert
	case "none", "":
		tlsCfg.ClientAuth = tls.NoClientCert
	default:
		return nil, fmt.Errorf("tlsutil: unknown client_auth %q (valid: require, request, none)", cfg.ClientAuth)
	}

	return tlsCfg, nil
}
