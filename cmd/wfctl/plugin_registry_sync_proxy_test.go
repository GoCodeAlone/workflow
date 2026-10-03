package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type registrySyncReleaseProxy struct {
	url, caFile, caDirectory string
	roots                    *x509.CertPool
	connects, requests       atomic.Int32
}

func newRegistrySyncReleaseProxy(t *testing.T, handler http.Handler) *registrySyncReleaseProxy {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "registry-sync fixture CA"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(2), DNSNames: []string{"api.github.com"},
		NotBefore: ca.NotBefore, NotAfter: ca.NotAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	p := &registrySyncReleaseProxy{
		caFile: filepath.Join(root, "ca.pem"), caDirectory: filepath.Join(root, "empty-roots"), roots: x509.NewCertPool(),
	}
	p.roots.AddCert(ca)
	if err := os.WriteFile(p.caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p.caDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "api.github.com" || r.Header.Get("Authorization") != "" || r.Header.Get("Proxy-Authorization") != "" {
			http.Error(w, "credential-free api.github.com requests only", http.StatusForbidden)
			return
		}
		p.requests.Add(1)
		handler.ServeHTTP(w, r)
	}))
	backend.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER, caDER}, PrivateKey: key}},
	}
	backend.StartTLS()
	var mu sync.Mutex
	connections := make(map[net.Conn]struct{})
	closing := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "api.github.com:443" || r.RequestURI != "api.github.com:443" || r.Header.Get("Proxy-Authorization") != "" {
			http.Error(w, "CONNECT api.github.com:443 only", http.StatusForbidden)
			return
		}
		upstream, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(r.Context(), "tcp", backend.Listener.Addr().String())
		if err != nil {
			http.Error(w, "fixture unavailable", http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		downstream, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer downstream.Close()
		mu.Lock()
		if closing {
			mu.Unlock()
			return
		}
		connections[upstream], connections[downstream] = struct{}{}, struct{}{}
		mu.Unlock()
		defer func() {
			mu.Lock()
			delete(connections, upstream)
			delete(connections, downstream)
			mu.Unlock()
		}()
		if _, err := fmt.Fprint(downstream, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		p.connects.Add(1)
		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(upstream, buffered.Reader)
			_ = upstream.Close()
			close(done)
		}()
		_, _ = io.Copy(downstream, upstream)
		_ = downstream.Close()
		_ = upstream.Close()
		<-done
	}))
	p.url = proxy.URL
	t.Cleanup(func() {
		mu.Lock()
		closing = true
		for connection := range connections {
			_ = connection.Close()
		}
		mu.Unlock()
		proxy.Close()
		backend.Close()
	})
	return p
}

func (p *registrySyncReleaseProxy) client(t *testing.T) *http.Client {
	t.Helper()
	proxyURL, err := url.Parse(p.url)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{
		Proxy: func(r *http.Request) (*url.URL, error) {
			if r.URL.Scheme == "https" {
				return proxyURL, nil
			}
			return nil, nil
		},
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: p.roots},
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 3 * time.Second}
}

func TestPluginRegistrySyncReleaseProxy_DefaultAPI(t *testing.T) {
	f := newRegistrySyncTargetFixture(t)
	proxy := newRegistrySyncReleaseProxy(t, f.handler)
	client := proxy.client(t)
	resp, err := client.Get("https://api.github.com/repos/owner/repo/releases/tags/v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var release struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			URL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || release.Tag != "v2.0.0" || len(release.Assets) != 2 || !strings.HasPrefix(release.Assets[0].URL, f.apiURL+"/checksums/") {
		t.Fatalf("proxy did not use the existing structured fixture: %#v status=%d", release, resp.StatusCode)
	}
	checksums, err := client.Get(release.Assets[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	defer checksums.Body.Close()
	body, err := io.ReadAll(checksums.Body)
	if err != nil || !strings.Contains(string(body), "workflow-plugin-foo-linux-amd64.tar.gz") {
		t.Fatalf("loopback checksum fixture = %q, %v", body, err)
	}
	if proxy.connects.Load() != 1 || proxy.requests.Load() != 1 {
		t.Fatalf("default GitHub API was not tunneled exactly once: CONNECT=%d API=%d", proxy.connects.Load(), proxy.requests.Load())
	}
}

func TestPluginRegistrySyncReleaseProxy_DeniesOtherHosts(t *testing.T) {
	proxy := newRegistrySyncReleaseProxy(t, http.NotFoundHandler())
	client := proxy.client(t)
	for _, host := range []string{"github.com", "api.github.com.evil.invalid", "api.github.com:444", "127.0.0.1:443"} {
		if resp, err := client.Get("https://" + host + "/"); err == nil {
			_ = resp.Body.Close()
			t.Fatalf("proxy allowed a foreign CONNECT destination: %s", host)
		}
	}
	if proxy.connects.Load() != 0 {
		t.Fatal("denied CONNECT reached the TLS backend")
	}
}

func TestPluginRegistrySyncReleaseProxy_DeniesCredentials(t *testing.T) {
	proxy := newRegistrySyncReleaseProxy(t, http.NotFoundHandler())
	client := proxy.client(t)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.github.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer fixture-secret")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || proxy.requests.Load() != 0 {
		t.Fatal("credential-bearing API request reached the fixture handler")
	}
	connect, err := http.NewRequestWithContext(t.Context(), http.MethodConnect, proxy.url, nil)
	if err != nil {
		t.Fatal(err)
	}
	connect.Host = "api.github.com:443"
	connect.URL.Opaque = "api.github.com:443"
	connect.Header.Set("Proxy-Authorization", "Basic fixture-secret")
	resp, err = http.DefaultClient.Do(connect)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || proxy.connects.Load() != 1 {
		t.Fatal("credential-bearing CONNECT reached the TLS backend")
	}
}
