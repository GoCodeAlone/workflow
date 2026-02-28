package tlsutil_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/pkg/tlsutil"
)

// generateSelfSignedCert writes a self-signed cert+key pair to tmpdir and
// returns (certFile, keyFile).
func generateSelfSignedCert(t *testing.T, dir string) (string, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:         true,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	cf, err := os.Create(certFile)
	if err != nil {
		t.Fatal(err)
	}
	defer cf.Close()
	if err := pem.Encode(cf, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatal(err)
	}

	kf, err := os.Create(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	defer kf.Close()
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(kf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatal(err)
	}

	return certFile, keyFile
}

func TestLoadTLSConfig_Disabled(t *testing.T) {
	cfg := tlsutil.TLSConfig{Enabled: false}
	result, err := tlsutil.LoadTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil tls.Config when disabled")
	}
}

func TestLoadTLSConfig_ValidCert(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := generateSelfSignedCert(t, dir)

	cfg := tlsutil.TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	result, err := tlsutil.LoadTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if len(result.Certificates) != 1 {
		t.Fatalf("expected 1 certificate, got %d", len(result.Certificates))
	}
}

func TestLoadTLSConfig_WithCA(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := generateSelfSignedCert(t, dir)

	cfg := tlsutil.TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
		CAFile:   certFile, // reuse the self-signed cert as CA
	}

	result, err := tlsutil.LoadTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if result.RootCAs == nil {
		t.Fatal("expected non-nil RootCAs")
	}
}

func TestLoadTLSConfig_ClientAuthRequire(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := generateSelfSignedCert(t, dir)

	cfg := tlsutil.TLSConfig{
		Enabled:    true,
		CertFile:   certFile,
		KeyFile:    keyFile,
		ClientAuth: "require",
	}

	result, err := tlsutil.LoadTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("expected RequireAndVerifyClientCert, got %v", result.ClientAuth)
	}
}

func TestLoadTLSConfig_ClientAuthRequest(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := generateSelfSignedCert(t, dir)

	cfg := tlsutil.TLSConfig{
		Enabled:    true,
		CertFile:   certFile,
		KeyFile:    keyFile,
		ClientAuth: "request",
	}

	result, err := tlsutil.LoadTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ClientAuth != tls.RequestClientCert {
		t.Errorf("expected RequestClientCert, got %v", result.ClientAuth)
	}
}

func TestLoadTLSConfig_SkipVerify(t *testing.T) {
	cfg := tlsutil.TLSConfig{
		Enabled:    true,
		SkipVerify: true,
	}

	result, err := tlsutil.LoadTLSConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.InsecureSkipVerify { //nolint:gosec // G402: test assertion
		t.Error("expected InsecureSkipVerify to be true")
	}
}

func TestLoadTLSConfig_InvalidCertFile(t *testing.T) {
	cfg := tlsutil.TLSConfig{
		Enabled:  true,
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
	}

	_, err := tlsutil.LoadTLSConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent cert files")
	}
}

func TestLoadTLSConfig_InvalidCAFile(t *testing.T) {
	cfg := tlsutil.TLSConfig{
		Enabled: true,
		CAFile:  "/nonexistent/ca.pem",
	}

	_, err := tlsutil.LoadTLSConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent CA file")
	}
}

func TestLoadTLSConfig_InvalidCAContent(t *testing.T) {
	dir := t.TempDir()
	badCA := filepath.Join(dir, "bad-ca.pem")
	if err := os.WriteFile(badCA, []byte("not a valid pem"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := tlsutil.TLSConfig{
		Enabled: true,
		CAFile:  badCA,
	}

	_, err := tlsutil.LoadTLSConfig(cfg)
	if err == nil {
		t.Fatal("expected error for invalid CA content")
	}
}

func TestLoadTLSConfig_InvalidClientAuth(t *testing.T) {
	cfg := tlsutil.TLSConfig{
		Enabled:    true,
		ClientAuth: "invalid-value",
	}

	_, err := tlsutil.LoadTLSConfig(cfg)
	if err == nil {
		t.Fatal("expected error for invalid client_auth value")
	}
}
