package main

import "testing"

// TestCheckAuthRequirement covers the security gate that refuses to start an
// unauthenticated remote code executor unless --allow-unauthenticated is set.
func TestCheckAuthRequirement(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		caFile      string
		allowUnauth bool
		wantErr     bool
	}{
		{name: "token only", token: "tok", wantErr: false},
		{name: "mTLS only", caFile: "/etc/ca.crt", wantErr: false},
		{name: "token and mTLS", token: "tok", caFile: "/etc/ca.crt", wantErr: false},
		{name: "no auth — refused", wantErr: true},
		{name: "no auth + allow-unauthenticated — permitted", allowUnauth: true, wantErr: false},
		// allow-unauthenticated is a no-op when auth IS configured (no error either way).
		{name: "token + allow-unauthenticated", token: "tok", allowUnauth: true, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkAuthRequirement(tt.token, tt.caFile, tt.allowUnauth)
			if tt.wantErr && err == nil {
				t.Errorf("checkAuthRequirement(%q,%q,%v): expected error, got nil", tt.token, tt.caFile, tt.allowUnauth)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("checkAuthRequirement(%q,%q,%v): unexpected error: %v", tt.token, tt.caFile, tt.allowUnauth, err)
			}
		})
	}
}

// TestBuildServerOptions_CAWithoutCertKey_Error verifies the fail-fast guard:
// --tls-ca set without --tls-cert/--tls-key must error rather than silently
// starting with insecure transport (which checkAuthRequirement believes is mTLS).
func TestBuildServerOptions_CAWithoutCertKey_Error(t *testing.T) {
	if _, err := buildServerOptions("", "", "/etc/ca.crt"); err == nil {
		t.Error("--tls-ca without cert/key must error, got nil")
	}
	if _, err := buildServerOptions("/etc/server.crt", "", "/etc/ca.crt"); err == nil {
		t.Error("--tls-ca with cert but no key must error, got nil")
	}
	if _, err := buildServerOptions("", "/etc/server.key", "/etc/ca.crt"); err == nil {
		t.Error("--tls-ca with key but no cert must error, got nil")
	}
}

// TestBuildServerOptions_NoTLS_Insecure verifies the no-TLS path still returns
// insecure credentials (used with --allow-unauthenticated or a bearer token).
func TestBuildServerOptions_NoTLS_Insecure(t *testing.T) {
	opts, err := buildServerOptions("", "", "")
	if err != nil {
		t.Fatalf("buildServerOptions(no TLS): %v", err)
	}
	if len(opts) == 0 {
		t.Error("expected at least one server option for insecure mode")
	}
}

// TestBuildServerOptions_CertWithoutKey_Error verifies cert/key both-or-neither.
func TestBuildServerOptions_CertWithoutKey_Error(t *testing.T) {
	if _, err := buildServerOptions("/etc/server.crt", "", ""); err == nil {
		t.Error("cert without key must error, got nil")
	}
	if _, err := buildServerOptions("", "/etc/server.key", ""); err == nil {
		t.Error("key without cert must error, got nil")
	}
}
