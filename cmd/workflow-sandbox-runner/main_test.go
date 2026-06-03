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
