package module

import (
	"testing"

	"github.com/GoCodeAlone/workflow/pkg/tlsutil"
)

func TestRedisCacheConfig_TLSField(t *testing.T) {
	cfg := RedisCacheConfig{
		Address: "localhost:6380",
		TLS: tlsutil.TLSConfig{
			Enabled:    true,
			SkipVerify: true,
		},
	}
	if !cfg.TLS.Enabled {
		t.Error("expected TLS.Enabled to be true")
	}
}

func TestKafkaBroker_SetTLSConfig(t *testing.T) {
	broker := NewKafkaBroker("test-kafka")
	broker.SetTLSConfig(KafkaTLSConfig{
		TLSConfig: tlsutil.TLSConfig{
			Enabled:    true,
			SkipVerify: true,
		},
		SASL: KafkaSASLConfig{
			Mechanism: "PLAIN",
			Username:  "user",
			Password:  "pass",
		},
	})

	broker.mu.RLock()
	defer broker.mu.RUnlock()
	if !broker.tlsCfg.Enabled {
		t.Error("expected TLS enabled")
	}
	if broker.tlsCfg.SASL.Mechanism != "PLAIN" {
		t.Errorf("expected PLAIN, got %s", broker.tlsCfg.SASL.Mechanism)
	}
}

func TestNATSBroker_SetTLSConfig(t *testing.T) {
	broker := NewNATSBroker("test-nats")
	broker.SetTLSConfig(tlsutil.TLSConfig{
		Enabled:    true,
		SkipVerify: true,
	})

	broker.mu.RLock()
	defer broker.mu.RUnlock()
	if !broker.tlsCfg.Enabled {
		t.Error("expected TLS enabled")
	}
}

func TestHTTPServer_SetTLSConfig(t *testing.T) {
	srv := NewStandardHTTPServer("test", ":8443")
	srv.SetTLSConfig(HTTPServerTLSConfig{
		Mode: "manual",
		Manual: tlsutil.TLSConfig{
			CertFile: "/tmp/cert.pem",
			KeyFile:  "/tmp/key.pem",
		},
	})
	if srv.tlsCfg.Mode != "manual" {
		t.Errorf("expected mode 'manual', got %q", srv.tlsCfg.Mode)
	}
}

func TestDatabaseConfig_TLSField(t *testing.T) {
	cfg := DatabaseConfig{
		Driver: "postgres",
		DSN:    "postgres://localhost:5432/mydb",
		TLS: DatabaseTLSConfig{
			Mode:   "verify-full",
			CAFile: "/etc/ssl/ca.pem",
		},
	}

	db := NewWorkflowDatabase("test-db", cfg)
	dsn := db.buildDSN()

	if dsn == cfg.DSN {
		t.Error("expected DSN to be modified with TLS parameters")
	}
	if !contains(dsn, "sslmode=verify-full") {
		t.Errorf("expected sslmode in DSN, got %s", dsn)
	}
	if !contains(dsn, "sslrootcert=/etc/ssl/ca.pem") {
		t.Errorf("expected sslrootcert in DSN, got %s", dsn)
	}
}

func TestDatabaseConfig_TLSDisabled(t *testing.T) {
	cfg := DatabaseConfig{
		Driver: "postgres",
		DSN:    "postgres://localhost:5432/mydb",
		TLS:    DatabaseTLSConfig{Mode: "disable"},
	}

	db := NewWorkflowDatabase("test-db", cfg)
	dsn := db.buildDSN()
	if dsn != cfg.DSN {
		t.Errorf("expected unchanged DSN when TLS disabled, got %s", dsn)
	}
}

func TestDatabaseConfig_TLSDefault(t *testing.T) {
	cfg := DatabaseConfig{
		Driver: "postgres",
		DSN:    "postgres://localhost:5432/mydb",
		// TLS field zero value: Mode=""
	}

	db := NewWorkflowDatabase("test-db", cfg)
	dsn := db.buildDSN()
	if dsn != cfg.DSN {
		t.Errorf("expected unchanged DSN when TLS not configured, got %s", dsn)
	}
}

func TestDatabaseConfig_TLS_ExistingQueryString(t *testing.T) {
	cfg := DatabaseConfig{
		Driver: "postgres",
		DSN:    "postgres://localhost:5432/mydb?connect_timeout=10",
		TLS:    DatabaseTLSConfig{Mode: "require"},
	}

	db := NewWorkflowDatabase("test-db", cfg)
	dsn := db.buildDSN()
	if !contains(dsn, "&sslmode=require") {
		t.Errorf("expected & separator when query string exists, got %s", dsn)
	}
}

// contains is a simple substring check helper.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && searchSubstring(s, sub))
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
